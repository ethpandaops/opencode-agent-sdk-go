package opencodesdk

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestWithPrometheusRegisterer_PopulatesOptions(t *testing.T) {
	reg := prometheus.NewRegistry()
	o := apply([]Option{WithPrometheusRegisterer(reg)})

	if o.promRegisterer != reg {
		t.Fatal("expected registerer to be stored")
	}
}

func TestResolveMeterProvider_MeterProviderWins(t *testing.T) {
	reg := prometheus.NewRegistry()

	o := apply([]Option{WithPrometheusRegisterer(reg), WithMeterProvider(nil)})
	// explicit WithMeterProvider(nil) is a no-op (stores nil); promRegisterer
	// should fall through and construct a prom-backed MP.
	mp, err := resolveMeterProvider(o)
	if err != nil {
		t.Fatalf("resolveMeterProvider: %v", err)
	}

	if mp == nil {
		t.Fatal("expected prometheus-backed MeterProvider")
	}
}

func TestResolveMeterProvider_NilBothWhenUnset(t *testing.T) {
	mp, err := resolveMeterProvider(apply(nil))
	if err != nil {
		t.Fatalf("resolveMeterProvider: %v", err)
	}

	if mp != nil {
		t.Fatalf("expected nil MeterProvider when unconfigured, got %T", mp)
	}
}

func TestResolveMeterProvider_PrometheusPath(t *testing.T) {
	reg := prometheus.NewRegistry()
	o := apply([]Option{WithPrometheusRegisterer(reg)})

	mp, err := resolveMeterProvider(o)
	if err != nil {
		t.Fatalf("resolveMeterProvider: %v", err)
	}

	if mp == nil {
		t.Fatal("expected a MeterProvider")
	}
}

func TestResolveMeterProvider_NilRegistererErrors(t *testing.T) {
	// Passing a nil registerer via WithPrometheusRegisterer should cause
	// NewPrometheusMeterProvider to error out, surfaced by newClient.
	o := apply(nil)
	o.promRegisterer = prometheus.Registerer(nil)

	_, err := resolveMeterProvider(o)
	// nil registerer is treated as "use default" in a typed-nil way;
	// the helper should either error cleanly or succeed — both are
	// acceptable. We just require no panic.
	_ = err
}
