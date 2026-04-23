package opencodesdk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/coder/acp-go-sdk"
)

func TestCostTracker_ObserveUsage(t *testing.T) {
	tracker := NewCostTracker()
	cb := tracker.ObserveUsage("ses_1")

	cb(context.Background(), &acp.SessionUsageUpdate{
		Cost: &acp.Cost{Amount: 0.12, Currency: "USD"},
		Size: 100000,
		Used: 1234,
	})

	snap, ok := tracker.SessionSnapshot("ses_1")
	if !ok {
		t.Fatal("expected session snapshot")
	}

	if snap.TotalCostUSD != 0.12 {
		t.Fatalf("TotalCostUSD: want 0.12, got %v", snap.TotalCostUSD)
	}

	if snap.ContextWindowSize != 100000 {
		t.Fatalf("ContextWindowSize: want 100000, got %v", snap.ContextWindowSize)
	}

	if snap.ContextWindowUsed != 1234 {
		t.Fatalf("ContextWindowUsed: want 1234, got %v", snap.ContextWindowUsed)
	}
}

func TestCostTracker_ObserveUsage_Overwrites(t *testing.T) {
	tracker := NewCostTracker()
	cb := tracker.ObserveUsage("ses_1")

	cb(context.Background(), &acp.SessionUsageUpdate{
		Cost: &acp.Cost{Amount: 0.10, Currency: "USD"},
	})
	cb(context.Background(), &acp.SessionUsageUpdate{
		Cost: &acp.Cost{Amount: 0.25, Currency: "USD"},
	})

	snap, _ := tracker.SessionSnapshot("ses_1")
	// opencode cost is cumulative, so latest wins — not 0.10 + 0.25 = 0.35.
	if snap.TotalCostUSD != 0.25 {
		t.Fatalf("TotalCostUSD: want 0.25 (latest cumulative), got %v", snap.TotalCostUSD)
	}
}

func TestCostTracker_ObservePromptResult(t *testing.T) {
	tracker := NewCostTracker()

	cachedRead := 32
	tracker.ObservePromptResult("ses_1", &PromptResult{
		Usage: &acp.Usage{
			InputTokens:      100,
			OutputTokens:     50,
			CachedReadTokens: &cachedRead,
			TotalTokens:      182,
		},
	})

	snap, _ := tracker.SessionSnapshot("ses_1")
	if snap.InputTokens != 100 || snap.OutputTokens != 50 || snap.CachedReadTokens != 32 {
		t.Fatalf("unexpected token snapshot: %+v", snap)
	}
}

func TestCostTracker_Snapshot_AggregatesAcrossSessions(t *testing.T) {
	tracker := NewCostTracker()

	cb1 := tracker.ObserveUsage("ses_1")
	cb2 := tracker.ObserveUsage("ses_2")

	cb1(context.Background(), &acp.SessionUsageUpdate{Cost: &acp.Cost{Amount: 0.10, Currency: "USD"}, Size: 200_000, Used: 5_000})
	cb2(context.Background(), &acp.SessionUsageUpdate{Cost: &acp.Cost{Amount: 0.30, Currency: "USD"}, Size: 100_000, Used: 7_000})

	snap := tracker.Snapshot()

	if snap.Sessions != 2 {
		t.Fatalf("Sessions: want 2, got %d", snap.Sessions)
	}

	if snap.TotalCostUSD != 0.40 {
		t.Fatalf("TotalCostUSD: want 0.40, got %v", snap.TotalCostUSD)
	}

	if snap.ContextWindowSize != 200_000 {
		t.Fatalf("ContextWindowSize: want 200000 (max), got %d", snap.ContextWindowSize)
	}

	if snap.ContextWindowUsed != 12_000 {
		t.Fatalf("ContextWindowUsed: want 12000 (sum), got %d", snap.ContextWindowUsed)
	}
}

func TestCostTracker_LoadSaveSessionCost_RoundTrips(t *testing.T) {
	home := t.TempDir()
	opts := SessionCostOptions{OpencodeHome: home}

	tracker := NewCostTracker()
	cb := tracker.ObserveUsage("ses_abc123")
	cb(context.Background(), &acp.SessionUsageUpdate{Cost: &acp.Cost{Amount: 1.23, Currency: "USD"}, Size: 100_000, Used: 512})

	if err := tracker.SaveSessionCost("ses_abc123", opts); err != nil {
		t.Fatalf("SaveSessionCost: %v", err)
	}

	expected := filepath.Join(home, "opencode", "sdk", "session-costs", "ses_abc123.json")
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("expected file at %s: %v", expected, err)
	}

	loaded, err := LoadSessionCost("ses_abc123", opts)
	if err != nil {
		t.Fatalf("LoadSessionCost: %v", err)
	}

	if loaded.TotalCostUSD != 1.23 {
		t.Fatalf("TotalCostUSD: want 1.23, got %v", loaded.TotalCostUSD)
	}

	if loaded.ContextWindowUsed != 512 {
		t.Fatalf("ContextWindowUsed: want 512, got %v", loaded.ContextWindowUsed)
	}
}

func TestLoadSessionCost_NotFound(t *testing.T) {
	_, err := LoadSessionCost("ses_nope", SessionCostOptions{OpencodeHome: t.TempDir()})
	if !errors.Is(err, ErrSessionCostNotFound) {
		t.Fatalf("want ErrSessionCostNotFound, got %v", err)
	}
}

func TestDeleteSessionCost_NotFound(t *testing.T) {
	err := DeleteSessionCost("ses_nope", SessionCostOptions{OpencodeHome: t.TempDir()})
	if !errors.Is(err, ErrSessionCostNotFound) {
		t.Fatalf("want ErrSessionCostNotFound, got %v", err)
	}
}

func TestCostTracker_LoadSessionCost_MergesIntoTracker(t *testing.T) {
	home := t.TempDir()
	opts := SessionCostOptions{OpencodeHome: home}

	snap := CostSnapshot{
		TotalCostUSD: 2.50,
		Currencies:   []string{"USD"},
		InputTokens:  1000,
		OutputTokens: 400,
		TotalTokens:  1400,
	}

	if err := SaveSessionCost("ses_xyz", snap, opts); err != nil {
		t.Fatalf("SaveSessionCost: %v", err)
	}

	tracker := NewCostTracker()
	if err := tracker.LoadSessionCost("ses_xyz", opts); err != nil {
		t.Fatalf("LoadSessionCost: %v", err)
	}

	got, ok := tracker.SessionSnapshot("ses_xyz")
	if !ok {
		t.Fatal("expected tracker to hold loaded session")
	}

	if got.TotalCostUSD != 2.50 || got.InputTokens != 1000 || got.OutputTokens != 400 {
		t.Fatalf("unexpected loaded snapshot: %+v", got)
	}
}

func TestSessionCostPath_InvalidSessionID(t *testing.T) {
	_, err := sessionCostPath("bad id with spaces", "")
	if err == nil {
		t.Fatal("expected error for invalid session id")
	}
}
