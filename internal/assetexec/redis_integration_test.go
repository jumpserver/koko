package assetexec

import (
	"context"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
)

// The opt-in fixture must be an isolated, passwordless local Redis instance.
func TestRedisAssetCommands(t *testing.T) {
	address := os.Getenv("KOKO_AGENT_REDIS_TEST_ADDR")
	if address == "" {
		t.Skip("isolated Redis fixture not configured")
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil || (host != "127.0.0.1" && host != "::1") {
		t.Fatal("Redis fixture must use a loopback IP and explicit port")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	useTLS := os.Getenv("KOKO_AGENT_REDIS_TEST_TLS") == "1"
	token := &model.ConnectToken{Id: "isolated-test", User: model.User{ID: "user"}, Protocol: "redis",
		Actions: model.Actions{{Value: model.ActionConnect}},
		Asset: model.Asset{ID: "asset", Address: host, Protocols: []model.Protocol{{Name: "redis", Port: port}},
			SpecInfo: model.SpecInfo{UseSSL: useTLS, AllowInvalidCert: useTLS}}}
	conn, err := Open(ctx, token, Options{ConnectTimeout: time.Second, MaxOutputBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	other, err := Open(ctx, token, Options{ConnectTimeout: time.Second, MaxOutputBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	for _, tc := range []struct{ command, output string }{
		{"SET koko_mcp_protocol_test value EX 10", "OK"},
		{"GET koko_mcp_protocol_test", "value"},
		{"MGET koko_mcp_protocol_test koko_mcp_missing", "value\n(nil)"},
	} {
		var output strings.Builder
		result, err := conn.Execute(ctx, Request{Command: tc.command}, func(_ string, data []byte) error { _, _ = output.Write(data); return nil })
		if err != nil || !result.Completed || result.ContextLost || output.String() != tc.output {
			t.Fatalf("Redis execution failed: %+v %v %q", result, err, output.String())
		}
	}
	for _, command := range []string{"MULTI", "CLIENT REPLY OFF"} {
		result, err := conn.Execute(ctx, Request{Command: command}, func(string, []byte) error { return nil })
		if !errors.Is(err, ErrUnsupported) || result.Dispatched {
			t.Fatal("interactive Redis state was dispatched")
		}
	}
	result, err := conn.Execute(ctx, Request{Command: "GET koko_mcp_protocol_test"}, func(string, []byte) error { return errors.New("evidence unavailable") })
	if err == nil || !result.Completed || !result.ContextLost {
		t.Fatalf("evidence failure did not close the context: %+v %v", result, err)
	}
	result, err = other.Execute(ctx, Request{Command: "PING"}, func(string, []byte) error { return nil })
	if err != nil || !result.Completed || result.ContextLost {
		t.Fatalf("closing another MCP connection affected this one: %+v %v", result, err)
	}
}
