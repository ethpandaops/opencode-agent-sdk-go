// Package observability defines the OTel metric and span instruments
// the SDK records. Instruments are created lazily from the supplied
// MeterProvider / TracerProvider; when those are nil the instruments
// resolve to noops via the otel API defaults, so instrumentation is
// always zero-cost when unused.
package observability

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Namespace is the prefix for all SDK-emitted metric and span names.
const Namespace = "opencodesdk"

// Observer bundles the SDK's OTel instruments. A nil Observer is not
// valid — always construct through NewObserver.
type Observer struct {
	tracer trace.Tracer

	promptDuration     metric.Float64Histogram
	promptTokens       metric.Float64Histogram
	sessionUpdate      metric.Int64Counter
	mcpBridge          metric.Int64Counter
	permissionReq      metric.Int64Counter
	fsDelegated        metric.Int64Counter
	toolCallDuration   metric.Float64Histogram
	toolCallCount      metric.Int64Counter
	costUSD            metric.Float64Counter
	initializeDuration metric.Float64Histogram
	cliSpawn           metric.Int64Counter
}

// NewObserver constructs an Observer. Either provider may be nil; in
// that case otel's global providers are used (which default to noops).
func NewObserver(mp metric.MeterProvider, tp trace.TracerProvider) *Observer {
	if mp == nil {
		mp = otel.GetMeterProvider()
	}

	if tp == nil {
		tp = otel.GetTracerProvider()
	}

	meter := mp.Meter(Namespace)
	tracer := tp.Tracer(Namespace)

	// All With* errors on meter instruments are ignored: in practice
	// they only occur with invalid names, which are compile-time
	// constants here. Noop meters never return errors.
	promptDuration, _ := meter.Float64Histogram(
		Namespace+".session.prompt.duration",
		metric.WithDescription("Duration of a session/prompt turn"),
		metric.WithUnit("s"),
	)

	promptTokens, _ := meter.Float64Histogram(
		Namespace+".session.prompt.tokens",
		metric.WithDescription("Token usage per prompt turn, bucketed by direction"),
	)

	sessionUpdate, _ := meter.Int64Counter(
		Namespace+".session.update.count",
		metric.WithDescription("session/update notifications received, by variant"),
	)

	mcpBridge, _ := meter.Int64Counter(
		Namespace+".mcp_bridge.request",
		metric.WithDescription("Inbound MCP bridge tool invocations, by tool + status"),
	)

	permissionReq, _ := meter.Int64Counter(
		Namespace+".permission.request",
		metric.WithDescription("session/request_permission callbacks, by outcome"),
	)

	fsDelegated, _ := meter.Int64Counter(
		Namespace+".fs.delegated",
		metric.WithDescription("fs/read_text_file and fs/write_text_file delegations, by op + outcome"),
	)

	toolCallDuration, _ := meter.Float64Histogram(
		Namespace+".tool_call.duration",
		metric.WithDescription("Duration of a tool call, start→terminal status"),
		metric.WithUnit("s"),
	)

	toolCallCount, _ := meter.Int64Counter(
		Namespace+".tool_call.count",
		metric.WithDescription("Tool call invocations, by tool_name + status"),
	)

	costUSD, _ := meter.Float64Counter(
		Namespace+".cost.usd",
		metric.WithDescription("Cumulative monetary cost reported via usage_update (USD)"),
		metric.WithUnit("USD"),
	)

	initializeDuration, _ := meter.Float64Histogram(
		Namespace+".initialize.duration",
		metric.WithDescription("Duration of the ACP initialize handshake"),
		metric.WithUnit("s"),
	)

	cliSpawn, _ := meter.Int64Counter(
		Namespace+".cli.spawn",
		metric.WithDescription("opencode CLI subprocess spawn events, by outcome"),
	)

	return &Observer{
		tracer:             tracer,
		promptDuration:     promptDuration,
		promptTokens:       promptTokens,
		sessionUpdate:      sessionUpdate,
		mcpBridge:          mcpBridge,
		permissionReq:      permissionReq,
		fsDelegated:        fsDelegated,
		toolCallDuration:   toolCallDuration,
		toolCallCount:      toolCallCount,
		costUSD:            costUSD,
		initializeDuration: initializeDuration,
		cliSpawn:           cliSpawn,
	}
}

// Tracer returns the SDK's OTel tracer.
func (o *Observer) Tracer() trace.Tracer { return o.tracer }

// PromptLabels describes the optional per-session attributes attached
// to prompt-scoped metrics. Empty strings are dropped so sessions
// without a configured model/mode don't emit empty labels.
type PromptLabels struct {
	Model string
	Mode  string
}

// StartPromptSpan opens a span covering one session/prompt turn.
func (o *Observer) StartPromptSpan(ctx context.Context, sessionID string, labels PromptLabels) (context.Context, trace.Span) {
	attrs := []attribute.KeyValue{attribute.String("session.id", sessionID)}
	if labels.Model != "" {
		attrs = append(attrs, attribute.String("model", labels.Model))
	}

	if labels.Mode != "" {
		attrs = append(attrs, attribute.String("mode", labels.Mode))
	}

	return o.tracer.Start(ctx, Namespace+".session.prompt", trace.WithAttributes(attrs...))
}

