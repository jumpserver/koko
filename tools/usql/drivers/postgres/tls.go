package postgres

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/lib/pq"
)

func caOnlyMode(q url.Values) bool {
	mode := q.Get("sslmode")
	return mode == "verify-ca" || (mode == "require" && q.Get("sslrootcert") != "")
}

func certificateAuthorityTLS(q url.Values, host string) (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host, InsecureSkipVerify: true}
	if sni := q.Get("sslsni"); sni != "" && !strings.HasPrefix(sni, "1") {
		cfg.ServerName = ""
	}
	read := func(option string) ([]byte, error) {
		value := q.Get(option)
		if q.Get("sslinline") == "true" {
			return []byte(value), nil
		}
		data, err := os.ReadFile(value)
		if err != nil {
			return nil, errors.New("cannot read PostgreSQL TLS certificate/key file")
		}
		return data, nil
	}
	if q.Get("sslrootcert") != "" {
		data, err := read("sslrootcert")
		if err != nil {
			return nil, err
		}
		cfg.RootCAs = x509.NewCertPool()
		if !cfg.RootCAs.AppendCertsFromPEM(data) {
			return nil, errors.New("invalid PostgreSQL CA certificate PEM")
		}
	}
	if (q.Get("sslcert") == "") != (q.Get("sslkey") == "") {
		return nil, errors.New("PostgreSQL client certificate and key must be configured together")
	}
	if q.Get("sslcert") != "" {
		cert, err := read("sslcert")
		if err != nil {
			return nil, err
		}
		key, err := read("sslkey")
		if err != nil {
			return nil, err
		}
		pair, err := tls.X509KeyPair(cert, key)
		if err != nil {
			return nil, errors.New("invalid PostgreSQL client certificate/key")
		}
		cfg.Certificates = []tls.Certificate{pair}
	}
	cfg.VerifyConnection = func(state tls.ConnectionState) error {
		if len(state.PeerCertificates) == 0 {
			return errors.New("PostgreSQL server certificate is missing")
		}
		intermediates := x509.NewCertPool()
		for _, cert := range state.PeerCertificates[1:] {
			intermediates.AddCert(cert)
		}
		_, err := state.PeerCertificates[0].Verify(x509.VerifyOptions{Roots: cfg.RootCAs, Intermediates: intermediates})
		return err
	}
	return cfg, nil
}

func negotiateTLS(ctx context.Context, conn net.Conn, cfg *tls.Config) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	stopped := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()); close(stopped) })
	defer func() {
		if !stop() {
			<-stopped
		}
		_ = conn.SetDeadline(time.Time{})
	}()
	request := make([]byte, 8)
	binary.BigEndian.PutUint32(request[:4], 8)
	binary.BigEndian.PutUint32(request[4:], 80877103)
	for len(request) > 0 {
		n, err := conn.Write(request)
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, io.ErrShortWrite
		}
		request = request[n:]
	}
	var reply [1]byte
	if _, err := io.ReadFull(conn, reply[:]); err != nil {
		return nil, err
	}
	if reply[0] == 'N' {
		return nil, pq.ErrSSLNotSupported
	}
	if reply[0] != 'S' {
		return nil, errors.New("invalid PostgreSQL SSLRequest response")
	}
	secure := tls.Client(conn, cfg)
	if err := secure.HandshakeContext(ctx); err != nil {
		return nil, err
	}
	return secure, nil
}
