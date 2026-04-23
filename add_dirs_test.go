package opencodesdk

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/acp-go-sdk"
)

// TestWithAddDirs_ForwardedWhenCapabilityAdvertised verifies session/new
// carries additionalDirectories when the fake agent advertises the
// SessionCapabilities.AdditionalDirectories capability.
func TestWithAddDirs_ForwardedWhenCapabilityAdvertised(t *testing.T) {
	var seen acp.NewSessionRequest

	agent := &fakeAgent{
		initialize: func(_ context.Context, _ acp.InitializeRequest) (acp.InitializeResponse, error) {
			return acp.InitializeResponse{
				ProtocolVersion: acp.ProtocolVersionNumber,
				AgentInfo:       &acp.Implementation{Name: "fake", Version: "0.0.0"},
				AgentCapabilities: acp.AgentCapabilities{
					LoadSession: true,
					SessionCapabilities: acp.SessionCapabilities{
						AdditionalDirectories: &acp.SessionAdditionalDirectoriesCapabilities{},
					},
				},
			}, nil
		},
		newSession: func(_ context.Context, params acp.NewSessionRequest) (acp.NewSessionResponse, error) {
			seen = params

			return acp.NewSessionResponse{SessionId: "ses_dirs"}, nil
		},
	}

	factory := func(_ context.Context, handler acp.Client) (Transport, error) {
		return newPipeTransport(handler, agent), nil
	}

	c, err := NewClient(
		WithTransport(factory),
		WithSkipVersionCheck(true),
		WithCwd("/tmp"),
		WithAddDirs("/tmp/extra-a", "/tmp/extra-b"),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	defer c.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	if startErr := c.Start(ctx); startErr != nil {
		t.Fatalf("Start: %v", startErr)
	}

	if _, err := c.NewSession(ctx); err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	if !slices.Equal(seen.AdditionalDirectories, []string{"/tmp/extra-a", "/tmp/extra-b"}) {
		t.Fatalf("AdditionalDirectories = %v, want [/tmp/extra-a /tmp/extra-b]", seen.AdditionalDirectories)
	}
}

// TestWithAddDirs_DroppedWhenCapabilityMissing verifies the SDK silently
// drops additionalDirectories when the agent does not advertise the
// capability, and logs a warning. Correctness target: no protocol
// error is produced; session/new succeeds without the field.
func TestWithAddDirs_DroppedWhenCapabilityMissing(t *testing.T) {
	var seen acp.NewSessionRequest

	agent := &fakeAgent{
		newSession: func(_ context.Context, params acp.NewSessionRequest) (acp.NewSessionResponse, error) {
			seen = params

			return acp.NewSessionResponse{SessionId: "ses_nocap"}, nil
		},
	}

	factory := func(_ context.Context, handler acp.Client) (Transport, error) {
		return newPipeTransport(handler, agent), nil
	}

	c, err := NewClient(
		WithTransport(factory),
		WithSkipVersionCheck(true),
		WithCwd("/tmp"),
		WithAddDirs("/tmp/extra"),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	defer c.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	if startErr := c.Start(ctx); startErr != nil {
		t.Fatalf("Start: %v", startErr)
	}

	if _, err := c.NewSession(ctx); err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	if len(seen.AdditionalDirectories) != 0 {
		t.Fatalf("AdditionalDirectories leaked without capability: %v", seen.AdditionalDirectories)
	}
}

// TestWithPure_AppendsFlag verifies the CLI flag list carries --pure
// after WithPure() is applied.
func TestWithPure_AppendsFlag(t *testing.T) {
	o := apply([]Option{WithPure()})

	if !slices.Contains(o.cliFlags, "--pure") {
		t.Fatalf("cliFlags = %v; want to contain --pure", o.cliFlags)
	}
}

// TestWithPure_MergesWithOtherCLIFlags verifies WithPure composes with
// other WithCLIFlags calls rather than overwriting them.
func TestWithPure_MergesWithOtherCLIFlags(t *testing.T) {
	o := apply([]Option{
		WithCLIFlags("--log-level", "DEBUG"),
		WithPure(),
	})

	if !slices.Contains(o.cliFlags, "--pure") {
		t.Fatalf("WithPure not appended: %v", o.cliFlags)
	}

	if !slices.Contains(o.cliFlags, "--log-level") {
		t.Fatalf("prior --log-level flag dropped: %v", o.cliFlags)
	}

	if !strings.Contains(strings.Join(o.cliFlags, " "), "--log-level DEBUG") {
		t.Fatalf("ordering of existing flags not preserved: %v", o.cliFlags)
	}
}
