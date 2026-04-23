package prometheus

import (
	"testing"

	prom "github.com/prometheus/client_golang/prometheus"
)

func TestNewMeterProvider(t *testing.T) {
	t.Parallel()

	reg := prom.NewRegistry()

	mp, err := NewMeterProvider(reg)
	if err != nil {
		t.Fatalf("NewMeterProvider: %v", err)
	}

	if mp == nil {
		t.Fatalf("NewMeterProvider returned nil provider")
	}

	// Verify it can create a meter and instruments without panicking.
	meter := mp.Meter("contrib-prometheus-test")
	if meter == nil {
		t.Fatal("Meter returned nil")
	}

	counter, err := meter.Int64Counter("test.counter")
	if err != nil {
		t.Fatalf("create counter: %v", err)
	}

	// Exercise the counter so the Prometheus exporter records a sample.
	counter.Add(t.Context(), 1)
}

func TestNewMeterProvider_NilRegistererErrors(t *testing.T) {
	t.Parallel()

	if _, err := NewMeterProvider(nil); err == nil {
		t.Fatalf("NewMeterProvider(nil) should error")
	}
}
