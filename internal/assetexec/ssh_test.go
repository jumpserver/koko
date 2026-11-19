package assetexec

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestPrepareCommandPreservesLiteralContext(t *testing.T) {
	cwd := t.TempDir()
	request := Request{Command: `printf '%s\n%s' "$PWD" "$AGENT_VALUE"`, Cwd: cwd, Env: map[string]string{"AGENT_VALUE": "a'$(exit 41); b"}}
	command, err := PrepareCommand(request)
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("/bin/sh", "-c", command).Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != cwd+"\n"+request.Env["AGENT_VALUE"] {
		t.Fatalf("context was interpreted as shell syntax: %q", output)
	}
	request.Env = map[string]string{"BAD;NAME": "value"}
	if _, err = PrepareCommand(request); err == nil {
		t.Fatal("invalid environment name accepted")
	}
}

func TestOutputStopsWhenEvidenceCannotBeAccepted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var recorded strings.Builder
	sink := &outputSink{remaining: 3, cancel: cancel, emit: func(_ string, p []byte) error { _, err := recorded.Write(p); return err }}
	if n, err := sink.write("stdout", []byte("abcd")); n != 3 || !errors.Is(err, ErrOutputLimit) {
		t.Fatalf("unexpected limit result: %d %v", n, err)
	}
	if recorded.String() != "abc" || ctx.Err() == nil {
		t.Fatal("overflow was not bounded and cancelled")
	}
	if _, err := sink.write("stderr", []byte("more")); !errors.Is(err, ErrOutputLimit) {
		t.Fatal("output resumed after evidence limit")
	}
}
