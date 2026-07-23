package proxy

import (
	"testing"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/srvconn"
	"github.com/jumpserver/koko/pkg/zmodem"
)

func TestTerminalAICommandGrantIsExactAndOneTime(t *testing.T) {
	server := &Server{}
	decision := CommandACLDecision{
		Action: model.ActionWarning, ACLID: "acl-1", ItemID: "item-1",
	}
	server.AuthorizeTerminalAICommand(" SELECT 1 ", &decision)
	if _, ok := server.consumeTerminalAICommandGrant("SELECT 2"); ok {
		t.Fatal("grant matched a different command")
	}
	value, ok := server.consumeTerminalAICommandGrant("SELECT 1")
	if !ok || value.ACLID != decision.ACLID {
		t.Fatal("exact command did not consume the grant")
	}
	if _, ok = server.consumeTerminalAICommandGrant("SELECT 1"); ok {
		t.Fatal("grant was consumed more than once")
	}
}

func TestTerminalAICommandGrantMatchesParsedMySQLInput(t *testing.T) {
	var parsed string
	parser := &Parser{
		protocolType: srvconn.ProtocolMySQL,
		zmodemParser: zmodem.New(),
		terminalAIGrant: func(command string) (CommandACLDecision, bool) {
			parsed = command
			return CommandACLDecision{Action: model.ActionAccept}, true
		},
	}
	if err := parser.initial(80, 24); err != nil {
		t.Fatal(err)
	}
	defer parser.Close()
	input := []byte("SELECT 1;\r")
	if output := parser.parseInputState(input); string(output) != string(input) {
		t.Fatalf("parsed input = %q", output)
	}
	if parsed != "SELECT 1;" {
		t.Fatalf("grant command = %q", parsed)
	}
}

func TestParserAppliesTerminalAIGrantAuditMetadata(t *testing.T) {
	parser := &Parser{cmdFilterACLs: model.CommandACLs{
		{
			ID: "acl-1", CommandGroups: []model.CommandFilterItem{{ID: "item-1"}},
		},
	}}
	parser.applyTerminalAIGrant(CommandACLDecision{
		Action: model.ActionAccept, ACLID: "acl-1", ItemID: "item-1",
		Reviewed: true,
	})
	rule := parser.getCurrentCmdFilterRule()
	if rule.Acl == nil || rule.Acl.ID != "acl-1" ||
		rule.Item == nil || rule.Item.ID != "item-1" {
		t.Fatal("grant metadata was not applied")
	}
	if parser.getCurrentCmdStatusLevel() != model.ReviewAccept {
		t.Fatal("reviewed grant did not preserve the audit risk level")
	}
}
