package gateway

import (
	"context"
	"net/http"

	"nvidia-api-gateway/pkg/models"

	"github.com/gofiber/fiber/v2"
)

func (g *Gateway) HandleOpenAIEmbeddings(c *fiber.Ctx) error {
	cfg := loadSystemConfig()
	if !protocolEnabled(cfg, "openai") {
		return c.Status(fiber.StatusNotFound).JSON(openAIError("route_disabled", "OpenAI compatibility route is disabled", "invalid_request_error"))
	}
	translatedBody, meta, err := TranslateEmbeddingsRequest(c.Body())
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(openAIError("invalid_request_error", err.Error(), "invalid_request_error"))
	}
	masterKey, _ := c.Locals("masterKey").(*models.MasterKey)
	estTokens := EstimateTokens(meta.Prompt)
	if err := g.usageTracker.Check(context.Background(), masterKey, estTokens); err != nil {
		status := fiber.StatusTooManyRequests
		errorCode := "rate_limit_exceeded"
		if err.Error() == "Quota exceeded" {
			status = fiber.StatusPaymentRequired
			errorCode = "quota_exceeded"
		}
		return c.Status(status).JSON(openAIError(errorCode, err.Error(), "rate_limit_error"))
	}
	result := g.executeOpenAIJSONPath(context.Background(), translatedBody, "embeddings", estTokens, masterKey)
	if result.ContentType != "" {
		c.Set("Content-Type", result.ContentType)
	}
	applyResponseHeaders(c, result.Headers)
	return c.Status(result.StatusCode).Send(result.Body)
}

func (g *Gateway) executeOpenAIJSONPath(ctx context.Context, translatedBody []byte, endpointPath string, estTokens int, masterKey *models.MasterKey) proxyResult {
	cfg := loadSystemConfig()
	diagnostics := newUpstreamAttemptDiagnostics(endpointPath)
	lastErr := "upstream request failed"
	for i := 0; i < cfg.MaxRetries; i++ {
		key, err := g.acquireKeyWithQueue(ctx, nil, nil, cfg.MaxConcurrency, false, endpointPath)
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
		result, retry := g.callUpstreamJSONPath(ctx, cfg, key, endpointPath, translatedBody)
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
			applyProxyHeaders(&result, diagnostics.headers())
			return result
		}
		if len(result.Body) > 0 {
			applyProxyHeaders(&result, diagnostics.headers())
			return result
		}
		lastErr = "upstream request failed"
	}
	result := proxyResult{StatusCode: http.StatusBadGateway, ContentType: "application/json", Body: mustJSON(openAIError("upstream_error", lastErr, "api_error"))}
	applyProxyHeaders(&result, diagnostics.headers())
	return result
}
