package opencodesdk

import (
	"context"
	"fmt"

	"github.com/coder/acp-go-sdk"
)

// ModelInfo is a re-export of acp.ModelInfo. opencode advertises its
// available model catalogue through the session/new lifecycle response;
// callers who want a one-shot enumeration without managing a Session
// can use ListModels below.
type ModelInfo = acp.ModelInfo

// ListModelsResponse is the aggregate response shape returned by
// ListModels. It mirrors the claude/codex SDKs so callers can write
// provider-agnostic model-discovery code.
type ListModelsResponse struct {
	// Models is the full list of models the opencode agent currently
	// advertises for the configured cwd/credentials.
	Models []ModelInfo
	// CurrentModelID is the id opencode would pick by default if no
	// WithModel override were supplied. Empty when opencode did not
	// report a default.
	CurrentModelID string
}

// ListModels is a one-shot helper that spawns an opencode acp
// subprocess, opens a throw-away session to read its advertised model
// catalogue, and tears everything down. All Options supplied to
// NewClient are honoured (cwd, env, logger, cli path, etc.).
//
// Returns an empty slice (no error) when opencode advertises no
// models — this happens for agents that haven't finished loading
// credentials. Wrap with a retry if that matters.
func ListModels(ctx context.Context, opts ...Option) ([]ModelInfo, error) {
	resp, err := ListModelsResponseFor(ctx, opts...)
	if err != nil {
		return nil, err
	}

	return resp.Models, nil
}

// ListModelsResponseFor is the long-form variant of ListModels that
// also returns the agent's default model id.
func ListModelsResponseFor(ctx context.Context, opts ...Option) (*ListModelsResponse, error) {
	var out *ListModelsResponse

	err := WithClient(ctx, func(c Client) error {
		sess, serr := c.NewSession(ctx)
		if serr != nil {
			return fmt.Errorf("opencodesdk: ListModels: open session: %w", serr)
		}

		models := sess.AvailableModels()
		current := ""

		if state := sess.InitialModels(); state != nil {
			current = string(state.CurrentModelId)
		}

		out = &ListModelsResponse{
			Models:         append([]ModelInfo(nil), models...),
			CurrentModelID: current,
		}

		return nil
	}, opts...)
	if err != nil {
		return nil, err
	}

	return out, nil
}
