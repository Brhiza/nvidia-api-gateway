package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestProbeAPIKeyReturnsChineseMessageAndModelsEndpoint(t *testing.T) {
	systemHealthStore = &healthReportStore{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer good-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": []map[string]any{{"id": "meta/llama-3.1-8b-instruct"}}})
	}))
	defer upstream.Close()

	sched := prepareGatewayTestState(t, upstream.URL+"/v1", []testAPIKey{{Name: "NVIDIA-01", Plaintext: "good-key", Weight: 1, Status: APIKeyStatusActive}})

	app := fiber.New()
	app.Post("/admin/keys/:id/probe", ProbeAPIKey(sched))

	resp, err := app.Test(httptest.NewRequest(http.MethodPost, "/admin/keys/1/probe", nil))
	if err != nil {
		t.Fatalf("probe request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var payload struct {
		Message string `json:"message"`
		Probe   struct {
			Endpoint   string `json:"endpoint"`
			Method     string `json:"method"`
			HTTPStatus int    `json:"httpStatus"`
			DurationMs int64  `json:"durationMs"`
			Detail     string `json:"detail"`
		} `json:"probe"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode probe response: %v", err)
	}
	if payload.Message == "" {
		t.Fatalf("expected localized message")
	}
	if payload.Probe.Method != http.MethodGet {
		t.Fatalf("expected GET probe method, got %s", payload.Probe.Method)
	}
	if payload.Probe.HTTPStatus != http.StatusOK {
		t.Fatalf("expected 200 status, got %d", payload.Probe.HTTPStatus)
	}
	if got := payload.Probe.Endpoint; got != upstream.URL+"/v1/models" {
		t.Fatalf("expected models endpoint, got %s", got)
	}
}
