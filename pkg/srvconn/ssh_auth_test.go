//go:build linux

package srvconn

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"reflect"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

func TestSSHClientAuthMethodOrder(t *testing.T) {
	tests := []struct {
		name             string
		explicitCallback bool
		prefer           bool
		want             []string
	}{
		{
			name: "password-only callers retain automatic fallback",
			want: []string{"password", "keyboard-interactive"},
		},
		{
			name:             "explicit callback keeps password first by default",
			explicitCallback: true,
			want:             []string{"password", "keyboard-interactive"},
		},
		{
			name:             "keyboard interactive can be explicitly preferred",
			explicitCallback: true,
			prefer:           true,
			want:             []string{"keyboard-interactive"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &SSHClientOptions{}
			SSHClientPassword("asset-password")(cfg)
			if tt.explicitCallback {
				SSHClientKeyboardAuth(func(_ string, _ string, _ []string, _ []bool) ([]string, error) {
					return []string{"asset-password"}, nil
				})(cfg)
			}
			if tt.prefer {
				SSHClientPreferKeyboardInteractive()(cfg)
			}

			if got := authenticateAndRecordOrder(t, cfg.AuthMethods(), true, false); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("authentication order = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPreferredKeyboardInteractiveFallsBackToPassword(t *testing.T) {
	cfg := &SSHClientOptions{}
	SSHClientPassword("asset-password")(cfg)
	SSHClientKeyboardAuth(func(_ string, _ string, _ []string, _ []bool) ([]string, error) {
		return []string{"asset-password"}, nil
	})(cfg)
	SSHClientPreferKeyboardInteractive()(cfg)

	want := []string{"password"}
	if got := authenticateAndRecordOrder(t, cfg.AuthMethods(), false, true); !reflect.DeepEqual(got, want) {
		t.Fatalf("authentication order = %v, want %v", got, want)
	}
}

func authenticateAndRecordOrder(t *testing.T, authMethods []gossh.AuthMethod, keyboardAvailable, acceptPassword bool) []string {
	t.Helper()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	hostSigner, err := gossh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("create host signer: %v", err)
	}

	var attempts []string
	serverConfig := &gossh.ServerConfig{
		PasswordCallback: func(_ gossh.ConnMetadata, _ []byte) (*gossh.Permissions, error) {
			attempts = append(attempts, "password")
			if acceptPassword {
				return nil, nil
			}
			return nil, errors.New("password rejected to test fallback")
		},
	}
	if keyboardAvailable {
		serverConfig.KeyboardInteractiveCallback = func(_ gossh.ConnMetadata, challenge gossh.KeyboardInteractiveChallenge) (*gossh.Permissions, error) {
			attempts = append(attempts, "keyboard-interactive")
			answers, challengeErr := challenge("", "", []string{"Password: "}, []bool{false})
			if challengeErr != nil {
				return nil, challengeErr
			}
			if len(answers) != 1 || answers[0] != "asset-password" {
				return nil, errors.New("unexpected keyboard-interactive answer")
			}
			return nil, nil
		}
	}
	serverConfig.AddHostKey(hostSigner)

	serverConn, clientConn := net.Pipe()
	deadline := time.Now().Add(5 * time.Second)
	_ = serverConn.SetDeadline(deadline)
	_ = clientConn.SetDeadline(deadline)

	serverErr := make(chan error, 1)
	go func() {
		conn, _, _, serverHandshakeErr := gossh.NewServerConn(serverConn, serverConfig)
		if conn != nil {
			_ = conn.Close()
		}
		serverErr <- serverHandshakeErr
	}()

	clientConfig := &gossh.ClientConfig{
		User:            "asset-user",
		Auth:            authMethods,
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
	}
	conn, _, _, clientErr := gossh.NewClientConn(clientConn, "pipe", clientConfig)
	if conn != nil {
		_ = conn.Close()
	}
	if clientErr != nil {
		t.Fatalf("client handshake failed: %v", clientErr)
	}
	if err = <-serverErr; err != nil {
		t.Fatalf("server handshake failed: %v", err)
	}
	return attempts
}
