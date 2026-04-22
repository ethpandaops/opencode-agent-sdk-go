// Package prometheus provides a convenience helper for wiring
// Prometheus metrics into the opencode Agent SDK without pulling the
// OTel SDK's Prometheus exporter dependency into every consumer that
// only wants OTel OTLP export.
//
// Usage:
//
//	reg := prometheus.NewRegistry()
//	mp, err := contribprom.NewMeterProvider(reg)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	client, _ := opencodesdk.NewClient(opencodesdk.WithMeterProvider(mp))
//
// Callers scrape the registerer from their HTTP handler with
// promhttp.HandlerFor. The returned MeterProvider uses the official
// go.opentelemetry.io/otel/exporters/prometheus bridge with the same
// settings the SDK applies when WithPrometheusRegisterer is used
// directly; the only difference is that this helper hands you the
// provider explicitly, so you can combine it with other OTel metrics
// or pass it to NewClient alongside a custom TracerProvider.
package prometheus

import (
	prom "github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/metric"

	"github.com/ethpandaops/opencode-agent-sdk-go/internal/observability"
)

// NewMeterProvider creates an OTel MeterProvider backed by the given
// Prometheus registerer. The returned provider can be passed to
// opencodesdk.WithMeterProvider.
//
// Returns an error if reg is nil or if the underlying Prometheus
// exporter fails to register its meter reader.
func NewMeterProvider(reg prom.Registerer) (metric.MeterProvider, error) {
	return observability.NewPrometheusMeterProvider(reg)
}
