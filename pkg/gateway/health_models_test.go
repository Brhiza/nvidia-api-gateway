package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nvidia-api-gateway/pkg/db"
	"nvidia-api-gateway/pkg/models"
	"nvidia-api-gateway/pkg/scheduler"
	"nvidia-api-gateway/pkg/utils"

	"github.com/gofiber/fiber/v2"
)

const testEncryptionKey = "12345678901234567890123456789012"

func TestGetHealthReportDoesNotProbeOnColdLoad(t *testing.T) {
	systemHealthStore = &healthReportStore{}
	requestCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		http.NotFound(w, r)
	}))
	defer upstream.Close()

	sched := prepareGatewayTestState(t, upstream.URL+"/v1", []testAPIKey{{Name: "NVIDIA-01", Plaintext: "good-key", Weight: 1, Status: APIKeyStatusActive}})

	app := fiber.New()
	app.Get("/admin/health/report", GetHealthReport(sched))

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/admin/health/report", nil))
	if err != nil {
		t.Fatalf("cold health report request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var payload healthReport
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode cold health report: %v", err)
	}
	if requestCount != 0 {
		t.Fatalf("expected cold GET to avoid upstream probing, got %d requests", requestCount)
	}
	if len(payload.Checks) != 0 {
		t.Fatalf("expected no live checks on cold GET, got %d", len(payload.Checks))
	}
}

