//go:build windows

package tsexec

import (
	"os/exec"
	"strconv"
)

// configureProcessTree is a no-op on Windows; taskkill terminates descendants.
func configureProcessTree(cmd *exec.Cmd) {}

// killProcessTree terminates cmd and all of its child processes. `npx` spawns a
// child `node`; killing only the shim would leave that child running and holding
// the stdout/stderr pipes, which makes cmd.Wait() block indefinitely.
func killProcessTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
	_ = cmd.Process.Kill()
}
