package opencodesdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/coder/acp-go-sdk"
)

// ErrSessionCostNotFound is returned by LoadSessionCost when no
// persisted snapshot exists for the requested session id.
var ErrSessionCostNotFound = errors.New("opencodesdk: session cost not found")

// CostSnapshot is a point-in-time view of aggregated usage and cost
// either for a single opencode session (SessionSnapshot) or across
// every session the tracker has observed (Snapshot).
//
// opencode emits SessionUsageUpdate notifications with CUMULATIVE
// per-session values. CostTracker tracks the latest per-session
// value; aggregate snapshots sum across sessions.
type CostSnapshot struct {
	// TotalCostUSD is the aggregated monetary cost observed. Only
	// USD-denominated costs are summed here; other currencies are
	// captured in Currencies for visibility.
	TotalCostUSD float64
	// Currencies lists the distinct currency codes observed during
	// aggregation. When all sessions are USD this contains just
	// ["USD"].
	Currencies []string
	// Sessions is the number of distinct opencode session ids the
	// tracker has observed.
	Sessions int
	// Token aggregates across sessions. Cached tokens and thought
	// tokens are optional in ACP; opencode populates them when the
	// model reports them.
	InputTokens       int
	OutputTokens      int
	CachedReadTokens  int
	CachedWriteTokens int
	ThoughtTokens     int
	TotalTokens       int
	// ContextWindowSize is the max context window reported across
	// sessions (models differ; this is the largest seen).
	ContextWindowSize int
	// ContextWindowUsed is the sum of context-tokens-used reported
	// across sessions. For a per-session snapshot this is the latest
	// value.
	ContextWindowUsed int
}

// sessionCost is the per-session cumulative state stored inside the
// tracker. SessionUsageUpdate overrides (rather than adds to) these
// values.
type sessionCost struct {
	CostUSD           float64 `json:"costUsd"`
	Currency          string  `json:"currency,omitempty"`
	InputTokens       int     `json:"inputTokens"`
	OutputTokens      int     `json:"outputTokens"`
	CachedReadTokens  int     `json:"cachedReadTokens,omitempty"`
	CachedWriteTokens int     `json:"cachedWriteTokens,omitempty"`
	ThoughtTokens     int     `json:"thoughtTokens,omitempty"`
	TotalTokens       int     `json:"totalTokens"`
	ContextSize       int     `json:"contextSize"`
	ContextUsed       int     `json:"contextUsed"`
}

// CostTracker accumulates per-session cost + usage across one or more
// opencode sessions. Safe for concurrent use.
//
// Wiring:
//
//	tracker := opencodesdk.NewCostTracker()
//	sess.Subscribe(opencodesdk.UpdateHandlers{
//	    Usage: tracker.ObserveUsage(sess.ID()),
//	})
//
// Or for a whole-session callback that captures both token usage on
// UsageUpdate and the final-turn usage on PromptResult:
//
//	tracker.ObserveNotification(sess.ID(), n)
//	tracker.ObservePromptResult(sess.ID(), result)
type CostTracker struct {
	mu       sync.Mutex
	sessions map[string]sessionCost
}

// NewCostTracker constructs an empty tracker.
func NewCostTracker() *CostTracker {
	return &CostTracker{sessions: make(map[string]sessionCost)}
}

// ObserveUsage returns a callback suitable for passing as
// UpdateHandlers.Usage. The returned callback records the cumulative
// values from every UsageUpdate for the supplied sessionID.
func (t *CostTracker) ObserveUsage(sessionID string) func(ctx context.Context, upd *acp.SessionUsageUpdate) {
	return func(_ context.Context, upd *acp.SessionUsageUpdate) {
		t.recordUsageUpdate(sessionID, upd)
	}
}

// ObserveNotification records token + cost state from a
// session/update notification. Non-usage variants are ignored, so
// callers can pipe every notification through without filtering.
func (t *CostTracker) ObserveNotification(sessionID string, n acp.SessionNotification) {
	if upd := n.Update.UsageUpdate; upd != nil {
		t.recordUsageUpdate(sessionID, upd)
	}
}

