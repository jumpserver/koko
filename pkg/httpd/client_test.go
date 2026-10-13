package httpd

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/internal/sessiontools"
	"github.com/jumpserver/koko/pkg/httpd/ws"
	"github.com/jumpserver/koko/pkg/proxy"
)

func TestInputLockPreservesActiveCall(t *testing.T) {
	first, cancelFirst := context.WithCancel(context.Background())
	defer cancelFirst()
	second, cancelSecond := context.WithCancel(context.Background())
	defer cancelSecond()
	client := &Client{}
	if !client.setInputLock(cancelFirst) || client.setInputLock(cancelSecond) {
		t.Fatal("concurrent call replaced the active input lock")
	}
	// Input stays protected, and Ctrl-C cancels the active call rather than the contender.
	client.WriteData([]byte("Get-Location\r"))
	client.WriteData([]byte{3})
	if first.Err() == nil || second.Err() != nil {
		t.Fatal("Ctrl-C cancelled the wrong call")
	}
	client.setInputLock(nil)
	if !client.setInputLock(cancelSecond) {
		t.Fatal("completed call did not release the input lock")
	}
	client.WriteData([]byte{3})
	if second.Err() == nil {
		t.Fatal("next call did not acquire cancellation ownership")
	}
}

func TestAgentToolWriteCancellation(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		name := "ctrl_c"
		if timeout {
			name = "timeout"
		}
		t.Run(name, func(t *testing.T) {
			reader, writer := net.Pipe()
			defer reader.Close()
			defer writer.Close()
			_ = reader.SetReadDeadline(time.Now().Add(3 * time.Second))
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			client := &Client{UserRead: reader, UserWrite: writer}
			client.setInputLock(cancel)
			written := make(chan error, 1)
			go func() { written <- client.WriteAgentToolData(ctx, []byte("SELECT 1;\r")) }()
			data := make([]byte, 1)
			if _, err := io.ReadFull(reader, data); err != nil {
				t.Fatal(err)
			}
			want := context.DeadlineExceeded
			if !timeout {
				want = context.Canceled
				go client.WriteData([]byte{3})
			}
			select {
			case err := <-written:
				if !errors.Is(err, want) {
					t.Fatalf("write error = %v, want %v", err, want)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("cancelled PTY write remained blocked")
			}
			client.setInputLock(nil)
			manual := make(chan struct{})
			go func() { client.WriteData([]byte("x")); close(manual) }()
			if _, err := io.ReadFull(reader, data); err != nil || string(data) != "x" {
				t.Fatalf("manual input did not recover: %q, %v", data, err)
			}
			<-manual
		})
	}
}

func TestInputLockRejectsPendingUserWrite(t *testing.T) {
	reader, writer := net.Pipe()
	defer reader.Close()
	defer writer.Close()
	_ = reader.SetReadDeadline(time.Now().Add(time.Second))
	client := &Client{UserWrite: writer}
	written := make(chan struct{})
	go func() { client.WriteData([]byte("xy")); close(written) }()
	data := make([]byte, 1)
	if _, err := io.ReadFull(reader, data); err != nil {
		t.Fatal(err)
	}
	if client.setInputLock(func() {}) {
		t.Fatal("PTY execution overlapped a pending user write")
	}
	if _, err := io.ReadFull(reader, data); err != nil {
		t.Fatal(err)
	}
	<-written
	if !client.setInputLock(func() {}) {
		t.Fatal("completed user input kept the terminal busy")
	}
}

