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

// Observer bundles the SDK's OTel instruments.
type Observer struct {
	tracer trace.Tracer

	promptDuration metric.Float64Histogram
	promptTokens   metric.Float64Histogram
	sessionUpdate  metric.Int64Counter
	mcpBridge      metric.Int64Counter
	permissionReq  metric.Int64Counter
	fsDelegated    metric.Int64Counter
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
		metric.WithDescription("Inbound MCP bridge tool invocations, by status"),
	)

	permissionReq, _ := meter.Int64Counter(
		Namespace+".permission.request",
		metric.WithDescription("session/request_permission callbacks, by outcome"),
	)

	fsDelegated, _ := meter.Int64Counter(
		Namespace+".fs.delegated",
		metric.WithDescription("fs/read_text_file and fs/write_text_file delegations, by op + outcome"),
	)

	return &Observer{
		tracer:         tracer,
		promptDuration: promptDuration,
		promptTokens:   promptTokens,
		sessionUpdate:  sessionUpdate,
		mcpBridge:      mcpBridge,
		permissionReq:  permissionReq,
		fsDelegated:    fsDelegated,
	}
}

// Tracer returns the SDK's OTel tracer.
func (o *Observer) Tracer() trace.Tracer { return o.tracer }

// StartPromptSpan opens a span covering one session/prompt turn.
func (o *Observer) StartPromptSpan(ctx context.Context, sessionID string) (context.Context, trace.Span) {
	return o.tracer.Start(ctx, Namespace+".session.prompt",
		trace.WithAttributes(attribute.String("session.id", sessionID)),
	)
}

// RecordPrompt records duration + token usage for a completed prompt.
func (o *Observer) RecordPrompt(ctx context.Context, duration time.Duration, stopReason string, tokens TokenCounts) {
	attrs := metric.WithAttributes(attribute.String("stop_reason", stopReason))
	o.promptDuration.Record(ctx, duration.Seconds(), attrs)

	if tokens.Input > 0 {
		o.promptTokens.Record(ctx, float64(tokens.Input),
			metric.WithAttributes(attribute.String("direction", "input")),
		)
	}

	if tokens.Output > 0 {
		o.promptTokens.Record(ctx, float64(tokens.Output),
			metric.WithAttributes(attribute.String("direction", "output")),
		)
	}

	if tokens.CachedRead > 0 {
		o.promptTokens.Record(ctx, float64(tokens.CachedRead),
			metric.WithAttributes(attribute.String("direction", "cached_read")),
		)
	}
}

// RecordSessionUpdate increments the session/update counter for a
// given variant (e.g. "agent_message_chunk", "tool_call", "usage_update").
func (o *Observer) RecordSessionUpdate(ctx context.Context, variant string) {
	o.sessionUpdate.Add(ctx, 1, metric.WithAttributes(attribute.String("variant", variant)))
}

// RecordMCPBridge increments the bridge counter.
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

// TokenCounts is the prompt-turn token accounting structure the
// Observer records.
type TokenCounts struct {
	Input      int64
	Output     int64
	CachedRead int64
}
