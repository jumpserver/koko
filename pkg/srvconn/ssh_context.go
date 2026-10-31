package srvconn

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/jumpserver/koko/pkg/common"
	gossh "golang.org/x/crypto/ssh"
)

// SSHClientContext bounds both dialing and the SSH authentication handshake.
// The context applies only while connecting, not to the established transport.
func SSHClientContext(ctx context.Context) SSHClientOption {
	return func(cfg *SSHClientOptions) { cfg.connectContext = ctx }
}

func newSSHClientContext(ctx context.Context, cfg *SSHClientOptions) (*SSHClient, error) {
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(cfg.Timeout)*time.Second)
		defer cancel()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cfg.PrivateKey != "" {
		var err error
		if cfg.Passphrase == "" {
			_, err = gossh.ParsePrivateKey([]byte(cfg.PrivateKey))
		} else {
			_, err = gossh.ParsePrivateKeyWithPassphrase([]byte(cfg.PrivateKey), []byte(cfg.Passphrase))
		}
		if err != nil {
			return nil, fmt.Errorf("invalid SSH private key: %w", err)
		}
	}
	config := &gossh.ClientConfig{User: cfg.Username, Auth: cfg.AuthMethods(),
		HostKeyCallback: common.NewTrustHostKeyCallback(), Config: createSSHConfig(),
		HostKeyAlgorithms: allHostKeyAlgorithms()}
	address := net.JoinHostPort(cfg.Host, cfg.Port)
	var transport net.Conn
	var proxy *SSHClient
	var err error
	if len(cfg.proxySSHClientOptions) == 0 {
		transport, err = (&net.Dialer{}).DialContext(ctx, "tcp", address)
	} else {
		for _, proxyCfg := range cfg.proxySSHClientOptions {
			proxy, err = newSSHClientContext(ctx, &proxyCfg)
			if err == nil {
				break
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}
		if err != nil {
			return nil, err
		}
		stop := context.AfterFunc(ctx, func() { _ = proxy.Close() })
		transport, err = proxy.DialContext(ctx, "tcp", address)
		stop()
	}
	if err != nil {
		if proxy != nil {
			_ = proxy.Close()
		}
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() {
		_ = transport.Close()
		if proxy != nil {
			_ = proxy.Close()
		}
	})
	clientConn, channels, requests, err := gossh.NewClientConn(transport, address, config)
	stop()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		_ = transport.Close()
		if proxy != nil {
			_ = proxy.Close()
		}
		return nil, err
	}
	return &SSHClient{Client: gossh.NewClient(clientConn, channels, requests), Cfg: cfg,
		ProxyClient: proxy, traceSessionMap: make(map[*gossh.Session]time.Time)}, nil
}
