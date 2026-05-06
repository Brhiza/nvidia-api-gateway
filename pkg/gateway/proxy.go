package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"nvidia-api-gateway/pkg/cache"
	"nvidia-api-gateway/pkg/middleware"
	"nvidia-api-gateway/pkg/models"
	"nvidia-api-gateway/pkg/scheduler"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp/fasthttpadaptor"
)

var bufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 4096)
	},
}

type Gateway struct {
	scheduler    *scheduler.Scheduler
	cache        *cache.SemanticCache
	usageTracker *middleware.UsageTracker
	client       *http.Client
}

type proxyResult struct {
	StatusCode  int
	ContentType string
	Body        []byte
	Headers     map[string]string
}

func NewGateway(sched *scheduler.Scheduler, semanticCache *cache.SemanticCache, usageTracker *middleware.UsageTracker) *Gateway {
	return &Gateway{
		scheduler:    sched,
		cache:        semanticCache,
		usageTracker: usageTracker,
		client:       &http.Client{Timeout: 10 * time.Minute},
	}
}

func (g *Gateway) acquireKeyWithQueue(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, maxConcurrency int, allowEarlyHeaders bool, operation string) (string, error) {
	if maxConcurrency <= 0 {
		maxConcurrency = 3
	}
	timeout := time.After(45 * time.Second)
	keepAliveTicker := time.NewTicker(15 * time.Second)
	defer keepAliveTicker.Stop()
	pollTicker := time.NewTicker(500 * time.Millisecond)
	defer pollTicker.Stop()

	headersSent := false
	recoveryAttempted := false

	for {
		key, err := g.scheduler.AcquireKey(ctx, maxConcurrency)
		if err != nil {
			recordUpstreamRuntimeEvent(operation, "scheduler_error", "", false, 0, err.Error())
			return "", err
		}
		if key != "" {
			recordUpstreamRuntimeEvent(operation, "key_selected", key, true, 0, "已从上游池选中可用的 NVIDIA 官方 Key")
			return key, nil
		}

		if !recoveryAttempted {
			recoveryAttempted = true
			recordUpstreamRuntimeEvent(operation, "restore_attempt", "", false, 0, "当前没有可用上游 Key，先尝试自动恢复 Cooling/Dead 状态")
			_ = RestoreRecoverableStatuses(ctx, g.scheduler)
			continue
		}

		if allowEarlyHeaders && flusher != nil && w != nil && !headersSent {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			w.WriteHeader(http.StatusOK)
			flusher.Flush()
			headersSent = true
		}

		select {
		case <-ctx.Done():
			recordUpstreamRuntimeEvent(operation, "request_cancelled", "", false, 0, ctx.Err().Error())
			return "", ctx.Err()
		case <-timeout:
			recordUpstreamRuntimeEvent(operation, "queue_timeout", "", false, 503, "上游池中没有可用的 NVIDIA 官方 Key")
			return "", fmt.Errorf("queue timeout")
		case <-keepAliveTicker.C:
			if flusher != nil && headersSent {
				_, _ = w.Write([]byte(": keep-alive\n\n"))
				flusher.Flush()
			}
		case <-pollTicker.C:
		}
	}
}

func shouldUseSemanticCache(temperature *float64) bool {
	return temperature != nil && *temperature == 0
}

func incrementQuota(masterKey *models.MasterKey, tokenCost int) {
	if masterKey == nil || tokenCost <= 0 {
		return
	}
	incrementMasterKeyQuota(masterKey.ID, tokenCost)
	masterKey.UsedQuota += int64(tokenCost)
}

