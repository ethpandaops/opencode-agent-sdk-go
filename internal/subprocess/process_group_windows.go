//go:build windows

package subprocess

import (
	stderrors "errors"
	"fmt"
	"os"
	"os/exec"
)

// configureProcessGroup is a no-op on Windows; the Job-object-based
// equivalent is out of scope for this SDK.
func configureProcessGroup(_ *exec.Cmd) {}

// killProcessTree is the Windows fallback: kill the top-level process
// directly. Any children opencode spawned will be reparented to the
// Windows init process, which is a known gap vs. unix process groups.
func killProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	if err := cmd.Process.Kill(); err != nil && !stderrors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("kill opencode process (pid %d): %w", cmd.Process.Pid, err)
	}

	return nil
}
