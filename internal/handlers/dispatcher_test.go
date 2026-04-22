package handlers

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAssertWithinCwdAcceptsInsidePaths(t *testing.T) {
	cwd := t.TempDir()
	nested := filepath.Join(cwd, "sub", "file.txt")

	if err := assertWithinCwd(nested, cwd); err != nil {
		t.Fatalf("expected nested path to be within cwd, got err: %v", err)
	}
}

func TestAssertWithinCwdRejectsEscape(t *testing.T) {
	cwd := t.TempDir()
	parent := filepath.Dir(cwd)
	outside := filepath.Join(parent, "elsewhere.txt")

	if err := assertWithinCwd(outside, cwd); err == nil {
		t.Fatalf("expected escape to be rejected; cwd=%q target=%q", cwd, outside)
	}
}

func TestAssertWithinCwdRejectsEmptyCwd(t *testing.T) {
	err := assertWithinCwd("/tmp/f.txt", "")
	if err == nil {
		t.Fatalf("expected rejection when cwd is empty")
	}

	if !strings.Contains(err.Error(), "no cwd configured") {
		t.Fatalf("expected specific error message, got %v", err)
	}
}

func TestAssertWithinCwdRejectsRelative(t *testing.T) {
	if err := assertWithinCwd("rel/path", "/tmp"); err == nil {
		t.Fatalf("expected rejection of relative path")
	}
}
