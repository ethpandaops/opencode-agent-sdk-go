package opencodesdk

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

const mimePNG = "image/png"

func TestText_WrapsInSingleTextBlock(t *testing.T) {
	blocks := Text("hello world")
	if len(blocks) != 1 {
		t.Fatalf("Text len = %d, want 1", len(blocks))
	}

	if blocks[0].Text == nil || blocks[0].Text.Text != "hello world" {
		t.Fatalf("Text did not populate TextBlock: %+v", blocks[0])
	}
}

func TestBlocks_ReturnsCopyAndPreservesOrder(t *testing.T) {
	first := TextBlock("one")
	second := TextBlock("two")

	out := Blocks(first, second)
	if len(out) != 2 {
		t.Fatalf("Blocks len = %d, want 2", len(out))
	}

	if out[0].Text == nil || out[0].Text.Text != "one" {
		t.Fatalf("out[0] not first block")
	}

	if out[1].Text == nil || out[1].Text.Text != "two" {
		t.Fatalf("out[1] not second block")
	}
}

func TestBlocks_EmptyReturnsNil(t *testing.T) {
	if out := Blocks(); out != nil {
		t.Fatalf("Blocks() = %v, want nil", out)
	}
}

// A tiny 2x2 PNG header so guessImageMime picks mimePNG via .png
// extension and the base64-encode path exercises non-empty bytes.
var tinyPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
}

func TestImageFileInput_ReadsAndEncodes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pixel.png")

	if err := os.WriteFile(path, tinyPNG, 0o600); err != nil {
		t.Fatalf("write test png: %v", err)
	}

	block, err := ImageFileInput(path)
	if err != nil {
		t.Fatalf("ImageFileInput: %v", err)
	}

	if block.Image == nil {
		t.Fatalf("expected Image block, got %+v", block)
	}

	if block.Image.MimeType != mimePNG {
		t.Fatalf("MimeType = %q, want image/png", block.Image.MimeType)
	}

	decoded, err := base64.StdEncoding.DecodeString(block.Image.Data)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}

	if !bytes.Equal(decoded, tinyPNG) {
		t.Fatalf("decoded bytes differ from source")
	}
}

func TestImageFileInput_MissingFile(t *testing.T) {
	_, err := ImageFileInput(filepath.Join(t.TempDir(), "does-not-exist.png"))
	if err == nil {
		t.Fatalf("expected error for missing file")
	}
}

func TestImageFileInputMime_OverridesDetection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blob.bin")

	if err := os.WriteFile(path, tinyPNG, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	block, err := ImageFileInputMime(path, "image/webp")
	if err != nil {
		t.Fatalf("ImageFileInputMime: %v", err)
	}

	if block.Image == nil || block.Image.MimeType != "image/webp" {
		t.Fatalf("MimeType = %q, want image/webp", block.Image.MimeType)
	}
}

func TestGuessMime(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"foo.png", mimePNG},
		{"foo.jpg", "image/jpeg"},
		{"foo.gif", "image/gif"},
		{"foo.webp", "image/webp"},
		{"foo", "application/octet-stream"},
		{"foo.unknown", "application/octet-stream"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := guessMime(tt.path, "application/octet-stream"); got != tt.want {
				t.Fatalf("guessMime(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