func (g *Gateway) HandleChatCompletions(c *fiber.Ctx) error {
	cfg := loadSystemConfig()
	if !protocolEnabled(cfg, "openai") {
		return c.Status(fiber.StatusNotFound).JSON(openAIError("route_disabled", "OpenAI compatibility route is disabled", "invalid_request_error"))
	}

	rawBody := append([]byte(nil), c.Body()...)
	translatedBody, translatedReq, promptStr, temperature, err := TranslateRequest(rawBody)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(openAIError("invalid_request_error", "Invalid request body format", "invalid_request_error"))
	}

	estTokens := EstimateTokens(promptStr)
	if estTokens > 100000 {
		return c.Status(fiber.StatusRequestEntityTooLarge).JSON(openAIError("context_length_exceeded", "Token limit exceeded", "invalid_request_error"))
	}

	masterKey, _ := c.Locals("masterKey").(*models.MasterKey)
	if err := g.usageTracker.Check(context.Background(), masterKey, estTokens); err != nil {
		status := fiber.StatusTooManyRequests
		errorCode := "rate_limit_exceeded"
		if err.Error() == "Quota exceeded" {
			status = fiber.StatusPaymentRequired
			errorCode = "quota_exceeded"
		}
		return c.Status(status).JSON(openAIError(errorCode, err.Error(), "rate_limit_error"))
	}

	if !translatedReq.Stream {
		result := g.executeOpenAINonStream(context.Background(), translatedBody, translatedReq.Model, promptStr, temperature, estTokens, masterKey)
		if result.ContentType != "" {
			c.Set("Content-Type", result.ContentType)
		}
		applyResponseHeaders(c, result.Headers)
		return c.Status(result.StatusCode).Send(result.Body)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.executeOpenAIStream(r.Context(), w, translatedBody, translatedReq.Model, promptStr, temperature, estTokens, masterKey)
	})
	fasthttpadaptor.NewFastHTTPHandler(handler)(c.Context())
	return nil
}

func (g *Gateway) HandleOpenAIModels(c *fiber.Ctx) error {
	cfg := loadSystemConfig()
	if !protocolEnabled(cfg, "openai") {
		return c.Status(fiber.StatusNotFound).JSON(openAIError("route_disabled", "OpenAI compatibility route is disabled", "invalid_request_error"))
	}
	result := g.fetchUpstreamModels(context.Background())
	if result.ContentType != "" {
		c.Set("Content-Type", result.ContentType)
	}
	applyResponseHeaders(c, result.Headers)
	return c.Status(result.StatusCode).Send(result.Body)
}

func (g *Gateway) HandleOpenAIModel(c *fiber.Ctx) error {
	result := g.fetchUpstreamModels(context.Background())
	if result.StatusCode < 200 || result.StatusCode >= 300 {
		if result.ContentType != "" {
			c.Set("Content-Type", result.ContentType)
		}
		applyResponseHeaders(c, result.Headers)
		return c.Status(result.StatusCode).Send(result.Body)
	}
	var payload map[string]any
	if err := json.Unmarshal(result.Body, &payload); err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(openAIError("upstream_parse_error", "Failed to parse upstream models response", "api_error"))
	}
	target := strings.TrimSpace(c.Params("modelId"))
	data, _ := payload["data"].([]any)
	for _, item := range data {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if strings.TrimSpace(stringValue(itemMap["id"])) == target {
			return c.JSON(itemMap)
		}
	}
	return c.Status(fiber.StatusNotFound).JSON(openAIError("model_not_found", "Requested model was not found", "invalid_request_error"))
}

func (g *Gateway) HandleClaudeModels(c *fiber.Ctx) error {
	cfg := loadSystemConfig()
	if !protocolEnabled(cfg, "claude") {
		return c.Status(fiber.StatusNotFound).JSON(claudeErrorResponse("Claude compatibility route is disabled"))
	}
	result := g.fetchUpstreamModels(context.Background())
	if result.StatusCode < 200 || result.StatusCode >= 300 {
		applyResponseHeaders(c, result.Headers)
		return c.Status(result.StatusCode).JSON(claudeErrorResponse(parseUpstreamError(result.Body, "Failed to fetch models")))
	}
	var payload map[string]any
	if err := json.Unmarshal(result.Body, &payload); err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(claudeErrorResponse("Failed to parse upstream models response"))
	}
	items := make([]map[string]any, 0)
	data, _ := payload["data"].([]any)
	for _, item := range data {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		items = append(items, map[string]any{
			"id":           stringValue(itemMap["id"]),
			"type":         "model",
			"display_name": stringValue(itemMap["id"]),
			"created_at":   time.Now().Unix(),
		})
	}
	applyResponseHeaders(c, result.Headers)
	return c.JSON(fiber.Map{"data": items, "has_more": false})
}

