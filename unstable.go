package opencodesdk

import (
	"context"
	"fmt"

	"github.com/coder/acp-go-sdk"
)

// ForkSession creates a new session that inherits the parent's state
// up to the current turn. The returned Session has a new SessionId.
//
// This wraps the opencode-specific `unstable_forkSession` RPC. The
// protocol is marked unstable in ACP; opencode 1.14.20 is the
// reference implementation the SDK is pinned against.
func (c *client) ForkSession(ctx context.Context, parentID string, opts ...Option) (Session, error) {
	if err := c.ensureStarted(); err != nil {
		return nil, err
	}

	merged := c.mergeOptions(opts)

	req := acp.UnstableForkSessionRequest{
		SessionId:  acp.SessionId(parentID),
		Cwd:        cwdOrEmpty(merged),
		McpServers: merged.mcpServers,
	}

	resp, err := c.proc.Conn().UnstableForkSession(ctx, req)
	if err != nil {
		return nil, wrapACPErr(err)
	}

	s := newSession(c, resp.SessionId, nil, resp.Modes, nil, resp.Meta, merged.updatesBuffer)

	if err := c.applySessionConfig(ctx, s, merged); err != nil {
		return nil, err
	}

	return s, nil
}

// ResumeSession re-attaches to an existing session without replaying
// history via session/update notifications. Prefer LoadSession if you
// want the replay to feed your UI.
//
// This wraps opencode's `unstable_resumeSession` RPC.
func (c *client) ResumeSession(ctx context.Context, sessionID string, opts ...Option) (Session, error) {
	if err := c.ensureStarted(); err != nil {
		return nil, err
	}

	merged := c.mergeOptions(opts)

	sid := acp.SessionId(sessionID)

	s := newSession(c, sid, nil, nil, nil, nil, merged.updatesBuffer)

	req := acp.UnstableResumeSessionRequest{
		SessionId:  sid,
		Cwd:        cwdOrEmpty(merged),
		McpServers: merged.mcpServers,
	}

	resp, err := c.proc.Conn().UnstableResumeSession(ctx, req)
	if err != nil {
		c.deregisterSession(sid)
		s.close()

		return nil, wrapACPErr(err)
	}

	s.initialModes = resp.Modes
	s.meta = resp.Meta

	if err := c.applySessionConfig(ctx, s, merged); err != nil {
		return nil, err
	}

	return s, nil
}

// VariantInfo describes the opencode-specific model variant state
// carried in a session's _meta.opencode block.
type VariantInfo struct {
	// ModelId is the base model id (e.g. "anthropic/claude-sonnet-4").
	ModelId string
	// Variant is the selected variant (e.g. "high") or empty.
	Variant string
	// AvailableVariants lists the variant ids the model supports.
	AvailableVariants []string
}

// OpencodeVariant extracts _meta.opencode.{modelId, variant,
// availableVariants} from a session's meta block. Returns (nil, false)
// if absent.
func OpencodeVariant(meta map[string]any) (*VariantInfo, bool) {
	oc, ok := meta["opencode"].(map[string]any)
	if !ok {
		return nil, false
	}

	info := &VariantInfo{}

	if v, ok := oc["modelId"].(string); ok {
		info.ModelId = v
	}

	if v, ok := oc["variant"].(string); ok {
		info.Variant = v
	}

	if raw, ok := oc["availableVariants"].([]any); ok {
		info.AvailableVariants = make([]string, 0, len(raw))

		for _, e := range raw {
			if s, ok := e.(string); ok {
				info.AvailableVariants = append(info.AvailableVariants, s)
			}
		}
	}

	if info.ModelId == "" && info.Variant == "" && len(info.AvailableVariants) == 0 {
		return nil, false
	}

	return info, true
}

// UnstableSetModel issues opencode's unstable_setSessionModel RPC.
// Prefer Session.SetModel (which goes through the stable
// session/set_config_option path) unless you specifically need the
// legacy path that returns only _meta.opencode.variant state.
func (c *client) UnstableSetModel(ctx context.Context, sessionID, modelID string) error {
	if err := c.ensureStarted(); err != nil {
		return err
	}

	_, err := c.proc.Conn().UnstableSetSessionModel(ctx, acp.UnstableSetSessionModelRequest{
		SessionId: acp.SessionId(sessionID),
		ModelId:   acp.UnstableModelId(modelID),
	})
	if err != nil {
		return fmt.Errorf("unstable_setSessionModel: %w", wrapACPErr(err))
	}

	return nil
}
