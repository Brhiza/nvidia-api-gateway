package gateway

import (
	"encoding/json"
	"fmt"
	"strings"
)

type translatedEmbeddingsRequest struct {
	RequestedModel string
	Prompt         string
}

func TranslateEmbeddingsRequest(body []byte) ([]byte, *translatedEmbeddingsRequest, error) {
	var reqMap map[string]any
	if err := json.Unmarshal(body, &reqMap); err != nil {
		return nil, nil, fmt.Errorf("invalid json: %v", err)
	}
	requestedModel := strings.TrimSpace(stringValue(reqMap["model"]))
	if requestedModel == "" {
		return nil, nil, fmt.Errorf("model is required")
	}
	inputs := ExtractEmbeddingInputs(reqMap["input"])
	if len(inputs) == 0 {
		return nil, nil, fmt.Errorf("input is required")
	}
	reqMap["model"] = normalizeModel(requestedModel)
	translated, err := json.Marshal(reqMap)
	if err != nil {
		return nil, nil, err
	}
	return translated, &translatedEmbeddingsRequest{
		RequestedModel: requestedModel,
		Prompt:         strings.Join(inputs, "\n"),
	}, nil
}

func ExtractEmbeddingInputs(raw any) []string {
	switch v := raw.(type) {
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return nil
		}
		return []string{text}
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			switch iv := item.(type) {
			case string:
				text := strings.TrimSpace(iv)
				if text != "" {
					out = append(out, text)
				}
			case []any:
				out = append(out, fmt.Sprintf("%v", iv))
			default:
				text := strings.TrimSpace(fmt.Sprintf("%v", iv))
				if text != "" {
					out = append(out, text)
				}
			}
		}
		return out
	default:
		return nil
	}
}
