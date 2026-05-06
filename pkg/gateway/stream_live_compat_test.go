package gateway

import (
	"bytes"
	"strings"
	"testing"
)

func TestRelayOpenAIStreamToClaudeTranslator(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"id":"chatcmpl_1","model":"meta/llama-3.1-70b-instruct","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"},"finish_reason":""}]}`,
		``,
		`data: {"choices":[{"index":0,"delta":{"content":" world"},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	var buf bytes.Buffer
	translator := &claudeLiveStreamTranslator{
		w:          &buf,
		model:      "claude-sonnet-4-6",
		usage:      map[string]any{},
		toolBlocks: make(map[int]*claudeToolBlock),
	}

	if err := relayOpenAIStream(strings.NewReader(stream), translator); err != nil {
		t.Fatalf("relayOpenAIStream failed: %v", err)
	}
	output := buf.String()
	for _, needle := range []string{"event: message_start", "Hello", " world", `"stop_reason":"end_turn"`, "event: message_stop"} {
		if !strings.Contains(output, needle) {
			t.Fatalf("expected output to contain %q, got %s", needle, output)
		}
	}
}

func TestRelayOpenAIStreamToGeminiTranslator(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"id":"chatcmpl_1","model":"meta/llama-3.1-70b-instruct","choices":[{"index":0,"delta":{"content":"Part-1"},"finish_reason":""}]}`,
		``,
		`data: {"choices":[{"index":0,"delta":{"content":"Part-2"},"finish_reason":"length"}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	var buf bytes.Buffer
	translator := &geminiLiveStreamTranslator{
		w:         &buf,
		model:     "gemini-2.5-pro",
		usage:     map[string]any{},
		toolCalls: make(map[int]*geminiToolCallState),
	}

	if err := relayOpenAIStream(strings.NewReader(stream), translator); err != nil {
		t.Fatalf("relayOpenAIStream failed: %v", err)
	}
	output := buf.String()
	if count := strings.Count(output, "data: "); count < 3 {
		t.Fatalf("expected multiple gemini data frames, got %d in %s", count, output)
	}
	for _, needle := range []string{"Part-1", "Part-2", `"finishReason":"MAX_TOKENS"`, `"totalTokenCount":7`} {
		if !strings.Contains(output, needle) {
			t.Fatalf("expected output to contain %q, got %s", needle, output)
		}
	}
}
