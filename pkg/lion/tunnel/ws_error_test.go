package tunnel

import (
	"fmt"
	"io"
	"testing"

	"github.com/jumpserver/koko/pkg/lion/guacd"
)

func TestGuacdErrorInstruction(t *testing.T) {
	for _, tc := range []struct {
		message string
		code    string
	}{
		{"Security negotiation failed (wrong security type?)", "769"},
		{"Authentication failure (invalid credentials?)", "769"},
		{"Server refused connection (wrong security type?)", "519"},
		{"Upstream error.", "515"},
	} {
		t.Run(tc.message, func(t *testing.T) {
			want := guacd.NewInstruction(guacd.InstructionServerError, tc.message, tc.code)
			err := fmt.Errorf("connect: %w", &guacd.ProtocolError{Instruction: want})
			got := guacdErrorInstruction(err)
			if got.String() != want.String() {
				t.Fatalf("error instruction = %q, want %q", got.String(), want.String())
			}
		})
	}
}

func TestGuacdErrorInstructionFallback(t *testing.T) {
	got := guacdErrorInstruction(io.EOF)
	if got.String() != ErrGuacamoleServer.String() {
		t.Fatalf("error instruction = %q, want guacamole service error", got.String())
	}
}
