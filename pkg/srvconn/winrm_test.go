package srvconn

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/investigato/go-psrpcore/fragments"
	"github.com/investigato/go-psrpcore/messages"
	"github.com/investigato/go-psrpcore/pipeline"
	"github.com/investigato/go-psrpcore/serialization"
)

func TestWinRMEncryptedFramingPreservesBinary(t *testing.T) {
	sealed := []byte("\r\nHeader: binary\r\n\r\n--Encrypted Boundary\r\n\x00\xff\r\n")
	signature := bytes.Repeat([]byte{0x2a}, 16)
	body := encodeWinRMMessage(sealed, signature, "application/soap+xml;charset=UTF-8", 123)
	got, sig, original, length, err := decodeWinRMMessage(body, winRMEncryptedType)
	if err != nil || !bytes.Equal(got, sealed) || !bytes.Equal(sig, signature) || original != "application/soap+xml;charset=UTF-8" || length != 123 {
		t.Fatalf("encrypted binary changed during framing: %v", err)
	}
	for _, corrupt := range [][]byte{body[:len(body)-1], encodeWinRMMessage(sealed, signature[:1], "application/soap+xml", 123)} {
		if _, _, _, _, err := decodeWinRMMessage(corrupt, winRMEncryptedType); err == nil {
			t.Fatal("invalid encrypted framing was accepted")
		}
	}
}

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
	var outputMessages []*messages.Message
	locationPrefix := "__JMS_LOCATION_test__"
	for _, line := range []string{"", text, "", "next line", "", "", locationPrefix + `C:\Windows`} {
		data, err := serializer.Serialize(line)
		if err != nil {
			t.Fatal(err)
		}
		outputMessages = append(outputMessages, messages.NewPipelineOutput(id, pl.ID(), data))
	}
	outputMessages = append(outputMessages, messages.NewPipelineState(id, pl.ID(), messages.PipelineStateCompleted, []byte("<I32>4</I32>")))
	for _, msg := range outputMessages {
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
	if err := receiveWinRMPipeline(&wire, pl); err != nil {
		t.Fatal(err)
	}
	streams := []<-chan *messages.Message{pl.Output(), pl.Error(), pl.Warning(), pl.Verbose(), pl.Debug(), pl.Progress(), pl.Information()}
	var location string
	if err := consumeWinRMStreams(ctx, streams, &output, locationPrefix, func(_ context.Context, path string) { location = path }); err != nil {
		t.Fatal(err)
	}
	if output.String() != "\n"+text+"\n\nnext line\n\n" || pl.Wait() != nil || location != `C:\Windows` {
		t.Fatalf("unexpected PowerShell output: %q", output.String())
	}
}

func TestWinRMOutputSpacing(t *testing.T) {
	for _, tc := range []struct {
		name     string
		lines    []string
		expected string
	}{
		{"plain", []string{"result"}, "result\n\n"},
		{"empty", nil, ""},
		{"blank", []string{"", ""}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := make(chan *messages.Message, len(tc.lines))
			serializer := serialization.NewSerializer()
			defer serializer.Close()
			for _, line := range tc.lines {
				data, err := serializer.Serialize(line)
				if err != nil {
					t.Fatal(err)
				}
				out <- &messages.Message{Data: data}
			}
			close(out)
			streams := make([]<-chan *messages.Message, 7)
			streams[0] = out
			var output bytes.Buffer
			if err := consumeWinRMStreams(context.Background(), streams, &output, "", nil); err != nil {
				t.Fatal(err)
			}
			if output.String() != tc.expected {
				t.Fatalf("unexpected spacing: %q", output.String())
			}
		})
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

func TestWinRMAuthenticationProbeCannotSendSOAP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		if len(body) != 0 {
			t.Errorf("authentication probe disclosed %d bytes", len(body))
		}
	}))
	defer server.Close()
	base := http.DefaultTransport.(*http.Transport).Clone()
	defer base.CloseIdleConnections()
	inner := &winRMBoundedTransport{Transport: base, authenticated: func() bool { return true }}
	req, _ := http.NewRequest(http.MethodPost, server.URL, strings.NewReader("SOAP command"))
	req = req.WithContext(context.WithValue(req.Context(), winRMExchangeKey{}, &winRMExchange{probe: true}))
	for range 2 {
		response, err := inner.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
	}
	// The plaintext exception applies only to the empty authentication probe.
	req, _ = http.NewRequest(http.MethodPost, server.URL, strings.NewReader("SOAP command"))
	if _, err := inner.RoundTrip(req); err == nil {
		t.Fatal("unencrypted SOAP was accepted")
	}
}