func (g *Gateway) HandleClaudeMessages(c *fiber.Ctx) error {
	cfg := loadSystemConfig()
	if !protocolEnabled(cfg, "claude") {
		return c.Status(fiber.StatusNotFound).JSON(claudeErrorResponse("Claude compatibility route is disabled"))
	}
	translatedBody, requestedModel, temperature, stream, promptStr, err := TranslateClaudeRequest(c.Body())
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(claudeErrorResponse(err.Error()))
	}
	masterKey, _ := c.Locals("masterKey").(*models.MasterKey)
	estTokens := EstimateTokens(promptStr)
	if err := g.usageTracker.Check(context.Background(), masterKey, estTokens); err != nil {
		status := fiber.StatusTooManyRequests
		if err.Error() == "Quota exceeded" {
			status = fiber.StatusPaymentRequired
		}
		return c.Status(status).JSON(claudeErrorResponse(err.Error()))
	}
	if stream {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			g.executeTranslatedCompatStream(r.Context(), w, translatedBody, requestedModel, "claude", estTokens, masterKey)
		})
		fasthttpadaptor.NewFastHTTPHandler(handler)(c.Context())
		return nil
	}
	result := g.executeOpenAINonStream(context.Background(), translatedBody, requestedModel, promptStr, temperature, estTokens, masterKey)
	if result.StatusCode < 200 || result.StatusCode >= 300 {
		applyResponseHeaders(c, result.Headers)
		return c.Status(result.StatusCode).JSON(claudeErrorResponse(parseUpstreamError(result.Body, "Claude request failed")))
	}
	converted, err := RenderClaudeResponse(result.Body, requestedModel)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(claudeErrorResponse("Failed to render Claude response"))
	}
	c.Set("Content-Type", "application/json")
	applyResponseHeaders(c, result.Headers)
	return c.Status(fiber.StatusOK).Send(converted)
}

func (g *Gateway) HandleGeminiContent(c *fiber.Ctx) error {
	cfg := loadSystemConfig()
	if !protocolEnabled(cfg, "gemini") {
		return c.Status(fiber.StatusNotFound).JSON(geminiErrorResponse("Gemini compatibility route is disabled"))
	}
	target := c.Params("target")
	isStream := strings.HasSuffix(target, ":streamGenerateContent")
	translatedBody, requestedModel, temperature, promptStr, err := TranslateGeminiRequest(target, c.Body(), isStream)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(geminiErrorResponse(err.Error()))
	}
	masterKey, _ := c.Locals("masterKey").(*models.MasterKey)
	estTokens := EstimateTokens(promptStr)
	if err := g.usageTracker.Check(context.Background(), masterKey, estTokens); err != nil {
		status := fiber.StatusTooManyRequests
		if err.Error() == "Quota exceeded" {
			status = fiber.StatusPaymentRequired
		}
		return c.Status(status).JSON(geminiErrorResponse(err.Error()))
	}
	if isStream {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			g.executeTranslatedCompatStream(r.Context(), w, translatedBody, requestedModel, "gemini", estTokens, masterKey)
		})
		fasthttpadaptor.NewFastHTTPHandler(handler)(c.Context())
		return nil
	}
	result := g.executeOpenAINonStream(context.Background(), translatedBody, requestedModel, promptStr, temperature, estTokens, masterKey)
	if result.StatusCode < 200 || result.StatusCode >= 300 {
		applyResponseHeaders(c, result.Headers)
		return c.Status(result.StatusCode).JSON(geminiErrorResponse(parseUpstreamError(result.Body, "Gemini request failed")))
	}
	converted, err := RenderGeminiResponse(result.Body, requestedModel)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(geminiErrorResponse("Failed to render Gemini response"))
	}
	c.Set("Content-Type", "application/json")
	applyResponseHeaders(c, result.Headers)
	return c.Status(fiber.StatusOK).Send(converted)
}

