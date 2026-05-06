package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func buildClaudeSyntheticStream(raw []byte, requestedModel string) ([]byte, error) {
	var upstream map[string]any
	if err := json.Unmarshal(raw, &upstream); err != nil {
		return nil, err
	}
	message, usage, finishReason := extractOpenAIResponsePieces(upstream)
	responseID := firstNonEmpty(stringValue(upstream["id"]), fmt.Sprintf("msg_%d", unixNowNano()))
	model := firstNonEmpty(requestedModel, stringValue(upstream["model"]))
	var buf bytes.Buffer

	writeNamedSSEEvent(&buf, "message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":    responseID,
			"type":  "message",
			"role":  "assistant",
			"model": model,
			"usage": map[string]any{"input_tokens": usage["prompt_tokens"], "output_tokens": 0},
		},
	})

	blockIndex := 0
	if text := strings.TrimSpace(message.Content); text != "" {
		writeNamedSSEEvent(&buf, "content_block_start", map[string]any{
			"type":  "content_block_start",
			"index": blockIndex,
			"content_block": map[string]any{
				"type": "text",
				"text": "",
			},
		})
		for _, chunk := range splitTextForStream(text, 80) {
			writeNamedSSEEvent(&buf, "content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": blockIndex,
				"delta": map[string]any{
					"type": "text_delta",
					"text": chunk,
				},
			})
		}
		writeNamedSSEEvent(&buf, "content_block_stop", map[string]any{"type": "content_block_stop", "index": blockIndex})
		blockIndex++
	}

	for _, toolCall := range message.ToolCalls {
		writeNamedSSEEvent(&buf, "content_block_start", map[string]any{
			"type":  "content_block_start",
			"index": blockIndex,
			"content_block": map[string]any{
				"type":  "tool_use",
				"id":    firstNonEmpty(toolCall.ID, fmt.Sprintf("toolu_%d", blockIndex)),
				"name":  toolCall.Function.Name,
				"input": map[string]any{},
			},
		})
		arguments := toolCall.Function.Arguments
		if strings.TrimSpace(arguments) != "" {
			for _, chunk := range splitTextForStream(arguments, 120) {
				writeNamedSSEEvent(&buf, "content_block_delta", map[string]any{
					"type":  "content_block_delta",
					"index": blockIndex,
					"delta": map[string]any{
						"type":         "input_json_delta",
						"partial_json": chunk,
					},
				})
			}
		}
		writeNamedSSEEvent(&buf, "content_block_stop", map[string]any{"type": "content_block_stop", "index": blockIndex})
		blockIndex++
	}

	writeNamedSSEEvent(&buf, "message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   mapFinishReasonToClaude(finishReason),
			"stop_sequence": nil,
		},
		"usage": map[string]any{
			"output_tokens": usage["completion_tokens"],
		},
	})
	writeNamedSSEEvent(&buf, "message_stop", map[string]any{"type": "message_stop"})
	return buf.Bytes(), nil
}

func buildGeminiSyntheticStream(raw []byte, requestedModel string) ([]byte, error) {
	var upstream map[string]any
	if err := json.Unmarshal(raw, &upstream); err != nil {
		return nil, err
	}
	message, usage, finishReason := extractOpenAIResponsePieces(upstream)
	model := firstNonEmpty(strings.TrimSpace(requestedModel), stringValue(upstream["model"]))
	var buf bytes.Buffer

	if text := strings.TrimSpace(message.Content); text != "" {
		for _, chunk := range splitTextForStream(text, 120) {
			writeDataOnlySSEEvent(&buf, map[string]any{
				"candidates": []map[string]any{{
					"index": 0,
					"content": map[string]any{
						"role":  "model",
						"parts": []map[string]any{{"text": chunk}},
					},
				}},
				"modelVersion": model,
			})
		}
	}

	for _, toolCall := range message.ToolCalls {
		args := map[string]any{}
		if strings.TrimSpace(toolCall.Function.Arguments) != "" {
			_ = json.Unmarshal([]byte(toolCall.Function.Arguments), &args)
		}
		writeDataOnlySSEEvent(&buf, map[string]any{
			"candidates": []map[string]any{{
				"index": 0,
				"content": map[string]any{
					"role": "model",
					"parts": []map[string]any{{
						"functionCall": map[string]any{
							"name": toolCall.Function.Name,
							"args": args,
						},
					}},
				},
			}},
			"modelVersion": model,
		})
	}

	writeDataOnlySSEEvent(&buf, map[string]any{
		"candidates": []map[string]any{{
			"index":        0,
			"finishReason": mapFinishReasonToGemini(finishReason),
			"content": map[string]any{
				"role":  "model",
				"parts": []map[string]any{},
			},
		}},
		"usageMetadata": map[string]any{
			"promptTokenCount":     usage["prompt_tokens"],
			"candidatesTokenCount": usage["completion_tokens"],
			"totalTokenCount":      usage["total_tokens"],
		},
		"modelVersion": model,
	})
	return buf.Bytes(), nil
}

func writeNamedSSEEvent(buf *bytes.Buffer, event string, payload map[string]any) {
	encoded, _ := json.Marshal(payload)
	buf.WriteString("event: ")
	buf.WriteString(event)
	buf.WriteByte('\n')
	buf.WriteString("data: ")
	buf.Write(encoded)
	buf.WriteString("\n\n")
}

func writeDataOnlySSEEvent(buf *bytes.Buffer, payload map[string]any) {
	encoded, _ := json.Marshal(payload)
	buf.WriteString("data: ")
	buf.Write(encoded)
	buf.WriteString("\n\n")
}

func splitTextForStream(text string, maxChunkSize int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if maxChunkSize <= 0 {
		maxChunkSize = 80
	}
	runes := []rune(text)
	if len(runes) <= maxChunkSize {
		return []string{text}
	}
	chunks := make([]string, 0, (len(runes)/maxChunkSize)+1)
	for start := 0; start < len(runes); start += maxChunkSize {
		end := start + maxChunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunk := strings.TrimSpace(string(runes[start:end]))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
	}
	return chunks
}

func unixNowNano() int64 {
	return time.Now().UnixNano()
}