func TestWinRMReusesAuthenticatedTransport(t *testing.T) {
	authenticated, attempts, sends := false, 0, 0
	outer := &winRMTransport{resetAuth: func() {}, authenticated: func() bool { return authenticated },
		authenticate: func(*http.Request) error {
			attempts++
			authenticated = true
			return nil
		}, RoundTripper: winRMTestRoundTripper(func(*http.Request) (*http.Response, error) {
			sends++
			return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
		})}
	for range 3 {
		req, _ := http.NewRequest(http.MethodPost, "http://winrm/wsman", strings.NewReader("SOAP command"))
		response, err := outer.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
	}
	if attempts != 1 || sends != 3 {
		t.Fatalf("authentication was not reused: attempts=%d sends=%d", attempts, sends)
	}
}

func TestWinRMReconnectsOnlyBeforeSendingSOAP(t *testing.T) {
	authenticated, attempts, sends, handshakes := true, 0, 0, 0
	outer := &winRMTransport{resetAuth: func() { authenticated = false }, authenticated: func() bool { return authenticated },
		authenticate: func(*http.Request) error { handshakes++; authenticated = true; return nil },
		RoundTripper: winRMTestRoundTripper(func(req *http.Request) (*http.Response, error) {
			attempts++
			body, _ := io.ReadAll(req.Body)
			_ = req.Body.Close()
			if string(body) != "SOAP command" || req.Header.Get("Content-Type") != "application/soap+xml" || req.GetBody != nil {
				t.Fatal("reconnect changed SOAP or enabled HTTP replay")
			}
			if attempts == 1 {
				req.Header.Set("Content-Type", winRMEncryptedType)
				return nil, errWinRMReconnect // Dial refused before any wire bytes.
			}
			sends++
			return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
		})}
	req, _ := http.NewRequest(http.MethodPost, "http://winrm/wsman", strings.NewReader("SOAP command"))
	req.Header.Set("Content-Type", "application/soap+xml")
	response, err := outer.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if attempts != 2 || sends != 1 || handshakes != 1 {
		t.Fatalf("unsafe reconnect: attempts=%d sends=%d handshakes=%d", attempts, sends, handshakes)
	}
}