func (g *Gateway) executeOpenAINonStream(ctx context.Context, translatedBody []byte, model, promptStr string, temperature *float64, estTokens int, masterKey *models.MasterKey) proxyResult {
	cfg := loadSystemConfig()
	diagnostics := newUpstreamAttemptDiagnostics("chat.nonstream")
	cacheEnabled := shouldUseSemanticCache(temperature)
	if cacheEnabled {
		cached, cacheErr := g.cache.Get(ctx, model, promptStr)
		if cacheErr == nil && cached != "" {
			if err := g.usageTracker.Record(ctx, masterKey, estTokens); err == nil {
				incrementQuota(masterKey, estTokens)
			}
			return proxyResult{StatusCode: http.StatusOK, ContentType: "application/json", Body: []byte(cached)}
		}
	}
	lastErr := "upstream request failed"
	for i := 0; i < cfg.MaxRetries; i++ {
		key, err := g.acquireKeyWithQueue(ctx, nil, nil, cfg.MaxConcurrency, false, "chat.nonstream")
		if key != "" {
			diagnostics.noteSelectedKey(key)
		}
		if err != nil {
			if err.Error() == "queue timeout" {
				return proxyResult{StatusCode: http.StatusServiceUnavailable, ContentType: "application/json", Body: mustJSON(openAIError("queue_timeout", "Queue timeout, no available keys", "server_error"))}
			}
			lastErr = err.Error()
			continue
		}
		result, retry := g.callUpstreamChat(ctx, cfg, key, translatedBody)
		_ = g.scheduler.ReleaseKey(ctx, key)
		if retry {
			diagnostics.noteRetry("上一个上游 NVIDIA 官方 Key 请求失败，已继续重试可用 Key")
			if diagnostics.LastRetryCause != "" {
				lastErr = diagnostics.LastRetryCause
			}
			continue
		}
		if result.StatusCode >= 200 && result.StatusCode < 300 {
			if err := g.usageTracker.Record(ctx, masterKey, estTokens); err == nil {
				incrementQuota(masterKey, estTokens)
			}
			if cacheEnabled && len(result.Body) > 0 {
				_ = g.cache.Set(ctx, model, promptStr, string(result.Body), 24*time.Hour)
			}
			applyProxyHeaders(&result, diagnostics.headers())
			return result
		}
		if len(result.Body) > 0 {
			applyProxyHeaders(&result, diagnostics.headers())
			return result
		}
		lastErr = fmt.Sprintf("upstream status %d", result.StatusCode)
	}
	result := proxyResult{StatusCode: http.StatusBadGateway, ContentType: "application/json", Body: mustJSON(openAIError("upstream_error", lastErr, "api_error"))}
	applyProxyHeaders(&result, diagnostics.headers())
	return result
}

