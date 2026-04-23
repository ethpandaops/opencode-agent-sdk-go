package opencodesdk

import (
	"encoding/base64"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

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

// Text wraps a string in a one-element ContentBlock slice suitable for
// passing directly to QueryContent / Session.Prompt. Mirrors the
// convenience constructor on sister claude/codex SDKs.
func Text(text string) []ContentBlock {
	return []ContentBlock{TextBlock(text)}
}

// Blocks returns a ContentBlock slice built from the supplied blocks.
// Purely cosmetic — equivalent to []ContentBlock{...} but reads more
// fluently at call sites:
//
//	blocks := opencodesdk.Blocks(
//	    opencodesdk.TextBlock("what is in this image?"),
//	    img,
//	)
func Blocks(blocks ...ContentBlock) []ContentBlock {
	if len(blocks) == 0 {
		return nil
	}

	out := make([]ContentBlock, len(blocks))
	copy(out, blocks)

	return out
}

// ImageFileInput reads an image file from disk and returns an
// ImageBlock with its contents base64-encoded inline. The MIME type is
// inferred from the file extension (fallback: "application/octet-stream").
// An explicit MIME type can be forced with ImageFileInputMime.
//
// Prefer ImageBlock when the image data is already in memory.
func ImageFileInput(path string) (ContentBlock, error) {
	return ImageFileInputMime(path, "")
}

// ImageFileInputMime is ImageFileInput with an explicit MIME type.
// Useful when the file extension is missing or unreliable (e.g. a
// generic .bin). Empty mimeType falls back to extension-based
// detection; final fallback is "application/octet-stream".
func ImageFileInputMime(path, mimeType string) (ContentBlock, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ContentBlock{}, fmt.Errorf("opencodesdk: read image file %q: %w", path, err)
	}

	if mimeType == "" {
		mimeType = guessMime(path, "application/octet-stream")
	}

	encoded := base64.StdEncoding.EncodeToString(data)

	return ImageBlock(encoded, mimeType), nil
}

// AudioFileInput reads an audio file from disk and returns an
// AudioBlock with its contents base64-encoded inline. The MIME type
// is inferred from the file extension; the final fallback is
// "application/octet-stream". Use AudioFileInputMime to force an
// explicit MIME type.
//
// Requires the agent to advertise PromptCapabilities.Audio; the SDK
// rejects the block up front otherwise.
func AudioFileInput(path string) (ContentBlock, error) {
	return AudioFileInputMime(path, "")
}

// AudioFileInputMime is AudioFileInput with an explicit MIME type.
func AudioFileInputMime(path, mimeType string) (ContentBlock, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ContentBlock{}, fmt.Errorf("opencodesdk: read audio file %q: %w", path, err)
	}

	if mimeType == "" {
		mimeType = guessMime(path, "application/octet-stream")
	}

	encoded := base64.StdEncoding.EncodeToString(data)

	return AudioBlock(encoded, mimeType), nil
}

// PathInput reads an arbitrary file from disk and returns the
// ContentBlock variant that matches its MIME type:
//
//   - image/*   → ImageBlock (base64-encoded inline)
//   - audio/*   → AudioBlock (base64-encoded inline)
//   - text/*    → TextBlock with the file contents
//   - anything else → ResourceBlock with an inline blob
//
// The MIME type is inferred from the file extension. For content
// where the extension is unreliable, use the type-specific helpers
// (ImageFileInputMime / AudioFileInputMime / ResourceFileInput).
//
// PathInput is a convenience wrapper; it loads the file synchronously
// and holds the entire payload in memory.
func PathInput(path string) (ContentBlock, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ContentBlock{}, fmt.Errorf("opencodesdk: read file %q: %w", path, err)
	}

	mimeType := guessMime(path, "application/octet-stream")

	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return ImageBlock(base64.StdEncoding.EncodeToString(data), mimeType), nil
	case strings.HasPrefix(mimeType, "audio/"):
		return AudioBlock(base64.StdEncoding.EncodeToString(data), mimeType), nil
	case strings.HasPrefix(mimeType, "text/"), mimeType == "application/json", mimeType == "application/xml":
		return TextBlock(string(data)), nil
	default:
		return resourceBlobBlock(path, mimeType, data), nil
	}
}

// PDFFileInput reads a PDF from disk and returns an embedded-resource
// ContentBlock whose MIME type is "application/pdf". Gated by the
// agent's PromptCapabilities.EmbeddedContext at Prompt time.
func PDFFileInput(path string) (ContentBlock, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ContentBlock{}, fmt.Errorf("opencodesdk: read pdf file %q: %w", path, err)
	}

	return resourceBlobBlock(path, "application/pdf", data), nil
}

// resourceBlobBlock builds a ResourceBlock carrying path's bytes as
// a base64-encoded blob with the supplied MIME type. The URI is a
// "file://" URI for the resolved absolute path; opencode ignores it
// but the ACP spec requires a URI.
func resourceBlobBlock(path, mimeType string, data []byte) ContentBlock {
	uri := fileURI(path)
	encoded := base64.StdEncoding.EncodeToString(data)

	return ResourceBlock(acp.EmbeddedResourceResource{
		BlobResourceContents: &acp.BlobResourceContents{
			Uri:      uri,
			MimeType: &mimeType,
			Blob:     encoded,
		},
	})
}

// fileURI returns a file:// URI for path, best-effort. Does not
// require the file to exist.
func fileURI(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return "file://" + filepath.ToSlash(abs)
	}

	return "file://" + filepath.ToSlash(path)
}

// guessMime returns the file's MIME type inferred from its extension,
// or fallback when the extension is missing or unrecognised.
func guessMime(path, fallback string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return fallback
	}

	if m := mime.TypeByExtension(ext); m != "" {
		// Strip charset parameter ("image/png; charset=utf-8" → "image/png").
		if head, _, ok := strings.Cut(m, ";"); ok {
			return strings.TrimSpace(head)
		}

		return m
	}

	return fallback
}

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
