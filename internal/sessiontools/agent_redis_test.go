package sessiontools

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestAgentRedisReplyValidationBeforeDriverAllocation(t *testing.T) {
	for _, wire := range []string{
		fmt.Sprintf("$%d\r\n", maxRedisRESPBytes+1),
		fmt.Sprintf("*%d\r\n", maxRedisRESPElements+1),
		"*50000\r\n*50001\r\n",
		strings.Repeat("*1\r\n", 66),
		strings.Repeat("+", (8<<10)+1),
	} {
		conn := &agentRedisConn{reader: bufio.NewReaderSize(strings.NewReader(wire), 8<<10)}
		var buffer [32]byte
		if n, err := conn.Read(buffer[:]); n != 0 || !errors.Is(err, errAgentRedisReplyLimit) {
			t.Fatalf("unbounded reply reached the driver: %d %v", n, err)
		}
	}
}

func TestAgentRedisReplyPreservesBinaryAndPipelinedResponses(t *testing.T) {
	wire := "*3\r\n$5\r\na\x00\r\nb\r\n$-1\r\n:42\r\n+OK\r\n-ERR denied\r\n"
	conn := &agentRedisConn{reader: bufio.NewReaderSize(strings.NewReader(wire), 8<<10)}
	var output bytes.Buffer
	if _, err := io.CopyBuffer(&output, conn, make([]byte, 3)); err != nil || output.String() != wire {
		t.Fatalf("bounded RESP changed a valid reply: %q %v", output.String(), err)
	}
	for _, malformed := range []string{"$-2\r\n", "*no\r\n", "$1\r\na!!", "+OK\n"} {
		conn := &agentRedisConn{reader: bufio.NewReader(strings.NewReader(malformed))}
		if _, err := conn.Read(make([]byte, 32)); err == nil {
			t.Fatalf("malformed RESP admitted: %q", malformed)
		}
	}
}
