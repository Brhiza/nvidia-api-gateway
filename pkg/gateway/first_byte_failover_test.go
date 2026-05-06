package gateway

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFirstByteFailoverForNonStreamRequests(t *testing.T) {
	var slowHits int32
	var fastHits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer slow-key":
			atomic.AddInt32(&slowHits, 1)
			time.Sleep(150 * time.Millisecond)
			writeJSON(w, http.StatusOK, map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{"role": "assistant", "content": "slow"},
				}},
			})
		case "Bearer fast-key":
			atomic.AddInt32(&fastHits, 1)
			writeJSON(w, http.StatusOK, map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{"role": "assistant", "content": "fast"},
				}},
			})
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer upstream.Close()

	sched := prepareGatewayTestState(t, upstream.URL+"/v1", []testAPIKey{
		{Name: "slow", Plaintext: "slow-key", Weight: 1, Status: APIKeyStatusActive},
		{Name: "fast", Plaintext: "fast-key", Weight: 1, Status: APIKeyStatusActive},
	})
	gw := NewGateway(sched, nil, nil)
	result := gw.executeOpenAINonStream(context.Background(), []byte(`{"model":"meta/llama-3.1-8b-instruct","messages":[{"role":"user","content":"hello"}]}`), "meta/llama-3.1-8b-instruct", "hello", nil, 0, nil)
	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d with body %s", result.StatusCode, string(result.Body))
	}
	if !bytes.Contains(result.Body, []byte("fast")) {
		t.Fatalf("expected fast-key response, got %s", string(result.Body))
	}
	if atomic.LoadInt32(&slowHits) == 0 || atomic.LoadInt32(&fastHits) == 0 {
		t.Fatalf("expected both keys to be attempted, slow=%d fast=%d", slowHits, fastHits)
	}
}

func TestFirstByteFailoverForStreamRequests(t *testing.T) {
	var slowHits int32
	var fastHits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		switch r.Header.Get("Authorization") {
		case "Bearer slow-key":
			atomic.AddInt32(&slowHits, 1)
			w.WriteHeader(http.StatusOK)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			time.Sleep(150 * time.Millisecond)
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"slow\"}}]}\n\n")
		case "Bearer fast-key":
			atomic.AddInt32(&fastHits, 1)
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"fast\"}}]}\n\n")
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer upstream.Close()

	sched := prepareGatewayTestState(t, upstream.URL+"/v1", []testAPIKey{
		{Name: "slow", Plaintext: "slow-key", Weight: 1, Status: APIKeyStatusActive},
		{Name: "fast", Plaintext: "fast-key", Weight: 1, Status: APIKeyStatusActive},
	})
	gw := NewGateway(sched, nil, nil)
	writer := newCaptureWriter()
	startedAt := time.Now()
	gw.executeOpenAIStream(context.Background(), writer, []byte(`{"model":"meta/llama-3.1-8b-instruct","messages":[{"role":"user","content":"hello"}],"stream":true}`), "meta/llama-3.1-8b-instruct", "hello", nil, 0, nil)
	if writer.status != http.StatusOK {
		t.Fatalf("expected 200, got %d", writer.status)
	}
	if !strings.Contains(writer.body.String(), "fast") || strings.Contains(writer.body.String(), "slow") {
		t.Fatalf("expected only fast stream content, got %s", writer.body.String())
	}
	if writer.firstWriteAt.Sub(startedAt) < 40*time.Millisecond {
		t.Fatalf("expected no downstream bytes before timeout-based switch, first write after %s", writer.firstWriteAt.Sub(startedAt))
	}
	if atomic.LoadInt32(&slowHits) == 0 || atomic.LoadInt32(&fastHits) == 0 {
		t.Fatalf("expected both keys to be attempted, slow=%d fast=%d", slowHits, fastHits)
	}
}

func TestStreamDoesNotRetryAfterFirstChunkWasSent(t *testing.T) {
	var slowHits int32
	var fastHits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer slow-key":
			atomic.AddInt32(&slowHits, 1)
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatalf("response writer does not support hijacking")
			}
			conn, rw, err := hj.Hijack()
			if err != nil {
				t.Fatalf("hijack failed: %v", err)
			}
			defer conn.Close()
			_, _ = rw.WriteString("HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nConnection: close\r\n\r\n")
			_, _ = rw.WriteString("data: {\"choices\":[{\"delta\":{\"content\":\"slow-first\"}}]}\n\n")
			_ = rw.Flush()
		case "Bearer fast-key":
			atomic.AddInt32(&fastHits, 1)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"second-key\"}}]}\n\n")
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer upstream.Close()

	sched := prepareGatewayTestState(t, upstream.URL+"/v1", []testAPIKey{
		{Name: "slow", Plaintext: "slow-key", Weight: 1, Status: APIKeyStatusActive},
		{Name: "fast", Plaintext: "fast-key", Weight: 1, Status: APIKeyStatusActive},
	})
	gw := NewGateway(sched, nil, nil)
	writer := newCaptureWriter()
	gw.executeOpenAIStream(context.Background(), writer, []byte(`{"model":"meta/llama-3.1-8b-instruct","messages":[{"role":"user","content":"hello"}],"stream":true}`), "meta/llama-3.1-8b-instruct", "hello", nil, 0, nil)
	if !strings.Contains(writer.body.String(), "slow-first") {
		t.Fatalf("expected first chunk from slow key, got %s", writer.body.String())
	}
	if strings.Contains(writer.body.String(), "second-key") {
		t.Fatalf("expected no hidden retry after first chunk, got %s", writer.body.String())
	}
	if atomic.LoadInt32(&fastHits) != 0 {
		t.Fatalf("expected fast key to remain unused after first chunk, fastHits=%d slowHits=%d", fastHits, slowHits)
	}
}

type captureWriter struct {
	header       http.Header
	body         bytes.Buffer
	status       int
	firstWriteAt time.Time
}

func newCaptureWriter() *captureWriter {
	return &captureWriter{header: make(http.Header)}
}

func (w *captureWriter) Header() http.Header {
	return w.header
}

func (w *captureWriter) WriteHeader(statusCode int) {
	if w.status == 0 {
		w.status = statusCode
		w.firstWriteAt = time.Now()
	}
}

func (w *captureWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if w.firstWriteAt.IsZero() {
		w.firstWriteAt = time.Now()
	}
	return w.body.Write(p)
}

func (w *captureWriter) Flush() {}

var _ http.ResponseWriter = (*captureWriter)(nil)
var _ http.Flusher = (*captureWriter)(nil)
