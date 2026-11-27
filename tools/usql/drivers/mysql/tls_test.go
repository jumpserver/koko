package mysql

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCustomTLSVerifiesByDefaultAndKeepsGatewayIdentity(t *testing.T) {
	q := url.Values{"ssl-server-name": {"database.example"}}
	cfg, err := connectionTLS(q, "127.0.0.1")
	if err != nil || cfg.InsecureSkipVerify || cfg.ServerName != "database.example" || cfg.MinVersion != tls.VersionTLS12 {
		t.Fatal("custom TLS must verify the logical database identity, not the SSH endpoint", err)
	}
	q.Set("ssl-insecure", "true")
	cfg, err = connectionTLS(q, "127.0.0.1")
	if err != nil || !cfg.InsecureSkipVerify {
		t.Fatal("explicit certificate-verification opt-out was not preserved", err)
	}
	q.Set("ssl-insecure", "typo")
	if _, err := connectionTLS(q, "host"); err == nil {
		t.Fatal("invalid TLS option accepted")
	}
	q = url.Values{"ssl-cert": {"client.pem"}}
	if _, err := connectionTLS(q, "host"); err == nil {
		t.Fatal("unpaired client certificate accepted")
	}
	q = url.Values{"ssl-ca": {"/nonexistent/ca.pem"}}
	if _, err := connectionTLS(q, "host"); err == nil {
		t.Fatal("unreadable CA ignored")
	}
}

func TestCustomTLSHandshakeCompatibility(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), DNSNames: []string{"database.test"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true,
		KeyUsage:    x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	pair, err := tls.X509KeyPair(certPEM, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	if err != nil {
		t.Fatal(err)
	}
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, certPEM, 0600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name        string
		version     uint16
		ciphers     []uint16
		wantCipher  uint16
		wrongName   bool
		untrustedCA bool
		wantError   bool
	}{
		{name: "RSA128GCM", version: tls.VersionTLS12, ciphers: []uint16{tls.TLS_RSA_WITH_AES_128_GCM_SHA256}, wantCipher: tls.TLS_RSA_WITH_AES_128_GCM_SHA256},
		{name: "RSA256GCM", version: tls.VersionTLS12, ciphers: []uint16{tls.TLS_RSA_WITH_AES_256_GCM_SHA384}, wantCipher: tls.TLS_RSA_WITH_AES_256_GCM_SHA384},
		{name: "ECDHEPreferred", version: tls.VersionTLS12, ciphers: []uint16{tls.TLS_RSA_WITH_AES_128_GCM_SHA256, tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256}, wantCipher: tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256},
		{name: "TLS13", version: tls.VersionTLS13},
		{name: "WrongHostname", version: tls.VersionTLS12, ciphers: []uint16{tls.TLS_RSA_WITH_AES_128_GCM_SHA256}, wrongName: true, wantError: true},
		{name: "UntrustedCA", version: tls.VersionTLS12, ciphers: []uint16{tls.TLS_RSA_WITH_AES_128_GCM_SHA256}, untrustedCA: true, wantError: true},
		{name: "RejectTLS11", version: tls.VersionTLS11, ciphers: []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA}, wantError: true},
		{name: "RejectRSACBC", version: tls.VersionTLS12, ciphers: []uint16{tls.TLS_RSA_WITH_AES_128_CBC_SHA}, wantError: true},
		{name: "Reject3DES", version: tls.VersionTLS12, ciphers: []uint16{tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA}, wantError: true},
		{name: "RejectRC4", version: tls.VersionTLS12, ciphers: []uint16{tls.TLS_RSA_WITH_RC4_128_SHA}, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
				Certificates: []tls.Certificate{pair}, CipherSuites: test.ciphers,
				MinVersion: test.version, MaxVersion: test.version,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			served := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err == nil {
					defer conn.Close()
					err = conn.SetDeadline(time.Now().Add(2 * time.Second))
					if err == nil {
						err = conn.(*tls.Conn).Handshake()
					}
				}
				served <- err
			}()
			params := url.Values{"ssl-ca": {ca}}
			if test.untrustedCA {
				params.Del("ssl-ca")
			}
			host := "database.test"
			if test.wrongName {
				host = "wrong.test"
			}
			cfg, err := connectionTLS(params, host)
			if err != nil {
				t.Fatal(err)
			}
			conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 2 * time.Second}, "tcp", listener.Addr().String(), cfg)
			if conn != nil {
				defer conn.Close()
			}
			if test.wantError {
				if err == nil {
					t.Fatal("untrusted certificate or unsupported TLS configuration was accepted")
				}
				var hostnameErr x509.HostnameError
				var verificationErr *tls.CertificateVerificationError
				if (test.wrongName && !errors.As(err, &hostnameErr)) || (test.untrustedCA && !errors.As(err, &verificationErr)) {
					t.Fatalf("expected certificate verification failure, got %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				state := conn.ConnectionState()
				if state.Version != test.version || (test.wantCipher != 0 && state.CipherSuite != test.wantCipher) || len(state.VerifiedChains) == 0 {
					t.Fatalf("unexpected TLS negotiation: version=%x cipher=%s verified=%t", state.Version, tls.CipherSuiteName(state.CipherSuite), len(state.VerifiedChains) > 0)
				}
			}
			if err := <-served; err != nil && !test.wantError {
				t.Fatal(err)
			}
		})
	}
}
