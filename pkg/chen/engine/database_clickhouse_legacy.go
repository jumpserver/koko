package engine

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	chproto "github.com/ClickHouse/ch-go/proto"
	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/column"
	"github.com/ClickHouse/clickhouse-go/v2/lib/proto"
)

func clickHouseLegacyHandshakeError(err error) bool {
	var exception *clickhouse.Exception
	if !errors.As(err, &exception) || exception.Code != 115 {
		return false
	}
	for _, name := range []string{"client_protocol_version", "'client_protocol_version'", `"client_protocol_version"`} {
		marker := "Unknown setting " + name
		if i := strings.Index(exception.Message, marker); i >= 0 {
			rest := exception.Message[i+len(marker):]
			if rest == "" || strings.ContainsRune(" \t\r\n.,;(", rune(rest[0])) {
				return true
			}
		}
	}
	return false
}

// Used only after the unmodified driver's initial handshake positively rejects
// client_protocol_version. Bridge that one fixed internal handshake; never
// rewrite or replay application SQL, and leave modern HTTP/Native untouched.
type clickHouseLegacyTransport struct {
	next      http.RoundTripper
	sessionID string
	helloDone bool // a physical driver connection serializes requests
}

func newClickHouseLegacyTransport(next http.RoundTripper) (*clickHouseLegacyTransport, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	return &clickHouseLegacyTransport{next: next, sessionID: hex.EncodeToString(id[:])}, nil
}

func (t *clickHouseLegacyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	current := request.Clone(request.Context())
	query := current.URL.Query()
	query.Del("client_protocol_version")
	if query.Get("session_id") == "" {
		query.Set("session_id", t.sessionID)
	}
	current.URL.RawQuery = query.Encode()
	if t.helloDone {
		return t.next.RoundTrip(current)
	}
	if request.Body != nil {
		defer request.Body.Close()
	}
	// This is pinned to the public driver's v2.49.0 hello. A future change
	// must fail closed instead of accidentally transforming a user query.
	if request.Method != http.MethodPost || request.GetBody == nil {
		return nil, errors.New("unsupported legacy ClickHouse HTTP handshake")
	}
	body, err := request.GetBody()
	if err != nil {
		return nil, err
	}
	hello, err := io.ReadAll(io.LimitReader(body, 1024))
	_ = body.Close()
	if err != nil {
		return nil, err
	}
	if string(hello) != "SELECT displayName(), version(), revision(), timezone()" {
		return nil, errors.New("unsupported legacy ClickHouse HTTP handshake")
	}
	// Fetch only metadata with the same endpoint/auth/TLS/database/context.
	// JSON avoids depending on the legacy Native block header during bootstrap.
	query.Set("default_format", "JSONCompact")
	current.URL.RawQuery = query.Encode()
	bootstrap := []byte("SELECT version(), timezone()")
	current.Body = io.NopCloser(bytes.NewReader(bootstrap))
	current.ContentLength = int64(len(bootstrap))
	current.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(bootstrap)), nil }
	current.Header.Del("Content-Length")
	response, err := t.next.RoundTrip(current)
	if err != nil || response.StatusCode != http.StatusOK {
		return response, err
	}
	defer response.Body.Close()
	encoded, err := clickHouseLegacyHello(response.Body)
	if err != nil {
		return nil, err
	}
	t.helloDone = true
	result := *response
	result.Header = response.Header.Clone()
	for _, header := range []string{"Content-Encoding", "Transfer-Encoding", "X-ClickHouse-Exception-Code", "X-ClickHouse-Exception-Name"} {
		result.Header.Del(header)
	}
	result.Header.Set("Content-Type", "application/octet-stream")
	result.Header.Set("X-ClickHouse-Format", "Native")
	result.Header.Set("Content-Length", strconv.Itoa(len(encoded)))
	result.ContentLength = int64(len(encoded))
	result.TransferEncoding = nil
	result.Body = io.NopCloser(bytes.NewReader(encoded))
	result.Request = request
	return &result, nil
}

func clickHouseLegacyHello(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, (64<<10)+1))
	if err != nil {
		return nil, err
	}
	var metadata struct {
		Data [][]string `json:"data"`
		Rows int        `json:"rows"`
	}
	if len(data) > 64<<10 || json.Unmarshal(data, &metadata) != nil || metadata.Rows != 1 ||
		len(metadata.Data) != 1 || len(metadata.Data[0]) != 2 {
		return nil, errors.New("invalid legacy ClickHouse HTTP handshake metadata")
	}
	version, timezone := metadata.Data[0][0], metadata.Data[0][1]
	if version == "" || len(version) > 256 || timezone == "" || len(timezone) > 256 {
		return nil, errors.New("invalid legacy ClickHouse HTTP handshake metadata")
	}
	block := proto.NewBlock()
	for i, typ := range []column.Type{"String", "String", "UInt32", "String"} {
		if err := block.AddColumn(strconv.Itoa(i), typ); err != nil {
			return nil, err
		}
	}
	// The client initially reads its current wire revision, then adopts this
	// zero revision for legacy HTTP. Zero means no HTTP protocol negotiated;
	// it is not a fabricated server TCP revision. Version/timezone are real.
	if err := block.Append("ClickHouse", version, uint32(0), timezone); err != nil {
		return nil, err
	}
	buffer := new(chproto.Buffer)
	if err := block.Encode(buffer, clickhouse.ClientTCPProtocolVersion); err != nil {
		return nil, err
	}
	return buffer.Buf, nil
}

func (t *clickHouseLegacyTransport) CloseIdleConnections() {
	if closer, ok := t.next.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}
