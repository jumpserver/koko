package handler

import (
	"bytes"
	"errors"
	"io"
	"strings"

	"github.com/jumpserver-dev/usql/internal/dbdiagnostic"
	"github.com/jumpserver-dev/usql/text"
	"github.com/xo/usql/rline"
)

// MaskDiagnostics applies to complete error writes inside usql, before the
// terminal combines them with command echo, prompts and query results.
func (h *Handler) MaskDiagnostics() {
	h.maskDiagnostics = true
	h.l = maskedIO{h.l}
}

func (h *Handler) DiagnosticError(err error) error {
	if err == nil || !h.maskDiagnostics {
		return err
	}
	return errors.New(diagnosticMessage(err.Error()))
}

func diagnosticMessage(raw string) string {
	switch raw {
	case text.ErrNotConnected.Error(), text.ErrNoPreviousTransactionExists.Error(),
		text.ErrPreviousTransactionExists.Error(), text.PasswordsDoNotMatch:
		return raw
	}
	return dbdiagnostic.Message(raw)
}

type maskedIO struct{ rline.IO }

func (m maskedIO) Stderr() io.Writer { return diagnosticWriter{m.IO.Stderr()} }

type diagnosticWriter struct{ out io.Writer }

func (w diagnosticWriter) Write(p []byte) (int, error) {
	// fmt emits the prefix, message (including embedded newlines), and final
	// newline in one write. Some meta-commands write the newline separately.
	body, newline := bytes.CutSuffix(p, []byte{'\n'})
	var output string
	switch raw := string(body); raw {
	case "":
	case text.PasswordsDoNotMatch:
		output = text.PasswordsDoNotMatch
	default:
		message := dbdiagnostic.Hidden
		if diagnostic, ok := strings.CutPrefix(raw, "error: "); ok {
			message = diagnosticMessage(diagnostic)
		}
		output = "error: " + message
	}
	if newline {
		output += "\n"
	}
	if _, err := io.WriteString(w.out, output); err != nil {
		return 0, err
	}
	return len(p), nil
}
