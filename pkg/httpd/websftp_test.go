package httpd

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/jumpserver/koko/pkg/httpd/ws"
)

func TestWebSFTPDownloadErrorFrame(t *testing.T) {
	user := &UserWebsocket{
		conn:           ws.NewSocket(nil, httptest.NewRequest("GET", "/", nil)),
		messageChannel: make(chan *Message, 2),
	}
	handler := newWebSFTP(user)
	response := &Message{Id: "download", Type: SFTPData}
	reader := io.MultiReader(strings.NewReader("partial"), iotest.ErrReader(io.ErrUnexpectedEOF))
	err := handler.streamFileContent(reader, response)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("download error = %v", err)
	}
	handler.sendError(response, err)
	chunk := <-user.messageChannel
	if chunk.Type != SFTPBinary || string(chunk.Raw) != "partial" {
		t.Fatalf("unexpected download chunk: %+v", chunk)
	}
	final := <-user.messageChannel
	if final.Type != SFTPData || final.Err != io.ErrUnexpectedEOF.Error() {
		t.Fatalf("download failure must terminate with a data error frame: %+v", final)
	}
}

type blockingSFTP struct {
	sftpFileSystem
	entered chan struct{}
	closed  chan struct{}
}

func (f *blockingSFTP) ReadDirWithCurrentPath(string) ([]os.FileInfo, string, error) {
	f.entered <- struct{}{}
	<-f.closed
	return nil, "", os.ErrClosed
}
func (f *blockingSFTP) Close() { close(f.closed) }

func TestWebSFTPRequestCleanup(t *testing.T) {
	fs := &blockingSFTP{entered: make(chan struct{}, 4), closed: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ws := &UserWebsocket{done: ctx.Done(), messageChannel: make(chan *Message, 8)}
	handler := newWebSFTP(ws)
	handler.volume = newSFTPVolume(fs, nil)
	close(handler.ready)
	for range 4 {
		handler.HandleMessage(&Message{Cmd: "list", Data: `{"path":"/"}`})
	}
	for range 4 {
		select {
		case <-fs.entered:
		case <-time.After(time.Second):
			t.Fatal("request did not start")
		}
	}
	cancel()
	cleaned := make(chan struct{})
	go func() { handler.CleanUp(); close(cleaned) }()
	select {
	case <-cleaned:
	case <-time.After(time.Second):
		t.Fatal("cleanup left a file request blocked")
	}
	handler.CleanUp()
	if len(handler.requests) != 0 {
		t.Fatal("request slots were not released")
	}
	handler.HandleMessage(&Message{Cmd: "list", Data: `{"path":"/"}`})
	if len(fs.entered) != 0 {
		t.Fatal("a new request reached the closed filesystem")
	}
}