// StartInitializeSpan opens a span covering the ACP initialize handshake.
func (o *Observer) StartInitializeSpan(ctx context.Context) (context.Context, trace.Span) {
	return o.tracer.Start(ctx, Namespace+".initialize")
}

// RecordInitializeDuration records the initialize handshake duration.
func (o *Observer) RecordInitializeDuration(ctx context.Context, d time.Duration, success bool) {
	outcome := "ok"
	if !success {
		outcome = "error"
	}

	o.initializeDuration.Record(ctx, d.Seconds(),
		metric.WithAttributes(attribute.String("outcome", outcome)),
	)
}

// StartSubprocessSpan opens a span covering the opencode subprocess's
// lifetime. The caller is expected to keep the span open until the
// subprocess exits; End the span at shutdown.
func (o *Observer) StartSubprocessSpan(ctx context.Context, path string) (context.Context, trace.Span) {
	return o.tracer.Start(ctx, Namespace+".subprocess",
		trace.WithAttributes(attribute.String("path", path)),
	)
}

// RecordCLISpawn increments the CLI spawn counter with an outcome
// label ("started" on successful launch, "error" otherwise).
func (o *Observer) RecordCLISpawn(ctx context.Context, outcome string) {
	o.cliSpawn.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
}

// RecordPrompt records duration + token usage for a completed prompt.
func (o *Observer) RecordPrompt(ctx context.Context, duration time.Duration, stopReason string, tokens TokenCounts, labels PromptLabels) {
	attrs := []attribute.KeyValue{attribute.String("stop_reason", stopReason)}
	if labels.Model != "" {
		attrs = append(attrs, attribute.String("model", labels.Model))
	}

	if labels.Mode != "" {
		attrs = append(attrs, attribute.String("mode", labels.Mode))
	}

	o.promptDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))

	modelAttr := attribute.String("model", labels.Model)

	record := func(direction string, value int64) {
		if value <= 0 {
			return
		}

		attrs := []attribute.KeyValue{attribute.String("direction", direction)}
		if labels.Model != "" {
			attrs = append(attrs, modelAttr)
		}

		o.promptTokens.Record(ctx, float64(value), metric.WithAttributes(attrs...))
	}

	record("input", tokens.Input)
	record("output", tokens.Output)
	record("cached_read", tokens.CachedRead)
	record("cached_write", tokens.CachedWrite)
	record("thought", tokens.Thought)
}

// RecordSessionUpdate increments the session/update counter for a
// given variant (e.g. "agent_message_chunk", "tool_call", "usage_update").
func (o *Observer) RecordSessionUpdate(ctx context.Context, variant string) {
	o.sessionUpdate.Add(ctx, 1, metric.WithAttributes(attribute.String("variant", variant)))
}

// RecordMCPBridge increments the bridge counter for one inbound tool
// invocation routed through the loopback HTTP MCP bridge.
func (o *Observer) RecordMCPBridge(ctx context.Context, tool, status string) {
	o.mcpBridge.Add(ctx, 1, metric.WithAttributes(
		attribute.String("tool", tool),
		attribute.String("status", status),
	))
}

// RecordPermission records a permission callback outcome.
func (o *Observer) RecordPermission(ctx context.Context, outcome string) {
	o.permissionReq.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
}

// RecordFsDelegation records an fs/read|write delegation.
func (o *Observer) RecordFsDelegation(ctx context.Context, op, outcome string) {
	o.fsDelegated.Add(ctx, 1, metric.WithAttributes(
		attribute.String("op", op),
		attribute.String("outcome", outcome),
	))
}

// RecordToolCall records one tool-call terminal event (a ToolCallUpdate
// carrying a terminal status, or a ToolCall arriving already in a
// terminal state). duration may be zero if the SDK never observed the
// start edge.
func (o *Observer) RecordToolCall(ctx context.Context, name, kind, status string, duration time.Duration) {
	attrs := []attribute.KeyValue{
		attribute.String("tool_name", name),
		attribute.String("status", status),
	}
	if kind != "" {
		attrs = append(attrs, attribute.String("tool_kind", kind))
	}

	o.toolCallCount.Add(ctx, 1, metric.WithAttributes(attrs...))

	if duration > 0 {
		o.toolCallDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
	}
}

// RecordCost adds a USD-denominated cost delta to the cumulative cost
// counter. Non-USD currencies are ignored with no metric emission.
func (o *Observer) RecordCost(ctx context.Context, amount float64, currency, model string) {
	if amount <= 0 {
		return
	}

	if currency != "" && currency != "USD" {
		return
	}

	attrs := []attribute.KeyValue{}
	if model != "" {
		attrs = append(attrs, attribute.String("model", model))
	}

	o.costUSD.Add(ctx, amount, metric.WithAttributes(attrs...))
}

// TokenCounts is the prompt-turn token accounting structure the
// Observer records.
type TokenCounts struct {
	Input       int64
	Output      int64
	CachedRead  int64
	CachedWrite int64
	Thought     int64
}
