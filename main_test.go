package opencodesdk

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain isolates XDG_STATE_HOME so tests never observe the
// developer's opencode TUI model preference (~/.local/state/opencode/
// model.json). Without this, mergeOptions would auto-apply a model
// and inflate the set_config_option request counts that several unit
// tests assert on.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "opencodesdk-xdg-state-*")
	if err != nil {
		panic(err)
	}

	defer os.RemoveAll(dir)

	_ = os.Setenv("XDG_STATE_HOME", filepath.Join(dir, "empty"))

	os.Exit(m.Run())
}
