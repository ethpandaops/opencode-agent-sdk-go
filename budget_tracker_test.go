package opencodesdk

import (
	"context"
	"errors"
	"testing"

	"github.com/coder/acp-go-sdk"
)

func TestBudgetTracker_WithinBudget(t *testing.T) {
	limit := 1.0

	bt, err := NewBudgetTracker(BudgetTrackerOptions{MaxCostUSD: &limit})
	if err != nil {
		t.Fatalf("NewBudgetTracker: %v", err)
	}

	upd := &acp.SessionUsageUpdate{
		Size: 1000,
		Used: 100,
		Cost: &acp.Cost{Amount: 0.10, Currency: "USD"},
	}
	bt.ObserveUsage("ses1")(context.Background(), upd)

	status := bt.Status()
	if status.BudgetExceeded {
		t.Fatalf("expected within-budget, got %+v", status)
	}

	if status.Reason != budgetReasonWithinBudget {
		t.Fatalf("reason = %q, want %q", status.Reason, budgetReasonWithinBudget)
	}
}

func TestBudgetTracker_ExceedsBudget(t *testing.T) {
	limit := 0.05

	bt, err := NewBudgetTracker(BudgetTrackerOptions{MaxCostUSD: &limit})
	if err != nil {
		t.Fatalf("NewBudgetTracker: %v", err)
	}

	upd := &acp.SessionUsageUpdate{
		Cost: &acp.Cost{Amount: 0.10, Currency: "USD"},
	}
	bt.ObserveUsage("ses1")(context.Background(), upd)

	if err := bt.CheckBudget(); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("CheckBudget: got %v, want ErrBudgetExceeded", err)
	}

	if bt.Status().Reason != budgetReasonExceeded {
		t.Fatalf("reason = %q, want %q", bt.Status().Reason, budgetReasonExceeded)
	}
}

func TestBudgetTracker_NearCompletion(t *testing.T) {
	limit := 1.0

	bt, err := NewBudgetTracker(BudgetTrackerOptions{
		MaxCostUSD:          &limit,
		CompletionThreshold: 0.5,
	})
	if err != nil {
		t.Fatalf("NewBudgetTracker: %v", err)
	}

	upd := &acp.SessionUsageUpdate{
		Cost: &acp.Cost{Amount: 0.60, Currency: "USD"},
	}
	bt.ObserveUsage("ses1")(context.Background(), upd)

	status := bt.Status()

	if !status.NearCompletion {
		t.Fatalf("NearCompletion = false, want true; %+v", status)
	}

	if status.BudgetExceeded {
		t.Fatalf("BudgetExceeded = true, want false before cap; %+v", status)
	}
}

func TestBudgetTracker_TokenCap(t *testing.T) {
	tokens := 100

	bt, err := NewBudgetTracker(BudgetTrackerOptions{MaxTotalTokens: &tokens})
	if err != nil {
		t.Fatalf("NewBudgetTracker: %v", err)
	}

	bt.ObservePromptResult("ses1", &PromptResult{
		Usage: &acp.Usage{
			InputTokens:  50,
			OutputTokens: 60,
			TotalTokens:  110,
		},
	})

	if !bt.Status().BudgetExceeded {
		t.Fatalf("expected token cap exceeded; snap = %+v", bt.Status())
	}
}

func TestBudgetTracker_NilSafety(t *testing.T) {
	var bt *BudgetTracker

	if err := bt.CheckBudget(); err != nil {
		t.Fatalf("nil tracker CheckBudget = %v", err)
	}

	if s := bt.Status(); s.Reason != budgetReasonWithinBudget {
		t.Fatalf("nil tracker Status.Reason = %q", s.Reason)
	}
}

func TestBudgetTracker_InvalidThreshold(t *testing.T) {
	_, err := NewBudgetTracker(BudgetTrackerOptions{CompletionThreshold: 1.5})
	if err == nil {
		t.Fatalf("expected error for threshold > 1")
	}
}

func TestFloat64Ptr_IntPtr(t *testing.T) {
	if p := Float64Ptr(1.5); *p != 1.5 {
		t.Fatalf("Float64Ptr: got %v", *p)
	}

	if p := IntPtr(42); *p != 42 {
		t.Fatalf("IntPtr: got %v", *p)
	}
}
