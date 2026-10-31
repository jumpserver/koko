package sessiontools

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func TestAgentSQLServerRedirectUsesControlledHostnameDialer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	_ = server.SetDeadline(time.Now().Add(2 * time.Second))
	served := make(chan error, 1)
	go func() {
		defer server.Close()
		served <- serveSQLServerRedirect(server)
	}()
	var targets []string
	_, _, _, err := OpenAgentSQLConnection(ctx, DatabaseConfig{
		Protocol: "sqlserver", Host: "asset.private.invalid", Port: 1433,
		Username: "agent", Password: "test", DisableEncrypt: true,
		DialContext: func(_ context.Context, _, address string) (net.Conn, error) {
			targets = append(targets, address)
			if len(targets) == 1 {
				return client, nil
			}
			return nil, errors.New("redirect target unavailable")
		},
	})
	if err == nil || len(targets) != 2 || targets[0] != "asset.private.invalid:1433" || targets[1] != "routed.private.invalid:1444" {
		t.Fatalf("routing escaped the configured dialer: %v %v", targets, err)
	}
	if err := <-served; err != nil {
		t.Fatal(err)
	}
}

// Only the two login exchanges needed to exercise the driver's routing path.
func serveSQLServerRedirect(conn net.Conn) error {
	readPacket := func(kind byte) error {
		var header [8]byte
		if _, err := io.ReadFull(conn, header[:]); err != nil {
			return err
		}
		length := int(binary.BigEndian.Uint16(header[2:4]))
		if header[0] != kind || length < 8 || length > 4096 {
			return errors.New("unexpected SQL Server login packet")
		}
		_, err := io.CopyN(io.Discard, conn, int64(length-8))
		return err
	}
	writePacket := func(payload []byte) error {
		header := []byte{4, 1, 0, 0, 0, 0, 0, 0}
		binary.BigEndian.PutUint16(header[2:4], uint16(len(payload)+8))
		_, err := conn.Write(append(header, payload...))
		return err
	}
	if err := readPacket(0x12); err != nil {
		return err
	}
	// PRELOGIN: encryption is not supported by this isolated fixture.
	if err := writePacket([]byte{1, 0, 6, 0, 1, 0xff, 2}); err != nil {
		return err
	}
	if err := readPacket(0x10); err != nil {
		return err
	}
	host := "routed.private.invalid"
	route := binary.LittleEndian.AppendUint16([]byte{0}, 1444)
	route = binary.LittleEndian.AppendUint16(route, uint16(len(host)))
	for _, character := range host {
		route = binary.LittleEndian.AppendUint16(route, uint16(character))
	}
	env := binary.LittleEndian.AppendUint16([]byte{20}, uint16(len(route)))
	env = append(env, route...)
	env = binary.LittleEndian.AppendUint16(env, 0)
	payload := binary.LittleEndian.AppendUint16([]byte{0xe3}, uint16(len(env)))
	payload = append(payload, env...)
	// LOGINACK (TDS 7.4), followed by DONE.
	payload = append(payload, 0xad, 10, 0, 1, 0x74, 0, 0, 4, 0, 0, 0, 0, 0, 0xfd)
	payload = append(payload, make([]byte, 12)...)
	return writePacket(payload)
}
