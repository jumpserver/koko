package nosql

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/bytebase/gomongo"
	"github.com/dop251/goja"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
	"go.mongodb.org/mongo-driver/v2/x/mongo/driver/connstring"
)

type mongoSession struct {
	client         *mongo.Client
	shell          *gomongo.Client
	database       string
	json           bool
	vm             *goja.Runtime
	ctx            context.Context
	out            io.Writer
	current        *mongoCursor
	cursors        map[*mongoCursor]struct{}
	sessions       map[*mongo.Session]*mongoSessionState
	uri            string
	scriptDir      string
	batchSize      int
	others         []*mongoSession
	readPreference *readpref.ReadPref
	readConcern    *readconcern.ReadConcern
	writeConcern   *writeconcern.WriteConcern
	rejections     map[*goja.Promise]goja.Value
}

func openMongo(ctx context.Context, u *url.URL, jsonOutput bool) (session, error) {
	q := u.Query()
	if len(q["proxy"]) > 1 {
		return nil, errors.New("duplicate MongoDB proxy option")
	}
	proxyURL := q.Get("proxy")
	q.Del("proxy")
	clean := *u
	clean.RawQuery = q.Encode()
	connectionString, err := connstring.ParseAndValidate(clean.String())
	if err != nil {
		return nil, errors.New("invalid MongoDB connection URL or options")
	}
	config := options.Client().ApplyURI(clean.String()).SetAppName("jumpserver-usql")
	if proxyURL != "" {
		dialer, err := newMongoProxy(proxyURL)
		if err != nil {
			return nil, err
		}
		config.SetDialer(dialer)
	}
	client, err := mongo.Connect(config)
	if err != nil {
		return nil, fmt.Errorf("initialize MongoDB client: %w", err)
	}
	if err = client.Ping(ctx, nil); err != nil {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.Disconnect(closeCtx)
		return nil, fmt.Errorf("connect MongoDB: %w", err)
	}
	database := connectionString.Database
	if database == "" {
		database = "test"
	}
	s := &mongoSession{client: client, shell: gomongo.NewClient(client), database: database, json: jsonOutput, uri: u.String(), batchSize: 20, cursors: make(map[*mongoCursor]struct{}), sessions: make(map[*mongo.Session]*mongoSessionState)}
	s.initRuntime()
	s.readPreference, s.readConcern, s.writeConcern = config.ReadPreference, config.ReadConcern, config.WriteConcern
	return s, nil
}

func (s *mongoSession) Prompt() string { return "mongodb:" + s.database + "> " }

func (s *mongoSession) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for cursor := range s.cursors {
		_ = cursor.close(ctx)
	}
	for session := range s.sessions {
		session.EndSession(ctx)
	}
	for _, other := range s.others {
		_ = other.Close()
	}
	return s.client.Disconnect(ctx)
}

func (s *mongoSession) Execute(ctx context.Context, w io.Writer, command string) error {
	return s.executeProgram(ctx, w, "<shell>", command, true)
}

// A wrapper permits BSON scalars and arrays at the top level. Canonical EJSON
// preserves int64/Decimal128/ObjectID/date values without float64 conversion.
func writeExtendedJSON(w io.Writer, value any, pretty bool) error {
	encoded, err := bson.MarshalExtJSON(bson.D{{Key: "value", Value: value}}, true, false)
	if err != nil {
		return err
	}
	encoded = encoded[len(`{"value":`) : len(encoded)-1]
	if pretty {
		var indented bytes.Buffer
		if err = json.Indent(&indented, encoded, "", "  "); err != nil {
			return err
		}
		encoded = indented.Bytes()
	}
	_, err = fmt.Fprintf(w, "%s\n", encoded)
	return err
}

// mongoProxy tunnels every topology connection, preserving SRV/replica-set
// discovery when KoKo provides an HTTP domain gateway.
type mongoProxy struct{ u *url.URL }

func newMongoProxy(raw string) (*mongoProxy, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, errors.New("MongoDB proxy must be an http:// or https:// URL")
	}
	return &mongoProxy{u: u}, nil
}

func (p *mongoProxy) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	port := p.u.Port()
	if port == "" {
		port = "80"
		if p.u.Scheme == "https" {
			port = "443"
		}
	}
	dialer := net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(p.u.Hostname(), port))
	if err != nil {
		return nil, errors.New("connect MongoDB HTTP proxy failed")
	}
	ok := false
	defer func() {
		if !ok {
			_ = conn.Close()
		}
	}()
	socket := conn
	stop := context.AfterFunc(ctx, func() { _ = socket.Close() })
	defer stop()
	if deadline, has := ctx.Deadline(); has {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	}
	if p.u.Scheme == "https" {
		secured := tls.Client(conn, &tls.Config{ServerName: p.u.Hostname(), MinVersion: tls.VersionTLS12})
		if err = secured.HandshakeContext(ctx); err != nil {
			return nil, errors.New("MongoDB proxy TLS handshake failed")
		}
		conn = secured
	}
	request := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: address}, Host: address, Header: make(http.Header)}
	if p.u.User != nil {
		password, _ := p.u.User.Password()
		request.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(p.u.User.Username()+":"+password)))
	}
	if err = request.Write(conn); err != nil {
		return nil, errors.New("write MongoDB proxy CONNECT failed")
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), request)
	if err != nil {
		return nil, errors.New("read MongoDB proxy CONNECT failed")
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MongoDB proxy CONNECT returned HTTP %d", response.StatusCode)
	}
	if !stop() && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	_ = conn.SetDeadline(time.Time{})
	ok = true
	return conn, nil
}
