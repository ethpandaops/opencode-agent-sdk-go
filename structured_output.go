package opencodesdk

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/coder/acp-go-sdk"
	"go.opentelemetry.io/otel"

	"github.com/ethpandaops/opencode-agent-sdk-go/internal/observability"
)

// structuredDecodeObserver returns a process-wide Observer used to
// emit opencodesdk.structured_output.decode metrics. The SDK does not
// thread a Client or Observer through DecodeStructuredOutput (they're
// pure functions operating on results), so we fall back to the OTel
// global providers here — users who want full attribution should
// install an SDK-level MeterProvider via otel.SetMeterProvider.
func structuredDecodeObserver() *observability.Observer {
	return observability.NewObserver(otel.GetMeterProvider(), otel.GetTracerProvider())
}

// structuredOutputMetaKey is the conventional key the SDK reads when
// looking for a structured-output payload in PromptResult.Meta or
// QueryResult.Notifications. opencode itself does not enforce a
// schema, so callers who want typed decoding should:
//
//  1. Instruct the model to reply with JSON matching the desired
//     shape (either inline in the prompt, or via agent-level system
//     prompt), and
//  2. Call DecodeStructuredOutput on the resulting QueryResult.
//
// The decoder tries Meta[structuredOutputMetaKey] first, then falls
// back to parsing AssistantText as JSON.
const (
	structuredOutputMetaKey = "structuredOutput"

	decodeOutcomeOK      = "ok"
	decodeOutcomeError   = "error"
	decodeOutcomeMissing = "missing"

	decodeSourceNotifications = "notifications"
	decodeSourceText          = "text"
	decodeSourcePromptMeta    = "prompt_meta"
)

// DecodeStructuredOutput[T] extracts a typed T value from a
// QueryResult. The precedence is:
//
//  1. A decoded object at QueryResult.Notifications[…].Update
//     containing a PromptResponse-side Meta["structuredOutput"] block.
//     (Opencode does not currently set this out of the box — it's a
//     convention reserved for agents that opt in via _meta.)
//  2. Parsing QueryResult.AssistantText as JSON — possibly wrapped in
//     a fenced code block.
//
// Returns ErrStructuredOutputMissing when neither path yields a
// decodable payload.
//
// For callers holding a raw PromptResult (not a QueryResult), use
// DecodePromptResult[T].
func DecodeStructuredOutput[T any](result *QueryResult) (T, error) {
	var zero T

	ctx := context.Background()
	obs := structuredDecodeObserver()

	if result == nil {
		obs.RecordStructuredDecode(ctx, decodeSourceNotifications, decodeOutcomeMissing)

		return zero, fmt.Errorf("opencodesdk: DecodeStructuredOutput: nil result")
	}

	if decoded, ok, err := decodeFromNotifications[T](result.Notifications); ok {
		outcome := decodeOutcomeOK
		if err != nil {
			outcome = decodeOutcomeError
		}

		obs.RecordStructuredDecode(ctx, decodeSourceNotifications, outcome)

		return decoded, err
	}

	if result.AssistantText != "" {
		raw := extractJSONCandidate(result.AssistantText)
		if raw != "" {
			var out T
			if err := json.Unmarshal([]byte(raw), &out); err == nil {
				obs.RecordStructuredDecode(ctx, decodeSourceText, decodeOutcomeOK)

				return out, nil
			}
		}
	}

	obs.RecordStructuredDecode(ctx, decodeSourceNotifications, decodeOutcomeMissing)

	return zero, ErrStructuredOutputMissing
}

