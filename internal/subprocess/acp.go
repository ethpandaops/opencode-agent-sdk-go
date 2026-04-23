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

	closeOnce    sync.Once
	closed       chan struct{} // closed when Close() is invoked
	shuttingDown chan struct{} // closed right before we attempt graceful stdin close
	closeErr     error
	waitErr      chan error

	exitErrOnce sync.Once
	exitErr     error
	exited      chan struct{} // closed after the subprocess has actually exited
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
	// server (used by its ACP bridge internally) on a loopback ephemeral
	// port. They default to 127.0.0.1:0 in opencode itself but setting
	// them explicitly insulates us from future default changes.
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

	// Put opencode in its own process group so Close() can deliver a
	// group-wide SIGKILL that also reaps any children opencode spawned
	// (stdio MCP servers, internal subprocesses, etc.).
	configureProcessGroup(cmd)

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
		cmd:          cmd,
		stdin:        stdin,
		stdout:       stdout,
		logger:       cfg.Logger,
		waitErr:      make(chan error, 1),
		closed:       make(chan struct{}),
		shuttingDown: make(chan struct{}),
		exited:       make(chan struct{}),
	}

	p.conn = acp.NewClientSideConnection(client, stdin, stdout)
	p.conn.SetLogger(cfg.Logger)

	go func() {
		err := cmd.Wait()
		p.setExitErr(err)
		close(p.exited)

		p.waitErr <- err
	}()

	return p, nil
}

// Exited returns a channel that is closed after the subprocess has
// exited (for any reason — clean shutdown, crash, or SIGKILL).
func (p *Process) Exited() <-chan struct{} { return p.exited }

// ExitErr returns the subprocess's exit error once Exited() has fired.
// Returns nil before exit, and nil if the subprocess exited cleanly or
// via our intentional shutdown.
func (p *Process) ExitErr() error {
	select {
	case <-p.exited:
	default:
		return nil
	}

	return p.exitErr
}

func (p *Process) setExitErr(err error) {
	p.exitErrOnce.Do(func() {
		select {
		case <-p.shuttingDown:
			// Close() initiated shutdown; any exit error is expected.
			return
		default:
		}

		if err != nil && !isNormalTermination(err) {
			p.exitErr = err
		}
	})
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
// shutdown), waits up to 5s for the process to exit, then delivers a
// process-group-wide SIGKILL so any children opencode spawned are also
// reaped. Close is idempotent and safe to call after Start failures.
func (p *Process) Close() error {
	p.closeOnce.Do(func() {
		close(p.shuttingDown)
		_ = p.stdin.Close()

		select {
		case <-p.waitErr:
		case <-time.After(5 * time.Second):
			p.logger.Warn("opencode acp did not exit within 5s; killing process group")

			if killErr := killProcessTree(p.cmd); killErr != nil {
				p.logger.Warn("killProcessTree", slog.Any("error", killErr))
			}

			select {
			case <-p.waitErr:
			case <-time.After(2 * time.Second):
				p.closeErr = errors.New("opencode acp did not exit after SIGKILL")
			}
		}

		close(p.closed)
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

// isNormalTermination reports whether err represents an exit we should
// not treat as a crash: cleanly closed (nil), or signal-terminated via
// our Close path (SIGKILL / SIGTERM).
func isNormalTermination(err error) bool {
	if err == nil {
		return true
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}

	if exitErr.ProcessState == nil {
		return false
	}

	return exitErr.ProcessState.Success() //nolint:staticcheck // prefer explicit selector over embedded method
}