func (g *Gateway) executeOpenAIStream(ctx context.Context, w http.ResponseWriter, translatedBody []byte, model, promptStr string, temperature *float64, estTokens int, masterKey *models.MasterKey) {
	cfg := loadSystemConfig()
	diagnostics := newUpstreamAttemptDiagnostics("chat.stream")
	flusher, canFlush := w.(http.Flusher)
	cacheEnabled := shouldUseSemanticCache(temperature)
	if cacheEnabled {
		cached, cacheErr := g.cache.Get(ctx, model, promptStr)
		if cacheErr == nil && cached != "" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(cached))
			if canFlush {
				flusher.Flush()
			}
			if err := g.usageTracker.Record(ctx, masterKey, estTokens); err == nil {
				incrementQuota(masterKey, estTokens)
			}
			return
		}
	}
	lastErr := "upstream request failed"
	for i := 0; i < cfg.MaxRetries; i++ {
		key, err := g.acquireKeyWithQueue(ctx, nil, nil, cfg.MaxConcurrency, false, "chat.stream")
		if key != "" {
			diagnostics.noteSelectedKey(key)
		}
		if err != nil {
			if err.Error() == "queue timeout" {
				http.Error(w, "Queue timeout, no available keys", http.StatusServiceUnavailable)
				return
			}
			lastErr = err.Error()
			continue
		}
		resp, reader, cancel, retry, err := g.openUpstreamStream(ctx, cfg, key, translatedBody, "chat.stream")
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
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write(mustJSON(openAIError("upstream_error", err.Error(), "api_error")))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		applyResponseHeaders(w.Header(), diagnostics.headers())
		w.WriteHeader(http.StatusOK)
		buf := bufferPool.Get().([]byte)
		var responseBuffer bytes.Buffer
		for {
			n, readErr := reader.Read(buf)
			if n > 0 {
				chunk := buf[:n]
				_, _ = w.Write(chunk)
				if cacheEnabled {
					responseBuffer.Write(chunk)
				}
				if canFlush {
					flusher.Flush()
				}
			}
			if readErr != nil {
				if readErr == io.EOF {
					break
				}
				_, _ = w.Write([]byte("\n\ndata: {\"error\": \"[STREAM_INTERRUPTED_BY_UPSTREAM]\"}\n\n"))
				if canFlush {
					flusher.Flush()
				}
				break
			}
		}
		bufferPool.Put(buf)
		_ = resp.Body.Close()
		if cancel != nil {
			cancel()
		}
		_ = g.scheduler.ReleaseKey(ctx, key)
		if err := g.usageTracker.Record(ctx, masterKey, estTokens); err == nil {
			incrementQuota(masterKey, estTokens)
		}
		if cacheEnabled && responseBuffer.Len() > 0 {
			_ = g.cache.Set(ctx, model, promptStr, responseBuffer.String(), 24*time.Hour)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	_, _ = w.Write(mustJSON(openAIError("upstream_error", lastErr, "api_error")))
}

func (g *Gateway) fetchUpstreamModels(ctx context.Context) proxyResult {
	cfg := loadSystemConfig()
	diagnostics := newUpstreamAttemptDiagnostics("models.list")
	lastErr := "upstream request failed"
	for i := 0; i < cfg.MaxRetries; i++ {
		key, err := g.acquireKeyWithQueue(ctx, nil, nil, cfg.MaxConcurrency, false, "models.list")
		if key != "" {
			diagnostics.noteSelectedKey(key)
		}
		if err != nil {
			if err.Error() == "queue timeout" {
				return proxyResult{StatusCode: http.StatusServiceUnavailable, ContentType: "application/json", Body: mustJSON(openAIError("queue_timeout", "Queue timeout, no available keys", "server_error"))}
			}
			lastErr = err.Error()
			continue
		}
		resp, cancel, err := g.openUpstreamHeadersWithTimeout(ctx, cfg, key, http.MethodGet, "models", nil, "application/json")
		if err != nil {
			if cancel != nil {
				cancel()
			}
			_ = g.scheduler.ReleaseKey(ctx, key)
			if errors.Is(err, errUpstreamFirstByteTimeout) {
				diagnostics.noteRetry("NVIDIA 官方 /models 首包超时，已切换下一个上游 Key")
				lastErr = diagnostics.LastRetryCause
				recordUpstreamRuntimeEvent("models.list", "first_byte_timeout", key, false, 0, "NVIDIA 官方 /models 首包超时，已切换下一个上游 Key")
				g.markCooling(ctx, key, "60")
				updateAPIKeyStatusByPlaintext(key, APIKeyStatusCooling)
				continue
			}
			diagnostics.noteRetry(err.Error())
			lastErr = diagnostics.LastRetryCause
			recordUpstreamRuntimeEvent("models.list", "upstream_error", key, false, 0, err.Error())
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if cancel != nil {
			cancel()
		}
		_ = g.scheduler.ReleaseKey(ctx, key)
		if resp.StatusCode == http.StatusTooManyRequests {
			diagnostics.noteRetry("上游 NVIDIA 官方 Key 被限流，已进入冷却")
			lastErr = diagnostics.LastRetryCause
			recordUpstreamRuntimeEvent("models.list", "rate_limited", key, false, resp.StatusCode, "上游 NVIDIA 官方 Key 被限流，已进入冷却")
			g.markCooling(ctx, key, resp.Header.Get("Retry-After"))
			updateAPIKeyStatusByPlaintext(key, APIKeyStatusCooling)
			continue
		}
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			diagnostics.noteRetry("上游 NVIDIA 官方 Key 鉴权失败，已标记为不可用")
			lastErr = diagnostics.LastRetryCause
			recordUpstreamRuntimeEvent("models.list", "auth_rejected", key, false, resp.StatusCode, "上游 NVIDIA 官方 Key 鉴权失败，已标记为不可用")
			_ = g.scheduler.MarkDead(ctx, key)
			updateAPIKeyStatusByPlaintext(key, APIKeyStatusDead)
			continue
		}
		contentType := resp.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "application/json"
		}
		recordUpstreamRuntimeEvent("models.list", "upstream_ok", key, resp.StatusCode >= 200 && resp.StatusCode < 300, resp.StatusCode, "已成功获取 NVIDIA 官方模型列表")
		result := proxyResult{StatusCode: resp.StatusCode, ContentType: contentType, Body: body}
		applyProxyHeaders(&result, diagnostics.headers())
		return result
	}
	result := proxyResult{StatusCode: http.StatusBadGateway, ContentType: "application/json", Body: mustJSON(openAIError("upstream_error", lastErr, "api_error"))}
	applyProxyHeaders(&result, diagnostics.headers())
	return result
}

