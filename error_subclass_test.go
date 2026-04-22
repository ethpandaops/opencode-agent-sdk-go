package opencodesdk

import (
	"errors"
	"testing"
)

func TestClassifyError_SubClass(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		subcls ErrorSubClass
	}{
		{"prompt too long (context window)", errors.New("context window exceeded"), ErrorSubClassPromptTooLong},
		{"prompt too long (tokens)", errors.New("prompt is too long for the model"), ErrorSubClassPromptTooLong},
		{"rate limit tokens", errors.New("tokens per minute quota hit"), ErrorSubClassRateLimitTokens},
		{"rate limit requests", errors.New("requests per minute reached"), ErrorSubClassRateLimitRequests},
		{"invalid schema", errors.New("invalid params: expected array, received null"), ErrorSubClassInvalidSchema},
		{"invalid model", errors.New("model not found: foo/bar"), ErrorSubClassInvalidModel},
		{"provider error", errors.New("upstream provider error"), ErrorSubClassProviderError},
		{"subprocess died", errors.New("opencode acp exited unexpectedly"), ErrorSubClassSubprocessDied},
		{"no match", errors.New("something else entirely"), ErrorSubClassNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyError(tt.err).SubClass
			if got != tt.subcls {
				t.Fatalf("SubClass = %q, want %q", got, tt.subcls)
			}
		})
	}
}

func TestClassifyError_PromptTooLong_NotRetryable(t *testing.T) {
	c := ClassifyError(errors.New("context length exceeded"))
	if c.Retryable {
		t.Fatalf("prompt-too-long should not be retryable even if class is transient")
	}

	if c.SubClass != ErrorSubClassPromptTooLong {
		t.Fatalf("SubClass = %q, want %q", c.SubClass, ErrorSubClassPromptTooLong)
	}
}

func TestClassifyError_SubClassEmptyOnNilErr(t *testing.T) {
	if c := ClassifyError(nil); c.SubClass != ErrorSubClassNone {
		t.Fatalf("SubClass for nil = %q, want empty", c.SubClass)
	}
}
