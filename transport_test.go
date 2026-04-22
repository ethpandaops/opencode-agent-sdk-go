package opencodesdk

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/coder/acp-go-sdk"
)

// fakeAgent is the minimum acp.Agent implementation needed to drive a
// WithTransport-backed smoke test. Every method returns a benign
// response; tests override specific methods to inject fixtures.
type fakeAgent struct {
	initialize func(ctx context.Context, params acp.InitializeRequest) (acp.InitializeResponse, error)
	newSession func(ctx context.Context, params acp.NewSessionRequest) (acp.NewSessionResponse, error)
}

func (f *fakeAgent) Initialize(ctx context.Context, params acp.InitializeRequest) (acp.InitializeResponse, error) {
	if f.initialize != nil {
		return f.initialize(ctx, params)
	}

	return acp.InitializeResponse{
		ProtocolVersion: acp.ProtocolVersionNumber,
		AgentInfo:       &acp.Implementation{Name: "fake", Version: "0.0.0"},
		AgentCapabilities: acp.AgentCapabilities{
			LoadSession:        true,
			PromptCapabilities: acp.PromptCapabilities{Image: true, EmbeddedContext: true},
		},
	}, nil
}

func (f *fakeAgent) Authenticate(_ context.Context, _ acp.AuthenticateRequest) (acp.AuthenticateResponse, error) {
	return acp.AuthenticateResponse{}, nil
}

func (f *fakeAgent) Cancel(_ context.Context, _ acp.CancelNotification) error { return nil }

func (f *fakeAgent) ListSessions(_ context.Context, _ acp.ListSessionsRequest) (acp.ListSessionsResponse, error) {
	return acp.ListSessionsResponse{Sessions: []acp.SessionInfo{}}, nil
}

func (f *fakeAgent) NewSession(ctx context.Context, params acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	if f.newSession != nil {
		return f.newSession(ctx, params)
	}

	return acp.NewSessionResponse{SessionId: "ses_fake"}, nil
}

func (f *fakeAgent) Prompt(_ context.Context, _ acp.PromptRequest) (acp.PromptResponse, error) {
	return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

func (f *fakeAgent) SetSessionConfigOption(_ context.Context, _ acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	return acp.SetSessionConfigOptionResponse{}, nil
}

func (f *fakeAgent) SetSessionMode(_ context.Context, _ acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	return acp.SetSessionModeResponse{}, nil
}

// pipeTransport wires a ClientSideConnection to an AgentSideConnection
// via io.Pipe, which is the canonical in-memory setup for driving ACP
// without a real subprocess.
type pipeTransport struct {
	conn          *acp.ClientSideConnection
	clientWriter  io.Closer
	clientReader  io.Closer
	agentDoneChan <-chan struct{}
}

func newPipeTransport(handler acp.Client, agent acp.Agent) *pipeTransport {
	clientRead, agentWrite := io.Pipe()
	agentRead, clientWrite := io.Pipe()

	agentConn := acp.NewAgentSideConnection(agent, agentWrite, agentRead)
	clientConn := acp.NewClientSideConnection(handler, clientWrite, clientRead)

	return &pipeTransport{
		conn:          clientConn,
		clientWriter:  clientWrite,
		clientReader:  clientRead,
		agentDoneChan: agentConn.Done(),
	}
}

func (p *pipeTransport) Conn() *acp.ClientSideConnection { return p.conn }

func (p *pipeTransport) Close() error {
	_ = p.clientWriter.Close()
	_ = p.clientReader.Close()

	select {
	case <-p.agentDoneChan:
	case <-time.After(2 * time.Second):
	}

	return nil
}

func TestWithTransport_FactoryIsCalled(t *testing.T) {
	called := false

	factory := func(_ context.Context, handler acp.Client) (Transport, error) {
		called = true

		return newPipeTransport(handler, &fakeAgent{}), nil
	}

	c, err := NewClient(WithTransport(factory), WithSkipVersionCheck(true))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	defer c.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start with WithTransport: %v", err)
	}

	if !called {
		t.Fatalf("transport factory was not invoked")
	}

	if c.AgentInfo().Name != "fake" {
		t.Fatalf("AgentInfo.Name = %q, want %q", c.AgentInfo().Name, "fake")
	}

	caps := c.Capabilities()
	if !caps.LoadSession {
		t.Fatalf("expected LoadSession capability from fake agent")
	}

	if !caps.PromptCapabilities.Image {
		t.Fatalf("expected Image prompt capability from fake agent")
	}
}

func TestWithTransport_FactoryErrorSurfacesOnStart(t *testing.T) {
	sentinel := errors.New("factory boom")

	factory := func(_ context.Context, _ acp.Client) (Transport, error) {
		return nil, sentinel
	}

	c, err := NewClient(WithTransport(factory), WithSkipVersionCheck(true))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	defer c.Close()

	err = c.Start(t.Context())
	if !errors.Is(err, sentinel) {
		t.Fatalf("want errors.Is(sentinel); got %v", err)
	}
}

func TestWithTransport_NilTransportRejected(t *testing.T) {
	factory := func(_ context.Context, _ acp.Client) (Transport, error) {
		return nil, nil //nolint:nilnil // test: exercise nil-transport guard
	}

	c, err := NewClient(WithTransport(factory), WithSkipVersionCheck(true))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	defer c.Close()

	err = c.Start(t.Context())
	if err == nil {
		t.Fatalf("expected error when factory returns nil transport")
	}
}

// TestWithTransport_NewSessionRoundtrip proves the full initialize +
// session/new path runs through a custom transport end-to-end.
func TestWithTransport_NewSessionRoundtrip(t *testing.T) {
	var seen acp.NewSessionRequest

	agent := &fakeAgent{
		newSession: func(_ context.Context, params acp.NewSessionRequest) (acp.NewSessionResponse, error) {
			seen = params

			return acp.NewSessionResponse{SessionId: "ses_xyz"}, nil
		},
	}

	factory := func(_ context.Context, handler acp.Client) (Transport, error) {
		return newPipeTransport(handler, agent), nil
	}

	c, err := NewClient(WithTransport(factory), WithSkipVersionCheck(true), WithCwd("/tmp"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	defer c.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	if startErr := c.Start(ctx); startErr != nil {
		t.Fatalf("Start: %v", startErr)
	}

	sess, err := c.NewSession(ctx)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	if sess.ID() != "ses_xyz" {
		t.Fatalf("sess.ID = %q, want %q", sess.ID(), "ses_xyz")
	}

	if seen.Cwd != "/tmp" {
		t.Fatalf("agent observed Cwd=%q, want /tmp", seen.Cwd)
	}
}
