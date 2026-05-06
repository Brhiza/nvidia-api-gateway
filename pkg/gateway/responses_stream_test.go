package gateway

import (
	"bytes"
	"strings"
	"testing"
)

func TestRelayOpenAIStreamToResponsesTranslator(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"id":"chatcmpl_1","model":"meta/llama-3.1-70b-instruct","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":""}]}`,
		``,
		`data: {"choices":[{"index":0,"delta":{"content":" responses"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":6,"total_tokens":11}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	var buf bytes.Buffer
	translator := &responsesStreamTranslator{
		w:                &buf,
		responseID:       "resp_test",
		model:            "gpt-4o",
		messageOutputIdx: -1,
		usage:            map[string]any{},
		functionCalls:    make(map[int]*responsesFunctionCallItem),
	}
	_ = translator.emitEvent("response.created", map[string]any{
		"type":        "response.created",
		"id":          "resp_test",
		"response_id": "resp_test",
		"object":      "response",
		"model":       "gpt-4o",
		"status":      "in_progress",
	})
	if err := relayOpenAIStream(strings.NewReader(stream), translator); err != nil {
		t.Fatalf("relayOpenAIStream failed: %v", err)
	}
	output := buf.String()
	for _, needle := range []string{"event: response.created", "event: response.output_text.delta", "Hello", " responses", "event: response.completed", `"output_text":"Hello responses"`, "data: [DONE]"} {
		if !strings.Contains(output, needle) {
			t.Fatalf("expected output to contain %q, got %s", needle, output)
		}
	}
}
