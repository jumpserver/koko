package srvconn

import (
	"slices"
	"testing"
)

func TestMySQLEnvsIncludesTerminalType(t *testing.T) {
	t.Setenv("TERM", "")
	opt := &sqlOption{}
	if !slices.Contains(opt.Envs(), "TERM=xterm") {
		t.Fatal("MySQL environment must include TERM=xterm for interactive line editing")
	}
}
