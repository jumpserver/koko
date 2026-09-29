package srvconn

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/investigato/go-psrpcore/fragments"
	"github.com/investigato/go-psrpcore/messages"
	"github.com/investigato/go-psrpcore/pipeline"
	"github.com/investigato/go-psrpcore/serialization"
)

func TestWinRMFragmentedOutput(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()
	pl := pipeline.NewWithContext(ctx, nil, id, "Get-Location")
	defer pl.Cancel()
	var wire, output bytes.Buffer
	fragmenter := fragments.NewFragmenter(70)
	serializer := serialization.NewSerializer()
	defer serializer.Close()
	text := "中文 output with PS> and _x000D_"
	data, err := serializer.Serialize(text)
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range []*messages.Message{
		messages.NewPipelineOutput(id, pl.ID(), data),
		messages.NewPipelineState(id, pl.ID(), messages.PipelineStateCompleted, []byte("<I32>4</I32>")),
	} {
		encoded, err := msg.Encode()
		if err != nil {
			t.Fatal(err)
		}
		parts, err := fragmenter.Fragment(encoded)
		if err != nil {
			t.Fatal(err)
		}
		for _, part := range parts {
			encoded, err = part.Encode()
			if err != nil {
				t.Fatal(err)
			}
			wire.Write(encoded)
		}
	}
	if err = receiveWinRMPipeline(&wire, pl); err != nil {
		t.Fatal(err)
	}
	streams := []<-chan *messages.Message{pl.Output(), pl.Error(), pl.Warning(), pl.Verbose(), pl.Debug(), pl.Progress(), pl.Information()}
	if err = consumeWinRMStreams(ctx, streams, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != text+"\n" || pl.Wait() != nil {
		t.Fatalf("unexpected PowerShell output: %q", output.String())
	}
}

func TestWinRMRejectsOversizedFragment(t *testing.T) {
	pl := pipeline.New(nil, uuid.New(), "Get-Location")
	defer pl.Cancel()
	header := make([]byte, 21)
	binary.BigEndian.PutUint32(header[17:], 1024*1024+1)
	if err := receiveWinRMPipeline(bytes.NewReader(header), pl); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected bounded decode failure, got %v", err)
	}
}

type winRMTestRoundTripper func(*http.Request) (*http.Response, error)

