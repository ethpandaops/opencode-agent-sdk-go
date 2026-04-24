//go:build windows

package serve

import (
	stderrors "errors"
	"fmt"
	"os"
	"os/exec"
)

// configureProcessGroup is a no-op on Windows; we rely on the direct
// Kill path instead. Matches internal/subprocess behaviour.
func configureProcessGroup(_ *exec.Cmd) {}

// killProcessTree kills the top-level serve process directly. Any
// children opencode spawned reparent to the Windows init — the same
// caveat internal/subprocess documents.
func killProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	if err := cmd.Process.Kill(); err != nil && !stderrors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("kill opencode serve process (pid %d): %w", cmd.Process.Pid, err)
	}

	return nil
}
