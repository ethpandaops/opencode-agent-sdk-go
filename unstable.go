package opencodesdk

import (
	"context"
	"fmt"

	"github.com/coder/acp-go-sdk"
)

// ForkSession creates a new session that inherits the parent's state
// up to the current turn. The returned Session has a new SessionId.
//
// Wire method: `session/fork`. The Go protocol layer exposes this as
// `UnstableForkSession` because the spec marks the capability unstable,
// but the JSON-RPC method string sent on the wire is the standard
// `session/fork` opencode 1.14.20+ accepts. opencode advertises support
// via `agentCapabilities.sessionCapabilities.fork` during initialize.
func (c *client) ForkSession(ctx context.Context, parentID string, opts ...Option) (Session, error) {
	if err := c.ensureStarted(); err != nil {
		return nil, err
	}

	merged := c.mergeOptions(opts)

	if err := c.checkMCPCapabilities(merged.mcpServers); err != nil {
		return nil, err
	}

	req := acp.UnstableForkSessionRequest{
		SessionId:             acp.SessionId(parentID),
		Cwd:                   cwdOrEmpty(merged),
		McpServers:            merged.mcpServers,
		AdditionalDirectories: c.resolveAdditionalDirs(ctx, merged),
	}

	resp, err := c.transport.Conn().UnstableForkSession(ctx, req)
	if err != nil {
		return nil, wrapACPErr(err)
	}

	s := newSession(c, resp.SessionId, nil, resp.Modes, nil, resp.Meta, merged.updatesBuffer)

	if err := c.applySessionConfig(ctx, s, merged); err != nil {
		c.teardownSession(s)

		return nil, err
	}

	c.attachBudgetTracker(s)
	c.fireHookSessionStart(ctx, s.ID())

	return s, nil
}

// ResumeSession re-attaches to an existing session without replaying
// history via session/update notifications. Prefer LoadSession if you
// want the replay to feed your UI.
//
// Wire method: `session/resume`. The Go protocol layer exposes this as
// `UnstableResumeSession`; the JSON-RPC method string is the standard
// `session/resume` opencode advertises via
// `agentCapabilities.sessionCapabilities.resume` during initialize.
func (c *client) ResumeSession(ctx context.Context, sessionID string, opts ...Option) (Session, error) {
	if err := c.ensureStarted(); err != nil {
		return nil, err
	}

	merged := c.mergeOptions(opts)

	if err := c.checkMCPCapabilities(merged.mcpServers); err != nil {
		return nil, err
	}

	sid := acp.SessionId(sessionID)

	s := newSession(c, sid, nil, nil, nil, nil, merged.updatesBuffer)

	req := acp.UnstableResumeSessionRequest{
		SessionId:             sid,
		Cwd:                   cwdOrEmpty(merged),
		McpServers:            merged.mcpServers,
		AdditionalDirectories: c.resolveAdditionalDirs(ctx, merged),
	}

	resp, err := c.transport.Conn().UnstableResumeSession(ctx, req)
	if err != nil {
		c.teardownSession(s)

		return nil, wrapACPErr(err)
	}

	s.initialModes = resp.Modes
	s.meta = resp.Meta

	if err := c.applySessionConfig(ctx, s, merged); err != nil {
		c.teardownSession(s)

		return nil, err
	}

	c.attachBudgetTracker(s)
	c.fireHookSessionStart(ctx, s.ID())

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

// UnstableSetModel issues ACP's `session/set_model` RPC. The Go
// protocol layer exposes this as `UnstableSetSessionModel`; the
// JSON-RPC method string sent on the wire is the standard
// `session/set_model` opencode 1.14.20+ accepts.
//
// Prefer Session.SetModel for routine model changes — it goes through
// `session/set_config_option`, persists across LoadSession, and keeps
// the SDK's observability labels in sync. Use UnstableSetModel only
// when you specifically need the variant-state response carried in
// `_meta.opencode` (modelId + variant + availableVariants), e.g. to
// probe which variants a model exposes. Variant changes made via this
// path are NOT persisted to opencode's session DB and must be
// re-applied after LoadSession.
func (c *client) UnstableSetModel(ctx context.Context, sessionID, modelID string) error {
	if err := c.ensureStarted(); err != nil {
		return err
	}

	resp, err := c.transport.Conn().UnstableSetSessionModel(ctx, acp.UnstableSetSessionModelRequest{
		SessionId: acp.SessionId(sessionID),
		ModelId:   acp.UnstableModelId(modelID),
	})
	if err != nil {
		return fmt.Errorf("session/set_model: %w", wrapACPErr(err))
	}

	if s := c.lookupSession(acp.SessionId(sessionID)); s != nil {
		if info, ok := OpencodeVariant(resp.Meta); ok {
			if info.ModelId == "" {
				info.ModelId = modelID
			}

			s.setResolvedVariant(info)
		}
	}

	return nil
}

// resolveVariants invokes session/set_model with a bare base modelId to
// retrieve the list of variants opencode exposes for that model. Used by
// WithEffort + setEffortOnSession to map a level enum to a concrete
// variant string. Returns the parsed VariantInfo response.
//
// Note: this is a side-effecting probe — it actually applies modelID
// (without a /variant suffix). Callers that don't want to clobber the
// session's current model should pass the model the session already
// uses, or follow up with another UnstableSetModel call to restore.
func (c *client) resolveVariants(ctx context.Context, sessionID, baseModelID string) (*VariantInfo, error) {
	if err := c.ensureStarted(); err != nil {
		return nil, err
	}

	resp, err := c.transport.Conn().UnstableSetSessionModel(ctx, acp.UnstableSetSessionModelRequest{
		SessionId: acp.SessionId(sessionID),
		ModelId:   acp.UnstableModelId(baseModelID),
	})
	if err != nil {
		return nil, fmt.Errorf("session/set_model probe: %w", wrapACPErr(err))
	}

	info, ok := OpencodeVariant(resp.Meta)
	if !ok {
		return &VariantInfo{ModelId: baseModelID}, nil
	}

	if info.ModelId == "" {
		info.ModelId = baseModelID
	}

	return info, nil
}
