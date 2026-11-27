package sqlanalysis

import "fmt"

type Code string

const (
	SyntaxError          Code = "syntax_error"
	Unsupported          Code = "unsupported"
	ProductUnknown       Code = "product_unknown"
	MetadataInsufficient Code = "metadata_insufficient"
)

type Diagnostic struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
	Span    Span   `json:"span"`
}

// Error preserves a typed diagnostic and the original parser error. A fatal
// error invalidates the entire document; consumers must not execute a prefix or
// silently retry with a scanner. Nonfatal unknowns live in Diagnostics.
type Error struct {
	Diagnostic
	Cause error `json:"-"`
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }
func (e *Error) Unwrap() error { return e.Cause }
