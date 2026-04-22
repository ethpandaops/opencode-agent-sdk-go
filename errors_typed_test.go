package opencodesdk

import (
	"errors"
	"testing"
)

func TestCLINotFoundError_IsMatchesSentinel(t *testing.T) {
	err := &CLINotFoundError{SearchedPaths: []string{"/nope"}}
	if !errors.Is(err, ErrCLINotFound) {
		t.Fatal("expected errors.Is(err, ErrCLINotFound)")
	}
}

func TestCLINotFoundError_ErrorMessage(t *testing.T) {
	err := &CLINotFoundError{SearchedPaths: []string{"/a", "/b"}}
	if err.Error() == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestCLINotFoundError_UnwrapsCause(t *testing.T) {
	cause := errors.New("lookpath failed")
	err := &CLINotFoundError{Err: cause}

	if !errors.Is(err, cause) {
		t.Fatal("expected errors.Is(err, cause)")
	}
}

func TestProcessError_ErrorMessage(t *testing.T) {
	err := &ProcessError{ExitCode: 137, Stderr: "killed"}
	if err.Error() == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestProcessError_UnwrapsCause(t *testing.T) {
	cause := errors.New("exec: exit status 1")
	err := &ProcessError{ExitCode: 1, Err: cause}

	if !errors.Is(err, cause) {
		t.Fatal("expected errors.Is(err, cause)")
	}
}
