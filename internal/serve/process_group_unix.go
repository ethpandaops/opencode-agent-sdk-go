//go:build !windows

package serve

import (
	stderrors "errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// configureProcessGroup mirrors internal/subprocess: put the child in
// its own process group so we can deliver a group-wide SIGKILL.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessTree delivers SIGKILL to the serve subprocess's process
// group, falling back to a direct per-pid kill if pgid lookup fails.
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
			return fmt.Errorf("kill opencode serve process group (pgid %d): %w", pgid, killErr)
		}
	} else if !stderrors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("get opencode serve process group (pid %d): %w", pid, err)
	}

	if err := cmd.Process.Kill(); err != nil && !stderrors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("kill opencode serve process (pid %d): %w", pid, err)
	}

	return nil
}
