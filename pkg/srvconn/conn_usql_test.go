package srvconn

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/jumpserver/koko/pkg/localcommand"
)

func TestUSQLCloseUnblocksPTYWrite(t *testing.T) {
	command, err := localcommand.New("/bin/sh", []string{
		"-c", "stty raw -echo && printf READY && exec /bin/sleep 30",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer command.Close()
	ready := make(chan error, 1)
	go func() { _, err := io.ReadFull(command, make([]byte, 5)); ready <- err }()
	select {
	case err := <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("PTY did not become ready")
	}
	written := make(chan error, 1)
	go func() { _, err := command.Write(bytes.Repeat([]byte("x"), 1024*1024)); written <- err }()
	select {
	case err := <-written:
		t.Fatalf("PTY write did not block: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	conn := &USQLConn{LocalCommand: command}
	closed := make(chan error, 1)
	go func() { closed <- conn.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("USQL close blocked behind the pending PTY write")
	}
	select {
	case <-written:
	case <-time.After(2 * time.Second):
		t.Fatal("USQL close did not unblock the PTY write")
	}
}
