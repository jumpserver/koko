package srvconn

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestGatewayKeyboardInteractive(t *testing.T) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name             string
		keyAccepted      bool
		keySufficient    bool
		rejectCompletion bool
		questions        []string
		challenges       int
		direct           bool
		wantOK           bool
		wantAttempts     int
	}{
		{name: "empty challenge after key", keyAccepted: true, challenges: 1, wantOK: true, wantAttempts: 1},
		{name: "key alone", keyAccepted: true, keySufficient: true, wantOK: true},
		{name: "prompt", keyAccepted: true, challenges: 1, questions: []string{"OTP?"}, wantAttempts: 1},
		{name: "second challenge", keyAccepted: true, challenges: 2, wantAttempts: 1},
		{name: "rejected completion", keyAccepted: true, challenges: 1, rejectCompletion: true, wantAttempts: 1},
		{name: "rejected key", challenges: 1},
		{name: "final target", keyAccepted: true, challenges: 1, direct: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			attempts := 0
			keyboard := func(_ ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
				attempts++
				for range tc.challenges {
					if _, err := challenge("", "", tc.questions, make([]bool, len(tc.questions))); err != nil {
						return nil, err
					}
				}
				if tc.rejectCompletion {
					return nil, errors.New("completion rejected")
				}
				return nil, nil
			}
			server := &ssh.ServerConfig{
				KeyboardInteractiveCallback: keyboard, // Advertise it even before the key succeeds.
				PublicKeyCallback: func(_ ssh.ConnMetadata, offered ssh.PublicKey) (*ssh.Permissions, error) {
					if !tc.keyAccepted || !bytes.Equal(offered.Marshal(), signer.PublicKey().Marshal()) {
						return nil, errors.New("unexpected key")
					}
					if tc.keySufficient {
						return nil, nil
					}
					return nil, &ssh.PartialSuccessError{Next: ssh.ServerAuthCallbacks{KeyboardInteractiveCallback: keyboard}}
				},
			}
			server.AddHostKey(signer)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				if c, _, _, err := ssh.NewServerConn(conn, server); err == nil {
					_ = c.Close()
				}
			}()

			addr := listener.Addr().(*net.TCPAddr)
			cfg := SSHClientOptions{Host: addr.IP.String(), Port: strconv.Itoa(addr.Port), Username: "test", PrivateAuth: signer}
			var client *SSHClient
			if tc.direct {
				client, err = NewSSHClientWithCfg(&cfg)
			} else {
				client, err = getAvailableProxyClient(cfg)
			}
			if client != nil {
				_ = client.Client.Close()
			}
			_ = listener.Close()
			<-done
			if (err == nil) != tc.wantOK || attempts != tc.wantAttempts {
				t.Errorf("handshake error = %v, keyboard attempts = %d; want success = %t, attempts = %d", err, attempts, tc.wantOK, tc.wantAttempts)
			}
		})
	}
}
