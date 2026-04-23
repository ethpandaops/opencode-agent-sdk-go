package opencodesdk

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/coder/acp-go-sdk"
)

// Sentinel errors surfaced by BudgetTracker.
var (
	// ErrBudgetExceeded is returned from BudgetTracker.CheckBudget and
	// wrapped by WithMaxBudgetUSD's automatic session cancellation
	// path when the configured cost/token cap is reached.
	ErrBudgetExceeded = errors.New("opencodesdk: budget exceeded")
)

const (
	defaultBudgetCompletionThreshold = 0.90

	// Machine-readable BudgetStatus.Reason values.
	budgetReasonWithinBudget   = "within_budget"
	budgetReasonNearCompletion = "near_completion"
	budgetReasonExceeded       = "budget_exceeded"
)

// BudgetTrackerOptions configures a BudgetTracker. Both caps are
// optional; when nil, the tracker records usage but never flags
// ShouldStop / ErrBudgetExceeded.
type BudgetTrackerOptions struct {
	// MaxCostUSD caps the tracker's accumulated USD spend. When
	// observed spend crosses this threshold, BudgetExceeded becomes
	// true.
	MaxCostUSD *float64
	// MaxTotalTokens caps accumulated input+output+cached tokens
	// across every session the tracker has observed. Useful when
	// cost is not reliably reported (some providers omit pricing).
	MaxTotalTokens *int
	// CompletionThreshold is the ratio in (0, 1] at which
	// NearCompletion flips on. Default: 0.90.
	CompletionThreshold float64
}

// BudgetStatus summarises budget progress at a single point in time.
type BudgetStatus struct {
	// Cost is the backing CostSnapshot from the embedded CostTracker.
	Cost CostSnapshot
	// CompletionRatio is the fraction of the tightest cap consumed,
	// in [0, +Inf). Zero when no caps are configured.
	CompletionRatio float64
	// NearCompletion is true once CompletionRatio crosses
	// options.CompletionThreshold.
	NearCompletion bool
	// BudgetExceeded is true once any cap has been crossed.
	BudgetExceeded bool
	// Reason is a short machine-readable explanation: "within_budget",
	// "near_completion", "budget_exceeded".
	Reason string
	// MaxCostUSD and MaxTotalTokens mirror the configured caps for
	// display.
	MaxCostUSD     *float64
	MaxTotalTokens *int
}

// BudgetTracker layers budget-enforcement heuristics over CostTracker.
// Safe for concurrent use.
//
// Wiring:
//
//	budget, _ := opencodesdk.NewBudgetTracker(opencodesdk.BudgetTrackerOptions{
//	    MaxCostUSD: opencodesdk.Float64Ptr(0.25),
//	})
//
//	sess.Subscribe(opencodesdk.UpdateHandlers{
//	    Usage: budget.ObserveUsage(sess.ID()),
//	})
//
// WithMaxBudgetUSD wires this automatically on every session created
// by a Client.
type BudgetTracker struct {
	mu                  sync.RWMutex
	cost                *CostTracker
	maxCostUSD          *float64
	maxTotalTokens      *int
	completionThreshold float64
}

// NewBudgetTracker constructs a tracker from options.
func NewBudgetTracker(o BudgetTrackerOptions) (*BudgetTracker, error) {
	threshold := o.CompletionThreshold
	if threshold == 0 {
		threshold = defaultBudgetCompletionThreshold
	}

	if threshold <= 0 || threshold > 1 {
		return nil, fmt.Errorf("opencodesdk: CompletionThreshold must be in (0, 1]")
	}

	return &BudgetTracker{
		cost:                NewCostTracker(),
		maxCostUSD:          o.MaxCostUSD,
		maxTotalTokens:      o.MaxTotalTokens,
		completionThreshold: threshold,
	}, nil
}

// ObserveUsage returns a callback suitable for passing as
// UpdateHandlers.Usage on Session.Subscribe.
func (t *BudgetTracker) ObserveUsage(sessionID string) func(ctx context.Context, upd *acp.SessionUsageUpdate) {
	return t.cost.ObserveUsage(sessionID)
}

// ObserveNotification records usage from any session/update
// notification (non-usage variants are ignored).
func (t *BudgetTracker) ObserveNotification(sessionID string, n acp.SessionNotification) {
	t.cost.ObserveNotification(sessionID, n)
}

// ObservePromptResult merges usage from a PromptResult.
func (t *BudgetTracker) ObservePromptResult(sessionID string, result *PromptResult) {
	t.cost.ObservePromptResult(sessionID, result)
}

// CostTracker returns the underlying CostTracker for callers that
// want direct access (e.g. to persist snapshots via SaveSessionCost).
func (t *BudgetTracker) CostTracker() *CostTracker { return t.cost }

// Status returns the current budget snapshot.
func (t *BudgetTracker) Status() BudgetStatus {
	if t == nil {
		return BudgetStatus{Reason: budgetReasonWithinBudget}
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.statusLocked()
}

func (t *BudgetTracker) statusLocked() BudgetStatus {
	cost := t.cost.Snapshot()
	status := BudgetStatus{
		Cost:           cost,
		MaxCostUSD:     t.maxCostUSD,
		MaxTotalTokens: t.maxTotalTokens,
	}

	var ratio float64

	if t.maxCostUSD != nil && *t.maxCostUSD > 0 {
		ratio = cost.TotalCostUSD / *t.maxCostUSD
	}

	if t.maxTotalTokens != nil && *t.maxTotalTokens > 0 {
		if tr := float64(cost.TotalTokens) / float64(*t.maxTotalTokens); tr > ratio {
			ratio = tr
		}
	}

	status.CompletionRatio = ratio
	status.NearCompletion = ratio >= t.completionThreshold && !t.exceededLocked(cost)
	status.BudgetExceeded = t.exceededLocked(cost)

	switch {
	case status.BudgetExceeded:
		status.Reason = budgetReasonExceeded
	case status.NearCompletion:
		status.Reason = budgetReasonNearCompletion
	default:
		status.Reason = budgetReasonWithinBudget
	}

	return status
}

func (t *BudgetTracker) exceededLocked(cost CostSnapshot) bool {
	if t.maxCostUSD != nil && *t.maxCostUSD > 0 && cost.TotalCostUSD >= *t.maxCostUSD {
		return true
	}

	if t.maxTotalTokens != nil && *t.maxTotalTokens > 0 && cost.TotalTokens >= *t.maxTotalTokens {
		return true
	}

	return false
}

// CheckBudget returns ErrBudgetExceeded when any cap has been crossed.
// Returns nil otherwise. Useful inside observer callbacks to abort a
// loop once the budget is gone.
func (t *BudgetTracker) CheckBudget() error {
	if t == nil {
		return nil
	}

	if t.Status().BudgetExceeded {
		return ErrBudgetExceeded
	}

	return nil
}

// Float64Ptr returns a pointer to v. Convenience for filling
// BudgetTrackerOptions inline.
func Float64Ptr(v float64) *float64 { return &v }

// IntPtr returns a pointer to v. Convenience for filling
// BudgetTrackerOptions inline.
func IntPtr(v int) *int { return &v }
