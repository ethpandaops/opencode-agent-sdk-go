// Example prometheus_metrics demonstrates how to expose opencodesdk's
// OTel metrics via a Prometheus /metrics endpoint using the SDK's
// built-in WithPrometheusRegisterer option. The SDK wires an OTel
// MeterProvider to the supplied registry and emits every
// opencodesdk.* metric through the Prometheus bridge.
//
// Run:
//
//	go run ./examples/prometheus_metrics
//
// Then open http://localhost:9090/metrics in another terminal.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	reg := prometheus.NewRegistry()

	// Serve /metrics in the background.
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))

		fmt.Println("Prometheus metrics available at http://localhost:9090/metrics")

		server := &http.Server{
			Addr:              ":9090",
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		}

		if serveErr := server.ListenAndServe(); serveErr != nil && serveErr != http.ErrServerClosed {
			fmt.Printf("metrics server error: %v\n", serveErr)
		}
	}()

	cwd, _ := os.Getwd()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// Run a single one-shot Query with the registerer attached. The SDK
	// builds a Prometheus-backed MeterProvider internally and records
	// initialize duration, session lifecycle counters, permission
	// outcomes, update-drops, and subprocess spans into reg.
	res, err := opencodesdk.Query(ctx, "Reply with one short sentence introducing yourself.",
		opencodesdk.WithLogger(logger),
		opencodesdk.WithCwd(cwd),
		opencodesdk.WithPrometheusRegisterer(reg),
	)
	if err != nil {
		exitf("Query: %v", err)
	}

	fmt.Printf("\nquery stop reason: %s\n", res.StopReason)

	if res.Usage != nil {
		fmt.Printf("tokens: %d\n", res.Usage.TotalTokens)
	}

	fmt.Println("\nQuery complete. Metrics are available at http://localhost:9090/metrics")
	fmt.Println("Press Ctrl+C to exit.")

	select {}
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
