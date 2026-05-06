package gateway

import (
	"context"
	"net/http"
	"time"
)

type apiKeyProbeResult struct {
	Status     string `json:"status"`
	Endpoint   string `json:"endpoint"`
	Method     string `json:"method"`
	HTTPStatus int    `json:"httpStatus"`
	DurationMs int64  `json:"durationMs"`
	Detail     string `json:"detail"`
}

func probeKeyStatus(ctx context.Context, apiKey string) (*apiKeyProbeResult, bool) {
	cfg := loadSystemConfig()
	result := &apiKeyProbeResult{
		Endpoint: buildUpstreamURL(cfg, "models"),
		Method:   http.MethodGet,
		Detail:   "已调用 NVIDIA 官方 /models 接口验证该 Key 是否可用",
	}

	startedAt := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, result.Endpoint, nil)
	if err != nil {
		result.DurationMs = time.Since(startedAt).Milliseconds()
		result.Detail = err.Error()
		return result, false
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := newHTTPClientForAPIKey(cfg, apiKey).Do(req)
	result.DurationMs = time.Since(startedAt).Milliseconds()
	if err != nil {
		result.Detail = err.Error()
		return result, false
	}
	defer resp.Body.Close()
	result.HTTPStatus = resp.StatusCode

	switch resp.StatusCode {
	case http.StatusOK:
		result.Status = APIKeyStatusActive
		return result, true
	case http.StatusUnauthorized, http.StatusForbidden:
		result.Status = APIKeyStatusDead
		return result, true
	case http.StatusTooManyRequests:
		result.Status = APIKeyStatusCooling
		return result, true
	default:
		return result, false
	}
}
