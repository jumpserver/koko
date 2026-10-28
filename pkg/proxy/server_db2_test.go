package proxy

import (
	"context"
	"io"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/srvconn"
)

func TestDB2USQLRoutingAndAudit(t *testing.T) {
	if !slices.Contains(srvconn.SupportedDBProtocols(), srvconn.ProtocolDB2) ||
		!slices.Contains(srvconn.SupportedProtocols(), srvconn.ProtocolDB2) ||
		usqlProtocolAlias[srvconn.ProtocolDB2] != "db2" || !isDatabaseProtocol(srvconn.ProtocolDB2) {
		t.Fatal("Db2 is missing from the supported database protocols or USQL route")
	}
	parser := &Parser{protocolType: srvconn.ProtocolDB2, platform: &model.Platform{}}
	if !parser.isEnterKeyPress([]byte("VALUES\n42;\n")) || parser.isEnterKeyPress([]byte("VALUES\n42")) {
		t.Fatal("pasted Db2 SQL must use the existing SQL command-audit boundary")
	}
	server := &Server{
		UserConn: &bridgeUSQLUser{ctx: context.Background()},
		connOpts: &ConnectionOptions{authInfo: &model.ConnectToken{
			Protocol: srvconn.ProtocolDB2,
			Asset:    model.Asset{Address: "127.0.0.1", Protocols: []model.Protocol{{Name: "db2", Port: 50000}}},
		}},
		account: &model.Account{},
	}
	// An empty database fails inside the real USQL argument builder, proving
	// getServerConn dispatches Db2 there without starting a subprocess or network I/O.
	if _, err := server.getServerConn(nil); err == nil || !strings.Contains(err.Error(), "database name") {
		t.Fatalf("Db2 was not dispatched to USQL: %v", err)
	}
}

func TestLiveDB2USQL(t *testing.T) {
	path := os.Getenv("KOKO_DB2_TEST_DSN_FILE")
	if path == "" {
		t.Skip("set KOKO_DB2_TEST_DSN_FILE to a private Db2 URL file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("could not read private Db2 URL file")
	}
	u, err := url.Parse(strings.TrimSpace(string(data)))
	if err != nil || u == nil || u.Scheme != "db2" || u.User == nil || u.RawQuery != "" {
		t.Fatal("this live test requires a plain db2:// URL with no query options")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal("live Db2 URL requires an explicit port")
	}
	if err := srvconn.IsSupportedProtocol(srvconn.ProtocolDB2); err != nil {
		t.Fatal(err)
	}
	password, _ := u.User.Password()
	server := &Server{
		UserConn: &bridgeUSQLUser{ctx: context.Background()},
		connOpts: &ConnectionOptions{authInfo: &model.ConnectToken{
			Protocol: srvconn.ProtocolDB2,
			Asset: model.Asset{Address: u.Hostname(), Protocols: []model.Protocol{{Name: "db2", Port: port}},
				SpecInfo: model.SpecInfo{DBName: strings.TrimPrefix(u.Path, "/")}},
		}},
		account: &model.Account{BaseAccount: model.BaseAccount{Username: u.User.Username(), Secret: password}},
	}
	conn, err := server.getServerConn(nil)
	if err != nil {
		t.Fatal("could not launch Db2 through KoKo's USQL route")
	}
	defer conn.Close()
	readPrompt := func(expected string) string {
		t.Helper()
		type result struct {
			output string
			err    error
		}
		done := make(chan result, 1)
		go func() {
			var output strings.Builder
			buf := make([]byte, 4096)
			for output.Len() < 64*1024 {
				n, err := conn.Read(buf)
				output.Write(buf[:n])
				if strings.Contains(output.String(), "db2=>") && strings.Contains(output.String(), expected) {
					done <- result{output: output.String()}
					return
				}
				if err != nil {
					done <- result{err: err}
					return
				}
			}
			done <- result{err: io.ErrShortBuffer}
		}()
		select {
		case got := <-done:
			if got.err != nil {
				t.Fatal("Db2 process exited before the next prompt (output withheld to protect credentials)")
			}
			return got.output
		case <-time.After(20 * time.Second):
			_ = conn.Close()
			t.Fatal("timed out waiting for the Db2 prompt")
			return ""
		}
	}
	readPrompt("")
	if _, err := conn.Write([]byte("VALUES (6 * 7);\r")); err != nil {
		t.Fatal("could not write the read-only Db2 query")
	}
	readPrompt("42")
}
