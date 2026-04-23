// Package cli discovers the opencode binary and validates its version.
package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
)

// Discoverer resolves the path to the opencode CLI binary.
type Discoverer struct {
	// Path pins a specific binary path. If set and non-empty, no $PATH
	// lookup is performed.
	Path string
	// SkipVersionCheck disables the MinimumVersion assertion.
	SkipVersionCheck bool
	// MinimumVersion is the lowest acceptable opencode version. Empty
	// disables the check.
	MinimumVersion string
	// Logger receives diagnostics. Must not be nil (caller is expected
	// to supply io.Discard-backed logger when silent).
	Logger *slog.Logger
}

// ErrNotFound is returned when the opencode binary cannot be located.
var ErrNotFound = errors.New("opencode binary not found")

// ErrUnsupportedVersion is returned when the discovered binary is older
// than MinimumVersion.
var ErrUnsupportedVersion = errors.New("opencode binary is older than minimum supported version")

// Discover resolves the opencode binary path and, unless
// SkipVersionCheck is set, validates its reported version against
// MinimumVersion.
func (d *Discoverer) Discover(ctx context.Context) (path, version string, err error) {
	path, err = d.resolvePath()
	if err != nil {
		return "", "", err
	}

	version, err = probeVersion(ctx, path)
	if err != nil {
		return "", "", fmt.Errorf("probing opencode version: %w", err)
	}

	d.Logger.DebugContext(ctx, "discovered opencode", slog.String("path", path), slog.String("version", version))

	if d.SkipVersionCheck || d.MinimumVersion == "" {
		return path, version, nil
	}

	ok, cmpErr := atLeast(version, d.MinimumVersion)
	if cmpErr != nil {
		return "", "", fmt.Errorf("comparing opencode version %q against minimum %q: %w", version, d.MinimumVersion, cmpErr)
	}

	if !ok {
		return "", "", fmt.Errorf("%w: found %s, require >= %s", ErrUnsupportedVersion, version, d.MinimumVersion)
	}

	return path, version, nil
}

func (d *Discoverer) resolvePath() (string, error) {
	if d.Path != "" {
		if _, err := exec.LookPath(d.Path); err != nil {
			return "", fmt.Errorf("%w: %s: %v", ErrNotFound, d.Path, err)
		}

		return d.Path, nil
	}

	p, err := exec.LookPath("opencode")
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrNotFound, err)
	}

	return p, nil
}

// probeVersion runs `<path> --version` and extracts the first line as
// the version string. opencode prints a single line like `1.14.20`.
func probeVersion(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, path, "--version")
	out := &bytes.Buffer{}
	cmd.Stdout = out
	cmd.Stderr = out

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s --version: %w (output: %q)", path, err, out.String())
	}

	first := strings.TrimSpace(strings.SplitN(out.String(), "\n", 2)[0])
	if first == "" {
		return "", fmt.Errorf("%s --version: empty output", path)
	}

	return first, nil
}

// atLeast reports whether got >= floor using semver-style
// major.minor.patch comparison. Non-numeric suffixes are stripped.
func atLeast(got, floor string) (bool, error) {
	g, err := parseVersion(got)
	if err != nil {
		return false, fmt.Errorf("parse got: %w", err)
	}

	f, err := parseVersion(floor)
	if err != nil {
		return false, fmt.Errorf("parse floor: %w", err)
	}

	for i := range 3 {
		if g[i] != f[i] {
			return g[i] > f[i], nil
		}
	}

	return true, nil
}

func parseVersion(v string) ([3]int, error) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	// Drop anything after a '-' (pre-release) or '+' (build metadata)
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}

	parts := strings.SplitN(v, ".", 4)

	var out [3]int

	for i := range 3 {
		if i >= len(parts) {
			break
		}

		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return out, fmt.Errorf("component %d of %q: %w", i, v, err)
		}

		out[i] = n
	}

	return out, nil
}