func (g *Gateway) callUpstreamJSONPath(ctx context.Context, cfg models.SystemConfig, key, endpointPath string, body []byte) (proxyResult, bool) {
	operation := endpointPath
	resp, cancel, err := g.openUpstreamHeadersWithTimeout(ctx, cfg, key, http.MethodPost, endpointPath, body, "application/json")
	if err != nil {
		if cancel != nil {
			cancel()
		}
		if errors.Is(err, errUpstreamFirstByteTimeout) {
			recordUpstreamRuntimeEvent(operation, "first_byte_timeout", key, false, 0, "上游首包超时，已切换到下一个 NVIDIA 官方 Key")
			g.markCooling(ctx, key, "60")
			updateAPIKeyStatusByPlaintext(key, APIKeyStatusCooling)
			return proxyResult{}, true
		}
		recordUpstreamRuntimeEvent(operation, "upstream_error", key, false, 0, err.Error())
		return proxyResult{StatusCode: http.StatusBadGateway, ContentType: "application/json", Body: mustJSON(openAIError("upstream_request_error", err.Error(), "api_error"))}, true
	}
	defer resp.Body.Close()
	defer func() {
		if cancel != nil {
			cancel()
		}
	}()
	respBody, _ := io.ReadAll(resp.Body)
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		g.markCooling(ctx, key, resp.Header.Get("Retry-After"))
		updateAPIKeyStatusByPlaintext(key, APIKeyStatusCooling)
		return proxyResult{}, true
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		_ = g.scheduler.MarkDead(ctx, key)
		updateAPIKeyStatusByPlaintext(key, APIKeyStatusDead)
		return proxyResult{}, true
	}
	return proxyResult{StatusCode: resp.StatusCode, ContentType: contentType, Body: respBody}, false
}

