package observability

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// harness bundles the Observer under test with handles on the in-memory
// metric reader and span recorder so test bodies can assert on emitted
// telemetry.
type harness struct {
	obs      *Observer
	reader   *sdkmetric.ManualReader
	recorder *tracetest.SpanRecorder
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

	t.Cleanup(func() {
		_ = mp.Shutdown(context.Background())
		_ = tp.Shutdown(context.Background())
	})

	return &harness{
		obs:      NewObserver(mp, tp),
		reader:   reader,
		recorder: recorder,
	}
}

func (h *harness) collect(t *testing.T) metricdata.ResourceMetrics {
	t.Helper()

	var rm metricdata.ResourceMetrics
	if err := h.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	return rm
}

// findMetric returns the named metric from a ResourceMetrics snapshot,
// or nil if absent. The opencodesdk namespace prefix is added for the
// caller.
func findMetric(rm metricdata.ResourceMetrics, name string) *metricdata.Metrics {
	qualified := Namespace + "." + name

	for _, sm := range rm.ScopeMetrics {
		for i, m := range sm.Metrics {
			if m.Name == qualified {
				return &sm.Metrics[i]
			}
		}
	}

	return nil
}

// attrValue returns the string attribute value at key in set, or "" if
// absent or not a string.
func attrValue(set attribute.Set, key string) string {
	v, ok := set.Value(attribute.Key(key))
	if !ok {
		return ""
	}

	return v.Emit()
}

func TestNewObserver_NilProvidersDoesNotPanic(t *testing.T) {
	obs := NewObserver(nil, nil)
	if obs == nil {
		t.Fatalf("NewObserver(nil,nil) returned nil")
	}

	// Calls on the noop instruments must be safe.
	ctx := context.Background()
	obs.RecordPrompt(ctx, time.Second, "end_turn", TokenCounts{Input: 1}, PromptLabels{Model: "m"})
	obs.RecordSessionUpdate(ctx, "agent_message_chunk")
	obs.RecordMCPBridge(ctx, "sum", "ok")
	obs.RecordPermission(ctx, "auto_reject")
	obs.RecordFsDelegation(ctx, "write", "handled")
	obs.RecordToolCall(ctx, "Bash", "execute", "completed", time.Millisecond)
	obs.RecordCost(ctx, 0.01, "USD", "x")
	obs.RecordInitializeDuration(ctx, 2*time.Second, true)
	obs.RecordCLISpawn(ctx, "started")

	_, span := obs.StartPromptSpan(ctx, "sess", PromptLabels{})
	span.End()
}

func TestRecordPrompt_EmitsDurationAndTokens(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	h.obs.RecordPrompt(ctx, 250*time.Millisecond, "end_turn",
		TokenCounts{Input: 100, Output: 50, CachedRead: 20, CachedWrite: 5, Thought: 3},
		PromptLabels{Model: "anthropic/claude-sonnet-4-6", Mode: "build"},
	)

	rm := h.collect(t)

	dur := findMetric(rm, "session.prompt.duration")
	if dur == nil {
		t.Fatalf("session.prompt.duration not found")
	}

	hist, ok := dur.Data.(metricdata.Histogram[float64])
	if !ok {
		t.Fatalf("prompt.duration data = %T, want Histogram[float64]", dur.Data)
	}

	if len(hist.DataPoints) != 1 {
		t.Fatalf("expected 1 duration data point; got %d", len(hist.DataPoints))
	}

	pt := hist.DataPoints[0]
	if pt.Count != 1 {
		t.Fatalf("duration count = %d, want 1", pt.Count)
	}

	if attrValue(pt.Attributes, "stop_reason") != "end_turn" {
		t.Fatalf("stop_reason attr = %q, want end_turn", attrValue(pt.Attributes, "stop_reason"))
	}

	if attrValue(pt.Attributes, "model") != "anthropic/claude-sonnet-4-6" {
		t.Fatalf("model attr = %q, want anthropic/claude-sonnet-4-6", attrValue(pt.Attributes, "model"))
	}

	tokens := findMetric(rm, "session.prompt.tokens")
	if tokens == nil {
		t.Fatalf("session.prompt.tokens not found")
	}

	tokHist, ok := tokens.Data.(metricdata.Histogram[float64])
	if !ok {
		t.Fatalf("prompt.tokens data = %T", tokens.Data)
	}

	// Expect one data point per direction (input, output, cached_read, cached_write, thought).
	seen := map[string]bool{}

	for _, p := range tokHist.DataPoints {
		seen[attrValue(p.Attributes, "direction")] = true
	}

	for _, want := range []string{"input", "output", "cached_read", "cached_write", "thought"} {
		if !seen[want] {
			t.Fatalf("missing direction=%s token histogram point; saw %v", want, seen)
		}
	}
}

