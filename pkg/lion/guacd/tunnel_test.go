package guacd

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func TestNewTunnelPreservesGuacdError(t *testing.T) {
	for _, stage := range []string{"args", "ready"} {
		t.Run(stage, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			want := NewInstruction(InstructionServerError,
				"Security negotiation failed (wrong security type?)", "769")
			done := make(chan error, 1)
			go func() {
				conn, serveErr := listener.Accept()
				if serveErr != nil {
					done <- serveErr
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
				decoder := NewInstructionDecoder(conn)
				_, serveErr = decoder.ReadInstruction() // select
				if serveErr == nil && stage == "ready" {
					args := NewInstruction("args")
					_, serveErr = io.WriteString(conn, args.String())
					for serveErr == nil {
						var instruction Instruction
						instruction, serveErr = decoder.ReadInstruction()
						if instruction.Opcode == "connect" {
							break
						}
					}
				}
				if serveErr == nil {
					_, serveErr = io.WriteString(conn, want.String())
				}
				done <- serveErr
			}()

			_, err = NewTunnel(listener.Addr().String(), Configuration{Protocol: "rdp"}, ClientInformation{})
			if serveErr := <-done; serveErr != nil {
				t.Fatal(serveErr)
			}
			var protocolErr *ProtocolError
			if !errors.As(err, &protocolErr) {
				t.Fatalf("NewTunnel error = %v, want ProtocolError", err)
			}
			if got := protocolErr.Instruction.String(); got != want.String() {
				t.Fatalf("error instruction = %q, want %q", got, want.String())
			}
		})
	}
}
