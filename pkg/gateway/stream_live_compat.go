package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"nvidia-api-gateway/pkg/models"
)

type openAIStreamChunk struct {
	ID      string               `json:"id,omitempty"`
	Model   string               `json:"model,omitempty"`
	Choices []openAIStreamChoice `json:"choices,omitempty"`
	Usage   map[string]any       `json:"usage,omitempty"`
}

type openAIStreamChoice struct {
	Index        int               `json:"index,omitempty"`
	Delta        openAIStreamDelta `json:"delta,omitempty"`
	FinishReason string            `json:"finish_reason,omitempty"`
}

type openAIStreamDelta struct {
	Role      string                 `json:"role,omitempty"`
	Content   string                 `json:"content,omitempty"`
	ToolCalls []openAIStreamToolCall `json:"tool_calls,omitempty"`
}

type openAIStreamToolCall struct {
	Index    int                      `json:"index,omitempty"`
	ID       string                   `json:"id,omitempty"`
	Type     string                   `json:"type,omitempty"`
	Function openAIStreamToolFunction `json:"function,omitempty"`
}

type openAIStreamToolFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type liveStreamTranslator interface {
	Consume(chunk openAIStreamChunk) error
	Finish() error
	Error(message string) error
}

type claudeLiveStreamTranslator struct {
	w             io.Writer
	flusher       http.Flusher
	messageID     string
	model         string
	started       bool
	textOpen      bool
	textIndex     int
	nextIndex     int
	finishReason  string
	usage         map[string]any
	finished      bool
	toolBlocks    map[int]*claudeToolBlock
	toolBlockKeys []int
}

type claudeToolBlock struct {
	OpenIndex int
	ToolID    string
	ToolName  string
	Closed    bool
}

type geminiLiveStreamTranslator struct {
	w            io.Writer
	flusher      http.Flusher
	model        string
	finishReason string
	usage        map[string]any
	toolCalls    map[int]*geminiToolCallState
	toolCallKeys []int
	finished     bool
}

type geminiToolCallState struct {
	Name string
	Args strings.Builder
}

func (g *Gateway) executeTranslatedCompatStream(
	ctx context.Context,
	w http.ResponseWriter,
	translatedBody []byte,
	requestedModel string,
	protocol string,
	estTokens int,
	masterKey *models.MasterKey,
) {
	cfg := loadSystemConfig()
	diagnostics := newUpstreamAttemptDiagnostics(protocol + ".stream")
	lastErr := "upstream request failed"
	for i := 0; i < cfg.MaxRetries; i++ {
		key, err := g.acquireKeyWithQueue(ctx, nil, nil, cfg.MaxConcurrency, false, protocol+".stream")
		if key != "" {
			diagnostics.noteSelectedKey(key)
		}
		if err != nil {
			if err.Error() == "queue timeout" {
				writeProtocolJSONError(w, protocol, http.StatusServiceUnavailable, "Queue timeout, no available keys")
				return
			}
			lastErr = err.Error()
			continue
		}

		resp, reader, cancel, retry, err := g.openUpstreamStream(ctx, cfg, key, translatedBody, protocol+".stream")
		if retry {
			diagnostics.noteRetry("上一个上游 NVIDIA 官方 Key 请求失败，已继续重试可用 Key")
			if diagnostics.LastRetryCause != "" {
				lastErr = diagnostics.LastRetryCause
			}
			_ = g.scheduler.ReleaseKey(ctx, key)
			if cancel != nil {
				cancel()
			}
			if err != nil {
				lastErr = err.Error()
			}
			continue
		}
		if err != nil {
			_ = g.scheduler.ReleaseKey(ctx, key)
			if cancel != nil {
				cancel()
			}
			writeProtocolJSONError(w, protocol, http.StatusBadGateway, err.Error())
			return
		}
		if resp == nil {
			_ = g.scheduler.ReleaseKey(ctx, key)
			if cancel != nil {
				cancel()
			}
			continue
		}

		applyResponseHeaders(w.Header(), diagnostics.headers())
		translator := newLiveStreamTranslator(protocol, w, requestedModel, resp)
		streamErr := relayOpenAIStream(reader, translator)
		_ = resp.Body.Close()
		if cancel != nil {
			cancel()
		}
		_ = g.scheduler.ReleaseKey(ctx, key)
		if streamErr != nil {
			_ = translator.Error(streamErr.Error())
			lastErr = streamErr.Error()
			return
		}
		if err := g.usageTracker.Record(ctx, masterKey, estTokens); err == nil {
			incrementQuota(masterKey, estTokens)
		}
		return
	}
	writeProtocolJSONError(w, protocol, http.StatusBadGateway, lastErr)
}

