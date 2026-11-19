package sessiontools

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	dm "gitee.com/chunanyong/dm"
)

func TestAgentDamengPreservesCredentials(t *testing.T) {
	config := DatabaseConfig{Protocol: "dameng", Username: "agent", Password: "p@ss?word&x=1/%#:", Database: "SAMPLE"}
	relay, err := newAgentDB2DamengConnector(config)
	if err != nil {
		t.Fatal(err)
	}
	connector, err := relay.connector("127.0.0.1", 1234)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(connector.(*dm.DmConnector).BuildDSN())
	if err != nil {
		t.Fatal(err)
	}
	password, _ := u.User.Password()
	if u.Host != "127.0.0.1:1234" || u.User.Username() != config.Username || password != config.Password {
		t.Fatal("DM driver changed credentials or endpoint")
	}
}

func TestAgentDB2TLSVerifiesOriginalHostname(t *testing.T) {
	server := httptest.NewTLSServer(nil)
	defer server.Close()
	for _, serverName := range []string{server.Certificate().DNSNames[0], "wrong.invalid"} {
		client, peer := net.Pipe()
		defer peer.Close()
		_ = peer.SetDeadline(time.Now().Add(time.Second))
		received := make(chan error, 1)
		go func() {
			defer peer.Close()
			secured := tls.Server(peer, server.TLS.Clone())
			err := secured.Handshake()
			if err == nil {
				var firstByte [1]byte
				_, err = io.ReadFull(secured, firstByte[:])
			}
			received <- err
		}()
		relay, err := newAgentDB2DamengConnector(DatabaseConfig{
			Protocol: "db2", Host: "asset.invalid", Port: 50000, Database: "SAMPLE", UseSSL: true, ServerName: serverName,
			CACert: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})),
			DialContext: func(_ context.Context, _, address string) (net.Conn, error) {
				if address != "asset.invalid:50000" {
					t.Errorf("TLS changed authorized endpoint: %s", address)
				}
				return client, nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		conn, _ := relay.Connect(ctx)
		if conn != nil {
			_ = conn.Close()
		}
		if err := <-received; (err == nil) != (serverName == server.Certificate().DNSNames[0]) {
			t.Fatalf("DB2 TLS did not protect the driver handshake: %v", err)
		}
	}
}

func TestAgentDB2DamengRejectInvalidConfigurationBeforeDial(t *testing.T) {
	for _, config := range []DatabaseConfig{
		{Protocol: "db2"}, {Protocol: "db2", Database: "1234567890123456789"},
		{Protocol: "db2", Database: "SAMPLE", UseSSL: true, CACert: "invalid"},
		{Protocol: "db2", Database: "SAMPLE", UseSSL: true, AllowInvalidCert: true},
		{Protocol: "dameng", UseSSL: true}, {Protocol: "dameng", CACert: "configured"},
		{Protocol: "dameng", Username: "invalid:user"}, {Protocol: "dameng", Database: "schema/other"},
	} {
		config.DialContext = func(context.Context, string, string) (net.Conn, error) {
			t.Fatal("invalid configuration reached the network")
			return nil, nil
		}
		if _, _, _, err := OpenAgentSQLConnection(context.Background(), config); err == nil {
			t.Fatalf("invalid configuration accepted: %s", config.Protocol)
		}
	}
}

func TestAgentDB2DamengStatementTerminators(t *testing.T) {
	for _, protocol := range []string{"db2", "dameng"} {
		statement, rows, err := PrepareAgentSQL("SELECT 'a;b' FROM dual; -- trailing; comment", protocol)
		if err != nil || !rows || statement != "SELECT 'a;b' FROM dual -- trailing; comment" {
			t.Fatalf("%s changed SQL content: %q %v", protocol, statement, err)
		}
		if _, _, err := PrepareAgentSQL("SELECT 1; DELETE FROM agent", protocol); err == nil {
			t.Fatalf("%s accepted a SQL batch", protocol)
		}
	}
}

func TestAgentDB2DamengHandshakeCancellation(t *testing.T) {
	for _, config := range []DatabaseConfig{{Protocol: "db2"}, {Protocol: "dameng"}, {Protocol: "db2", UseSSL: true}} {
		name := config.Protocol
		if config.UseSSL {
			name += "-tls"
		}
		t.Run(name, func(t *testing.T) {
			client, server := net.Pipe()
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			done := make(chan error, 1)
			config.Host, config.Port, config.Database = "asset.invalid", 1234, "SAMPLE"
			config.Username, config.Password = "agent", "p@ss?word&x=1/%#:"
			config.DialContext = func(context.Context, string, string) (net.Conn, error) { return client, nil }
			go func() {
				_, _, _, err := OpenAgentSQLConnection(ctx, config)
				done <- err
			}()
			_ = server.SetReadDeadline(time.Now().Add(time.Second))
			var firstByte [1]byte
			if _, err := io.ReadFull(server, firstByte[:]); err != nil {
				t.Fatalf("driver handshake did not reach the guarded transport: %v", err)
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("connection cancellation was lost: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("driver handshake did not stop on cancellation")
			}
		})
	}
}

func TestAgentDamengRejectsOversizedFramesAndUnverifiedTLS(t *testing.T) {
	for _, tlsMode := range []uint32{0, 1, 2, 4} {
		client, server := net.Pipe()
		conn := &agentDamengWireConn{Conn: client}
		var header [64]byte
		binary.LittleEndian.PutUint32(header[20:24], tlsMode)
		if tlsMode == 0 {
			binary.LittleEndian.PutUint32(header[6:10], 16<<20+1)
		}
		done := make(chan struct{})
		go func() { defer close(done); defer server.Close(); _, _ = server.Write(header[:]) }()
		if n, err := conn.Read(header[:]); n != 0 || err == nil {
			t.Fatal("unsafe DM response header reached the driver")
		}
		_ = conn.Close()
		<-done
	}
}

func TestAgentDamengPreservesFrames(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	var header [64]byte
	binary.LittleEndian.PutUint32(header[6:10], 1)
	frame := append(header[:], 'x')
	done := make(chan struct{})
	go func() { defer close(done); defer server.Close(); _, _ = server.Write(append(frame, frame...)) }()
	data, err := io.ReadAll(&agentDamengWireConn{Conn: client})
	if err != nil || string(data) != string(append(frame, frame...)) {
		t.Fatalf("DM frame contents changed: %v", err)
	}
	<-done
}
