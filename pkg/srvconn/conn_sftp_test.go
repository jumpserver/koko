package srvconn

import (
	"bytes"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

// Concurrent writes split one WriteAt into overlapping 32KB packets. A wrong
// option set would corrupt or truncate the file rather than fail loudly.
func TestSftpClientOptionsWriteLargeChunk(t *testing.T) {
	clientPipe, serverPipe := net.Pipe()
	_ = clientPipe.SetDeadline(time.Now().Add(10 * time.Second))
	t.Cleanup(func() { _ = clientPipe.Close(); _ = serverPipe.Close() })
	server, err := sftp.NewServer(serverPipe)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve() }()
	client, err := sftp.NewClientPipe(clientPipe, clientPipe, sftpClientOptions()...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })

	path := filepath.Join(t.TempDir(), "chunk.bin")
	file, err := client.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	chunk := bytes.Repeat([]byte("jumpserver"), 64*1024) // 640KB, many packets
	if n, err := file.WriteAt(chunk, 0); err != nil || n != len(chunk) {
		t.Fatalf("WriteAt = %d, %v", n, err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	read, err := client.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = read.Close() })
	got := make([]byte, len(chunk))
	if _, err := read.ReadAt(got, 0); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, chunk) {
		t.Fatal("concurrent write corrupted the file")
	}
}
