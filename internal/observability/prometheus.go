package observability

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// NewPrometheusMeterProvider wires an OTel MeterProvider to the
// supplied Prometheus registerer via the official
// go.opentelemetry.io/otel/exporters/prometheus bridge. Callers scrape
// the registerer from their HTTP handler as usual.
func NewPrometheusMeterProvider(reg prometheus.Registerer) (metric.MeterProvider, error) {
	if reg == nil {
		return nil, fmt.Errorf("opencodesdk/observability: nil prometheus registerer")
	}

	exporter, err := otelprom.New(otelprom.WithRegisterer(reg))
	if err != nil {
		return nil, fmt.Errorf("opencodesdk/observability: create prometheus exporter: %w", err)
	}

	return sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter)), nil
}