// DecodePromptResult[T] is the companion for callers who interact
// with Session.Prompt directly and don't have access to the streamed
// AssistantText. It reads PromptResult.Meta["structuredOutput"] only
// — no text fallback — so returns ErrStructuredOutputMissing if the
// agent did not advertise a structured payload via _meta.
func DecodePromptResult[T any](result *PromptResult) (T, error) {
	var zero T

	ctx := context.Background()
	obs := structuredDecodeObserver()

	if result == nil {
		obs.RecordStructuredDecode(ctx, decodeSourcePromptMeta, decodeOutcomeMissing)

		return zero, fmt.Errorf("opencodesdk: DecodePromptResult: nil result")
	}

	raw, ok := result.Meta[structuredOutputMetaKey]
	if !ok {
		obs.RecordStructuredDecode(ctx, decodeSourcePromptMeta, decodeOutcomeMissing)

		return zero, ErrStructuredOutputMissing
	}

	out, err := convertStructured[T](raw)

	outcome := decodeOutcomeOK
	if err != nil {
		outcome = decodeOutcomeError
	}

	obs.RecordStructuredDecode(ctx, decodeSourcePromptMeta, outcome)

	return out, err
}

// WithOutputSchema embeds a structured-output hint into the session's
// _meta block so agents that honour the convention know what shape
// to emit. opencode itself does not enforce the schema; this option
// is a best-effort passthrough that sets
// session.meta["structuredOutputSchema"] so downstream logic (SDK
// helpers, UI, or the model via prompt engineering) can read it.
//
// schema must be a JSON-schema compatible map (see SimpleSchema for
// the common case). Pass nil to clear any previously set schema.
//
// Returns an Option usable with NewClient / NewSession / Query.
func WithOutputSchema(schema map[string]any) Option {
	return func(o *options) { o.outputSchema = schema }
}

// decodeFromNotifications scans a slice of session notifications for
// a Meta block carrying a structuredOutput field. The third return
// signals whether the key was observed at all — when true but err is
// non-nil the caller has enough info to report a meaningful failure.
func decodeFromNotifications[T any](notifications []acp.SessionNotification) (T, bool, error) {
	var zero T

	for _, n := range notifications {
		meta := n.Meta
		if meta == nil {
			continue
		}

		raw, ok := meta[structuredOutputMetaKey]
		if !ok {
			continue
		}

		out, err := convertStructured[T](raw)

		return out, true, err
	}

	return zero, false, nil
}

// convertStructured converts an arbitrary JSON-decoded value into a
// typed T via a JSON round-trip.
func convertStructured[T any](raw any) (T, error) {
	var zero T

	data, err := json.Marshal(raw)
	if err != nil {
		return zero, fmt.Errorf("marshal structured output: %w", err)
	}

	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		return zero, fmt.Errorf("decode structured output: %w", err)
	}

	return out, nil
}

// extractJSONCandidate pulls the most plausible JSON blob out of a
// plain-text assistant reply. Handles:
//   - fenced ```json blocks
//   - fenced ``` blocks
//   - the first {...} or [...] span in the text
//
// Returns "" when no candidate is found.
func extractJSONCandidate(text string) string {
	if raw := extractFenced(text, "```json"); raw != "" {
		return raw
	}

	if raw := extractFenced(text, "```"); raw != "" {
		return raw
	}

	return extractBracedSpan(text)
}

func extractFenced(text, opener string) string {
	_, remainder, ok := strings.Cut(text, opener)
	if !ok {
		return ""
	}

	if nl := strings.IndexByte(remainder, '\n'); nl >= 0 {
		remainder = remainder[nl+1:]
	}

	body, _, ok := strings.Cut(remainder, "```")
	if !ok {
		return ""
	}

	return strings.TrimSpace(body)
}

func extractBracedSpan(text string) string {
	openIdx := -1
	openCh := byte(0)

	for i := 0; i < len(text); i++ {
		c := text[i]
		if c == '{' || c == '[' {
			openIdx = i
			openCh = c

			break
		}
	}

	if openIdx < 0 {
		return ""
	}

	closeCh := byte('}')
	if openCh == '[' {
		closeCh = ']'
	}

	depth := 0
	inString := false
	escape := false

	for i := openIdx; i < len(text); i++ {
		c := text[i]

		if escape {
			escape = false

			continue
		}

		if c == '\\' {
			escape = true

			continue
		}

		if c == '"' {
			inString = !inString

			continue
		}

		if inString {
			continue
		}

		switch c {
		case openCh:
			depth++
		case closeCh:
			depth--
			if depth == 0 {
				return text[openIdx : i+1]
			}
		}
	}

	return ""
}