func TestPTYCancellationInterruptDelivery(t *testing.T) {
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	}))
	defer core.Close()
	api, err := service.NewAuthJMService(service.JMSCoreHost(core.URL))
	if err != nil {
		t.Fatal(err)
	}
	for _, delayedRead := range []bool{false, true} {
		name := "blocked_interrupt"
		if delayedRead {
			name = "delayed_delivery"
		}
		t.Run(name, func(t *testing.T) {
			reader, writer := net.Pipe()
			defer reader.Close()
			defer writer.Close()
			_ = reader.SetReadDeadline(time.Now().Add(time.Second))
			requestContext, _ := gin.CreateTestContext(httptest.NewRecorder())
			requestContext.Request = httptest.NewRequest("GET", "/", nil)
			clientCtx, clientCancel := context.WithCancel(context.Background())
			defer clientCancel()
			client := &Client{UserRead: reader, UserWrite: writer, ctx: clientCtx, cancel: clientCancel, Conn: &UserWebsocket{
				ctx: requestContext,
			}}
			server, err := proxy.NewServer(client, api, proxy.ConnectTokenAuthInfo(&model.ConnectToken{
				Protocol: "ssh", Asset: model.Asset{Protocols: []model.Protocol{{Name: "ssh"}}},
				Actions: model.Actions{{Value: model.ActionConnect}}, ExpireAt: model.ExpireInfo(time.Now().Add(time.Hour).Unix()),
			}))
			if err != nil {
				t.Fatal(err)
			}
			observer, err := sessiontools.NewTerminalObserver(80, 24, "ssh")
			if err != nil {
				t.Fatal(err)
			}
			defer observer.Close()
			observer.Feed([]byte("$ "))
			client.setTerminalObserver(observer)
			controller := &terminalToolController{client: client, server: server}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			finished := make(chan error, 1)
			go func() { _, _, err := controller.executePTY(ctx, "echo hello", nil); finished <- err }()
			data := make([]byte, 1)
			if _, err = io.ReadFull(reader, data); err != nil {
				t.Fatal(err)
			}
			// Cancel while the rest of the command is still waiting for a reader.
			cancel()
			if delayedRead {
				select {
				case err = <-finished:
					t.Fatalf("interrupt delivery stopped before the reader resumed: %v", err)
				case <-time.After(2 * time.Second):
				}
				_ = reader.SetReadDeadline(time.Now().Add(time.Second))
				if _, err = io.ReadFull(reader, data); err != nil || data[0] != 3 {
					t.Fatalf("interrupt = %v, error = %v", data, err)
				}
			}
			select {
			case err = <-finished:
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case <-time.After(ptyInterruptDeliveryTimeout + 2*time.Second):
				t.Fatal("blocked interrupt kept the PTY call active")
			}
			manual := make(chan struct{})
			go func() { client.WriteData([]byte("x")); close(manual) }()
			if delayedRead {
				if _, err = io.ReadFull(reader, data); err != nil || string(data) != "x" {
					t.Fatalf("manual input after interrupt = %q, error = %v", data, err)
				}
			}
			select {
			case <-manual:
			case <-time.After(time.Second):
				t.Fatal("manual input remained blocked after PTY cancellation")
			}
			if delayedRead {
				return
			}
			if clientCtx.Err() == nil {
				t.Fatal("failed interrupt left the unusable terminal session active")
			}
			_ = writer.SetWriteDeadline(time.Now().Add(time.Second))
			if _, err = writer.Write([]byte("x")); !errors.Is(err, io.ErrClosedPipe) {
				t.Fatalf("input pipe was not closed after the interrupt failed: %v", err)
			}
			// Close recreates the pipe; late messages must still return without a reader.
			client.initPipe()
			defer client.UserRead.Close()
			defer client.UserWrite.Close()
			manual = make(chan struct{})
			go func() { client.WriteData([]byte("x")); close(manual) }()
			select {
			case <-manual:
			case <-time.After(time.Second):
				t.Fatal("input to the cancelled terminal blocked on a recreated pipe")
			}
		})
	}
}

func TestDatabaseOutputBufferOwnsQueuedFrames(t *testing.T) {
	token := &model.ConnectToken{}
	token.Platform.Category.Value = "database"
	client := &Client{Conn: &UserWebsocket{
		ConnectToken:   token,
		conn:           ws.NewSocket(nil, httptest.NewRequest("GET", "/", nil)),
		messageChannel: make(chan *Message, 1),
	}}
	var first *Message
	for _, output := range []string{"first", "other", "last"} {
		_, _ = client.Write([]byte(output))
		select {
		case msg := <-client.Conn.messageChannel:
			if string(msg.Raw) != output {
				t.Fatalf("output = %q, want %q", msg.Raw, output)
			}
			if first == nil {
				first = msg
			}
		case <-time.After(time.Second):
			t.Fatal("database output stopped flushing")
		}
	}
	if string(first.Raw) != "first" {
		t.Fatalf("queued output was overwritten: %q", first.Raw)
	}
}
