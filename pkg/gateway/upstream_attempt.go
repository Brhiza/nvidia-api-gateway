package gateway

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"nvidia-api-gateway/pkg/models"
)

var errUpstreamFirstByteTimeout = errors.New("upstream first byte timeout")

type upstreamDoResult struct {
	resp *http.Response
	err  error
}

type firstReadResult struct {
	data []byte
	err  error
}

func (g *Gateway) openUpstreamHeadersWithTimeout(
	ctx context.Context,
	cfg models.SystemConfig,
	key, method, endpointPath string,
	body []byte,
	accept string,
) (*http.Response, context.CancelFunc, error) {
	reqCtx, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(reqCtx, method, buildUpstreamURL(cfg, endpointPath), bytes.NewReader(body))
	if err != nil {
		cancel()
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}

	resultCh := make(chan upstreamDoResult, 1)
	go func() {
		resp, doErr := g.httpClient(cfg, key).Do(req)
		resultCh <- upstreamDoResult{resp: resp, err: doErr}
	}()

	timeout := firstByteTimeout(cfg)
	if timeout <= 0 {
		result := <-resultCh
		if result.err != nil {
			cancel()
			return nil, nil, result.err
		}
		return result.resp, cancel, nil
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case result := <-resultCh:
		if result.err != nil {
			cancel()
			return nil, nil, result.err
		}
		return result.resp, cancel, nil
	case <-timer.C:
		cancel()
		return nil, nil, errUpstreamFirstByteTimeout
	case <-ctx.Done():
		cancel()
		return nil, nil, ctx.Err()
	}
}

func (g *Gateway) openUpstreamStreamWithPrefetch(
	ctx context.Context,
	cfg models.SystemConfig,
	key string,
	body []byte,
) (*http.Response, io.Reader, context.CancelFunc, error) {
	timeout := firstByteTimeout(cfg)
	if timeout <= 0 {
		timeout = time.Duration(models.DefaultFirstByteTimeoutMs) * time.Millisecond
	}

	reqCtx, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, buildUpstreamURL(cfg, "chat/completions"), bytes.NewReader(body))
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "text/event-stream")

	startedAt := time.Now()
	resultCh := make(chan upstreamDoResult, 1)
	go func() {
		resp, doErr := g.httpClient(cfg, key).Do(req)
		resultCh <- upstreamDoResult{resp: resp, err: doErr}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var resp *http.Response
	select {
	case result := <-resultCh:
		if result.err != nil {
			cancel()
			return nil, nil, nil, result.err
		}
		resp = result.resp
	case <-timer.C:
		cancel()
		return nil, nil, nil, errUpstreamFirstByteTimeout
	case <-ctx.Done():
		cancel()
		return nil, nil, nil, ctx.Err()
	}

	remaining := timeout - time.Since(startedAt)
	if remaining <= 0 {
		_ = resp.Body.Close()
		cancel()
		return nil, nil, nil, errUpstreamFirstByteTimeout
	}

	readCh := make(chan firstReadResult, 1)
	go func() {
		buf := make([]byte, 4096)
		n, readErr := resp.Body.Read(buf)
		payload := []byte(nil)
		if n > 0 {
			payload = append(payload, buf[:n]...)
		}
		readCh <- firstReadResult{data: payload, err: readErr}
	}()

	timer.Reset(remaining)
	select {
	case result := <-readCh:
		if len(result.data) == 0 {
			_ = resp.Body.Close()
			cancel()
			if result.err != nil {
				return nil, nil, nil, result.err
			}
			return nil, nil, nil, fmt.Errorf("upstream stream returned no data")
		}
		return resp, io.MultiReader(bytes.NewReader(result.data), resp.Body), cancel, nil
	case <-timer.C:
		_ = resp.Body.Close()
		cancel()
		return nil, nil, nil, errUpstreamFirstByteTimeout
	case <-ctx.Done():
		_ = resp.Body.Close()
		cancel()
		return nil, nil, nil, ctx.Err()
	}
}
