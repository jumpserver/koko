package dbconn

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"strings"
)

// MakeTLS builds the transport policy without opening a database connection.
// includeSystemRoots retains the background executors' system-plus-custom CA policy.
func MakeTLS(c Config, includeSystemRoots bool) (*tls.Config, error) {
	if !c.TLS.Enabled {
		return nil, nil
	}
	mode := strings.ToLower(strings.TrimSpace(c.TLS.Mode))
	if c.Dialect != "postgresql" {
		mode = ""
	}
	switch mode {
	case "", "require", "verify-ca", "verify-full", "prefer", "allow":
	case "disable":
		return nil, nil
	default:
		return nil, errors.New("unsupported database TLS mode; select require, verify-ca or verify-full")
	}
	t := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: c.TLS.ServerName, InsecureSkipVerify: c.TLS.InsecureSkipVerify}
	if t.ServerName == "" {
		t.ServerName = c.Host
	}
	if c.TLS.CACert != "" {
		pool := x509.NewCertPool()
		if includeSystemRoots {
			if system, err := x509.SystemCertPool(); err == nil && system != nil {
				pool = system
			}
		}
		if !pool.AppendCertsFromPEM([]byte(c.TLS.CACert)) {
			return nil, errors.New("invalid database CA certificate")
		}
		t.RootCAs = pool
	}
	if mode == "verify-full" || mode == "verify-ca" {
		if c.TLS.InsecureSkipVerify {
			return nil, errors.New("certificate verification mode conflicts with allow-invalid-certificate")
		}
		t.InsecureSkipVerify = false
	}
	// libpq require guarantees encryption; a supplied root CA also verifies the
	// chain, without hostname validation. verify-full uses Go's normal hostname
	// and chain verification. No plaintext fallback is configured.
	if mode == "verify-ca" || ((mode == "require" || mode == "prefer" || mode == "allow") && c.TLS.CACert != "") {
		t.InsecureSkipVerify = true
		t.VerifyConnection = func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return errors.New("database certificate missing")
			}
			intermediates := x509.NewCertPool()
			for _, cert := range state.PeerCertificates[1:] {
				intermediates.AddCert(cert)
			}
			_, err := state.PeerCertificates[0].Verify(x509.VerifyOptions{Roots: t.RootCAs, Intermediates: intermediates})
			return err
		}
	} else if mode == "require" || mode == "prefer" || mode == "allow" {
		t.InsecureSkipVerify = true
	}
	if c.TLS.ClientCert != "" || c.TLS.ClientKey != "" {
		cert, err := tls.X509KeyPair([]byte(c.TLS.ClientCert), []byte(c.TLS.ClientKey))
		if err != nil {
			return nil, errors.New("invalid database client certificate")
		}
		t.Certificates = []tls.Certificate{cert}
	}
	return t, nil
}