func TestRecordPrompt_SkipsZeroTokens(t *testing.T) {
	h := newHarness(t)

	h.obs.RecordPrompt(context.Background(), time.Second, "end_turn",
		TokenCounts{Input: 10}, // output, cached_*, thought all zero
		PromptLabels{},
	)

	rm := h.collect(t)

	tokens := findMetric(rm, "session.prompt.tokens")
	if tokens == nil {
		t.Fatalf("session.prompt.tokens missing")
	}

	hist, ok := tokens.Data.(metricdata.Histogram[float64])
	if !ok {
		t.Fatalf("unexpected tokens data type %T", tokens.Data)
	}

	if len(hist.DataPoints) != 1 {
		t.Fatalf("expected only 1 direction (input), got %d points", len(hist.DataPoints))
	}

	if dir := attrValue(hist.DataPoints[0].Attributes, "direction"); dir != "input" {
		t.Fatalf("expected input, got %q", dir)
	}
}

func TestRecordSessionUpdate_IncrementsCounterByVariant(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	h.obs.RecordSessionUpdate(ctx, "agent_message_chunk")
	h.obs.RecordSessionUpdate(ctx, "agent_message_chunk")
	h.obs.RecordSessionUpdate(ctx, "tool_call")

	rm := h.collect(t)

	m := findMetric(rm, "session.update.count")
	if m == nil {
		t.Fatalf("session.update.count not found")
	}

	sum, ok := m.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("data = %T, want Sum[int64]", m.Data)
	}

	counts := map[string]int64{}
	for _, p := range sum.DataPoints {
		counts[attrValue(p.Attributes, "variant")] = p.Value
	}

	if counts["agent_message_chunk"] != 2 {
		t.Fatalf("agent_message_chunk count = %d, want 2", counts["agent_message_chunk"])
	}

	if counts["tool_call"] != 1 {
		t.Fatalf("tool_call count = %d, want 1", counts["tool_call"])
	}
}

func TestRecordPermission_TracksOutcome(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	h.obs.RecordPermission(ctx, "selected:allow_once")
	h.obs.RecordPermission(ctx, "auto_reject")

	rm := h.collect(t)

	m := findMetric(rm, "permission.request")
	if m == nil {
		t.Fatalf("permission.request not found")
	}

	sum, ok := m.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("data = %T", m.Data)
	}

	if len(sum.DataPoints) != 2 {
		t.Fatalf("expected 2 distinct outcomes, got %d", len(sum.DataPoints))
	}
}

func TestRecordFsDelegation_OpAndOutcome(t *testing.T) {
	h := newHarness(t)

	h.obs.RecordFsDelegation(context.Background(), "write", "default_write")
	h.obs.RecordFsDelegation(context.Background(), "write", "handled")

	m := findMetric(h.collect(t), "fs.delegated")
	if m == nil {
		t.Fatalf("fs.delegated not found")
	}

	sum, ok := m.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("fs.delegated data = %T, want Sum[int64]", m.Data)
	}

	if len(sum.DataPoints) != 2 {
		t.Fatalf("expected 2 data points, got %d", len(sum.DataPoints))
	}
}

func TestRecordMCPBridge_TrackToolAndStatus(t *testing.T) {
	h := newHarness(t)

	h.obs.RecordMCPBridge(context.Background(), "sum", "ok")
	h.obs.RecordMCPBridge(context.Background(), "sum", "error")
	h.obs.RecordMCPBridge(context.Background(), "sum", "ok")

	m := findMetric(h.collect(t), "mcp_bridge.request")
	if m == nil {
		t.Fatalf("mcp_bridge.request not found")
	}

	sum, ok := m.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("mcp_bridge.request data = %T, want Sum[int64]", m.Data)
	}

	total := int64(0)
	for _, p := range sum.DataPoints {
		total += p.Value
	}

	if total != 3 {
		t.Fatalf("total requests = %d, want 3", total)
	}
}

