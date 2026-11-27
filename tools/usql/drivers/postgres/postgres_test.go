package postgres

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql/driver"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/xo/dburl"
	"github.com/xo/usql/drivers"
)

func TestManagedPostgresOptionsPreserveUpstreamFeatures(t *testing.T) {
	d := drivers.Available()["postgres"]
	if d.Copy == nil || d.NewMetadataReader == nil || d.NewMetadataWriter == nil || d.ChangePassword == nil {
		t.Fatal("connection options must not remove PostgreSQL metadata, COPY or password support")
	}
	for _, test := range []struct {
		query string
		ok    bool
	}{
		{"sslmode=verify-full&hostaddr=127.0.0.1", true},
		{"sslmode=prefer&hostaddr=::1", true},
		{"sslmode=prefer", true},
		{"sslmode=require&hostaddr=not-an-ip", false},
	} {
		u, err := dburl.Parse("postgres://user:password@database.example:15432/app?" + test.query)
		if err != nil {
			t.Fatal(err)
		}
		before := u.String()
		_, err = managedConnector(u)
		if (err == nil) != test.ok || u.String() != before {
			t.Fatal("invalid connection options result or mutated source URL", err)
		}
	}
}

type probeConnector struct {
	err   error
	calls int
}

func (c *probeConnector) Connect(context.Context) (driver.Conn, error) { c.calls++; return nil, c.err }
func (*probeConnector) Driver() driver.Driver                          { return &pq.Driver{} }

func TestPreferNeverDowngradesCertificateOrAuthenticationFailure(t *testing.T) {
	for _, err := range []error{pq.ErrSSLNotSupported, errors.New("certificate verification failed"), &pq.Error{Code: "28P01"}} {
		primary, fallback := &probeConnector{err: err}, &probeConnector{}
		_, got := (&preferConnector{primary, fallback}).Connect(context.Background())
		if errors.Is(err, pq.ErrSSLNotSupported) {
			if got != nil || fallback.calls != 1 {
				t.Fatal("explicit TLS refusal did not use the configured prefer fallback")
			}
		} else if got != err || fallback.calls != 0 {
			t.Fatal("certificate/authentication error caused a plaintext retry")
		}
	}
}

func TestCertificateAuthorityModeKeepsSNIWithoutVerifyingHostname(t *testing.T) {
	server := httptest.NewTLSServer(nil)
	defer server.Close()
	cert := server.Certificate()
	q := url.Values{"sslmode": {"verify-ca"}, "sslinline": {"true"},
		"sslrootcert": {string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}))}}
	cfg, err := certificateAuthorityTLS(q, "different-identity.example")
	if err != nil || cfg.ServerName != "different-identity.example" || cfg.MinVersion != tls.VersionTLS12 {
		t.Fatal("CA-only mode lost SNI or TLS policy", err)
	}
	state := tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
	if err := cfg.VerifyConnection(state); err != nil {
		t.Fatal("verify-ca must validate the chain without requiring a matching hostname", err)
	}
	q.Del("sslrootcert")
	cfg, err = certificateAuthorityTLS(q, "different-identity.example")
	if err != nil || cfg.VerifyConnection(state) == nil {
		t.Fatal("CA-only mode accepted an untrusted certificate")
	}
	q.Set("sslcert", "unpaired")
	if _, err := certificateAuthorityTLS(q, "host"); err == nil {
		t.Fatal("unpaired client certificate accepted")
	}
}

func TestTLSRefusalAndCanceledHandshake(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	go func() {
		request := make([]byte, 8)
		if _, err := io.ReadFull(server, request); err == nil {
			_, _ = server.Write([]byte{'N'})
		}
	}()
	_, err := negotiateTLS(context.Background(), client, &tls.Config{})
	if !errors.Is(err, pq.ErrSSLNotSupported) {
		t.Fatal("SSLRequest refusal was not classified for explicit prefer", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = negotiateTLS(ctx, client, &tls.Config{})
	if err == nil {
		t.Fatal("stalled SSL negotiation ignored cancellation")
	}
}