func (g *Gateway) callUpstreamChat(ctx context.Context, cfg models.SystemConfig, key string, body []byte) (proxyResult, bool) {
	operation := "chat/completions"
	resp, cancel, err := g.openUpstreamHeadersWithTimeout(ctx, cfg, key, http.MethodPost, "chat/completions", body, "application/json")
	if err != nil {
		if cancel != nil {
			cancel()
		}
		if errors.Is(err, errUpstreamFirstByteTimeout) {
			recordUpstreamRuntimeEvent(operation, "first_byte_timeout", key, false, 0, "上游首包超时，已切换到下一个 NVIDIA 官方 Key")
			g.markCooling(ctx, key, "60")
			updateAPIKeyStatusByPlaintext(key, APIKeyStatusCooling)
			return proxyResult{}, true
		}
		recordUpstreamRuntimeEvent(operation, "upstream_error", key, false, 0, err.Error())
		return proxyResult{StatusCode: http.StatusBadGateway, ContentType: "application/json", Body: mustJSON(openAIError("upstream_request_error", err.Error(), "api_error"))}, true
	}
	defer resp.Body.Close()
	defer func() {
		if cancel != nil {
			cancel()
		}
	}()
	respBody, _ := io.ReadAll(resp.Body)
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		g.markCooling(ctx, key, resp.Header.Get("Retry-After"))
		updateAPIKeyStatusByPlaintext(key, APIKeyStatusCooling)
		return proxyResult{}, true
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		_ = g.scheduler.MarkDead(ctx, key)
		updateAPIKeyStatusByPlaintext(key, APIKeyStatusDead)
		return proxyResult{}, true
	}
	return proxyResult{StatusCode: resp.StatusCode, ContentType: contentType, Body: respBody}, false
}

func (g *Gateway) httpClient(cfg models.SystemConfig, key string) *http.Client {
	client := newHTTPClientForAPIKey(cfg, key)
	if g == nil || g.client == nil {
		return client
	}
	if g.client.Transport != nil {
		client.Transport = g.client.Transport
	}
	if g.client.Jar != nil {
		client.Jar = g.client.Jar
	}
	if g.client.CheckRedirect != nil {
		client.CheckRedirect = g.client.CheckRedirect
	}
	return client
}

func (g *Gateway) markCooling(ctx context.Context, key, retryAfter string) {
	duration := 60 * time.Second
	if seconds, err := time.ParseDuration(strings.TrimSpace(retryAfter) + "s"); err == nil {
		duration = seconds
	}
	_ = g.scheduler.MarkCooling(ctx, key, duration)
}

func setStreamFlag(body []byte, stream bool) []byte {
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	payload["stream"] = stream
	updated, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return updated
}

func claudeErrorResponse(message string) fiber.Map {
	return fiber.Map{
		"type": "error",
		"error": fiber.Map{
			"type":    "invalid_request_error",
			"message": message,
		},
	}
}

func geminiErrorResponse(message string) fiber.Map {
	return fiber.Map{
		"error": fiber.Map{
			"code":    400,
			"message": message,
			"status":  "INVALID_ARGUMENT",
		},
	}
}

func parseUpstreamError(raw []byte, fallback string) string {
	message := strings.TrimSpace(string(raw))
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err == nil {
		if errObj, ok := payload["error"].(map[string]any); ok {
			if msg, ok := errObj["message"].(string); ok && strings.TrimSpace(msg) != "" {
				return strings.TrimSpace(msg)
			}
		}
	}
	if message == "" {
		return fallback
	}
	return message
}

func mustJSON(v any) []byte {
	data, _ := json.Marshal(v)
	return data
}
