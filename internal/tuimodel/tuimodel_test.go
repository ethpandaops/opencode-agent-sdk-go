package tuimodel

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolve(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name: "recent entry with explicit variant",
			payload: `{
				"recent": [{"providerID": "openrouter", "modelID": "anthropic/claude-opus-4.7"}],
				"variant": {"openrouter/anthropic/claude-opus-4.7": "medium"}
			}`,
			want: "openrouter/anthropic/claude-opus-4.7/medium",
		},
		{
			name: "default variant is dropped",
			payload: `{
				"recent": [{"providerID": "openai", "modelID": "gpt-5.3-codex-spark"}],
				"variant": {"openai/gpt-5.3-codex-spark": "default"}
			}`,
			want: "openai/gpt-5.3-codex-spark",
		},
		{
			name:    "no variant entry",
			payload: `{"recent": [{"providerID": "opencode", "modelID": "big-pickle"}]}`,
			want:    "opencode/big-pickle",
		},
		{
			name:    "empty recent",
			payload: `{"recent": []}`,
			want:    "",
		},
		{
			name:    "missing provider",
			payload: `{"recent": [{"modelID": "x"}]}`,
			want:    "",
		},
		{
			name:    "malformed json",
			payload: `not json`,
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			subdir := filepath.Join(dir, "opencode")
			if err := os.MkdirAll(subdir, 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}

			if err := os.WriteFile(filepath.Join(subdir, "model.json"), []byte(tt.payload), 0o600); err != nil {
				t.Fatalf("write: %v", err)
			}

			t.Setenv("XDG_STATE_HOME", dir)

			got := Resolve()
			if got != tt.want {
				t.Errorf("Resolve() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolve_FileMissing(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	if got := Resolve(); got != "" {
		t.Errorf("Resolve() = %q, want empty string when file missing", got)
	}
}
