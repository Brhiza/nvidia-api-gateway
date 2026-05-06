package gateway

import (
	"context"
	"net/http"
	"time"

	"nvidia-api-gateway/pkg/models"
)

func newHTTPClient(cfg models.SystemConfig) *http.Client {
	return newHTTPClientWithProxyOverride(cfg, nil)
}

func newFirstByteContext(ctx context.Context, cfg models.SystemConfig) (context.Context, context.CancelFunc) {
	timeout := time.Duration(cfg.FirstByteTimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = time.Duration(models.DefaultFirstByteTimeoutMs) * time.Millisecond
	}
	return context.WithTimeout(ctx, timeout)
}

func firstByteTimeout(cfg models.SystemConfig) time.Duration {
	timeout := time.Duration(cfg.FirstByteTimeoutMs) * time.Millisecond
	if timeout <= 0 {
		return time.Duration(models.DefaultFirstByteTimeoutMs) * time.Millisecond
	}
	return timeout
}
