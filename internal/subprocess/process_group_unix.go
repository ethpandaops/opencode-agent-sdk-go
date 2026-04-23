//go:build !windows

package subprocess

import (
	stderrors "errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// configureProcessGroup sets the subprocess up to live in its own
// process group so we can deliver a group-wide signal on shutdown.
// Without this, children opencode spawns (stdio MCP servers, the
// internal HTTP bridge's listeners, etc.) can outlive opencode itself.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}

// killProcessTree delivers SIGKILL to the subprocess's process group,
// tearing down any grandchildren along with opencode. Falls back to a
// direct per-pid kill if the pgid lookup fails.
func killProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	pid := cmd.Process.Pid

	pgid, err := syscall.Getpgid(pid)
	if err == nil {
		if killErr := syscall.Kill(-pgid, syscall.SIGKILL); killErr == nil {
			return nil
		} else if !stderrors.Is(killErr, syscall.ESRCH) {
			return fmt.Errorf("kill opencode process group (pgid %d): %w", pgid, killErr)
		}
	} else if !stderrors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("get opencode process group (pid %d): %w", pid, err)
	}

	if err := cmd.Process.Kill(); err != nil && !stderrors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("kill opencode process (pid %d): %w", pid, err)
	}

	return nil
}
