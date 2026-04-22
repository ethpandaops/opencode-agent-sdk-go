// Package subprocess manages the opencode acp child process and wires
// its stdio into the coder/acp-go-sdk protocol layer.
package subprocess

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/coder/acp-go-sdk"
)

// Config configures the opencode acp subprocess.
type Config struct {
	// Path is the resolved path to the opencode binary.
	Path string
	// Args are user-supplied flags appended to `opencode acp`. The
	// SDK always prepends `--host 127.0.0.1 --port 0` and `--cwd <Cwd>`
	// (when Cwd is set) to these flags.
	Args []string
	// Env overlays the inherited environment. nil means inherit
	// os.Environ() unchanged.
	Env map[string]string
	// Cwd is the working directory for the subprocess.
	Cwd string
	// Logger receives diagnostics. Must not be nil.
	Logger *slog.Logger
	// StderrCallback, if set, receives each stderr line. If nil, stderr
	// is forwarded to Logger at debug level.
	StderrCallback func(line string)
}

// Process is a running `opencode acp` subprocess attached to a coder
// ClientSideConnection.
type Process struct {
	cmd    *exec.Cmd
	conn   *acp.ClientSideConnection
	stdin  io.WriteCloser
	stdout io.ReadCloser
	logger *slog.Logger

	closeOnce sync.Once
	closeErr  error
	waitErr   chan error
}

// Spawn launches opencode acp with the supplied configuration and wires
// its stdio into a ClientSideConnection bound to the provided client.
// The returned Process is responsible for the subprocess lifetime; call
// Close to terminate.
//
// Spawn does not run the ACP initialize handshake — callers invoke
// Process.Conn().Initialize themselves so they can control timeout and
// parameters.
func Spawn(ctx context.Context, cfg Config, client acp.Client) (*Process, error) {
	if cfg.Logger == nil {
		return nil, errors.New("subprocess: Config.Logger is required")
	}

	if cfg.Path == "" {
		return nil, errors.New("subprocess: Config.Path is required")
	}

	// --hostname and --port are explicit to keep opencode's internal HTTP
	// server (used by its ACP bridge internally, see INIT.md Part 2) on a
	// loopback ephemeral port. They default to 127.0.0.1:0 in opencode
	// itself but setting them explicitly insulates us from future
	// default changes.
	args := []string{"acp", "--hostname", "127.0.0.1", "--port", "0"}
	if cfg.Cwd != "" {
		args = append(args, "--cwd", cfg.Cwd)
	}

	args = append(args, cfg.Args...)

	// Spawning the opencode binary is the whole point of this package; the
	// path comes from our own Discoverer and the args are SDK-controlled
	// plus user-supplied flags that the caller is responsible for.
	cmd := exec.CommandContext(ctx, cfg.Path, args...) //nolint:gosec // intentional subprocess spawn
	cmd.Env = buildEnv(cfg.Env)

	if cfg.Cwd != "" {
		cmd.Dir = cfg.Cwd
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()

		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()

		return nil, fmt.Errorf("creating stderr pipe: %w", err)
	}

	cfg.Logger.InfoContext(ctx, "starting opencode acp",
		slog.String("path", cfg.Path),
		slog.Any("args", args),
	)

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()

		return nil, fmt.Errorf("starting opencode acp: %w", err)
	}

	go drainStderr(stderr, cfg.Logger, cfg.StderrCallback)

	p := &Process{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		logger:  cfg.Logger,
		waitErr: make(chan error, 1),
	}

	p.conn = acp.NewClientSideConnection(client, stdin, stdout)
	p.conn.SetLogger(cfg.Logger)

	go func() {
		p.waitErr <- cmd.Wait()
	}()

	return p, nil
}

// Conn returns the underlying ClientSideConnection for making outbound
// ACP calls.
func (p *Process) Conn() *acp.ClientSideConnection {
	return p.conn
}

// Done returns a channel that closes when the peer disconnects.
func (p *Process) Done() <-chan struct{} {
	return p.conn.Done()
}

// Close shuts down the subprocess. It closes stdin (requesting graceful
// shutdown), waits up to 5s for the process to exit, then SIGKILLs.
// Close is idempotent and safe to call after Start failures.
func (p *Process) Close() error {
	p.closeOnce.Do(func() {
		_ = p.stdin.Close()

		select {
		case err := <-p.waitErr:
			if err != nil && !isExpectedExitErr(err) {
				p.closeErr = fmt.Errorf("opencode acp exited with error: %w", err)
			}
		case <-time.After(5 * time.Second):
			p.logger.Warn("opencode acp did not exit within 5s; killing")

			_ = p.cmd.Process.Kill()

			select {
			case <-p.waitErr:
			case <-time.After(2 * time.Second):
				p.closeErr = errors.New("opencode acp did not exit after SIGKILL")
			}
		}
	})

	return p.closeErr
}

func buildEnv(overlay map[string]string) []string {
	env := os.Environ()
	if len(overlay) == 0 {
		return env
	}

	// Build index of existing keys so overlay overrides instead of duplicating.
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

func drainStderr(r io.ReadCloser, logger *slog.Logger, cb func(string)) {
	defer func() { _ = r.Close() }()

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	for scanner.Scan() {
		line := scanner.Text()
		if cb != nil {
			cb(line)

			continue
		}

		logger.Debug("opencode stderr", slog.String("line", line))
	}
}

// isExpectedExitErr reports whether err represents a normal termination
// (e.g. we closed stdin and the process exited cleanly, or we sent
// SIGKILL and it died). We only want to surface unexpected failures.
func isExpectedExitErr(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	// Signals or non-zero exits after intentional shutdown.
	return true
}