func TestRecordToolCall_WithDurationRecordsBothMetrics(t *testing.T) {
	h := newHarness(t)

	h.obs.RecordToolCall(context.Background(), "Bash", "execute", "completed", 50*time.Millisecond)

	rm := h.collect(t)

	count := findMetric(rm, "tool_call.count")
	if count == nil {
		t.Fatalf("tool_call.count not found")
	}

	dur := findMetric(rm, "tool_call.duration")
	if dur == nil {
		t.Fatalf("tool_call.duration not found")
	}

	durHist, ok := dur.Data.(metricdata.Histogram[float64])
	if !ok {
		t.Fatalf("tool_call.duration data = %T, want Histogram[float64]", dur.Data)
	}

	if len(durHist.DataPoints) != 1 || durHist.DataPoints[0].Count != 1 {
		t.Fatalf("expected 1 duration point; got %+v", durHist.DataPoints)
	}

	if attrValue(durHist.DataPoints[0].Attributes, "tool_name") != "Bash" {
		t.Fatalf("missing tool_name attr on duration")
	}
}

func TestRecordToolCall_ZeroDurationSkipsHistogram(t *testing.T) {
	h := newHarness(t)

	h.obs.RecordToolCall(context.Background(), "Bash", "execute", "failed", 0)

	rm := h.collect(t)

	count := findMetric(rm, "tool_call.count")
	if count == nil {
		t.Fatalf("tool_call.count not found (should still record)")
	}

	dur := findMetric(rm, "tool_call.duration")
	if dur != nil {
		if hist, ok := dur.Data.(metricdata.Histogram[float64]); ok && len(hist.DataPoints) > 0 {
			t.Fatalf("expected no duration points for zero duration; got %d", len(hist.DataPoints))
		}
	}
}

func TestRecordCost_USDRecorded(t *testing.T) {
	h := newHarness(t)

	h.obs.RecordCost(context.Background(), 0.05, "USD", "anthropic/claude")
	h.obs.RecordCost(context.Background(), 0.10, "", "anthropic/claude") // empty currency also valid

	m := findMetric(h.collect(t), "cost.usd")
	if m == nil {
		t.Fatalf("cost.usd not found")
	}

	sum, ok := m.Data.(metricdata.Sum[float64])
	if !ok {
		t.Fatalf("cost.usd data = %T, want Sum[float64]", m.Data)
	}

	total := 0.0
	for _, p := range sum.DataPoints {
		total += p.Value
	}

	if diff := total - 0.15; diff < -1e-9 || diff > 1e-9 {
		t.Fatalf("total cost = %v, want ~0.15", total)
	}
}

func TestRecordCost_NonUSDIgnored(t *testing.T) {
	h := newHarness(t)

	h.obs.RecordCost(context.Background(), 1.00, "EUR", "x")

	m := findMetric(h.collect(t), "cost.usd")
	if m == nil {
		return // legitimately absent is fine
	}

	sum, ok := m.Data.(metricdata.Sum[float64])
	if !ok {
		t.Fatalf("cost.usd data = %T, want Sum[float64]", m.Data)
	}

	for _, p := range sum.DataPoints {
		if p.Value != 0 {
			t.Fatalf("EUR should not produce a non-zero USD point; got %v", p.Value)
		}
	}
}

func TestRecordCost_ZeroOrNegativeIgnored(t *testing.T) {
	h := newHarness(t)

	h.obs.RecordCost(context.Background(), 0, "USD", "x")
	h.obs.RecordCost(context.Background(), -1, "USD", "x")

	m := findMetric(h.collect(t), "cost.usd")
	if m == nil {
		return
	}

	sum, ok := m.Data.(metricdata.Sum[float64])
	if !ok {
		t.Fatalf("cost.usd data = %T, want Sum[float64]", m.Data)
	}

	for _, p := range sum.DataPoints {
		if p.Value != 0 {
			t.Fatalf("zero/negative should not produce a point; got %v", p.Value)
		}
	}
}