// ObservePromptResult merges token usage from a PromptResult. Cost is
// not carried in PromptResult — only in UsageUpdate notifications —
// so this path updates tokens without touching the cost total.
func (t *CostTracker) ObservePromptResult(sessionID string, result *PromptResult) {
	if t == nil || result == nil || result.Usage == nil {
		return
	}

	u := result.Usage

	t.mu.Lock()
	defer t.mu.Unlock()

	cur := t.sessions[sessionID]

	if u.InputTokens > cur.InputTokens {
		cur.InputTokens = u.InputTokens
	}

	if u.OutputTokens > cur.OutputTokens {
		cur.OutputTokens = u.OutputTokens
	}

	if u.CachedReadTokens != nil && *u.CachedReadTokens > cur.CachedReadTokens {
		cur.CachedReadTokens = *u.CachedReadTokens
	}

	if u.CachedWriteTokens != nil && *u.CachedWriteTokens > cur.CachedWriteTokens {
		cur.CachedWriteTokens = *u.CachedWriteTokens
	}

	if u.ThoughtTokens != nil && *u.ThoughtTokens > cur.ThoughtTokens {
		cur.ThoughtTokens = *u.ThoughtTokens
	}

	if u.TotalTokens > cur.TotalTokens {
		cur.TotalTokens = u.TotalTokens
	}

	t.sessions[sessionID] = cur
}

func (t *CostTracker) recordUsageUpdate(sessionID string, upd *acp.SessionUsageUpdate) {
	if t == nil || upd == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	cur := t.sessions[sessionID]

	if upd.Cost != nil {
		cur.CostUSD = upd.Cost.Amount
		cur.Currency = upd.Cost.Currency
	}

	cur.ContextSize = upd.Size
	cur.ContextUsed = upd.Used
	t.sessions[sessionID] = cur
}

// Snapshot returns an aggregate view across every observed session.
func (t *CostTracker) Snapshot() CostSnapshot {
	if t == nil {
		return CostSnapshot{}
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	snap := CostSnapshot{Sessions: len(t.sessions)}
	seen := map[string]struct{}{}

	for _, s := range t.sessions {
		if s.Currency != "" {
			if _, ok := seen[s.Currency]; !ok {
				seen[s.Currency] = struct{}{}

				snap.Currencies = append(snap.Currencies, s.Currency)
			}
		}

		if s.Currency == "" || s.Currency == "USD" {
			snap.TotalCostUSD += s.CostUSD
		}

		snap.InputTokens += s.InputTokens
		snap.OutputTokens += s.OutputTokens
		snap.CachedReadTokens += s.CachedReadTokens
		snap.CachedWriteTokens += s.CachedWriteTokens
		snap.ThoughtTokens += s.ThoughtTokens
		snap.TotalTokens += s.TotalTokens

		if s.ContextSize > snap.ContextWindowSize {
			snap.ContextWindowSize = s.ContextSize
		}

		snap.ContextWindowUsed += s.ContextUsed
	}

	return snap
}

// SessionSnapshot returns the latest cumulative snapshot for
// sessionID, or (zero, false) if the tracker has not observed it.
func (t *CostTracker) SessionSnapshot(sessionID string) (CostSnapshot, bool) {
	if t == nil {
		return CostSnapshot{}, false
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	s, ok := t.sessions[sessionID]
	if !ok {
		return CostSnapshot{}, false
	}

	out := CostSnapshot{
		TotalCostUSD:      s.CostUSD,
		Sessions:          1,
		InputTokens:       s.InputTokens,
		OutputTokens:      s.OutputTokens,
		CachedReadTokens:  s.CachedReadTokens,
		CachedWriteTokens: s.CachedWriteTokens,
		ThoughtTokens:     s.ThoughtTokens,
		TotalTokens:       s.TotalTokens,
		ContextWindowSize: s.ContextSize,
		ContextWindowUsed: s.ContextUsed,
	}

	if s.Currency != "" {
		out.Currencies = []string{s.Currency}
	}

	return out, true
}

// SessionCostOptions controls persisted session-cost file locations.
type SessionCostOptions struct {
	// OpencodeHome is the opencode data directory. When empty the SDK
	// falls back to $XDG_DATA_HOME, then $HOME/.local/share.
	OpencodeHome string
}

// LoadSessionCost reads a persisted snapshot from disk. Returns
// ErrSessionCostNotFound when no file exists.
func LoadSessionCost(sessionID string, opts SessionCostOptions) (*CostSnapshot, error) {
	path, err := sessionCostPath(sessionID, opts.OpencodeHome)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrSessionCostNotFound
		}

		return nil, fmt.Errorf("read session cost: %w", err)
	}

	var stored persistedSessionCost
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("parse session cost: %w", err)
	}

	return &stored.Snapshot, nil
}

