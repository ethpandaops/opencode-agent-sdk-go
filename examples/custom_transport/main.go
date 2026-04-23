// Demonstrates opencodesdk.WithTransport — injecting a custom Transport
// in place of the default `opencode acp` subprocess.
//
// The primary use case is testing: wire an in-memory ACP pair via
// io.Pipe so your code can exercise the SDK end-to-end without
// requiring a real opencode binary. You can also use it to bridge to
// a remote ACP server over TCP / TLS / an existing session.
//
// This example implements a minimal fake agent that advertises
// loadSession + image prompt capability and answers session/prompt
// with a canned response, then runs opencodesdk.QueryContent against
// it.
//
//	go run ./examples/custom_transport
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	acp "github.com/coder/acp-go-sdk"
	opencodesdk "github.com/ethpandaops/opencode-agent-sdk-go"
)

// fakeAgent is the minimum acp.Agent implementation needed by the SDK.
type fakeAgent struct{}

func (f *fakeAgent) Initialize(_ context.Context, _ acp.InitializeRequest) (acp.InitializeResponse, error) {
	return acp.InitializeResponse{
		ProtocolVersion: acp.ProtocolVersionNumber,
		AgentInfo:       &acp.Implementation{Name: "fake", Version: "0.0.1"},
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

func (f *fakeAgent) NewSession(_ context.Context, _ acp.NewSessionRequest) (acp.NewSessionResponse, error) {
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
// via io.Pipe — no subprocess, no sockets. Exactly how the SDK's own
// transport_test.go drives its WithTransport coverage.
type pipeTransport struct {
	conn         *acp.ClientSideConnection
	clientWriter io.Closer
	clientReader io.Closer
}

func newPipeTransport(handler acp.Client, agent acp.Agent) *pipeTransport {
	clientRead, agentWrite := io.Pipe()
	agentRead, clientWrite := io.Pipe()

	acp.NewAgentSideConnection(agent, agentWrite, agentRead)

	clientConn := acp.NewClientSideConnection(handler, clientWrite, clientRead)

	return &pipeTransport{
		conn:         clientConn,
		clientWriter: clientWrite,
		clientReader: clientRead,
	}
}

func (p *pipeTransport) Conn() *acp.ClientSideConnection { return p.conn }

func (p *pipeTransport) Close() error {
	_ = p.clientWriter.Close()
	_ = p.clientReader.Close()

	return nil
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	factory := func(_ context.Context, handler acp.Client) (opencodesdk.Transport, error) {
		return newPipeTransport(handler, &fakeAgent{}), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res, err := opencodesdk.QueryContent(ctx,
		opencodesdk.Text("What is the meaning of life?"),
		opencodesdk.WithLogger(logger),
		opencodesdk.WithTransport(factory),
		opencodesdk.WithSkipVersionCheck(true),
		opencodesdk.WithCwd(os.TempDir()),
		opencodesdk.WithModel("opencode/big-pickle"),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "QueryContent: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("session: %s\n", res.SessionID)
	fmt.Printf("stop: %s\n", res.StopReason)
	fmt.Printf("(the fake agent returns no text, this is just a plumbing demo)\n")
}
