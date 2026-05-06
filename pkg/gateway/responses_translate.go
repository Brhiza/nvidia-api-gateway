package gateway

import (
	"encoding/json"
	"fmt"
	"strings"
)

type translatedResponsesRequest struct {
	RequestedModel string
	Prompt         string
	Temperature    *float64
	Stream         bool
}

func TranslateResponsesRequest(body []byte) ([]byte, *translatedResponsesRequest, error) {
	var reqMap map[string]any
	if err := json.Unmarshal(body, &reqMap); err != nil {
		return nil, nil, fmt.Errorf("invalid json: %v", err)
	}
	requestedModel := strings.TrimSpace(stringValue(reqMap["model"]))
	if requestedModel == "" {
		return nil, nil, fmt.Errorf("model is required")
	}

	openAIReq := map[string]any{
		"model":    normalizeModel(requestedModel),
		"stream":   false,
		"messages": make([]map[string]any, 0),
	}
	if stream, ok := boolValue(reqMap["stream"]); ok {
		openAIReq["stream"] = stream
	}
	if temperature, ok := floatValue(reqMap["temperature"]); ok {
		openAIReq["temperature"] = temperature
	}
	if maxOutputTokens, ok := intValue(reqMap["max_output_tokens"]); ok {
		openAIReq["max_tokens"] = maxOutputTokens
	} else if maxTokens, ok := intValue(reqMap["max_tokens"]); ok {
		openAIReq["max_tokens"] = maxTokens
	}
	if tools, ok := normalizeResponsesTools(reqMap["tools"]); ok && len(tools) > 0 {
		openAIReq["tools"] = tools
	}
	if toolChoice, exists := reqMap["tool_choice"]; exists {
		openAIReq["tool_choice"] = toolChoice
	}
	if instructions := strings.TrimSpace(extractResponsesInstructions(reqMap["instructions"])); instructions != "" {
		openAIReq["messages"] = append(openAIReq["messages"].([]map[string]any), map[string]any{
			"role":    "system",
			"content": instructions,
		})
	}

	var messages []map[string]any
	switch {
	case reqMap["messages"] != nil:
		normalized, ok := normalizeOpenAIMessages(reqMap["messages"])
		if !ok || len(normalized) == 0 {
			return nil, nil, fmt.Errorf("messages are required")
		}
		messages = normalized
	case reqMap["input"] != nil:
		normalized, ok := normalizeResponsesInput(reqMap["input"])
		if !ok || len(normalized) == 0 {
			return nil, nil, fmt.Errorf("input is required")
		}
		messages = normalized
	default:
		return nil, nil, fmt.Errorf("input or messages is required")
	}
	openAIReq["messages"] = append(openAIReq["messages"].([]map[string]any), messages...)

	translated, err := json.Marshal(openAIReq)
	if err != nil {
		return nil, nil, err
	}
	stream, _ := boolValue(openAIReq["stream"])
	temperature, _ := floatPtrValue(openAIReq["temperature"])
	return translated, &translatedResponsesRequest{
		RequestedModel: requestedModel,
		Prompt:         buildPromptFromMessageMaps(openAIReq["messages"].([]map[string]any)),
		Temperature:    temperature,
		Stream:         stream,
	}, nil
}

func normalizeResponsesInput(raw any) ([]map[string]any, bool) {
	switch v := raw.(type) {
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return nil, false
		}
		return []map[string]any{{"role": "user", "content": text}}, true
	case []any:
		messages := make([]map[string]any, 0, len(v))
		for _, item := range v {
			switch entry := item.(type) {
			case string:
				text := strings.TrimSpace(entry)
				if text != "" {
					messages = append(messages, map[string]any{"role": "user", "content": text})
				}
			case map[string]any:
				if roleRaw, hasRole := entry["role"]; hasRole {
					role := normalizeResponsesRole(stringValue(roleRaw))
					content := extractResponsesContent(entry["content"])
					if role != "" && content != "" {
						messages = append(messages, map[string]any{"role": role, "content": content})
					}
					continue
				}
				typ := strings.ToLower(strings.TrimSpace(stringValue(entry["type"])))
				switch typ {
				case "input_text", "text":
					text := strings.TrimSpace(stringValue(entry["text"]))
					if text != "" {
						messages = append(messages, map[string]any{"role": "user", "content": text})
					}
				case "output_text":
					text := strings.TrimSpace(stringValue(entry["text"]))
					if text != "" {
						messages = append(messages, map[string]any{"role": "assistant", "content": text})
					}
				case "message":
					role := normalizeResponsesRole(stringValue(entry["role"]))
					content := extractResponsesContent(entry["content"])
					if role != "" && content != "" {
						messages = append(messages, map[string]any{"role": role, "content": content})
					}
				case "function_call_output":
					content := extractResponsesContent(entry["output"])
					if content != "" {
						messages = append(messages, map[string]any{"role": "user", "content": content})
					}
				}
			}
		}
		return messages, len(messages) > 0
	default:
		return nil, false
	}
}

func normalizeResponsesRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "assistant":
		return "assistant"
	case "system", "developer":
		return "system"
	case "tool":
		return "user"
	default:
		return "user"
	}
}

func extractResponsesInstructions(raw any) string {
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			switch entry := item.(type) {
			case string:
				if text := strings.TrimSpace(entry); text != "" {
					parts = append(parts, text)
				}
			case map[string]any:
				if text := strings.TrimSpace(stringValue(entry["text"])); text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func extractResponsesContent(raw any) string {
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			switch entry := item.(type) {
			case string:
				if text := strings.TrimSpace(entry); text != "" {
					parts = append(parts, text)
				}
			case map[string]any:
				typ := strings.ToLower(strings.TrimSpace(stringValue(entry["type"])))
				switch typ {
				case "input_text", "output_text", "text":
					if text := strings.TrimSpace(stringValue(entry["text"])); text != "" {
						parts = append(parts, text)
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func normalizeResponsesTools(raw any) ([]map[string]any, bool) {
	items, ok := raw.([]any)
	if !ok {
		if typed, ok2 := raw.([]map[string]any); ok2 {
			return typed, len(typed) > 0
		}
		return nil, false
	}
	tools := make([]map[string]any, 0, len(items))
	for _, item := range items {
		toolMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if strings.TrimSpace(stringValue(toolMap["type"])) == "function" {
			tools = append(tools, toolMap)
		}
	}
	return tools, len(tools) > 0
}
