package httpd

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
)

const (
	sftpBinaryVersion    byte = 0x01
	sftpBinaryHeaderSize      = 5
	sftpBinaryJSONMax         = 64 * 1024
)

type sftpBinaryHeader struct {
	Id        string `json:"id"`
	Type      string `json:"type"`
	Cmd       string `json:"cmd,omitempty"`
	Data      string `json:"data,omitempty"`
	Err       string `json:"err,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
}

func encodeSftpBinaryFrame(msg *Message) ([]byte, error) {
	headerType := msg.Type
	if headerType == SFTPTransferBinary {
		headerType = SFTPBinary
	}
	payload, err := json.Marshal(sftpBinaryHeader{
		Id:        msg.Id,
		Type:      headerType,
		Cmd:       msg.Cmd,
		Data:      msg.Data,
		Err:       msg.Err,
		ErrorCode: msg.ErrorCode,
	})
	if err != nil {
		return nil, err
	}
	if len(payload) > sftpBinaryJSONMax {
		return nil, fmt.Errorf("sftp binary header too large")
	}
	if len(msg.Raw) > transferChunkMaxSize {
		return nil, fmt.Errorf("sftp binary payload too large")
	}
	frame := make([]byte, sftpBinaryHeaderSize+len(payload)+len(msg.Raw))
	frame[0] = sftpBinaryVersion
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(payload)))
	copy(frame[sftpBinaryHeaderSize:], payload)
	copy(frame[sftpBinaryHeaderSize+len(payload):], msg.Raw)
	return frame, nil
}

func parseSftpBinaryFrame(frame []byte) (*Message, error) {
	if len(frame) < sftpBinaryHeaderSize || frame[0] != sftpBinaryVersion {
		return nil, fmt.Errorf("invalid sftp binary frame")
	}
	jsonLen := int(binary.BigEndian.Uint32(frame[1:5]))
	if jsonLen > sftpBinaryJSONMax {
		return nil, fmt.Errorf("invalid sftp binary header length")
	}
	headerEnd := sftpBinaryHeaderSize + jsonLen
	if headerEnd > len(frame) {
		return nil, fmt.Errorf("invalid sftp binary frame")
	}
	payload := frame[headerEnd:]
	if len(payload) > transferChunkMaxSize {
		return nil, fmt.Errorf("sftp binary payload too large")
	}
	var header sftpBinaryHeader
	if err := json.Unmarshal(frame[sftpBinaryHeaderSize:headerEnd], &header); err != nil {
		return nil, fmt.Errorf("invalid sftp binary header")
	}
	if header.Id == "" || header.Type == "" {
		return nil, fmt.Errorf("invalid sftp binary header")
	}
	raw := append([]byte(nil), payload...)
	return &Message{
		Id:        header.Id,
		Type:      header.Type,
		Cmd:       header.Cmd,
		Data:      header.Data,
		Err:       header.Err,
		ErrorCode: header.ErrorCode,
		Raw:       raw,
	}, nil
}
