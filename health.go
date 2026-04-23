package opencodesdk

import (
	"sync"
	"time"
)

// TransportHealth is a point-in-time snapshot of the transport-layer
// health observed by the Client. It is cheap to fetch and safe for
// diagnostic dashboards or liveness probes.
//
// Parity with the claude/codex sister SDKs. Unlike those, opencode's
// transport is an ACP JSON-RPC subprocess — failures are counted for
// subprocess crashes and ACP read/send errors surfaced through
// coder/acp-go-sdk.
type TransportHealth struct {
	// Degraded is true once the subprocess has exited unexpectedly or
	// the Client has been Closed. It does not un-set itself.
	Degraded bool
	// ConsecutiveFailures is the number of transport errors observed
	// since the last success. opencode's SDK currently increments this
	// only on subprocess crash, which also flips Degraded.
	ConsecutiveFailures int
	// SendFailures counts outbound RPC errors classified as transport
	// failures (connection closed, broken pipe, subprocess dead). JSON-
	// RPC application errors do NOT count here.
	SendFailures int
	// ReadFailures counts inbound-stream read failures.
	ReadFailures int
	// LastError is the most recent transport-layer error observed, or
	// nil if none. Points at a stable string owned by the health
	// tracker.
	LastError *string
	// LastFailureAt is the timestamp of the most recent failure, or
	// nil if none has been observed.
	LastFailureAt *time.Time
}

// healthTracker is the mutable backing store for TransportHealth
// snapshots. A zero value is usable; all mutations take the mutex.
type healthTracker struct {
	mu       sync.Mutex
	snapshot TransportHealth
}

// recordFailure bumps the transport counters and captures the most
// recent error for diagnostic reporting. kind is one of "send",
// "read", or "subprocess".
func (h *healthTracker) recordFailure(kind, message string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	switch kind {
	case "send":
		h.snapshot.SendFailures++
	case "read":
		h.snapshot.ReadFailures++
	}

	h.snapshot.ConsecutiveFailures++
	now := time.Now()
	h.snapshot.LastFailureAt = &now

	if message != "" {
		msg := message
		h.snapshot.LastError = &msg
	}
}

// markDegraded forces Degraded=true. Irreversible — matches the
// claude/codex semantics where a dead transport cannot recover.
func (h *healthTracker) markDegraded() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.snapshot.Degraded = true
}

// get returns a deep copy of the current snapshot safe for caller
// retention.
func (h *healthTracker) get() TransportHealth {
	h.mu.Lock()
	defer h.mu.Unlock()

	out := h.snapshot

	if h.snapshot.LastError != nil {
		msg := *h.snapshot.LastError
		out.LastError = &msg
	}

	if h.snapshot.LastFailureAt != nil {
		ts := *h.snapshot.LastFailureAt
		out.LastFailureAt = &ts
	}

	return out
}
