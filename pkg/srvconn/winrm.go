package srvconn

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/investigato/go-psrp/powershell"
	"github.com/investigato/go-psrp/wsman"
	"github.com/investigato/go-psrp/wsman/transport"
	"github.com/investigato/go-psrpcore/fragments"
	"github.com/investigato/go-psrpcore/messages"
	"github.com/investigato/go-psrpcore/pipeline"
	"github.com/investigato/go-psrpcore/runspace"
	"github.com/investigato/go-psrpcore/serialization"
	"github.com/investigato/ntlmssp"
	ntlmhttp "github.com/investigato/ntlmssp/http"
	"github.com/jumpserver/koko/pkg/logger"
)

type WinRMConfig struct {
	Host, ServerName, Username, Password, CACert string
	Port                                         int
	UseSSL, AllowInvalidCert                     bool
}

// WinRMClient owns one runspace. Commands are serialized so that variables and
// the working directory belong to the same PowerShell session.
type WinRMClient struct {
	ExecutionGuard func() error
	backend        *powershell.WSManBackend
	pool           *runspace.Pool
	http           *http.Client
	resetAuth      func()
	permit         chan struct{}
	ctx            context.Context
	cancel         context.CancelFunc
	messageID      uint64
}

func NewWinRMClient(config WinRMConfig) (*WinRMClient, error) {
	if config.Host == "" || config.Port < 1 || config.Port > 65535 || config.Username == "" || config.Password == "" {
		return nil, errors.New("WinRM requires a host, valid port, username and password")
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: config.ServerName,
		InsecureSkipVerify: config.AllowInvalidCert}
	if config.CACert != "" {
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM([]byte(config.CACert)) {
			return nil, errors.New("invalid WinRM CA certificate")
		}
		tlsConfig.RootCAs = roots
	}
	tr := transport.NewHTTPTransport(transport.WithTimeout(75*time.Second),
		transport.WithTLSConfig(tlsConfig), transport.WithProxy("direct"))
	// Bound each SOAP response before authentication or XML decoding reads it.
	base := tr.Client().Transport.(*http.Transport)
	base.DialContext = (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	username, domain := splitWinRMUsername(config.Username)
	ntlm, err := ntlmssp.NewClient(ntlmssp.SetUserInfo(username, config.Password, nil), ntlmssp.SetDomain(domain))
	if err != nil {
		return nil, err
	}
	innerHTTP := &http.Client{Transport: base, Timeout: 75 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	roundTripper, err := ntlmhttp.NewClient(innerHTTP, ntlm, ntlmhttp.Encryption(true), ntlmhttp.SendCBT(config.UseSSL))
	if err != nil {
		return nil, err
	}
	resetAuth := func() { ntlm.Reset(); base.CloseIdleConnections() }
	innerHTTP.Transport = &winRMBoundedTransport{Transport: base, authenticated: ntlm.Complete}
	tr.Client().Transport = &winRMTransport{RoundTripper: roundTripper, resetAuth: resetAuth}
	tr.Client().CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	scheme := "http"
	if config.UseSSL {
		scheme = "https"
	}
	endpoint := (&url.URL{Scheme: scheme, Host: net.JoinHostPort(config.Host, strconv.Itoa(config.Port)), Path: "/wsman"}).String()
	ws := wsman.NewClient(endpoint, tr)
	bridge := powershell.NewWSManTransport(ws, nil, "")
	backend := powershell.NewWSManBackend(ws, bridge)
	backend.SetResourceURI(wsman.ResourceURIPowerShell)
	backend.SetIdleTimeout("PT24H") // Koko enforces the user's idle and maximum session limits.
	ctx, cancel := context.WithCancel(context.Background())
	bridge.SetContext(ctx)
	// Pool reads are only used for the initial handshake; pipeline reads below
	// have their own limits. Bound the entire handshake as well as each SOAP body.
	pool := runspace.New(struct {
		io.Reader
		io.Writer
	}{io.LimitReader(bridge, 2*1024*1024), bridge}, uuid.New())
	_ = pool.SetMinRunspaces(1)
	_ = pool.SetMaxRunspaces(1)
	openCtx, openCancel := context.WithTimeout(ctx, 30*time.Second)
	defer openCancel()
	bridge.SetContext(openCtx)
	if err := backend.Init(openCtx, pool); err != nil {
		cancel()
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		_ = backend.Close(closeCtx)
		tr.Client().CloseIdleConnections()
		return nil, fmt.Errorf("open WinRM PowerShell session: %w", err)
	}
	bridge.SetContext(ctx)
	pool.SetMessageID(2)
	return &WinRMClient{backend: backend, pool: pool, http: tr.Client(), resetAuth: resetAuth,
		permit: make(chan struct{}, 1), ctx: ctx, cancel: cancel, messageID: 2}, nil
}

func splitWinRMUsername(value string) (username, domain string) {
	if domain, username, ok := strings.Cut(value, `\`); ok {
		return username, domain
	}
	// UPN names stay intact, matching Windows SSPI and pyspnego semantics.
	return value, ""
}

func (c *WinRMClient) Execute(ctx context.Context, command string, output io.Writer) (err error) {
	select {
	case c.permit <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	case <-c.ctx.Done():
		return io.ErrClosedPipe
	}
	defer func() { <-c.permit }()
	if c.ctx.Err() != nil {
		return io.ErrClosedPipe
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(c.ctx, cancel)
	defer stop()
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.ExecutionGuard != nil {
		if err := c.ExecutionGuard(); err != nil {
			return err
		}
	}
	// Start each command on a fresh authenticated connection. An idle NTLM
	// connection can expire independently of the PowerShell runspace.
	c.resetAuth()
	// Out-String formats objects on the target, using PowerShell's normal table
	// and list formatting. Dot sourcing retains the user's runspace variables.
	encoded := base64.StdEncoding.EncodeToString([]byte(command))
	script := `. ([scriptblock]::Create([Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('` + encoded + `')))) | Out-String -Stream`
	pl, err := c.pool.CreatePipeline(script)
	if err != nil {
		return err
	}
	defer pl.Cancel()
	c.messageID++
	c.pool.SetMessageID(c.messageID)
	payload, err := pl.GetCreatePipelineDataWithID(c.messageID)
	if err != nil {
		pl.Fail(err)
		return err
	}
	reader, _, err := c.backend.PreparePipeline(ctx, pl, base64.StdEncoding.EncodeToString(payload))
	if err != nil {
		pl.Fail(err)
		// A lost Command response may leave a running pipeline with no returned
		// command ID. Close the session so backend cleanup can terminate it.
		c.cancel()
		return err
	}
	// Cleanup must still reach the target when the command context was cancelled.
	defer func() {
		if err != nil {
			c.resetAuth()
		}
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		if bridge, ok := reader.(*powershell.WSManTransport); ok {
			bridge.SetContext(closeCtx)
			if closeErr := bridge.Close(); closeErr != nil && err != nil {
				c.cancel()
			}
		}
	}()
	if err := pl.Invoke(ctx); err != nil {
		pl.Fail(err)
		return err
	}
	done := make(chan error, 1)
	go func() { done <- receiveWinRMPipeline(reader, pl) }()
	streams := []<-chan *messages.Message{pl.Output(), pl.Error(), pl.Warning(), pl.Verbose(), pl.Debug(), pl.Progress(), pl.Information()}
	consumeErr := consumeWinRMStreams(ctx, streams, output)
	if consumeErr != nil {
		cancel()
		pl.Cancel() // Wake any receive loop blocked on a full output channel.
	}
	readErr := <-done
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if consumeErr != nil {
		return consumeErr
	}
	if readErr != nil {
		return readErr
	}
	return pl.Wait()
}

func receiveWinRMPipeline(reader io.Reader, pl *pipeline.Pipeline) error {
	assembler := fragments.NewAssemblerWithLimits(4, 64)
	sizes := make(map[uint64]int)
	for {
		select {
		case <-pl.Done():
			return nil
		default:
		}
		header := make([]byte, 21)
		_, err := io.ReadFull(reader, header)
		if err != nil {
			pl.Fail(err)
			return err
		}
		size := binary.BigEndian.Uint32(header[17:])
		if size > 1024*1024 {
			err = errors.New("WinRM fragment exceeds 1 MiB")
			pl.Fail(err)
			return err
		}
		data := make([]byte, 21+int(size))
		copy(data, header)
		if _, err = io.ReadFull(reader, data[21:]); err != nil {
			pl.Fail(err)
			return err
		}
		fragment, err := fragments.Decode(data)
		if err != nil {
			pl.Fail(err)
			return err
		}
		sizes[fragment.ObjectID] += len(fragment.Data)
		if sizes[fragment.ObjectID] > 2*1024*1024 {
			err = errors.New("WinRM message exceeds 2 MiB")
			pl.Fail(err)
			return err
		}
		complete, message, err := assembler.Add(fragment)
		if err != nil {
			pl.Fail(err)
			return err
		}
		if !complete {
			continue
		}
		delete(sizes, fragment.ObjectID)
		msg, err := messages.Decode(message)
		// Host input and full-screen programs cannot use this line-oriented
		// session. Reject host calls before the library starts concurrent I/O.
		if err == nil && msg.Type == messages.MessageTypePipelineHostCall {
			err = errors.New("interactive PowerShell host calls are unsupported; supply command arguments explicitly")
		}
		if err == nil {
			err = pl.HandleMessage(msg)
		}
		if err != nil {
			pl.Fail(err)
			return err
		}
	}
}

func consumeWinRMStreams(ctx context.Context, streams []<-chan *messages.Message, output io.Writer) error {
	out, errs, warn, verbose, debug, progress, info := streams[0], streams[1], streams[2], streams[3], streams[4], streams[5], streams[6]
	for out != nil || errs != nil || warn != nil || verbose != nil || debug != nil || progress != nil || info != nil {
		var msg *messages.Message
		var ok bool
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok = <-out:
			if !ok {
				out = nil
			}
		case msg, ok = <-errs:
			if !ok {
				errs = nil
			}
		case msg, ok = <-warn:
			if !ok {
				warn = nil
			}
		case msg, ok = <-verbose:
			if !ok {
				verbose = nil
			}
		case msg, ok = <-debug:
			if !ok {
				debug = nil
			}
		case _, ok = <-progress:
			if !ok {
				progress = nil
			}
			continue
		case msg, ok = <-info:
			if !ok {
				info = nil
			}
		}
		if msg == nil {
			continue
		}
		deserializer := serialization.NewDeserializer()
		values, err := deserializer.Deserialize(msg.Data)
		deserializer.Close()
		if err != nil {
			return err
		}
		for _, value := range values {
			if _, err := fmt.Fprintln(output, value); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *WinRMClient) Close() error {
	c.cancel()
	c.permit <- struct{}{}
	defer func() { <-c.permit }()
	c.resetAuth()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = c.pool.Close(ctx)
	err := c.backend.Close(ctx)
	c.http.CloseIdleConnections()
	return err
}

type winRMExchangeKey struct{}
type winRMExchange struct {
	authenticated bool
	body          io.ReadCloser
}

type winRMTransport struct {
	http.RoundTripper
	resetAuth func()
}

func (t *winRMTransport) RoundTrip(req *http.Request) (response *http.Response, err error) {
	exchange := &winRMExchange{}
	// The authentication library decodes server-controlled MIME and signature
	// lengths. Contain malformed responses at the network boundary.
	defer func() {
		if recover() != nil {
			response, err = nil, errors.New("invalid WinRM authentication response; command outcome may be unknown")
		}
		if err != nil {
			if exchange.body != nil {
				_ = exchange.body.Close()
			}
			t.resetAuth()
		}
	}()
	req = req.Clone(context.WithValue(req.Context(), winRMExchangeKey{}, exchange))
	req.GetBody = nil // Do not let net/http replay an authenticated POST either.
	return t.RoundTripper.RoundTrip(req)
}

func (t *winRMTransport) CloseIdleConnections() { t.resetAuth() }

type winRMBoundedTransport struct {
	*http.Transport
	authenticated func() bool
}

func (t *winRMBoundedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	authenticated := t.authenticated()
	if exchange, ok := req.Context().Value(winRMExchangeKey{}).(*winRMExchange); ok {
		if exchange.authenticated {
			return nil, errors.New("WinRM authenticated request replay blocked; command outcome may be unknown")
		}
		exchange.authenticated = authenticated
	}
	if !authenticated {
		// Negotiate authentication with empty POSTs. The library retains the SOAP
		// body for the final encrypted request; never send commands in plaintext.
		if req.Body != nil {
			_ = req.Body.Close()
		}
		req = req.Clone(req.Context())
		req.Body, req.ContentLength, req.GetBody = http.NoBody, 0, nil
	} else if !strings.HasPrefix(req.Header.Get("Content-Type"), "multipart/encrypted") {
		return nil, errors.New("WinRM requires encrypted authenticated requests")
	}
	response, err := t.Transport.RoundTrip(req)
	if err == nil {
		logger.Debugf("WinRM exchange authenticated=%t request_bytes=%d status=%d response_type=%q", authenticated, req.ContentLength, response.StatusCode, response.Header.Get("Content-Type"))
		response.Body = &winRMBoundedBody{ReadCloser: response.Body, remaining: 2 * 1024 * 1024}
		if exchange, ok := req.Context().Value(winRMExchangeKey{}).(*winRMExchange); ok {
			exchange.body = response.Body
		}
		if response.StatusCode == http.StatusUnauthorized {
			err = validateWinRMChallenge(response.Header)
		} else if response.StatusCode < 300 || response.StatusCode == http.StatusInternalServerError {
			if !t.authenticated() || !strings.HasPrefix(response.Header.Get("Content-Type"), "multipart/encrypted") {
				err = errors.New("WinRM requires NTLM authentication and encrypted responses")
			}
		}
		if err != nil {
			_ = response.Body.Close()
			return nil, err
		}
	}
	return response, err
}

func validateWinRMChallenge(header http.Header) error {
	for _, value := range header.Values("WWW-Authenticate") {
		if token, ok := strings.CutPrefix(value, "Negotiate "); ok {
			data, err := base64.StdEncoding.DecodeString(token)
			if err != nil || len(data) < 24 || string(data[:8]) != "NTLMSSP\x00" || binary.LittleEndian.Uint32(data[8:12]) != 2 {
				return errors.New("invalid WinRM NTLM challenge")
			}
			// MS-NLMP 2.2.2.5: SIGN (0x10) and SEAL (0x20) are mandatory
			// here. The authentication library otherwise permits plaintext fallback.
			if binary.LittleEndian.Uint32(data[20:24])&0x30 != 0x30 {
				return errors.New("WinRM server did not negotiate NTLM signing and encryption")
			}
		}
	}
	return nil
}

type winRMBoundedBody struct {
	io.ReadCloser
	remaining int
}

func (b *winRMBoundedBody) Read(data []byte) (int, error) {
	if b.remaining == 0 {
		return 0, errors.New("WinRM response exceeds 2 MiB")
	}
	if len(data) > b.remaining {
		data = data[:b.remaining]
	}
	n, err := b.ReadCloser.Read(data)
	b.remaining -= n
	return n, err
}
