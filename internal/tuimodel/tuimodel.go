// Package tuimodel reads the opencode TUI's persisted model selection
// from $XDG_STATE_HOME/opencode/model.json so the SDK can default new
// sessions to whatever model the user last picked interactively.
package tuimodel

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type file struct {
	Recent  []entry           `json:"recent"`
	Variant map[string]string `json:"variant"`
}

// Field tags mirror opencode's on-disk layout verbatim: opencode
// writes `providerID` and `modelID` (capital ID), not the strictly
// lowerCamel `providerId`/`modelId` some linters prefer.
type entry struct {
	ProviderID string `json:"providerID"` //nolint:tagliatelle // matches opencode on-disk schema
	ModelID    string `json:"modelID"`    //nolint:tagliatelle // matches opencode on-disk schema
}

// Resolve returns the model id the opencode TUI would open with, in the
// `providerID/modelID[/variant]` form opencode accepts via
// session/set_config_option. Returns "" when the file is absent,
// unreadable, malformed, or carries no recent entry.
//
// The path resolution honors XDG_STATE_HOME and falls back to
// ~/.local/state/opencode/model.json, matching opencode's own layout.
func Resolve() string {
	path := statePath()
	if path == "" {
		return ""
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	var f file

	if err := json.Unmarshal(raw, &f); err != nil {
		return ""
	}

	if len(f.Recent) == 0 {
		return ""
	}

	head := f.Recent[0]
	if head.ProviderID == "" || head.ModelID == "" {
		return ""
	}

	base := head.ProviderID + "/" + head.ModelID

	if v := f.Variant[base]; v != "" && v != "default" {
		return base + "/" + v
	}

	return base
}

func statePath() string {
	if s := os.Getenv("XDG_STATE_HOME"); s != "" {
		return filepath.Join(s, "opencode", "model.json")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	return filepath.Join(home, ".local", "state", "opencode", "model.json")
}
