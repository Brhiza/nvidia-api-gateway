package gateway

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTranslateEmbeddingsRequest(t *testing.T) {
	body := []byte(`{"model":"gpt-3.5-turbo","input":["hello", "world"],"encoding_format":"float"}`)
	translated, meta, err := TranslateEmbeddingsRequest(body)
	if err != nil {
		t.Fatalf("TranslateEmbeddingsRequest failed: %v", err)
	}
	if meta == nil || meta.RequestedModel != "gpt-3.5-turbo" {
		t.Fatalf("unexpected meta: %#v", meta)
	}
	if !strings.Contains(meta.Prompt, "hello") || !strings.Contains(meta.Prompt, "world") {
		t.Fatalf("expected prompt to contain input text, got %q", meta.Prompt)
	}
	var payload map[string]any
	if err := json.Unmarshal(translated, &payload); err != nil {
		t.Fatalf("unmarshal translated failed: %v", err)
	}
	if payload["model"] != normalizeModel("gpt-3.5-turbo") {
		t.Fatalf("expected mapped model, got %#v", payload["model"])
	}
}

func TestExtractEmbeddingInputs(t *testing.T) {
	inputs := ExtractEmbeddingInputs([]any{"first", []any{1, 2, 3}, 42})
	if len(inputs) != 3 {
		t.Fatalf("expected 3 embedding inputs, got %#v", inputs)
	}
	if inputs[0] != "first" {
		t.Fatalf("unexpected first input: %#v", inputs[0])
	}
	if !strings.Contains(inputs[1], "1") || !strings.Contains(inputs[2], "42") {
		t.Fatalf("unexpected normalized inputs: %#v", inputs)
	}
}
