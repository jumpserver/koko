package srvconn

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

// silentTunnelLab is koko -> gateway sshd -> 127.0.0.1:port tunnel -> asset sshd.
// The tunnel can go silent: it keeps every TCP connection open but drops all
// bytes, like a port-forwarding tunnel whose far end is gone.
type silentTunnelLab struct {
	gateway SSHClientOptions
	tunnel  *net.TCPAddr
	silent  atomic.Bool

	mu    sync.Mutex
	conns []net.Conn
}

func newSilentTunnelLab(t *testing.T) *silentTunnelLab {
	t.Helper()
	lab := &silentTunnelLab{}
	asset := lab.listen(t)
	gateway := lab.listen(t)
	tunnel := lab.listen(t)
	go serveTestAsset(asset)
	go serveTestGateway(gateway)
	go lab.serveTunnel(tunnel, asset.Addr().String())
	lab.tunnel = tunnel.Addr().(*net.TCPAddr)
	gwAddr := gateway.Addr().(*net.TCPAddr)
	lab.gateway = SSHClientOptions{Host: gwAddr.IP.String(), Port: strconv.Itoa(gwAddr.Port),
		Username: "root", Password: "gateway", Timeout: 5}
	t.Cleanup(func() {
		lab.mu.Lock()
		defer lab.mu.Unlock()
		for _, c := range lab.conns {
			_ = c.Close()
		}
	})
	return lab
}

func (lab *silentTunnelLab) listen(t *testing.T) net.Listener {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

func (lab *silentTunnelLab) track(c net.Conn) {
	lab.mu.Lock()
	lab.conns = append(lab.conns, c)
	lab.mu.Unlock()
}

func (lab *silentTunnelLab) serveTunnel(ln net.Listener, upstream string) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		lab.track(c)
		if lab.silent.Load() {
			go func() { _, _ = io.Copy(io.Discard, c) }()
			continue
		}
		up, err := net.Dial("tcp", upstream)
		if err != nil {
			_ = c.Close()
			continue
		}
		lab.track(up)
		go lab.pipe(up, c)
		go lab.pipe(c, up)
	}
}

func (lab *silentTunnelLab) pipe(dst, src net.Conn) {
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 && !lab.silent.Load() {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			_ = dst.Close()
			return
		}
	}
}

func (lab *silentTunnelLab) assetOptions(timeout int, extra ...SSHClientOption) []SSHClientOption {
	opts := []SSHClientOption{
		SSHClientUsername("nvidia"),
		SSHClientHost("127.0.0.1"),
		SSHClientPort(lab.tunnel.Port),
		SSHClientTimeout(timeout),
		SSHClientProxyClient(lab.gateway),
	}
	return append(opts, extra...)
}

func testServerConfig() *gossh.ServerConfig {
	cfg := &gossh.ServerConfig{
		PasswordCallback: func(gossh.ConnMetadata, []byte) (*gossh.Permissions, error) {
			return nil, nil
		},
		KeyboardInteractiveCallback: func(_ gossh.ConnMetadata, client gossh.KeyboardInteractiveChallenge) (*gossh.Permissions, error) {
			answers, err := client("", "", []string{"Verification code: "}, []bool{false})
			if err != nil {
				return nil, err
			}
			if len(answers) != 1 || answers[0] != "123456" {
				return nil, errors.New("wrong code")
			}
			return nil, nil
		},
	}
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := gossh.NewSignerFromKey(key)
	cfg.AddHostKey(signer)
	return cfg
}

func serveTestAsset(ln net.Listener) {
	cfg := testServerConfig()
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			_, chans, reqs, err := gossh.NewServerConn(c, cfg)
			if err != nil {
				return
			}
			go gossh.DiscardRequests(reqs)
			for newChan := range chans {
				ch, chReqs, err := newChan.Accept()
				if err != nil {
					continue
				}
				go func() {
					for req := range chReqs {
						_ = req.Reply(req.Type == "pty-req" || req.Type == "shell", nil)
						if req.Type == "shell" {
							_, _ = ch.Write([]byte("asset$ "))
						}
					}
				}()
			}
		}()
	}
}

