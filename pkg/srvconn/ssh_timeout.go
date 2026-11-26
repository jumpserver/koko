package srvconn

import (
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

var ErrSSHNoResponse = errors.New("ssh server no response")

// NewSSHConnectionTimeout opens a shell session on an already connected client,
// usually one taken from the reuse cache.
//
// A cached client can sit on a transport that stopped answering without being
// closed, e.g. a tunnel behind a gateway whose far end is gone. x/crypto/ssh
// waits forever for the channel, pty and shell replies in that case, so the
// client is closed when they do not arrive within timeout, which makes every
// pending call on it return.
func (s *SSHClient) NewSSHConnectionTimeout(timeout time.Duration, opts ...SSHOption) (*gossh.Session, *SSHConnection, error) {
	type result struct {
		sess *gossh.Session
		conn *SSHConnection
		err  error
	}
	done := make(chan result, 1)
	go func() {
		sess, err := s.AcquireSession()
		if err != nil {
			done <- result{err: err}
			return
		}
		conn, err := NewSSHConnection(sess, opts...)
		if err != nil {
			_ = sess.Close()
			s.ReleaseSession(sess)
			done <- result{err: err}
			return
		}
		done <- result{sess: sess, conn: conn}
	}()
	if timeout <= 0 {
		ret := <-done
		return ret.sess, ret.conn, ret.err
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case ret := <-done:
		return ret.sess, ret.conn, ret.err
	case <-timer.C:
		_ = s.Close()
		go func() {
			if ret := <-done; ret.conn != nil {
				_ = ret.conn.Close()
				s.ReleaseSession(ret.sess)
			}
		}()
		return nil, nil, fmt.Errorf("%w in %s", ErrSSHNoResponse, timeout)
	}
}

// firstReadTimeoutConn calls onTimeout when nothing has been read from the
// connection within the timeout. The timer stops at the first byte, so a slow
// keyboard-interactive answer from the user later in the handshake is not cut.
type firstReadTimeoutConn struct {
	net.Conn
	timer    *time.Timer
	timedOut atomic.Bool
}

func newFirstReadTimeoutConn(conn net.Conn, timeout time.Duration, onTimeout func()) *firstReadTimeoutConn {
	c := &firstReadTimeoutConn{Conn: conn}
	c.timer = time.AfterFunc(timeout, func() {
		c.timedOut.Store(true)
		onTimeout()
	})
	return c
}

func (c *firstReadTimeoutConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.timer.Stop()
	}
	return n, err
}

func (c *firstReadTimeoutConn) Close() error {
	c.timer.Stop()
	return c.Conn.Close()
}
