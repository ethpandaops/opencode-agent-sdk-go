// Demonstrates opencode-specific model variant handling:
// OpencodeVariant, Session.CurrentVariant, and Client.UnstableSetModel.
// opencode exposes reasoning-effort variants (e.g. "high", "max") under
// a base model id via _meta.opencode.variant. These are not part of
// stable ACP — they live under Session.Meta() and the typed
// CurrentVariant accessor.
//
//	go run ./examples/model_variant
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	cwd, _ := os.Getwd()

	c, err := opencodesdk.NewClient(
		opencodesdk.WithLogger(logger),
		opencodesdk.WithCwd(cwd),
	)
	if err != nil {
		exitf("NewClient: %v", err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if startErr := c.Start(ctx); startErr != nil {
		exitf("Start: %v", startErr)
	}

	sess, err := c.NewSession(ctx)
	if err != nil {
		exitf("NewSession: %v", err)
	}

	go func() {
		for range sess.Updates() {
		}
	}()

	fmt.Println("== session variant snapshot ==")

	variant := sess.CurrentVariant()
	if variant == nil {
		// Fall back to the raw meta accessor + typed helper in case
		// CurrentVariant() cached a nil.
		if v, ok := opencodesdk.OpencodeVariant(sess.Meta()); ok {
			variant = v
		}
	}

	if variant == nil {
		fmt.Println("no _meta.opencode.variant on this session — the agent")
		fmt.Println("probably does not expose variant info. Nothing more to show.")

		return
	}

	fmt.Printf("model:     %s\n", variant.ModelId)
	fmt.Printf("variant:   %q\n", variant.Variant)
	fmt.Printf("available: %s\n", strings.Join(variant.AvailableVariants, ", "))

	// Pick an alternate variant different from the current one and switch
	// via the unstable RPC. Most common opencode models expose "default",
	// "high", and/or "max".
	alt := pickAlternate(variant.AvailableVariants, variant.Variant)
	if alt == "" {
		fmt.Println("\nno alternate variant available; skipping switch.")

		return
	}

	target := variant.ModelId + "/" + alt
	fmt.Printf("\n== UnstableSetModel → %s ==\n", target)

	if setErr := c.UnstableSetModel(ctx, sess.ID(), target); setErr != nil {
		fmt.Printf("UnstableSetModel failed (non-fatal): %v\n", setErr)

		return
	}

	// CurrentVariant() refreshes from the session/set_model response,
	// so after UnstableSetModel it reflects the newly applied variant.
	if updated := sess.CurrentVariant(); updated != nil {
		fmt.Printf("switched %s to variant %q (CurrentVariant now reports %q).\n",
			variant.ModelId, alt, updated.Variant)

		return
	}

	fmt.Printf("switched %s to variant %q.\n", variant.ModelId, alt)
}

func pickAlternate(opts []string, current string) string {
	for _, v := range opts {
		if v != current && v != "" {
			return v
		}
	}

	return ""
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