func serveTestGateway(ln net.Listener) {
	cfg := testServerConfig()
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			_, chans, reqs, err := gossh.NewServerConn(c, cfg)
			if err != nil {
				return
			}
			go gossh.DiscardRequests(reqs)
			for newChan := range chans {
				var target struct {
					Host     string
					Port     uint32
					OrigHost string
					OrigPort uint32
				}
				if newChan.ChannelType() != "direct-tcpip" ||
					gossh.Unmarshal(newChan.ExtraData(), &target) != nil {
					_ = newChan.Reject(gossh.UnknownChannelType, "unsupported")
					continue
				}
				dst, err := net.Dial("tcp", net.JoinHostPort(target.Host, strconv.Itoa(int(target.Port))))
				if err != nil {
					_ = newChan.Reject(gossh.ConnectionFailed, err.Error())
					continue
				}
				ch, chReqs, err := newChan.Accept()
				if err != nil {
					_ = dst.Close()
					continue
				}
				go gossh.DiscardRequests(chReqs)
				go func() { _, _ = io.Copy(ch, dst); _ = ch.Close() }()
				go func() { _, _ = io.Copy(dst, ch); _ = dst.Close() }()
			}
		}()
	}
}

func within(t *testing.T, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("still blocked after %s", d)
	}
}

func TestNewSSHConnectionTimeoutClosesSilentCachedClient(t *testing.T) {
	lab := newSilentTunnelLab(t)
	client, err := NewSSHClient(lab.assetOptions(5, SSHClientPassword("asset"))...)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	sess, conn, err := client.NewSSHConnectionTimeout(5 * time.Second)
	if err != nil {
		t.Fatalf("healthy client: %v", err)
	}
	buf := make([]byte, 16)
	if n, _ := conn.Read(buf); string(buf[:n]) != "asset$ " {
		t.Fatalf("unexpected shell output %q", buf[:n])
	}

	lab.silent.Store(true)
	within(t, 5*time.Second, func() {
		_, _, err = client.NewSSHConnectionTimeout(500 * time.Millisecond)
	})
	if !errors.Is(err, ErrSSHNoResponse) {
		t.Fatalf("want ErrSSHNoResponse, got %v", err)
	}
	// The silent client is closed, so sessions already on it end and later
	// reuse attempts fail at once instead of hanging again.
	within(t, 5*time.Second, func() { _ = sess.Wait() })
	within(t, 5*time.Second, func() {
		if _, err := client.AcquireSession(); err == nil {
			t.Error("closed client still opens sessions")
		}
	})
}

func TestGatewayHandshakeStopsWhenTargetIsSilent(t *testing.T) {
	lab := newSilentTunnelLab(t)
	lab.silent.Store(true)
	var err error
	within(t, 5*time.Second, func() {
		_, err = NewSSHClient(lab.assetOptions(1, SSHClientPassword("asset"))...)
	})
	if !errors.Is(err, ErrSSHNoResponse) {
		t.Fatalf("want ErrSSHNoResponse, got %v", err)
	}
}

func TestGatewayHandshakeTimeoutAllowsSlowKeyboardInteractive(t *testing.T) {
	lab := newSilentTunnelLab(t)
	slowUser := SSHClientKeyboardAuth(func(string, string, []string, []bool) ([]string, error) {
		time.Sleep(1500 * time.Millisecond) // longer than the 1s timeout
		return []string{"123456"}, nil
	})
	client, err := NewSSHClient(lab.assetOptions(1, slowUser)...)
	if err != nil {
		t.Fatalf("slow keyboard-interactive answer was cut off: %v", err)
	}
	_ = client.Close()
}
