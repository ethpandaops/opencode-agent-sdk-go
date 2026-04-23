package opencodesdk

import (
	"encoding/base64"
	"testing"
)

func TestTextResult(t *testing.T) {
	r := TextResult("hello")
	if r.Text != "hello" || r.IsError {
		t.Fatalf("unexpected: %+v", r)
	}
}

func TestErrorResult(t *testing.T) {
	r := ErrorResult("boom")
	if r.Text != "boom" || !r.IsError {
		t.Fatalf("unexpected: %+v", r)
	}
}

func TestImageResult(t *testing.T) {
	data := []byte{0x1, 0x2, 0x3}
	r := ImageResult(data, "image/png")

	structured, ok := r.Structured.(map[string]any)
	if !ok {
		t.Fatalf("expected Structured map, got %T", r.Structured)
	}

	if structured["type"] != "image" {
		t.Fatalf("expected type=image, got %v", structured["type"])
	}

	if structured["mimeType"] != "image/png" {
		t.Fatalf("expected mimeType=image/png, got %v", structured["mimeType"])
	}

	expected := base64.StdEncoding.EncodeToString(data)
	if structured["data"] != expected {
		t.Fatalf("expected data=%q, got %v", expected, structured["data"])
	}
}

func TestParseArguments(t *testing.T) {
	type args struct {
		Path    string `json:"path"`
		Recurse bool   `json:"recurse"`
		Limit   int    `json:"limit"`
	}

	in := map[string]any{
		"path":    "/tmp",
		"recurse": true,
		"limit":   float64(42),
	}

	var out args
	if err := ParseArguments(in, &out); err != nil {
		t.Fatalf("ParseArguments: %v", err)
	}

	if out.Path != "/tmp" || !out.Recurse || out.Limit != 42 {
		t.Fatalf("unexpected: %+v", out)
	}
}

func TestParseArguments_NilDst(t *testing.T) {
	if err := ParseArguments(map[string]any{}, nil); err == nil {
		t.Fatal("expected error on nil dst")
	}
}

func TestSimpleSchema(t *testing.T) {
	schema := SimpleSchema(map[string]string{
		"name":  "string",
		"count": "int",
		"tags":  "[]string",
		"flag":  "bool",
		"val":   "float",
	})

	if schema["type"] != schemaTypeObject {
		t.Fatalf("expected type=object, got %v", schema["type"])
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties map, got %T", schema["properties"])
	}

	cases := map[string]string{
		"name":  "string",
		"count": "integer",
		"flag":  "boolean",
		"val":   "number",
	}

	for field, wantType := range cases {
		entry, entryOK := props[field].(map[string]any)
		if !entryOK {
			t.Fatalf("property %q missing", field)
		}

		if entry["type"] != wantType {
			t.Fatalf("%s: expected type=%s, got %v", field, wantType, entry["type"])
		}
	}

	arr, arrOK := props["tags"].(map[string]any)
	if !arrOK {
		t.Fatalf("tags missing")
	}

	if arr["type"] != "array" {
		t.Fatalf("tags: expected type=array, got %v", arr["type"])
	}

	required, reqOK := schema["required"].([]string)
	if !reqOK {
		t.Fatalf("expected required []string, got %T", schema["required"])
	}

	if len(required) != 5 {
		t.Fatalf("expected 5 required fields, got %d", len(required))
	}
}
