package httpd

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestSftpBinaryFrameRoundTrip(t *testing.T) {
	msg := &Message{
		Id:   "req-1",
		Type: SFTPTransferBinary,
		Cmd:  "transfer_write",
		Data: `{"offset":0}`,
		Raw:  []byte{1, 2, 3, 4},
	}
	frame, err := encodeSftpBinaryFrame(msg)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseSftpBinaryFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Id != "req-1" || parsed.Type != SFTPBinary || parsed.Cmd != "transfer_write" || parsed.Data != `{"offset":0}` {
		t.Fatalf("header = %+v", parsed)
	}
	if !bytes.Equal(parsed.Raw, msg.Raw) {
		t.Fatalf("payload = %v", parsed.Raw)
	}
}

func TestSftpBinaryFrameRejectsBadVersionAndLength(t *testing.T) {
	if _, err := parseSftpBinaryFrame([]byte{0x02, 0, 0, 0, 2, '{', '}'}); err == nil {
		t.Fatal("accepted unsupported version")
	}
	frame := make([]byte, 5)
	frame[0] = sftpBinaryVersion
	binary.BigEndian.PutUint32(frame[1:5], 8)
	if _, err := parseSftpBinaryFrame(frame); err == nil {
		t.Fatal("accepted truncated frame")
	}
}
