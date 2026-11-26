package runtime

import (
	"os/exec"
	"strconv"
)

func isolateProcess(cmd *exec.Cmd) {}
func killProcessTree(cmd *exec.Cmd) {
	_ = exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F").Run()
	_ = cmd.Process.Kill()
}
