package opencodesdk

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/coder/acp-go-sdk"
)

// killablePipeTransport wraps pipeTransport with a controllable Exited
// channel so tests can simulate a subprocess death. Implements
// WatchableTransport so watchSubprocess picks up the failure.
type killablePipeTransport struct {
	*pipeTransport
	exited  chan struct{}
	exitErr error
}

func (k *killablePipeTransport) Exited() <-chan struct{} { return k.exited }
func (k *killablePipeTransport) ExitErr() error          { return k.exitErr }

func (k *killablePipeTransport) kill(err error) {
	k.exitErr = err
	close(k.exited)
	_ = k.Close()
}

func TestTransportDeath_SurfacesTransportErrorFromNewSession(t *testing.T) {
	var killable *killablePipeTransport

	factory := func(_ context.Context, handler acp.Client) (Transport, error) {
		killable = &killablePipeTransport{
			pipeTransport: newPipeTransport(handler, &fakeAgent{}),
			exited:        make(chan struct{}),
		}

		return killable, nil
	}

	c, err := NewClient(WithTransport(factory), WithSkipVersionCheck(true), WithCwd("/tmp"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	killable.kill(io.ErrUnexpectedEOF)

	// Allow watchSubprocess to run and stash the TransportError.
	deadline := time.Now().Add(2 * time.Second)

	var observed error
	for time.Now().Before(deadline) {
		_, observed = c.NewSession(ctx)

		if observed != nil && errors.Is(observed, ErrTransport) {
			break
		}

		time.Sleep(10 * time.Millisecond)
	}

	if !errors.Is(observed, ErrTransport) {
		t.Fatalf("expected errors.Is(err, ErrTransport); got %v", observed)
	}

	var te *TransportError
	if !errors.As(observed, &te) {
		t.Fatalf("expected *TransportError via errors.As; got %v", observed)
	}

	if te.Reason != "subprocess" {
		t.Errorf("TransportError.Reason = %q, want %q", te.Reason, "subprocess")
	}

	if !errors.Is(te, io.ErrUnexpectedEOF) {
		t.Errorf("TransportError should chain to underlying cause; got %v", te.Err)
	}
}