func (g *Gateway) openUpstreamStream(ctx context.Context, cfg models.SystemConfig, key string, body []byte, operation string) (*http.Response, io.Reader, context.CancelFunc, bool, error) {
	resp, reader, cancel, err := g.openUpstreamStreamWithPrefetch(ctx, cfg, key, body)
	if err != nil {
		if cancel != nil {
			cancel()
		}
		if errors.Is(err, errUpstreamFirstByteTimeout) {
			recordUpstreamRuntimeEvent(operation, "first_chunk_timeout", key, false, 0, "流式首个 chunk 超时，已切换到下一个 NVIDIA 官方 Key")
			g.markCooling(ctx, key, "60")
			updateAPIKeyStatusByPlaintext(key, APIKeyStatusCooling)
			return nil, nil, nil, true, err
		}
		recordUpstreamRuntimeEvent(operation, "upstream_error", key, false, 0, err.Error())
		return nil, nil, nil, true, err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		_ = resp.Body.Close()
		if cancel != nil {
			cancel()
		}
		recordUpstreamRuntimeEvent(operation, "rate_limited", key, false, resp.StatusCode, "上游 NVIDIA 官方 Key 被限流，已进入冷却")
		g.markCooling(ctx, key, resp.Header.Get("Retry-After"))
		updateAPIKeyStatusByPlaintext(key, APIKeyStatusCooling)
		return nil, nil, nil, true, fmt.Errorf("upstream rate limited")
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		_ = resp.Body.Close()
		if cancel != nil {
			cancel()
		}
		recordUpstreamRuntimeEvent(operation, "auth_rejected", key, false, resp.StatusCode, "上游 NVIDIA 官方 Key 鉴权失败，已标记为不可用")
		_ = g.scheduler.MarkDead(ctx, key)
		updateAPIKeyStatusByPlaintext(key, APIKeyStatusDead)
		return nil, nil, nil, true, fmt.Errorf("upstream auth rejected key")
	}
	if resp.StatusCode >= http.StatusBadRequest {
		bodyBytes, _ := io.ReadAll(reader)
		_ = resp.Body.Close()
		if cancel != nil {
			cancel()
		}
		recordUpstreamRuntimeEvent(operation, "upstream_failed", key, false, resp.StatusCode, parseUpstreamError(bodyBytes, "upstream stream request failed"))
		return nil, nil, nil, false, errors.New(parseUpstreamError(bodyBytes, "upstream stream request failed"))
	}
	recordUpstreamRuntimeEvent(operation, "upstream_ok", key, true, resp.StatusCode, "已成功建立到 NVIDIA 官方接口的流式连接")
	return resp, reader, cancel, false, nil
}

func relayOpenAIStream(body io.Reader, translator liveStreamTranslator) error {
	reader := bufio.NewReader(body)
	dataLines := make([]string, 0, 2)
	flushData := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		payload := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		if payload == "[DONE]" {
			return translator.Finish()
		}
		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return err
		}
		return translator.Consume(chunk)
	}

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			trimmed := strings.TrimRight(line, "\r\n")
			switch {
			case trimmed == "":
				if flushErr := flushData(); flushErr != nil {
					return flushErr
				}
			case strings.HasPrefix(trimmed, "data:"):
				dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
			case strings.HasPrefix(trimmed, ":"):
				continue
			}
		}
		if err != nil {
			if err == io.EOF {
				if flushErr := flushData(); flushErr != nil {
					return flushErr
				}
				return translator.Finish()
			}
			return err
		}
	}
}

func newLiveStreamTranslator(protocol string, w http.ResponseWriter, requestedModel string, resp *http.Response) liveStreamTranslator {
	flusher, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if flusher != nil {
		flusher.Flush()
	}
	usage := map[string]any{}
	if protocol == "claude" {
		return &claudeLiveStreamTranslator{
			w:          w,
			flusher:    flusher,
			model:      requestedModel,
			usage:      usage,
			toolBlocks: make(map[int]*claudeToolBlock),
		}
	}
	return &geminiLiveStreamTranslator{
		w:         w,
		flusher:   flusher,
		model:     requestedModel,
		usage:     usage,
		toolCalls: make(map[int]*geminiToolCallState),
	}
}

