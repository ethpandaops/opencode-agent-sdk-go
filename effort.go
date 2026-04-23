package opencodesdk

import (
	"context"
	"log/slog"
	"strings"
)

// Effort selects an opencode model variant by reasoning depth. opencode
// encodes variants as a `/<variant>` suffix on the model id (e.g.
// "anthropic/claude-opus-4-7/high"); this enum maps abstract levels
// onto whatever variant strings the chosen model actually exposes.
//
// The mapping is best-effort:
//
//   - EffortNone is preserved when the model exposes a "none" variant
//     (the gpt-5.x family does); other models silently drop the option.
//   - EffortLow / EffortMedium / EffortHigh map to "low" / "medium" /
//     "high" verbatim when present.
//   - EffortMax tries "max", then "xhigh", then "high" — falling back
//     to whatever the model offers as its highest tier.
//
// When the model exposes no variants at all (chat-only / embedding /
// TTS models), WithEffort is a silent no-op: the SDK logs a debug
// notice and keeps the unsuffixed model id. Callers who want stricter
// behaviour should inspect Session.CurrentVariant() after session
// creation.
//
// Variant choice is not persisted in opencode's session DB. After
// LoadSession the SDK re-applies the configured Effort.
type Effort string

// Effort levels. The string values are stable; do not rely on them
// matching opencode's internal variant ids one-for-one.
const (
	EffortNone   Effort = "none"
	EffortLow    Effort = "low"
	EffortMedium Effort = "medium"
	EffortHigh   Effort = "high"
	EffortMax    Effort = "max"
)

// WithEffort selects a model variant by reasoning depth. See [Effort].
//
// When the caller's WithModel value already carries a `/variant`
// suffix (e.g. "anthropic/claude-opus-4-7/high"), the explicit suffix
// wins and WithEffort is ignored — the caller has already pinned a
// variant and the SDK does not second-guess it.
//
// Applied at session creation: the SDK probes opencode for the model's
// available variants via session/set_model with the bare model id, then
// re-applies session/set_model with the chosen `<base>/<variant>`. The
// probe round-trip happens once per session (NewSession, LoadSession,
// ForkSession, ResumeSession).
func WithEffort(level Effort) Option {
	return func(o *options) { o.effort = level }
}

// effortPriority returns the variant strings to try in order for level,
// most preferred first. Returns nil for the empty string (no
// preference).
func effortPriority(level Effort) []string {
	switch level {
	case EffortNone:
		return []string{"none"}
	case EffortLow:
		return []string{"low"}
	case EffortMedium:
		return []string{"medium", "low"}
	case EffortHigh:
		return []string{"high", "medium"}
	case EffortMax:
		return []string{"max", "xhigh", "high"}
	default:
		return nil
	}
}

// chooseVariant returns the first variant from priority that appears
// in available, or empty string when none match.
func chooseVariant(available, priority []string) string {
	for _, want := range priority {
		for _, have := range available {
			if have == want {
				return have
			}
		}
	}

	return ""
}

// modelHasExplicitVariant reports whether modelID carries a `/variant`
// suffix. Heuristic: opencode model ids look like "<provider>/<model>"
// or "<provider>/<model>/<variant>"; an entry with three or more
// slash-separated segments is treated as already pinning a variant.
func modelHasExplicitVariant(modelID string) bool {
	parts := strings.Split(modelID, "/")

	return len(parts) >= 3
}

// applyEffortOnSession resolves the model's available variants and
// re-applies session/set_model with the chosen suffix. It is called
// from applySessionConfig after WithModel runs (so s.currentModel is
// the base id the user requested).
//
// Returns nil and logs at debug when the configured Effort can't be
// realised (model has no variants, or no priority entry matched);
// callers don't observe a hard failure for the silent no-op path.
func (c *client) applyEffortOnSession(ctx context.Context, s *session, level Effort, modelID string) error {
	priority := effortPriority(level)
	if len(priority) == 0 {
		return nil
	}

	if modelHasExplicitVariant(modelID) {
		s.logger.Debug("WithEffort ignored: model id already carries explicit variant",
			slog.String("model", modelID),
			slog.String("effort", string(level)),
		)

		return nil
	}

	info, err := c.resolveVariants(ctx, s.ID(), modelID)
	if err != nil {
		return err
	}

	s.setResolvedVariant(info)

	if len(info.AvailableVariants) == 0 {
		s.logger.Debug("WithEffort no-op: model exposes no variants",
			slog.String("model", modelID),
			slog.String("effort", string(level)),
		)

		return nil
	}

	chosen := chooseVariant(info.AvailableVariants, priority)
	if chosen == "" {
		s.logger.Debug("WithEffort no-op: no priority variant available for model",
			slog.String("model", modelID),
			slog.String("effort", string(level)),
			slog.Any("available", info.AvailableVariants),
		)

		return nil
	}

	if chosen == info.Variant {
		// Already on the chosen variant after the probe (opencode applies
		// the unsuffixed id as a no-op when the prior selection was a
		// variant of the same base). Skip the redundant round-trip.
		s.mu.Lock()
		s.currentModel = info.ModelId + "/" + chosen
		s.mu.Unlock()

		return nil
	}

	target := info.ModelId + "/" + chosen
	if err := c.UnstableSetModel(ctx, s.ID(), target); err != nil {
		return err
	}

	s.mu.Lock()
	s.currentModel = target
	s.mu.Unlock()

	applied := *info
	applied.Variant = chosen
	s.setResolvedVariant(&applied)

	return nil
}