func TestWinRMOnlyRetriesAuthentication(t *testing.T) {
	for _, fail := range []bool{false, true} {
		attempts, sends := 0, 0
		outer := &winRMTransport{resetAuth: func() {}, authenticate: func(*http.Request) error {
			attempts++
			if fail || attempts == 1 {
				return io.EOF
			}
			return nil
		}, RoundTripper: winRMTestRoundTripper(func(*http.Request) (*http.Response, error) {
			sends++
			return nil, io.EOF
		})}
		req, _ := http.NewRequest(http.MethodPost, "http://winrm/wsman", strings.NewReader("SOAP command"))
		_, err := outer.RoundTrip(req)
		wantSends := 1
		if fail {
			wantSends = 0
		}
		if attempts != 2 || sends != wantSends || errors.Is(err, errWinRMAuthentication) != fail {
			t.Fatalf("unsafe retry: attempts=%d SOAP sends=%d err=%v", attempts, sends, err)
		}
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
	c.setLocation(context.Background(), `C:\Windows`)
	if c.Prompt() != `PS C:\Windows> ` {
		t.Fatalf("prompt did not follow the runspace location: %q", c.Prompt())
	}
	c.setLocation(context.Background(), "C:\\Windows\nforged prompt")
	if c.Prompt() != `PS C:\Windows> ` {
		t.Fatal("prompt accepted a control character")
	}
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
	// Editing a long line must preserve all text instead of executing a truncation.
	_, _ = c.WriteInput([]byte(strings.Repeat("x", 4097)+"\x7f\r"), "shared user")
	select {
	case command := <-commands:
		if command != "shared user:"+strings.Repeat("x", 4096) {
			t.Fatalf("long input was changed: length %d", len(command))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("terminal stalled editing a long line")
	}
}

func TestWinRMTerminalStreamsLargeInput(t *testing.T) {
	for _, test := range []struct {
		name, text string
		chunk      int
		paste      bool
	}{
		{name: "single write", text: "#" + strings.Repeat("A", 100000), chunk: 100001},
		{name: "split UTF-8", text: "#" + strings.Repeat("中文", 17000), chunk: 1021},
		{name: "bracketed paste", text: "#" + strings.Repeat("A", 100000), chunk: 1021, paste: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := newWinRMTestTerminal(t)
			commands := make(chan string, 1)
			c.Start(func(ctx context.Context, command, user string, output io.Writer) error {
				commands <- user + ":" + command
				return nil
			})
			send := func(text string, chunk int) {
				for len(text) > 0 {
					n := min(chunk, len(text))
					if written, err := c.WriteInput([]byte(text[:n]), "user"); err != nil || written != n {
						t.Fatalf("input write: %d, %v", written, err)
					}
					text = text[n:]
				}
			}
			if test.paste {
				send("\x1b[200~", 1)
			}
			send(test.text, test.chunk)
			if test.paste {
				send("\x1b[201~", 1)
			}
			// Escape sequences split across writes must still edit the completed line.
			send("\x1b[D\x7fZ\x1b[F\r", 1)
			runes := []rune(test.text)
			want := "user:" + string(runes[:len(runes)-2]) + "Z" + string(runes[len(runes)-1:])
			select {
			case got := <-commands:
				if got != want {
					t.Fatalf("long command changed: got %d bytes, want %d", len(got), len(want))
				}
			case <-time.After(5 * time.Second):
				t.Fatal("large input stalled")
			}
			send("Write-Output \"WINRM_INPUT_TEST_OK\"\r", 1024)
			select {
			case got := <-commands:
				if got != "user:Write-Output \"WINRM_INPUT_TEST_OK\"" {
					t.Fatalf("next command changed: %q", got)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("session unusable after large input")
			}
		})
	}
}

func TestWinRMEmptyInputDoesNotWaitForQueue(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &WinRMConnection{ctx: ctx, input: make(chan winRMInput, 1)}
	c.input <- winRMInput{data: []byte("queued")}
	done := make(chan error, 1)
	go func() { _, err := c.Write(nil); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("empty bridge write blocked on the input queue")
	}
}

func newWinRMTestTerminal(t *testing.T) *WinRMConnection {
	t.Helper()
	reader, writer := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	c := &WinRMConnection{input: make(chan winRMInput, 16), interrupt: make(chan struct{}, 1),
		output: reader, writer: writer, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	c.resetTerminal()
	go func() { _, _ = io.Copy(io.Discard, reader) }()
	t.Cleanup(func() { cancel(); _ = reader.Close(); _ = writer.Close(); <-c.done })
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
		if err := validateWinRMChallenge(header, true); (err == nil) != (flags == 0x30) {
			t.Fatalf("encryption flags %#x: %v", flags, err)
		}
		if err := validateWinRMChallenge(header, false); err != nil {
			t.Fatalf("TLS unnecessarily required message encryption: %v", err)
		}
	}
}

func TestWinRMTLSProtectsAuthenticatedSOAP(t *testing.T) {
	const soap = "<SOAP>command</SOAP>"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		if req.TLS == nil || string(body) != soap || req.Header.Get("Content-Type") != "application/soap+xml" {
			t.Error("TLS changed the SOAP request or message encryption was used")
		}
		w.Header().Set("Content-Type", "application/soap+xml")
		_, _ = io.WriteString(w, soap)
	}))
	defer server.Close()
	client := server.Client()
	defer client.CloseIdleConnections()
	client.Transport = &winRMBoundedTransport{Transport: client.Transport.(*http.Transport), authenticated: func() bool { return true }}
	protected := &winRMEncryptedTransport{http: client} // TLS does not need an NTLM sealing session.
	request, _ := http.NewRequest(http.MethodPost, server.URL, strings.NewReader(soap))
	request.Header.Set("Content-Type", "application/soap+xml")
	response, err := protected.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != soap {
		t.Fatalf("TLS SOAP response rejected or changed: %q, %v", body, err)
	}
}

func TestWinRMTLSCertificateValidation(t *testing.T) {
	var probes atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		probes.Add(1)
		body, _ := io.ReadAll(req.Body)
		if len(body) != 0 || req.TLS == nil {
			t.Error("unauthenticated TLS probe disclosed SOAP")
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "https://"))
	portNumber, _ := strconv.Atoi(port)
	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	for _, test := range []struct {
		name, serverName, ca string
		skip, reachable      bool
	}{
		{name: "untrusted certificate", serverName: "example.com"},
		{name: "trusted certificate through tunnel", serverName: "example.com", ca: ca, reachable: true},
		{name: "wrong server name", serverName: "wrong.invalid", ca: ca},
		{name: "allow invalid certificate", serverName: "wrong.invalid", skip: true, reachable: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := probes.Load()
			client, err := NewWinRMClient(WinRMConfig{Host: host, Port: portNumber, ServerName: test.serverName,
				Username: "user", Password: "test-password", UseSSL: true, CACert: test.ca, AllowInvalidCert: test.skip})
			if client != nil {
				_ = client.Close()
				t.Fatal("unauthenticated TLS endpoint accepted")
			}
			if (probes.Load() > before) != test.reachable {
				t.Fatalf("unexpected TLS reachability: %v", err)
			}
			var certificateErr *tls.CertificateVerificationError
			if test.reachable {
				if !errors.Is(err, errWinRMAuthentication) {
					t.Fatalf("TLS did not reach authentication: %v", err)
				}
			} else if !errors.As(err, &certificateErr) {
				t.Fatalf("certificate validation failure missing: %v", err)
			}
		})
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
