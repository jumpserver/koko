package proxy

import (
	"testing"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/zmodem"
)

func TestParserWarningConfirmation(t *testing.T) {
	for _, tt := range []struct {
		name   string
		typed  string
		data   string
		answer string
	}{
		{"cr", "read answer", "\r", "y"},
		{"lf", "read answer", "\n", "y"},
		{"crlf", "read answer", "\r\n", "y"},
		{"command_tail", "read ", "answer\r", "y"},
		{"uppercase", "read answer", "\r", "Y"},
		{"reject", "read answer", "\r", "n"},
		{"uppercase_reject", "read answer", "\r", "N"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := &Parser{
				userOutputChan: make(chan []byte, 1),
				srvOutputChan:  make(chan []byte, 2),
				zmodemParser:   zmodem.New(),
				platform:       &model.Platform{Name: "Linux"},
				cmdFilterACLs: model.CommandACLs{{
					Action:        model.ActionNotifyAndWarn,
					CommandGroups: []model.CommandFilterItem{{RePattern: "^read answer$"}},
				}},
			}
			if err := p.initial(80, 24); err != nil {
				t.Fatal(err)
			}
			defer p.TerminalParser.Close()
			p.TerminalParser.Feed([]byte("$ "))
			p.ParseUserInput([]byte(tt.typed))
			p.TerminalParser.Feed([]byte(tt.typed))
			if got := p.ParseUserInput([]byte(tt.data)); len(got) != 0 || !p.confirmStatus.InQuery() {
				t.Fatalf("command was not held for confirmation: %q", got)
			}
			if len(p.userOutputChan) != 0 {
				t.Fatal("command was forwarded before confirmation")
			}
			wantReturn, wantRemote := "", tt.data
			if tt.answer == "n" || tt.answer == "N" {
				wantReturn, wantRemote = "\x05\x15\r", ""
			}
			// A single key must complete confirmation without another Enter.
			if got := p.ParseUserInput([]byte(tt.answer)); string(got) != wantReturn {
				t.Fatalf("returned input = %q, want %q", got, wantReturn)
			}
			select {
			case got := <-p.userOutputChan:
				if string(got) != wantRemote {
					t.Fatalf("remote input = %q, want %q", got, wantRemote)
				}
			default:
				if wantRemote != "" {
					t.Fatalf("no remote input, want %q", wantRemote)
				}
			}
			if p.confirmStatus.InQuery() {
				t.Fatal("confirmation is still pending")
			}
			if wantRemote == "" && p.command != "" {
				t.Fatal("rejected command was not cleared")
			}
		})
	}
}