func (t *claudeLiveStreamTranslator) Consume(chunk openAIStreamChunk) error {
	if chunk.Model != "" {
		t.model = firstNonEmpty(t.model, chunk.Model)
	}
	if chunk.ID != "" {
		t.messageID = firstNonEmpty(t.messageID, chunk.ID)
	}
	if chunk.Usage != nil {
		t.usage = chunk.Usage
	}
	if err := t.ensureStarted(); err != nil {
		return err
	}
	for _, choice := range chunk.Choices {
		if choice.Delta.Content != "" {
			if err := t.ensureTextBlock(); err != nil {
				return err
			}
			if err := t.emit("content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": t.textIndex,
				"delta": map[string]any{
					"type": "text_delta",
					"text": choice.Delta.Content,
				},
			}); err != nil {
				return err
			}
		}
		if len(choice.Delta.ToolCalls) > 0 {
			if t.textOpen {
				if err := t.closeTextBlock(); err != nil {
					return err
				}
			}
			for _, toolCall := range choice.Delta.ToolCalls {
				if err := t.consumeToolCall(toolCall); err != nil {
					return err
				}
			}
		}
		if choice.FinishReason != "" {
			t.finishReason = choice.FinishReason
		}
	}
	return nil
}

func (t *claudeLiveStreamTranslator) Finish() error {
	if t.finished {
		return nil
	}
	if err := t.ensureStarted(); err != nil {
		return err
	}
	if t.textOpen {
		if err := t.closeTextBlock(); err != nil {
			return err
		}
	}
	for _, key := range t.toolBlockKeys {
		block := t.toolBlocks[key]
		if block == nil || block.Closed {
			continue
		}
		if err := t.emit("content_block_stop", map[string]any{"type": "content_block_stop", "index": block.OpenIndex}); err != nil {
			return err
		}
		block.Closed = true
	}
	if err := t.emit("message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   mapFinishReasonToClaude(t.finishReason),
			"stop_sequence": nil,
		},
		"usage": map[string]any{
			"output_tokens": lookupUsageValue(t.usage, "completion_tokens"),
		},
	}); err != nil {
		return err
	}
	if err := t.emit("message_stop", map[string]any{"type": "message_stop"}); err != nil {
		return err
	}
	t.finished = true
	return nil
}

func (t *claudeLiveStreamTranslator) Error(message string) error {
	if t.finished {
		return nil
	}
	return t.emit("error", claudeErrorResponse(message))
}

func (t *claudeLiveStreamTranslator) ensureStarted() error {
	if t.started {
		return nil
	}
	t.started = true
	messageID := firstNonEmpty(t.messageID, fmt.Sprintf("msg_%d", unixNowNano()))
	return t.emit("message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":    messageID,
			"type":  "message",
			"role":  "assistant",
			"model": t.model,
			"usage": map[string]any{"input_tokens": lookupUsageValue(t.usage, "prompt_tokens"), "output_tokens": 0},
		},
	})
}

func (t *claudeLiveStreamTranslator) ensureTextBlock() error {
	if t.textOpen {
		return nil
	}
	t.textIndex = t.nextIndex
	t.nextIndex++
	t.textOpen = true
	return t.emit("content_block_start", map[string]any{
		"type":  "content_block_start",
		"index": t.textIndex,
		"content_block": map[string]any{
			"type": "text",
			"text": "",
		},
	})
}

func (t *claudeLiveStreamTranslator) closeTextBlock() error {
	if !t.textOpen {
		return nil
	}
	t.textOpen = false
	return t.emit("content_block_stop", map[string]any{"type": "content_block_stop", "index": t.textIndex})
}

func (t *claudeLiveStreamTranslator) consumeToolCall(toolCall openAIStreamToolCall) error {
	block, ok := t.toolBlocks[toolCall.Index]
	if !ok {
		block = &claudeToolBlock{
			OpenIndex: t.nextIndex,
			ToolID:    firstNonEmpty(toolCall.ID, fmt.Sprintf("toolu_%d", toolCall.Index)),
			ToolName:  firstNonEmpty(toolCall.Function.Name, fmt.Sprintf("tool_%d", toolCall.Index)),
		}
		t.toolBlocks[toolCall.Index] = block
		t.toolBlockKeys = append(t.toolBlockKeys, toolCall.Index)
		t.nextIndex++
		if err := t.emit("content_block_start", map[string]any{
			"type":  "content_block_start",
			"index": block.OpenIndex,
			"content_block": map[string]any{
				"type":  "tool_use",
				"id":    block.ToolID,
				"name":  block.ToolName,
				"input": map[string]any{},
			},
		}); err != nil {
			return err
		}
	} else {
		if block.ToolID == "" && toolCall.ID != "" {
			block.ToolID = toolCall.ID
		}
		if block.ToolName == "" && toolCall.Function.Name != "" {
			block.ToolName = toolCall.Function.Name
		}
	}
	if toolCall.Function.Arguments != "" {
		return t.emit("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": block.OpenIndex,
			"delta": map[string]any{
				"type":         "input_json_delta",
				"partial_json": toolCall.Function.Arguments,
			},
		})
	}
	return nil
}

