// Example contrib_prometheus demonstrates building a Prometheus-backed
// OTel MeterProvider via the contrib/prometheus package and passing it
// explicitly through WithMeterProvider. This differs from the sibling
// prometheus_metrics example in that the caller owns the provider —
// useful when you want to merge SDK metrics with your own OTel
// instrumentation, or pass the same MeterProvider to multiple
// Clients / SDKs.
//
// Run:
//
//	go run ./examples/contrib_prometheus
//
// Then open http://localhost:9091/metrics in another terminal.
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
	contribprom "github.com/ethpandaops/opencode-agent-sdk-go/contrib/prometheus"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	reg := prometheus.NewRegistry()

	// Build the MeterProvider explicitly so you can pass it to the
	// SDK AND to any other OTel-instrumented code running in-process.
	mp, err := contribprom.NewMeterProvider(reg)
	if err != nil {
		exitf("NewMeterProvider: %v", err)
	}

	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))

		fmt.Println("Prometheus metrics available at http://localhost:9091/metrics")

		server := &http.Server{
			Addr:              ":9091",
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

	res, err := opencodesdk.Query(ctx, "Reply with a short one-liner.",
		opencodesdk.WithLogger(logger),
		opencodesdk.WithCwd(cwd),
		opencodesdk.WithMeterProvider(mp),
	)
	if err != nil {
		exitf("Query: %v", err)
	}

	fmt.Printf("\nstop reason: %s\n", res.StopReason)

	if res.Usage != nil {
		fmt.Printf("tokens: %d\n", res.Usage.TotalTokens)
	}

	fmt.Println("\nMetrics available at http://localhost:9091/metrics. Ctrl+C to exit.")

	select {}
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