func TestHealthProbeUsesDedicatedKeyAndSkipsScheduler(t *testing.T) {
	systemHealthStore = &healthReportStore{}
	authHeaders := make([]string, 0)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/v1/models":
			writeJSON(w, http.StatusOK, map[string]any{
				"data": []map[string]any{{"id": "nvidia/nemotron-mini-4b-instruct"}, {"id": "nvidia/nv-embed-v1"}},
			})
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"O\"}}]}\n\n"))
		case "/v1/embeddings":
			writeJSON(w, http.StatusOK, map[string]any{
				"model": "nvidia/nv-embed-v1",
				"data":  []map[string]any{{"embedding": []float64{0.1, 0.2}}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	sched := prepareGatewayTestState(t, upstream.URL+"/v1", []testAPIKey{
		{Name: "NVIDIA-shared", Plaintext: "shared-key", Weight: 1, Status: APIKeyStatusActive},
		{Name: "NVIDIA-probe", Plaintext: "probe-key", Weight: 1, Status: APIKeyStatusActive, ProbeOnly: true},
	})
	stats, err := sched.Stats(context.Background())
	if err != nil {
		t.Fatalf("scheduler stats failed: %v", err)
	}
	if stats.Active != 1 {
		t.Fatalf("expected scheduler to load only shared keys, got %d active", stats.Active)
	}

	report, err := buildSystemHealthReport(context.Background(), sched, healthRunRequest{Scope: "all", Protocol: "auto"})
	if err != nil {
		t.Fatalf("build system health report failed: %v", err)
	}
	if !report.ProbeKeyDedicated {
		t.Fatalf("expected dedicated probe key to be selected")
	}
	if report.ProbeKeyName != "NVIDIA-probe" {
		t.Fatalf("expected probe key NVIDIA-probe, got %s", report.ProbeKeyName)
	}
	for _, header := range authHeaders {
		if header != "Bearer probe-key" {
			t.Fatalf("expected all health probe requests to use dedicated key, got %q", header)
		}
	}
}

func TestUpstreamRuntimeEventLabels(t *testing.T) {
	systemUpstreamRuntimeStore = &upstreamRuntimeStore{}
	recordUpstreamRuntimeEvent("chat.nonstream", "first_byte_timeout", "", false, 0, "timeout")
	snapshot := systemUpstreamRuntimeStore.snapshot(nil)
	if snapshot.LastEvent == nil {
		t.Fatal("expected last event")
	}
	if snapshot.LastEvent.OperationLabel != upstreamOperationLabel("chat.nonstream") {
		t.Fatalf("unexpected operation label: %q", snapshot.LastEvent.OperationLabel)
	}
	if snapshot.LastEvent.StageLabel != upstreamStageLabel("first_byte_timeout") {
		t.Fatalf("unexpected stage label: %q", snapshot.LastEvent.StageLabel)
	}
}

func TestAdminUpstreamModelsAndHealthRuns(t *testing.T) {
	systemHealthStore = &healthReportStore{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			writeJSON(w, http.StatusOK, map[string]any{
				"data": []map[string]any{
					{"id": "meta/llama-3.1-8b-instruct"},
					{"id": "nvidia/nv-embed-v1"},
				},
			})
		case "/v1/chat/completions":
			writeJSON(w, http.StatusOK, map[string]any{
				"id":    "chatcmpl_test",
				"model": "meta/llama-3.1-8b-instruct",
				"choices": []map[string]any{{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "OK",
					},
					"finish_reason": "stop",
				}},
			})
		case "/v1/embeddings":
			writeJSON(w, http.StatusOK, map[string]any{
				"model": "nvidia/nv-embed-v1",
				"data": []map[string]any{{
					"embedding": []float64{0.1, 0.2, 0.3},
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	sched := prepareGatewayTestState(t, upstream.URL+"/v1", []testAPIKey{{Name: "NVIDIA-01", Plaintext: "good-key", Weight: 1, Status: APIKeyStatusActive}})

	app := fiber.New()
	app.Get("/admin/upstream/models", GetUpstreamModels())
	app.Post("/admin/health/report/run", RunHealthReport(sched))
	app.Get("/admin/health/report", GetHealthReport(sched))

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/admin/upstream/models", nil))
	if err != nil {
		t.Fatalf("upstream models request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var modelsPayload upstreamModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelsPayload); err != nil {
		t.Fatalf("decode upstream models response: %v", err)
	}
	if len(modelsPayload.Models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(modelsPayload.Models))
	}
	if !modelsPayload.Models[0].SupportsChatCandidate {
		t.Fatalf("expected chat candidate flag on %s", modelsPayload.Models[0].ID)
	}
	foundEmbedding := false
	for _, item := range modelsPayload.Models {
		if item.ID == "nvidia/nv-embed-v1" {
			foundEmbedding = item.SupportsEmbeddingsCandidate
		}
	}
	if !foundEmbedding {
		t.Fatalf("expected embedding candidate flag for nvidia/nv-embed-v1")
	}

	fullReq := httptest.NewRequest(http.MethodPost, "/admin/health/report/run", strings.NewReader(`{"scope":"all","protocol":"auto"}`))
	fullReq.Header.Set("Content-Type", "application/json")
	fullResp, err := app.Test(fullReq)
	if err != nil {
		t.Fatalf("full health run failed: %v", err)
	}
	defer fullResp.Body.Close()
	var fullReport healthReport
	if err := json.NewDecoder(fullResp.Body).Decode(&fullReport); err != nil {
		t.Fatalf("decode full report: %v", err)
	}
	if fullReport.FullSweep == nil {
		t.Fatalf("expected fullSweep in full health run")
	}
	if fullReport.FullSweep.Summary.Total != 2 {
		t.Fatalf("expected full sweep total 2, got %d", fullReport.FullSweep.Summary.Total)
	}
	if fullReport.ActiveRun == nil || fullReport.ActiveRun.Summary.Total != 2 {
		t.Fatalf("expected activeRun total 2, got %+v", fullReport.ActiveRun)
	}

	singleReq := httptest.NewRequest(http.MethodPost, "/admin/health/report/run", strings.NewReader(`{"scope":"single","modelId":"meta/llama-3.1-8b-instruct","protocol":"chat"}`))
	singleReq.Header.Set("Content-Type", "application/json")
	singleResp, err := app.Test(singleReq)
	if err != nil {
		t.Fatalf("single health run failed: %v", err)
	}
	defer singleResp.Body.Close()
	var singleReport healthReport
	if err := json.NewDecoder(singleResp.Body).Decode(&singleReport); err != nil {
		t.Fatalf("decode single report: %v", err)
	}
	if singleReport.ActiveRun == nil || singleReport.ActiveRun.Summary.Total != 1 {
		t.Fatalf("expected single activeRun total 1, got %+v", singleReport.ActiveRun)
	}
	if singleReport.FullSweep == nil || singleReport.FullSweep.Summary.Total != 2 {
		t.Fatalf("expected preserved fullSweep total 2, got %+v", singleReport.FullSweep)
	}

	systemHealthStore = &healthReportStore{}
	persistedResp, err := app.Test(httptest.NewRequest(http.MethodGet, "/admin/health/report", nil))
	if err != nil {
		t.Fatalf("persisted health report request failed: %v", err)
	}
	defer persistedResp.Body.Close()
	var persistedReport healthReport
	if err := json.NewDecoder(persistedResp.Body).Decode(&persistedReport); err != nil {
		t.Fatalf("decode persisted report: %v", err)
	}
	if persistedReport.FullSweep == nil || persistedReport.FullSweep.Summary.Total != 2 {
		t.Fatalf("expected persisted fullSweep total 2 after refresh, got %+v", persistedReport.FullSweep)
	}
}

type testAPIKey struct {
	Name      string
	Plaintext string
	Weight    float64
	Status    string
	ProbeOnly bool
}

func prepareGatewayTestState(t *testing.T, upstreamBaseURL string, apiKeys []testAPIKey) *scheduler.Scheduler {
	t.Helper()
	if err := os.Setenv("ENCRYPTION_KEY", testEncryptionKey); err != nil {
		t.Fatalf("set encryption key: %v", err)
	}
	storePath := filepath.Join(t.TempDir(), "gateway.json")
	db.InitDB(storePath)
	if err := db.UpdateStore(func(store *db.Store) error {
		store.SystemConfig = models.NormalizeSystemConfig(models.SystemConfig{
			UpstreamBaseURL:       upstreamBaseURL,
			SchedulerStrategy:     models.DefaultSchedulerStrategy,
			MaxRetries:            3,
			MaxConcurrency:        2,
			RequestTimeoutSecond:  10,
			EnableOpenAI:          true,
			EnableClaude:          true,
			EnableGemini:          true,
			AnonymousAccess:       false,
			FirstByteTimeoutMs:    50,
			HealthProbeTimeoutSec: 2,
		})
		store.APIKeys = make([]models.APIKey, 0, len(apiKeys))
		store.NextAPIID = 1
		for _, item := range apiKeys {
			encrypted, err := utils.Encrypt(item.Plaintext, testEncryptionKey)
			if err != nil {
				return err
			}
			store.APIKeys = append(store.APIKeys, models.APIKey{
				ID:        store.NextAPIID,
				Key:       encrypted,
				Name:      item.Name,
				Weight:    item.Weight,
				Status:    item.Status,
				ProbeOnly: item.ProbeOnly,
			})
			store.NextAPIID++
		}
		return nil
	}); err != nil {
		t.Fatalf("update store: %v", err)
	}
	sched := scheduler.NewScheduler(nil)
	if err := LoadActiveKeys(context.Background(), sched); err != nil {
		t.Fatalf("load active keys: %v", err)
	}
	return sched
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
