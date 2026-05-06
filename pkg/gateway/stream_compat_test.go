package gateway

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildClaudeSyntheticStreamIncludesExpectedEvents(t *testing.T) {
	raw := mustMarshalTestJSON(map[string]any{
		"id":    "chatcmpl_test",
		"model": "meta/llama-3.1-70b-instruct",
		"choices": []map[string]any{{
			"finish_reason": "tool_calls",
			"message": map[string]any{
				"role":    "assistant",
				"content": "hello from assistant",
				"tool_calls": []map[string]any{{
					"id":   "call_123",
					"type": "function",
					"function": map[string]any{
						"name":      "lookup_weather",
						"arguments": `{"city":"Shanghai"}`,
					},
				}},
			},
		}},
		"usage": map[string]any{
			"prompt_tokens":     10,
			"completion_tokens": 5,
			"total_tokens":      15,
		},
	})

	payload, err := buildClaudeSyntheticStream(raw, "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("buildClaudeSyntheticStream failed: %v", err)
	}
	text := string(payload)
	for _, needle := range []string{
		"event: message_start",
		"event: content_block_start",
		"event: content_block_delta",
		"tool_use",
		"lookup_weather",
		"event: message_stop",
	} {
		if !strings.Contains(text, needle) {
			t.Fatalf("expected stream payload to contain %q, got %s", needle, text)
		}
	}
}

func TestBuildGeminiSyntheticStreamEmitsSSEDataFrames(t *testing.T) {
	raw := mustMarshalTestJSON(map[string]any{
		"id":    "chatcmpl_test",
		"model": "meta/llama-3.1-70b-instruct",
		"choices": []map[string]any{{
			"finish_reason": "stop",
			"message": map[string]any{
				"role":    "assistant",
				"content": strings.Repeat("A", 140),
			},
		}},
		"usage": map[string]any{
			"prompt_tokens":     11,
			"completion_tokens": 7,
			"total_tokens":      18,
		},
	})

	payload, err := buildGeminiSyntheticStream(raw, "gemini-2.5-pro")
	if err != nil {
		t.Fatalf("buildGeminiSyntheticStream failed: %v", err)
	}
	text := string(payload)
	if count := strings.Count(text, "data: "); count < 2 {
		t.Fatalf("expected multiple SSE data frames, got %d in %s", count, text)
	}
	for _, needle := range []string{"modelVersion", "usageMetadata", "finishReason"} {
		if !strings.Contains(text, needle) {
			t.Fatalf("expected payload to contain %q, got %s", needle, text)
		}
	}
}

func mustMarshalTestJSON(value any) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}
