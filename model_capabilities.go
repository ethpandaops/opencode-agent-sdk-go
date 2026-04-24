package opencodesdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"time"

	"github.com/ethpandaops/opencode-agent-sdk-go/internal/cli"
	"github.com/ethpandaops/opencode-agent-sdk-go/internal/serve"
)

// ModelCapabilities describes per-model feature flags as opencode
// computes them (from models.dev metadata for known providers, from
// `opencode.json` for custom providers). ACP's model catalogue does
// not transmit these; [ListModelCapabilities] fetches them via
// `opencode serve`'s HTTP API.
type ModelCapabilities struct {
	// ProviderID is the provider key ("lmstudio", "anthropic", …).
	ProviderID string

	// ModelID is the model key within the provider ("qwen/qwen3.6-27b").
	ModelID string

	// Name is the human-readable model name opencode advertises.
	Name string

	// Temperature reports whether the model honors a sampling-temperature
	// parameter.
	Temperature bool

	// Reasoning reports whether opencode will request a reasoning /
	// thinking channel from this model and surface the chunks as
	// [UpdateHandlers.AgentThought] notifications.
	Reasoning bool

	// Toolcall reports whether the model supports native function /
	// tool calling.
	Toolcall bool

	// Attachment reports whether prompts to this model may include
	// non-text content blocks (image / audio / file).
	Attachment bool

	// Interleaved reports whether the model supports interleaved
	// thinking (reasoning chunks alternating with content chunks).
	Interleaved bool

	// Input is the set of modalities the model accepts.
	Input ModalityCapabilities

	// Output is the set of modalities the model emits.
	Output ModalityCapabilities

	// ContextLimit is the total token window, or 0 when unknown.
	ContextLimit int

	// OutputLimit is the max tokens per response, or 0 when unknown.
	OutputLimit int
}

// ModalityCapabilities enumerates the content kinds a model can handle
// on a single side (input or output).
type ModalityCapabilities struct {
	Text, Audio, Image, Video, PDF bool
}

// ListModelCapabilities enumerates every model opencode knows about and
// returns its capability flags keyed by "<providerID>/<modelID>" —
// the same form accepted by [WithModel].
//
// Under the hood this spawns a throw-away `opencode serve` subprocess
// on a loopback ephemeral port, calls its `GET /config/providers`
// endpoint, and tears the server down. All [Option] values are
// honoured where meaningful (logger, cli path, env, cwd,
// opencode-home) — session-only options such as WithModel or
// WithEffort are ignored.
//
// Returns an empty map (nil error) when opencode reports no providers.
func ListModelCapabilities(ctx context.Context, opts ...Option) (map[string]ModelCapabilities, error) {
	o := apply(opts)

	path, _, err := (&cli.Discoverer{
		Path:             o.cliPath,
		SkipVersionCheck: o.skipVersionCheck,
		MinimumVersion:   MinimumCLIVersion,
		Logger:           o.logger,
	}).Discover(ctx)
	if err != nil {
		switch {
		case errors.Is(err, cli.ErrNotFound):
			searched := []string{"$PATH"}
			if o.cliPath != "" {
				searched = []string{o.cliPath}
			}

			return nil, &CLINotFoundError{SearchedPaths: searched, Err: err}
		case errors.Is(err, cli.ErrUnsupportedVersion):
			return nil, fmt.Errorf("%w: %v", ErrUnsupportedCLIVersion, err)
		default:
			return nil, fmt.Errorf("discovering opencode CLI: %w", err)
		}
	}

	proc, err := serve.Spawn(ctx, serve.Config{
		Path:   path,
		Env:    serveEnv(o),
		Cwd:    o.cwd,
		Logger: o.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("spawning opencode serve: %w", err)
	}

	defer func() {
		if closeErr := proc.Close(); closeErr != nil {
			o.logger.WarnContext(ctx, "closing opencode serve", slog.Any("error", closeErr))
		}
	}()

	return fetchModelCapabilities(ctx, proc.BaseURL())
}

// serveEnv mirrors subprocessEnv but is scoped to the serve subprocess.
func serveEnv(o *options) map[string]string {
	if o.opencodeHome == "" {
		return o.env
	}

	out := make(map[string]string, len(o.env)+1)
	maps.Copy(out, o.env)

	if _, ok := out["XDG_DATA_HOME"]; !ok {
		out["XDG_DATA_HOME"] = o.opencodeHome
	}

	return out
}

// providersResponse matches opencode's /config/providers JSON.
// Only the fields this SDK exposes are modelled; unknown keys are
// ignored so the decoder survives new opencode releases.
type providersResponse struct {
	Providers []providerEntry `json:"providers"`
}

type providerEntry struct {
	ID     string                `json:"id"`
	Models map[string]modelEntry `json:"models"`
}

type modelEntry struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Capabilities capabilitiesBlock `json:"capabilities"`
	Limit        limitBlock        `json:"limit"`
}

type capabilitiesBlock struct {
	Temperature bool          `json:"temperature"`
	Reasoning   bool          `json:"reasoning"`
	Toolcall    bool          `json:"toolcall"`
	Attachment  bool          `json:"attachment"`
	Interleaved flexBool      `json:"interleaved"`
	Input       modalityBlock `json:"input"`
	Output      modalityBlock `json:"output"`
}

// flexBool decodes a field that opencode reports as either a boolean
// ("interleaved": false) or an object carrying provider-specific
// details ("interleaved": {"field": "reasoning_details"}). Presence of
// any object value is treated as true; we don't currently surface the
// inner detail fields on [ModelCapabilities].
type flexBool bool

func (f *flexBool) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*f = false

		return nil
	}

	switch data[0] {
	case 't':
		*f = true

		return nil
	case 'f':
		*f = false

		return nil
	case '{':
		*f = true

		return nil
	default:
		return fmt.Errorf("flexBool: unexpected JSON %q", data)
	}
}

type modalityBlock struct {
	Text  bool `json:"text"`
	Audio bool `json:"audio"`
	Image bool `json:"image"`
	Video bool `json:"video"`
	PDF   bool `json:"pdf"`
}

type limitBlock struct {
	Context int `json:"context"`
	Output  int `json:"output"`
}

func fetchModelCapabilities(ctx context.Context, baseURL string) (map[string]ModelCapabilities, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/config/providers", nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	// 30s covers the worst-case cold start where opencode is still
	// populating its model catalogue from models.dev.
	hc := &http.Client{Timeout: 30 * time.Second}

	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET /config/providers: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))

		return nil, fmt.Errorf("GET /config/providers: %s: %s", resp.Status, body)
	}

	var parsed providersResponse

	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decoding /config/providers: %w", err)
	}

	out := make(map[string]ModelCapabilities)

	for _, p := range parsed.Providers {
		for mk, m := range p.Models {
			key := p.ID + "/" + mk
			out[key] = ModelCapabilities{
				ProviderID:   p.ID,
				ModelID:      mk,
				Name:         m.Name,
				Temperature:  m.Capabilities.Temperature,
				Reasoning:    m.Capabilities.Reasoning,
				Toolcall:     m.Capabilities.Toolcall,
				Attachment:   m.Capabilities.Attachment,
				Interleaved:  bool(m.Capabilities.Interleaved),
				Input:        toModality(m.Capabilities.Input),
				Output:       toModality(m.Capabilities.Output),
				ContextLimit: m.Limit.Context,
				OutputLimit:  m.Limit.Output,
			}
		}
	}

	return out, nil
}

func toModality(m modalityBlock) ModalityCapabilities {
	return ModalityCapabilities(m)
}
