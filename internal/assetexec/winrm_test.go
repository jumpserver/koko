package assetexec

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/jumpserver/koko/pkg/srvconn"
)

type winRMCommandFunc func(context.Context, string, io.Writer, io.Writer) (srvconn.WinRMResult, error)

func (f winRMCommandFunc) ExecuteCommand(ctx context.Context, command string, out, errs io.Writer) (srvconn.WinRMResult, error) {
	return f(ctx, command, out, errs)
}
func (winRMCommandFunc) Close() error { return nil }

func TestWinRMExecutionEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, state string
		err         error
		completed   bool
	}{
		{"completed", "Completed", nil, true},
		{"remote_failure", "Failed", errors.New("PowerShell error"), true},
		{"lost_response", "", io.ErrUnexpectedEOF, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := &Connection{protocol: "winrm", opts: Options{MaxOutputBytes: 1024},
				winRM: winRMCommandFunc(func(_ context.Context, command string, out, errs io.Writer) (srvconn.WinRMResult, error) {
					if command != "Get-Location" {
						t.Fatal("PowerShell command was wrapped as a POSIX shell command")
					}
					_, _ = io.WriteString(out, "result")
					_, _ = io.WriteString(errs, "diagnostic")
					return srvconn.WinRMResult{Dispatched: true, Completed: tc.completed, PipelineState: tc.state}, tc.err
				})}
			streams := make(map[string]string)
			result, err := conn.Execute(context.Background(), Request{Command: "Get-Location"}, func(stream string, p []byte) error {
				streams[stream] += string(p)
				return nil
			})
			defer conn.Close()
			if !errors.Is(err, tc.err) || !result.Dispatched || result.Completed != tc.completed || result.ContextLost == tc.completed || result.ExitCode != nil {
				t.Fatalf("incorrect PowerShell evidence: %+v %v", result, err)
			}
			if streams["stdout"] != "result" || streams["stderr"] != "diagnostic" {
				t.Fatal("PowerShell output streams were merged")
			}
		})
	}
}

func TestWinRMRejectsSSHContextAndBoundsOutput(t *testing.T) {
	called := false
	conn := &Connection{protocol: "winrm", opts: Options{MaxOutputBytes: 4},
		winRM: winRMCommandFunc(func(_ context.Context, _ string, out, _ io.Writer) (srvconn.WinRMResult, error) {
			called = true
			_, err := io.WriteString(out, "oversized")
			return srvconn.WinRMResult{Dispatched: true}, err
		})}
	for _, request := range []Request{{Command: "Get-Location", Cwd: "/tmp"}, {Command: "Get-Location", Env: map[string]string{"PATH": "/tmp"}}} {
		if result, err := conn.Execute(context.Background(), request, func(string, []byte) error { return nil }); err == nil || called || result.Dispatched {
			t.Fatal("SSH context was accepted by WinRM")
		}
	}
	var output strings.Builder
	result, err := conn.Execute(context.Background(), Request{Command: "Get-Location"}, func(_ string, p []byte) error { _, _ = output.Write(p); return nil })
	if !errors.Is(err, ErrOutputLimit) || !result.Truncated || !result.ContextLost || result.Completed || output.Len() != 4 {
		t.Fatalf("PowerShell exceeded its audit budget: %+v %v", result, err)
	}
}
