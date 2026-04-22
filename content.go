package opencodesdk

import (
	"github.com/coder/acp-go-sdk"
)

// ContentBlock is the ACP content-block union used in prompt requests
// and streamed update payloads. Re-exported from coder/acp-go-sdk so
// callers don't need a direct dependency on the protocol package.
type ContentBlock = acp.ContentBlock

// TextBlock constructs a text content block. Forwards to
// acp.TextBlock so callers don't have to import the protocol package
// for the common case.
func TextBlock(text string) ContentBlock { return acp.TextBlock(text) }

// ImageBlock constructs an image content block. The image bytes must
// be supplied as a base64-encoded string along with the MIME type
// (e.g. "image/png"). Prefer checking PromptCapabilities.Image before
// sending — Session.Prompt will reject image blocks if the agent did
// not advertise image support.
func ImageBlock(data, mimeType string) ContentBlock { return acp.ImageBlock(data, mimeType) }

// AudioBlock constructs an audio content block. Same capability
// guard applies as ImageBlock (PromptCapabilities.Audio).
func AudioBlock(data, mimeType string) ContentBlock { return acp.AudioBlock(data, mimeType) }

// ResourceLinkBlock constructs a resource_link content block pointing
// at a named URI. No capability gate — resource links are always allowed.
func ResourceLinkBlock(name, uri string) ContentBlock { return acp.ResourceLinkBlock(name, uri) }

// ResourceBlock constructs an embedded-resource content block. The
// resource carries its body inline. Gated by
// PromptCapabilities.EmbeddedContext.
func ResourceBlock(res acp.EmbeddedResourceResource) ContentBlock { return acp.ResourceBlock(res) }

// ToolKind categorises a streamed tool call for UI treatment (icons,
// follow-along highlighting). Re-exported for caller ergonomics.
type ToolKind = acp.ToolKind

// Tool-call kind constants mirror the ACP spec.
const (
	ToolKindRead       = acp.ToolKindRead
	ToolKindEdit       = acp.ToolKindEdit
	ToolKindDelete     = acp.ToolKindDelete
	ToolKindMove       = acp.ToolKindMove
	ToolKindSearch     = acp.ToolKindSearch
	ToolKindExecute    = acp.ToolKindExecute
	ToolKindThink      = acp.ToolKindThink
	ToolKindFetch      = acp.ToolKindFetch
	ToolKindSwitchMode = acp.ToolKindSwitchMode
	ToolKindOther      = acp.ToolKindOther
)

// ToolCallStatus is the lifecycle state of a streamed tool call.
type ToolCallStatus = acp.ToolCallStatus

// Tool-call status constants mirror the ACP spec.
const (
	ToolCallStatusPending    = acp.ToolCallStatusPending
	ToolCallStatusInProgress = acp.ToolCallStatusInProgress
	ToolCallStatusCompleted  = acp.ToolCallStatusCompleted
	ToolCallStatusFailed     = acp.ToolCallStatusFailed
)

// StopReason is the reason a prompt turn ended.
type StopReason = acp.StopReason

// Stop-reason constants mirror the ACP spec. opencode currently emits
// EndTurn on success and surfaces cancellation via ErrCancelled on the
// Prompt error path.
const (
	StopReasonEndTurn         = acp.StopReasonEndTurn
	StopReasonMaxTokens       = acp.StopReasonMaxTokens
	StopReasonMaxTurnRequests = acp.StopReasonMaxTurnRequests
	StopReasonRefusal         = acp.StopReasonRefusal
	StopReasonCancelled       = acp.StopReasonCancelled
)
