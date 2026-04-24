// Package serve manages a short-lived `opencode serve` subprocess and
// exposes the base URL of its HTTP API.
//
// `opencode serve` is a distinct transport from `opencode acp`: it runs
// an HTTP/OpenAPI 3.1 server instead of stdio JSON-RPC. The SDK's
// primary transport is ACP, but a few pieces of metadata — notably
// per-model capability flags (reasoning, tool_call, attachment, …) —
// are only exposed over the HTTP surface. This package lets the SDK
// fetch that metadata on demand without altering the ACP lifecycle.
package serve

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"sync"
	"time"
)

// Config configures the opencode serve subprocess.
type Config struct {
	// Path is the resolved path to the opencode binary.
	Path string
	// Env overlays the inherited environment. nil means inherit
	// os.Environ() unchanged.
	Env map[string]string
	// Cwd is the working directory for the subprocess. Empty defers to
	// the caller's cwd.
	Cwd string
	// Logger receives diagnostics. Must not be nil.
	Logger *slog.Logger
	// ReadyTimeout caps how long Spawn waits for the server to announce
	// its listening URL. Zero defaults to 10s.
	ReadyTimeout time.Duration
}

// Process is a running `opencode serve` subprocess with a reachable
// HTTP endpoint.
type Process struct {
	cmd    *exec.Cmd
	stderr io.ReadCloser
	stdout io.ReadCloser
	logger *slog.Logger
	url    string

	closeOnce sync.Once
	closeErr  error
	waitErr   chan error
}

// BaseURL returns the HTTP base URL the subprocess is listening on,
// e.g. "http://127.0.0.1:41023". Safe to call after Spawn returns nil.
func (p *Process) BaseURL() string { return p.url }

// ErrServerNotReady is returned when the serve subprocess exits or
// fails to announce a listening URL within the configured timeout.
var ErrServerNotReady = errors.New("opencode serve did not become ready")

var listeningRE = regexp.MustCompile(`listening on (https?://[^\s]+)`)

// Spawn launches `opencode serve` on an ephemeral loopback port,
// waits for the "listening on …" banner, and returns a Process that
// owns the subprocess lifetime. Callers MUST call Close.
//
// Port allocation: Spawn picks a free 127.0.0.1 port by briefly
// binding a net.Listener on :0, then hands the port to opencode via
// --port. The window between our Close and opencode's bind is racy in
// principle; in practice it is microseconds and the fallback is a
// clear "address in use" error from opencode that surfaces as
// ErrServerNotReady.
func Spawn(ctx context.Context, cfg Config) (*Process, error) {
	if cfg.Logger == nil {
		return nil, errors.New("serve: Config.Logger is required")
	}

	if cfg.Path == "" {
		return nil, errors.New("serve: Config.Path is required")
	}

	timeout := cfg.ReadyTimeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	port, err := pickFreePort()
	if err != nil {
		return nil, fmt.Errorf("picking free port: %w", err)
	}

	args := []string{"serve", "--hostname", "127.0.0.1", "--port", strconv.Itoa(port)}

	// #nosec G204 -- path resolved by cli.Discoverer, args fully SDK-owned.
	cmd := exec.CommandContext(ctx, cfg.Path, args...)
	cmd.Env = buildEnv(cfg.Env)

	if cfg.Cwd != "" {
		cmd.Dir = cfg.Cwd
	}

	configureProcessGroup(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdout.Close()

		return nil, fmt.Errorf("creating stderr pipe: %w", err)
	}

	cfg.Logger.InfoContext(ctx, "starting opencode serve",
		slog.String("path", cfg.Path),
		slog.Int("port", port),
	)

	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()

		return nil, fmt.Errorf("starting opencode serve: %w", err)
	}

	p := &Process{
		cmd:     cmd,
		stdout:  stdout,
		stderr:  stderr,
		logger:  cfg.Logger,
		waitErr: make(chan error, 1),
	}

	go func() { p.waitErr <- cmd.Wait() }()

	urlCh := make(chan string, 1)

	// opencode 1.14.x writes the banner to stdout; stderr gets drained
	// to avoid pipe back-pressure but carries nothing we parse today.
	go drain(stderr, cfg.Logger, "opencode serve stderr", nil)
	go drain(stdout, cfg.Logger, "opencode serve stdout", func(line string) {
		if m := listeningRE.FindStringSubmatch(line); m != nil {
			select {
			case urlCh <- m[1]:
			default:
			}
		}
	})

	select {
	case u := <-urlCh:
		p.url = u

		cfg.Logger.InfoContext(ctx, "opencode serve ready", slog.String("url", u))

		return p, nil
	case err := <-p.waitErr:
		return nil, fmt.Errorf("%w: exited before ready: %v", ErrServerNotReady, err)
	case <-time.After(timeout):
		_ = p.Close()

		return nil, fmt.Errorf("%w: no listening banner within %s", ErrServerNotReady, timeout)
	case <-ctx.Done():
		_ = p.Close()

		return nil, ctx.Err()
	}
}

// Close terminates the subprocess. Idempotent and safe to call after
// partial Spawn failures.
func (p *Process) Close() error {
	p.closeOnce.Do(func() {
		if p.cmd.Process != nil {
			if killErr := killProcessTree(p.cmd); killErr != nil {
				p.logger.Warn("killProcessTree", slog.Any("error", killErr))
				p.closeErr = killErr
			}
		}

		select {
		case <-p.waitErr:
		case <-time.After(5 * time.Second):
			p.closeErr = errors.New("opencode serve did not exit after SIGKILL")
		}
	})

	return p.closeErr
}

// pickFreePort binds 127.0.0.1:0, reads the assigned port, and closes
// the listener. There is a small race window between our close and
// opencode's bind; callers get ErrServerNotReady if it triggers.
func pickFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}

	defer func() { _ = l.Close() }()

	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.New("unexpected listener address type")
	}

	return addr.Port, nil
}

func buildEnv(overlay map[string]string) []string {
	env := os.Environ()
	if len(overlay) == 0 {
		return env
	}

	idx := make(map[string]int, len(env))

	for i, kv := range env {
		for j := range len(kv) {
			if kv[j] == '=' {
				idx[kv[:j]] = i

				break
			}
		}
	}

	for k, v := range overlay {
		kv := k + "=" + v
		if i, ok := idx[k]; ok {
			env[i] = kv
		} else {
			env = append(env, kv)
		}
	}

	return env
}

func drain(r io.ReadCloser, logger *slog.Logger, label string, cb func(line string)) {
	defer func() { _ = r.Close() }()

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	for scanner.Scan() {
		line := scanner.Text()

		if cb != nil {
			cb(line)
		}

		logger.Debug(label, slog.String("line", line))
	}
}
