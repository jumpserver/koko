package proxy

import (
	"testing"

	"github.com/jumpserver-dev/sdk-go/model"
)

func TestAgentToolPTYGrantIsExactAndSingleUse(t *testing.T) {
	server := &Server{}
	decision := &CommandACLDecision{
		Action: model.ActionAccept, ACLID: "acl-1", ItemID: "item-1",
	}
	server.AuthorizeAgentToolCommand("id", decision)
	if _, ok := server.consumeAgentToolCommandGrant("whoami"); ok {
		t.Fatal("grant accepted a different command")
	}
	got, ok := server.consumeAgentToolCommandGrant("id")
	if !ok || got.ACLID != decision.ACLID {
		t.Fatal("exact PTY grant was not consumed")
	}
	if _, ok = server.consumeAgentToolCommandGrant("id"); ok {
		t.Fatal("PTY grant was reusable")
	}
	server.AuthorizeAgentToolCommand("id", decision)
	server.RevokeAgentToolCommand("id")
	if _, ok = server.consumeAgentToolCommandGrant("id"); ok {
		t.Fatal("revoked PTY grant was accepted")
	}
}
