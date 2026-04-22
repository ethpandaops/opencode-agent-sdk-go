// Package bridge runs a loopback HTTP MCP server that exposes user-
// supplied in-process tools to opencode. opencodesdk.Client adds the
// bridge to every session/new's mcpServers list when WithSDKTools was
// configured.
package bridge

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// HandlerFunc is the tool-invocation handler the bridge invokes when
// opencode calls one of the registered tools. It receives the raw
// arguments as a map and returns a structured ToolOutput or an error.
type HandlerFunc func(ctx context.Context, input map[string]any) (*ToolOutput, error)

// InvocationRecorder receives a single observation for each tool call
// routed through the bridge. status is one of "ok", "error",
// "app_error" (the tool returned IsError=true). Callers can implement
// this with observability.Observer.RecordMCPBridge.
type InvocationRecorder interface {
	RecordMCPBridge(ctx context.Context, tool, status string)
}

// ToolOutput carries the bridge-friendly form of a tool's result. It
// decouples the bridge from opencodesdk's public Tool API so the
// bridge package has no cyclic dependencies.
type ToolOutput struct {
	Text       string
	Structured any
	IsError    bool
}

// ToolDef is the bridge-internal tool definition. opencodesdk maps its
// public Tool interface to this before passing to New.
type ToolDef struct {
	Name        string
	Description string
	Schema      map[string]any
	Handler     HandlerFunc
}

// Bridge is a running loopback HTTP MCP server.
type Bridge struct {
	mu         sync.Mutex
	logger     *slog.Logger
	httpServer *http.Server
	listener   net.Listener
	url        string
	token      string
	started    bool
	closed     bool
	recorder   InvocationRecorder
}

// New constructs a bridge configured with the supplied tools. The
// bridge is not listening until Start is called. If recorder is non-nil
// it is invoked once per tool call with (tool, status).
func New(tools []ToolDef, logger *slog.Logger, recorder InvocationRecorder) (*Bridge, error) {
	if logger == nil {
		return nil, errors.New("bridge: logger is required")
	}

	token, err := randomToken()
	if err != nil {
		return nil, fmt.Errorf("generating bearer token: %w", err)
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "opencodesdk-inproc",
		Version: "1",
	}, nil)

	for _, t := range tools {
		addTool(server, t, recorder)
	}

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, nil)

	b := &Bridge{
		logger:   logger.With(slog.String("component", "mcp-bridge")),
		token:    token,
		recorder: recorder,
	}

	// Bearer-auth middleware. opencode passes our token in the
	// Authorization header per the mcpServers entry we build below.
	mux := http.NewServeMux()
	mux.Handle("/mcp", requireBearer(token, handler))
	mux.Handle("/mcp/", requireBearer(token, handler))

	b.httpServer = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	return b, nil
}

// Start binds the HTTP server to 127.0.0.1:0 and begins serving.
// Safe to call at most once per Bridge.
func (b *Bridge) Start(_ context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.started {
		return errors.New("bridge: already started")
	}

	if b.closed {
		return errors.New("bridge: closed")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen on loopback: %w", err)
	}

	b.listener = ln
	b.url = "http://" + ln.Addr().String() + "/mcp"
	b.started = true

	go func() {
		if err := b.httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			b.logger.Error("mcp bridge server stopped", slog.Any("error", err))
		}
	}()

	b.logger.Info("mcp bridge listening", slog.String("url", b.url))

	return nil
}

// Close stops the HTTP server gracefully. Safe to call multiple times.
func (b *Bridge) Close(ctx context.Context) error {
	b.mu.Lock()

	if b.closed {
		b.mu.Unlock()

		return nil
	}

	b.closed = true
	server := b.httpServer
	b.mu.Unlock()

	if server == nil {
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return server.Shutdown(shutdownCtx)
}

// URL returns the MCP endpoint URL (e.g. http://127.0.0.1:4321/mcp).
// Only valid after Start succeeds.
func (b *Bridge) URL() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.url
}

// Token returns the bearer token clients must pass in the
// Authorization header. Only valid after New.
func (b *Bridge) Token() string {
	return b.token
}

// addTool registers a single ToolDef on the MCP server, wiring it up
// to invoke the caller-supplied HandlerFunc.
func addTool(server *mcp.Server, def ToolDef, recorder InvocationRecorder) {
	tool := &mcp.Tool{
		Name:        def.Name,
		Description: def.Description,
		InputSchema: def.Schema,
	}

	record := func(ctx context.Context, status string) {
		if recorder != nil {
			recorder.RecordMCPBridge(ctx, def.Name, status)
		}
	}

	server.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		input, err := coerceArguments(req.Params.Arguments)
		if err != nil {
			record(ctx, "error")
			//nolint:nilerr // application-level error surfaced via IsError
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
				IsError: true,
			}, nil
		}

		out, err := def.Handler(ctx, input)
		if err != nil {
			record(ctx, "error")
			//nolint:nilerr // application-level error surfaced via IsError
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
				IsError: true,
			}, nil
		}

		if out == nil {
			record(ctx, "ok")

			return &mcp.CallToolResult{}, nil
		}

		result := &mcp.CallToolResult{IsError: out.IsError}

		if out.Text != "" {
			result.Content = append(result.Content, &mcp.TextContent{Text: out.Text})
		}

		if out.Structured != nil {
			result.StructuredContent = out.Structured
		}

		if out.IsError {
			record(ctx, "app_error")
		} else {
			record(ctx, "ok")
		}

		return result, nil
	})
}

func coerceArguments(args any) (map[string]any, error) {
	switch v := args.(type) {
	case nil:
		return map[string]any{}, nil
	case map[string]any:
		return v, nil
	case json.RawMessage:
		m := map[string]any{}
		if len(v) == 0 {
			return m, nil
		}

		if err := json.Unmarshal(v, &m); err != nil {
			return nil, fmt.Errorf("unmarshal arguments: %w", err)
		}

		return m, nil
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("re-marshal arguments: %w", err)
		}

		m := map[string]any{}
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("unmarshal arguments: %w", err)
		}

		return m, nil
	}
}

func requireBearer(token string, next http.Handler) http.Handler {
	expected := "Bearer " + token

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != expected {
			http.Error(w, "unauthorized", http.StatusUnauthorized)

			return
		}

		next.ServeHTTP(w, r)
	})
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}