func TestRecordInitializeDuration_Outcome(t *testing.T) {
	h := newHarness(t)

	h.obs.RecordInitializeDuration(context.Background(), time.Second, true)
	h.obs.RecordInitializeDuration(context.Background(), 2*time.Second, false)

	m := findMetric(h.collect(t), "initialize.duration")
	if m == nil {
		t.Fatalf("initialize.duration not found")
	}

	hist, ok := m.Data.(metricdata.Histogram[float64])
	if !ok {
		t.Fatalf("initialize.duration data = %T, want Histogram[float64]", m.Data)
	}

	outcomes := map[string]int{}
	for _, p := range hist.DataPoints {
		outcomes[attrValue(p.Attributes, "outcome")]++
	}

	if outcomes["ok"] != 1 || outcomes["error"] != 1 {
		t.Fatalf("outcomes = %v, want ok=1 error=1", outcomes)
	}
}

func TestRecordCLISpawn_Outcome(t *testing.T) {
	h := newHarness(t)

	h.obs.RecordCLISpawn(context.Background(), "started")
	h.obs.RecordCLISpawn(context.Background(), "error")
	h.obs.RecordCLISpawn(context.Background(), "started")

	m := findMetric(h.collect(t), "cli.spawn")
	if m == nil {
		t.Fatalf("cli.spawn not found")
	}

	sum, ok := m.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("cli.spawn data = %T, want Sum[int64]", m.Data)
	}

	counts := map[string]int64{}
	for _, p := range sum.DataPoints {
		counts[attrValue(p.Attributes, "outcome")] = p.Value
	}

	if counts["started"] != 2 || counts["error"] != 1 {
		t.Fatalf("counts = %v, want started=2 error=1", counts)
	}
}

func TestStartPromptSpan_AttributesSet(t *testing.T) {
	h := newHarness(t)

	_, span := h.obs.StartPromptSpan(context.Background(), "ses_42",
		PromptLabels{Model: "anthropic/claude", Mode: "build"},
	)
	span.End()

	spans := h.recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	got := spans[0]
	if got.Name() != Namespace+".session.prompt" {
		t.Fatalf("span name = %q", got.Name())
	}

	attrs := got.Attributes()

	have := map[string]string{}
	for _, kv := range attrs {
		have[string(kv.Key)] = kv.Value.Emit()
	}

	if have["session.id"] != "ses_42" {
		t.Fatalf("session.id = %q", have["session.id"])
	}

	if have["model"] != "anthropic/claude" {
		t.Fatalf("model = %q", have["model"])
	}

	if have["mode"] != "build" {
		t.Fatalf("mode = %q", have["mode"])
	}
}

func TestStartPromptSpan_EmptyLabelsDropped(t *testing.T) {
	h := newHarness(t)

	_, span := h.obs.StartPromptSpan(context.Background(), "ses_1", PromptLabels{})
	span.End()

	s := h.recorder.Ended()[0]

	for _, kv := range s.Attributes() {
		if kv.Key == "model" || kv.Key == "mode" {
			t.Fatalf("empty %s label should not be emitted", kv.Key)
		}
	}
}

func TestStartInitializeSpan(t *testing.T) {
	h := newHarness(t)

	_, span := h.obs.StartInitializeSpan(context.Background())
	span.End()

	s := h.recorder.Ended()[0]
	if s.Name() != Namespace+".initialize" {
		t.Fatalf("name = %q", s.Name())
	}
}

func TestStartSubprocessSpan_PathAttribute(t *testing.T) {
	h := newHarness(t)

	_, span := h.obs.StartSubprocessSpan(context.Background(), "/usr/local/bin/opencode")
	span.End()

	s := h.recorder.Ended()[0]
	if s.Name() != Namespace+".subprocess" {
		t.Fatalf("name = %q", s.Name())
	}

	var path string

	for _, kv := range s.Attributes() {
		if kv.Key == "path" {
			path = kv.Value.Emit()
		}
	}

	if path != "/usr/local/bin/opencode" {
		t.Fatalf("path attr = %q", path)
	}
}

func TestObserver_Tracer_NotNil(t *testing.T) {
	h := newHarness(t)

	if h.obs.Tracer() == nil {
		t.Fatalf("Tracer() returned nil")
	}
}
