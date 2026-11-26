//go:build !windows

package runtime

import (
	"os/exec"
	"syscall"
)

// Cancel the whole Codex process group, including any child processes.
func isolateProcess(cmd *exec.Cmd)  { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func killProcessTree(cmd *exec.Cmd) { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
