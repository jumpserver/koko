package sshd

import (
	"context"

	"github.com/gliderlabs/ssh"
	uuid "github.com/satori/go.uuid"
)

// ssh方式连接coco的用户request 将实现Conn接口
type SSHConn struct {
	conn ssh.Session
	uuid uuid.UUID
}

func (s *SSHConn) SessionID() string {
	return s.uuid.String()
}

func (s *SSHConn) User() string {
	return s.conn.User()
}

func (s *SSHConn) UUID() uuid.UUID {
	return s.uuid
}

func (s *SSHConn) Pty() (ssh.Pty, <-chan ssh.Window, bool) {
	return s.conn.Pty()
}

func (s *SSHConn) Context() context.Context {
	return s.conn.Context()
}

func (s *SSHConn) Read(b []byte) (n int, err error) {
	return s.conn.Read(b)
}

func (s *SSHConn) Write(b []byte) (n int, err error) {
	return s.conn.Write(b)
}

func (s *SSHConn) Close() error {
	return s.conn.Close()
}

// ws方式连接coco的用户request 将实现Conn接口
type WSConn struct {
}