// SaveSessionCost writes the tracker's current snapshot for
// sessionID to disk. It is safe to call repeatedly — each call
// overwrites the previous file via an atomic rename.
func (t *CostTracker) SaveSessionCost(sessionID string, opts SessionCostOptions) error {
	snap, ok := t.SessionSnapshot(sessionID)
	if !ok {
		return fmt.Errorf("opencodesdk: SaveSessionCost: unknown session %q", sessionID)
	}

	return SaveSessionCost(sessionID, snap, opts)
}

// SaveSessionCost persists a CostSnapshot for sessionID under
// opts.OpencodeHome. Exposed as a free function so callers who build
// snapshots outside of a tracker can still persist them.
func SaveSessionCost(sessionID string, snap CostSnapshot, opts SessionCostOptions) error {
	path, err := sessionCostPath(sessionID, opts.OpencodeHome)
	if err != nil {
		return err
	}

	if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
		return fmt.Errorf("create session cost dir: %w", mkErr)
	}

	data, err := json.MarshalIndent(persistedSessionCost{
		SessionID: sessionID,
		Snapshot:  snap,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session cost: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write session cost: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("commit session cost: %w", err)
	}

	return nil
}

// DeleteSessionCost removes a persisted session-cost file. Returns
// ErrSessionCostNotFound when the file does not exist.
func DeleteSessionCost(sessionID string, opts SessionCostOptions) error {
	path, err := sessionCostPath(sessionID, opts.OpencodeHome)
	if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return ErrSessionCostNotFound
		}

		return fmt.Errorf("delete session cost: %w", err)
	}

	return nil
}

// LoadSessionCost reads a persisted snapshot into the tracker's
// in-memory state. The persisted data overrides any existing session
// record.
func (t *CostTracker) LoadSessionCost(sessionID string, opts SessionCostOptions) error {
	snap, err := LoadSessionCost(sessionID, opts)
	if err != nil {
		return err
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	currency := ""
	if len(snap.Currencies) > 0 {
		currency = snap.Currencies[0]
	}

	t.sessions[sessionID] = sessionCost{
		CostUSD:           snap.TotalCostUSD,
		Currency:          currency,
		InputTokens:       snap.InputTokens,
		OutputTokens:      snap.OutputTokens,
		CachedReadTokens:  snap.CachedReadTokens,
		CachedWriteTokens: snap.CachedWriteTokens,
		ThoughtTokens:     snap.ThoughtTokens,
		TotalTokens:       snap.TotalTokens,
		ContextSize:       snap.ContextWindowSize,
		ContextUsed:       snap.ContextWindowUsed,
	}

	return nil
}

// persistedSessionCost is the on-disk envelope for snapshots.
//
//nolint:tagliatelle // snake_case for on-disk readability
type persistedSessionCost struct {
	SessionID string       `json:"session_id"`
	Snapshot  CostSnapshot `json:"snapshot"`
}

// sessionIDPattern matches opencode session ids. Kept permissive:
// ACP/opencode emits IDs like "ses_24d2fc1e0ffe5YxDJSq64vW9LD" but
// integration tests sometimes seed arbitrary strings.
var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_\-]{3,128}$`)

func sessionCostPath(sessionID, opencodeHome string) (string, error) {
	if !sessionIDPattern.MatchString(sessionID) {
		return "", fmt.Errorf("opencodesdk: invalid session ID %q", sessionID)
	}

	base := resolveOpencodeDataDir(opencodeHome)

	return filepath.Join(base, "sdk", "session-costs", sessionID+".json"), nil
}

// resolveOpencodeDataDir mirrors opencode's XDG_DATA_HOME lookup so
// persisted cost snapshots sit next to opencode's own data.
func resolveOpencodeDataDir(override string) string {
	if override != "" {
		return filepath.Join(override, "opencode")
	}

	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "opencode")
	}

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".", ".opencode")
	}

	return filepath.Join(home, ".local", "share", "opencode")
}
