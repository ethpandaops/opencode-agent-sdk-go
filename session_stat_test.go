package opencodesdk

import (
	"errors"
	"testing"
)

func TestCallExtensionRejectsNonExtensionMethods(t *testing.T) {
	c := newTestClient()
	// Mark started so we exercise the prefix validation path rather than
	// the "not started" guard.
	c.started = true

	_, err := c.CallExtension(t.Context(), "session/prompt", nil)
	if !errors.Is(err, ErrExtensionMethodRequired) {
		t.Fatalf("expected ErrExtensionMethodRequired, got %v", err)
	}
}

func TestCallExtensionGuardsNotStarted(t *testing.T) {
	c := newTestClient()

	_, err := c.CallExtension(t.Context(), "_ext", nil)
	if !errors.Is(err, ErrClientNotStarted) {
		t.Fatalf("expected ErrClientNotStarted, got %v", err)
	}
}