func (t *claudeLiveStreamTranslator) emit(event string, payload any) error {
	var encoded []byte
	switch v := payload.(type) {
	case []byte:
		encoded = v
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		encoded = data
	}
	if _, err := io.WriteString(t.w, "event: "+event+"\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(t.w, "data: "); err != nil {
		return err
	}
	if _, err := t.w.Write(encoded); err != nil {
		return err
	}
	if _, err := io.WriteString(t.w, "\n\n"); err != nil {
		return err
	}
	if t.flusher != nil {
		t.flusher.Flush()
	}
	return nil
}

func (t *geminiLiveStreamTranslator) Consume(chunk openAIStreamChunk) error {
	if chunk.Model != "" {
		t.model = firstNonEmpty(t.model, chunk.Model)
	}
	if chunk.Usage != nil {
		t.usage = chunk.Usage
	}
	for _, choice := range chunk.Choices {
		if choice.Delta.Content != "" {
			if err := t.emit(map[string]any{
				"candidates": []map[string]any{{
					"index": 0,
					"content": map[string]any{
						"role":  "model",
						"parts": []map[string]any{{"text": choice.Delta.Content}},
					},
				}},
				"modelVersion": t.model,
			}); err != nil {
				return err
			}
		}
		for _, toolCall := range choice.Delta.ToolCalls {
			state, ok := t.toolCalls[toolCall.Index]
			if !ok {
				state = &geminiToolCallState{Name: toolCall.Function.Name}
				t.toolCalls[toolCall.Index] = state
				t.toolCallKeys = append(t.toolCallKeys, toolCall.Index)
			}
			if state.Name == "" && toolCall.Function.Name != "" {
				state.Name = toolCall.Function.Name
			}
			if toolCall.Function.Arguments != "" {
				state.Args.WriteString(toolCall.Function.Arguments)
			}
		}
		if choice.FinishReason != "" {
			t.finishReason = choice.FinishReason
		}
	}
	return nil
}

func (t *geminiLiveStreamTranslator) Finish() error {
	if t.finished {
		return nil
	}
	sort.Ints(t.toolCallKeys)
	for _, idx := range t.toolCallKeys {
		state := t.toolCalls[idx]
		if state == nil {
			continue
		}
		args := parseFunctionArgs(state.Args.String())
		if err := t.emit(map[string]any{
			"candidates": []map[string]any{{
				"index": 0,
				"content": map[string]any{
					"role": "model",
					"parts": []map[string]any{{
						"functionCall": map[string]any{
							"name": state.Name,
							"args": args,
						},
					}},
				},
			}},
			"modelVersion": t.model,
		}); err != nil {
			return err
		}
	}
	if err := t.emit(map[string]any{
		"candidates": []map[string]any{{
			"index":        0,
			"finishReason": mapFinishReasonToGemini(t.finishReason),
			"content": map[string]any{
				"role":  "model",
				"parts": []map[string]any{},
			},
		}},
		"usageMetadata": map[string]any{
			"promptTokenCount":     lookupUsageValue(t.usage, "prompt_tokens"),
			"candidatesTokenCount": lookupUsageValue(t.usage, "completion_tokens"),
			"totalTokenCount":      lookupUsageValue(t.usage, "total_tokens"),
		},
		"modelVersion": t.model,
	}); err != nil {
		return err
	}
	t.finished = true
	return nil
}

func (t *geminiLiveStreamTranslator) Error(message string) error {
	if t.finished {
		return nil
	}
	return t.emit(geminiErrorResponse(message))
}

func (t *geminiLiveStreamTranslator) emit(payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(t.w, "data: "); err != nil {
		return err
	}
	if _, err := t.w.Write(encoded); err != nil {
		return err
	}
	if _, err := io.WriteString(t.w, "\n\n"); err != nil {
		return err
	}
	if t.flusher != nil {
		t.flusher.Flush()
	}
	return nil
}

func writeProtocolJSONError(w http.ResponseWriter, protocol string, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	var payload []byte
	if protocol == "claude" {
		payload = mustJSON(claudeErrorResponse(message))
	} else {
		payload = mustJSON(geminiErrorResponse(message))
	}
	_, _ = w.Write(payload)
}

func parseFunctionArgs(raw string) any {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return map[string]any{}
	}
	var parsed any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
		return parsed
	}
	return map[string]any{"raw": trimmed}
}

func lookupUsageValue(usage map[string]any, key string) any {
	if usage == nil {
		return 0
	}
	if value, ok := usage[key]; ok {
		return value
	}
	return 0
}
