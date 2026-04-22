package opencodesdk

import "testing"

func TestHealthTracker_ZeroValue(t *testing.T) {
	var h healthTracker

	snap := h.get()
	if snap.Degraded || snap.ConsecutiveFailures != 0 || snap.LastError != nil || snap.LastFailureAt != nil {
		t.Fatalf("unexpected zero snapshot: %+v", snap)
	}
}

func TestHealthTracker_RecordFailure(t *testing.T) {
	var h healthTracker

	h.recordFailure("send", "boom")
	h.recordFailure("read", "pipe closed")

	snap := h.get()
	if snap.SendFailures != 1 {
		t.Fatalf("SendFailures: want 1, got %d", snap.SendFailures)
	}

	if snap.ReadFailures != 1 {
		t.Fatalf("ReadFailures: want 1, got %d", snap.ReadFailures)
	}

	if snap.ConsecutiveFailures != 2 {
		t.Fatalf("ConsecutiveFailures: want 2, got %d", snap.ConsecutiveFailures)
	}

	if snap.LastError == nil || *snap.LastError != "pipe closed" {
		t.Fatalf("LastError: unexpected %+v", snap.LastError)
	}

	if snap.LastFailureAt == nil {
		t.Fatal("LastFailureAt: expected non-nil")
	}
}

func TestHealthTracker_MarkDegraded(t *testing.T) {
	var h healthTracker

	h.markDegraded()

	if !h.get().Degraded {
		t.Fatal("expected Degraded=true")
	}
}

func TestHealthTracker_GetReturnsCopy(t *testing.T) {
	var h healthTracker

	h.recordFailure("send", "boom")

	snap1 := h.get()

	h.recordFailure("read", "again")

	if snap1.SendFailures != 1 || snap1.ReadFailures != 0 {
		t.Fatalf("expected snapshot to be detached; got %+v", snap1)
	}
}
