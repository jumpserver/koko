package proxy

import (
	"testing"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/zmodem"
)

func TestParserWarningConfirmation(t *testing.T) {
	for _, tt := range []struct {
		input      string
		wantReturn string
		wantRemote string
	}{
		{input: "y", wantRemote: "\r"},
		{input: "Y", wantRemote: "\r"},
		{input: "n", wantReturn: "\x05\x15\r"},
		{input: "N", wantReturn: "\x05\x15\r"},
	} {
		t.Run(tt.input, func(t *testing.T) {
			p := &Parser{
				userOutputChan:      make(chan []byte, 1),
				srvOutputChan:       make(chan []byte, 1),
				TerminalParser:      &TerminalParser{},
				zmodemParser:        zmodem.New(),
				confirmStatus:       commandConfirmStatus{Status: StatusQuery},
				currentCmdRiskLevel: model.WarningLevel,
				command:             "read answer",
			}
			// A single key must complete confirmation without another Enter.
			if got := p.ParseUserInput([]byte(tt.input)); string(got) != tt.wantReturn {
				t.Fatalf("returned input = %q, want %q", got, tt.wantReturn)
			}
			select {
			case got := <-p.userOutputChan:
				if string(got) != tt.wantRemote {
					t.Fatalf("remote input = %q, want %q", got, tt.wantRemote)
				}
			default:
				if tt.wantRemote != "" {
					t.Fatalf("no remote input, want %q", tt.wantRemote)
				}
			}
			if p.confirmStatus.InQuery() {
				t.Fatal("confirmation is still pending")
			}
			if tt.wantRemote == "" && p.command != "" {
				t.Fatal("rejected command was not cleared")
			}
		})
	}
}
