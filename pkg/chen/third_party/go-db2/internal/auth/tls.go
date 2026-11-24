package auth

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
)

// TLSConfig holds connection security parameters for TLS/SSL.
type TLSConfig struct {
	EnableTLS          bool
	InsecureSkipVerify bool
	ServerName         string
	CACertPath         string
	ClientCertPath     string
	ClientKeyPath      string
}

// WrapTLSContext establishes a TLS connection using the given context and TLSConfig.
func WrapTLSContext(ctx context.Context, conn net.Conn, cfg TLSConfig) (net.Conn, error) {
	if !cfg.EnableTLS {
		return conn, nil
	}

	tlsConf := &tls.Config{
		ServerName:         cfg.ServerName,
		InsecureSkipVerify: cfg.InsecureSkipVerify,
		MinVersion:         tls.VersionTLS12,
	}

	if cfg.CACertPath != "" {
		caData, err := os.ReadFile(cfg.CACertPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read TLS CA certificate at %s: %w", cfg.CACertPath, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caData) {
			return nil, fmt.Errorf("failed to parse TLS CA certificate from %s", cfg.CACertPath)
		}
		tlsConf.RootCAs = pool
	}

	if cfg.ClientCertPath != "" && cfg.ClientKeyPath != "" {
		cert, err := tls.LoadX509KeyPair(cfg.ClientCertPath, cfg.ClientKeyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load TLS client certificate/key pair: %w", err)
		}
		tlsConf.Certificates = []tls.Certificate{cert}
	}

	tlsConn := tls.Client(conn, tlsConf)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("TLS handshake failed: %w", err)
	}

	return tlsConn, nil
}

// WrapTLS delegates to WrapTLSContext with context.Background() for backwards compatibility.
func WrapTLS(conn net.Conn, cfg TLSConfig) (net.Conn, error) {
	return WrapTLSContext(context.Background(), conn, cfg)
}
