package sessiontools

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
)

var errAgentRedisReplyLimit = errors.New("agent Redis response exceeds its protocol limit")

// agentRedisConn admits one fully bounded RESP2 reply before exposing its
// length headers to the driver. It is installed after TLS by the MCP factory.
type agentRedisConn struct {
	net.Conn
	reader  *bufio.Reader
	pending []byte
	err     error
}

func (c *agentRedisConn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if c.err != nil {
		return 0, c.err
	}
	if len(c.pending) == 0 {
		var reply bytes.Buffer
		elements := 0
		if err := readAgentRedisReply(c.reader, &reply, &elements, 0); err != nil {
			c.err = err
			return 0, err
		}
		c.pending = reply.Bytes()
	}
	n := copy(p, c.pending)
	c.pending = c.pending[n:]
	if len(c.pending) == 0 {
		c.pending = nil
	}
	return n, nil
}

func readAgentRedisReply(reader *bufio.Reader, reply *bytes.Buffer, elements *int, depth int) error {
	if depth > 64 {
		return errAgentRedisReplyLimit
	}
	header, err := reader.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) {
		return errAgentRedisReplyLimit
	}
	if err != nil {
		return err
	}
	if len(header) < 3 || header[len(header)-2] != '\r' {
		return errors.New("invalid agent Redis response header")
	}
	if len(header) > maxRedisRESPBytes-reply.Len() {
		return errAgentRedisReplyLimit
	}
	kind := header[0]
	var length int64
	switch kind {
	case '$', '*', ':':
		length, err = strconv.ParseInt(string(header[1:len(header)-2]), 10, 64)
		if err != nil || (kind != ':' && length < -1) {
			return errors.New("invalid agent Redis response length")
		}
	case '+', '-':
	default:
		return fmt.Errorf("unsupported agent Redis response type %q", kind)
	}
	_, _ = reply.Write(header)
	switch kind {
	case '$':
		if length == -1 {
			return nil
		}
		if length > int64(maxRedisRESPBytes-reply.Len()-2) {
			return errAgentRedisReplyLimit
		}
		if _, err := io.CopyN(reply, reader, length); err != nil {
			return err
		}
		var trailer [2]byte
		if _, err := io.ReadFull(reader, trailer[:]); err != nil {
			return err
		}
		if trailer != [2]byte{'\r', '\n'} {
			return errors.New("invalid agent Redis bulk string terminator")
		}
		_, _ = reply.Write(trailer[:])
	case '*':
		if length == -1 {
			return nil
		}
		if length > int64(maxRedisRESPElements-*elements) {
			return errAgentRedisReplyLimit
		}
		*elements += int(length)
		for range int(length) {
			if err := readAgentRedisReply(reader, reply, elements, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}
