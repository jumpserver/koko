package db2

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTLSCertificateVerification(t *testing.T) {
	server := httptest.NewTLSServer(http.NotFoundHandler())
	certificate := server.TLS.Certificates[0]
	server.Close()
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}), 0600); err != nil {
		t.Fatal(err)
	}
	for _, trusted := range []bool{false, true} {
		t.Run(map[bool]string{false: "RejectUntrusted", true: "TrustConfiguredCA"}[trusted], func(t *testing.T) {
			listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			handshaken := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					handshaken <- err
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(3 * time.Second))
				err = conn.(*tls.Conn).Handshake()
				handshaken <- err
				if err == nil {
					// End after the first DRDA header: this fixture validates TLS,
					// not a simulated database authentication success.
					var header [6]byte
					io.ReadFull(conn, header[:])
				}
			}()
			q := url.Values{"ssl": {"true"}}
			if trusted {
				q.Set("ssl-ca", caFile)
			}
			db, err := open("db2", "db2://probe:test@"+listener.Addr().String()+"/sample?"+q.Encode())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			err = db.PingContext(ctx)
			if err == nil {
				t.Fatal("fixture cannot complete a Db2 login")
			}
			serverErr := <-handshaken
			if trusted && (serverErr != nil || strings.Contains(err.Error(), "TLS handshake")) {
				t.Fatalf("configured CA did not validate: client=%v, server=%v", err, serverErr)
			}
			if !trusted && (serverErr == nil || !strings.Contains(err.Error(), "certificate")) {
				t.Fatalf("untrusted certificate was not rejected: %v", err)
			}
		})
	}
}