func (f winRMTestRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestWinRMNeverReplaysAuthenticatedRequest(t *testing.T) {
	sends := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()
	base := http.DefaultTransport.(*http.Transport).Clone()
	defer base.CloseIdleConnections()
	authenticated := true
	inner := &winRMBoundedTransport{Transport: base, authenticated: func() bool { return authenticated }}
	outer := &winRMTransport{resetAuth: func() {}, RoundTripper: winRMTestRoundTripper(func(req *http.Request) (*http.Response, error) {
		response, err := inner.RoundTrip(req)
		if err != nil {
			return nil, err
		}
		response.Body.Close()
		authenticated = false // The authentication library resets before replaying.
		return inner.RoundTrip(req)
	})}
	req, _ := http.NewRequest(http.MethodPost, server.URL, strings.NewReader("mutating command"))
	req.Header.Set("Content-Type", "multipart/encrypted")
	if _, err := outer.RoundTrip(req); err == nil || !strings.Contains(err.Error(), "replay blocked") {
		t.Fatalf("expected replay refusal, got %v", err)
	}
	if sends != 1 {
		t.Fatalf("authenticated request sent %d times", sends)
	}
}

func TestWinRMChecksPermissionAfterWaitingForRunspace(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &WinRMClient{ctx: ctx, permit: make(chan struct{}, 1)}
	client.permit <- struct{}{}
	denied := errors.New("session locked")
	client.ExecutionGuard = func() error { return denied }
	done := make(chan error, 1)
	go func() { done <- client.Execute(ctx, "Restart-Service Spooler", io.Discard) }()
	<-client.permit
	if err := <-done; !errors.Is(err, denied) {
		t.Fatalf("queued command bypassed permission check: %v", err)
	}
}

func TestWinRMTerminalEditsAndInterruptsInput(t *testing.T) {
	c := newWinRMTestTerminal(t)
	commands := make(chan string, 1)
	c.Start(func(ctx context.Context, command, user string, output io.Writer) error {
		commands <- user + ":" + command
		return nil
	})
	// Ctrl-C inside a buffered frame clears the old line and preserves the session.
	_, _ = c.WriteInput([]byte("ignored\x03Get-LocX\x7fation\r"), "shared user")
	select {
	case command := <-commands:
		if command != "shared user:Get-Location" {
			t.Fatalf("unexpected submitted command: %q", command)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("terminal input stalled")
	}
	// AI output goes through the terminal stream without becoming input or changing a typed command.
	_, _ = c.WriteInput([]byte("Get-"), "shared user")
	if _, err := c.WriteOutput(context.Background(), []byte("PS> Write-Output 'AI'\nAI-PTY-OK\n")); err != nil {
		t.Fatal(err)
	}
	_, _ = c.WriteInput([]byte("Location\r"), "shared user")
	select {
	case command := <-commands:
		if command != "shared user:Get-Location" {
			t.Fatalf("foreground output changed terminal input: %q", command)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("terminal input stalled after AI output")
	}
	// An overflow followed by editing must not turn truncated input into a command.
	_, _ = c.WriteInput([]byte(strings.Repeat("x", 4097)+"\x7f\r"), "shared user")
	_, _ = c.WriteInput([]byte("Get-Service\r"), "shared user")
	select {
	case command := <-commands:
		if command != "shared user:Get-Service" {
			t.Fatalf("truncated input was executed: %q", command)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("terminal did not recover from oversized input")
	}
}

func newWinRMTestTerminal(t *testing.T) *WinRMConnection {
	t.Helper()
	reader, writer := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	c := &WinRMConnection{input: make(chan winRMInput, 16), interrupt: make(chan struct{}, 1),
		output: reader, writer: writer, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	c.resetTerminal()
	go io.Copy(io.Discard, reader)
	t.Cleanup(func() { cancel(); reader.Close(); writer.Close(); <-c.done })
	return c
}

func TestWinRMCancelsConfirmation(t *testing.T) {
	c := newWinRMTestTerminal(t)
	started, finished := make(chan struct{}), make(chan bool, 1)
	c.Start(func(ctx context.Context, command, user string, output io.Writer) error {
		close(started)
		finished <- c.Confirm(ctx, "Continue?")
		return nil
	})
	_, _ = c.WriteInput([]byte("Restart-Service Spooler\r"), "user")
	<-started
	_, _ = c.WriteInput([]byte{3}, "user")
	select {
	case approved := <-finished:
		if approved {
			t.Fatal("cancelled confirmation approved execution")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("confirmation remained blocked after cancellation")
	}
}

func TestWinRMRefusesEncryptionDowngrade(t *testing.T) {
	data := make([]byte, 24)
	copy(data, "NTLMSSP\x00")
	binary.LittleEndian.PutUint32(data[8:], 2)
	header := make(http.Header)
	for _, flags := range []uint32{0, 0x10, 0x30} {
		binary.LittleEndian.PutUint32(data[20:], flags)
		header.Set("WWW-Authenticate", "Negotiate "+base64.StdEncoding.EncodeToString(data))
		if err := validateWinRMChallenge(header); (err == nil) != (flags == 0x30) {
			t.Fatalf("encryption flags %#x: %v", flags, err)
		}
	}
}

func TestWinRMUsernameForms(t *testing.T) {
	for _, value := range []string{"user", `DOMAIN\user`, "user@example.com"} {
		user, domain := splitWinRMUsername(value)
		if value == `DOMAIN\user` {
			if user != "user" || domain != "DOMAIN" {
				t.Fatalf("invalid domain account: %s\\%s", domain, user)
			}
		} else if user != value || domain != "" {
			t.Fatalf("invalid local or UPN account: %s\\%s", domain, user)
		}
	}
}

func TestWinRMAuthenticationDoesNotSendPlaintextCommand(t *testing.T) {
	sent := make(chan int, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		sent <- len(body)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	portNumber, _ := strconv.Atoi(port)
	if client, err := NewWinRMClient(WinRMConfig{Host: host, Port: portNumber, Username: "user", Password: "test-password"}); err == nil {
		client.Close()
		t.Fatal("unauthenticated endpoint was accepted")
	}
	if size := <-sent; size != 0 {
		t.Fatalf("NTLM handshake disclosed %d bytes of SOAP payload", size)
	}
}
