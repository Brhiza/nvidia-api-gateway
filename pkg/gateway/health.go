package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"nvidia-api-gateway/pkg/db"
	"nvidia-api-gateway/pkg/models"
	"nvidia-api-gateway/pkg/scheduler"
	"nvidia-api-gateway/pkg/utils"

	"github.com/gofiber/fiber/v2"
)

type healthSummary struct {
	OverallStatus   string  `json:"overallStatus"`
	TotalKeys       int     `json:"totalKeys"`
	ActiveKeys      int     `json:"activeKeys"`
	CoolingKeys     int     `json:"coolingKeys"`
	DeadKeys        int     `json:"deadKeys"`
	DisabledKeys    int     `json:"disabledKeys"`
	HealthyChecks   int     `json:"healthyChecks"`
	UnhealthyChecks int     `json:"unhealthyChecks"`
	AvgLatencyMs    float64 `json:"avgLatencyMs"`
}

type healthKeySnapshot struct {
	ID        uint      `json:"id"`
	Name      string    `json:"name"`
	Weight    float64   `json:"weight"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type healthCheckResult struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Method      string         `json:"method"`
	Endpoint    string         `json:"endpoint"`
	Success     bool           `json:"success"`
	HTTPStatus  int            `json:"httpStatus"`
	DurationMs  int64          `json:"durationMs"`
	StatusLabel string         `json:"statusLabel"`
	Detail      string         `json:"detail"`
	Meta        map[string]any `json:"meta,omitempty"`
}

type healthHistoryPoint struct {
	GeneratedAt     time.Time `json:"generatedAt"`
	Label           string    `json:"label"`
	AvgLatencyMs    float64   `json:"avgLatencyMs"`
	HealthyChecks   int       `json:"healthyChecks"`
	UnhealthyChecks int       `json:"unhealthyChecks"`
	OverallStatus   string    `json:"overallStatus"`
}

type healthModelCatalogItem struct {
	ID                          string `json:"id"`
	SupportsChatCandidate       bool   `json:"supportsChatCandidate"`
	SupportsEmbeddingsCandidate bool   `json:"supportsEmbeddingsCandidate"`
}

type healthLatencyChartPoint struct {
	Label string `json:"label"`
	Value int64  `json:"value"`
	Meta  string `json:"meta,omitempty"`
}

type healthModelRunSummary struct {
	Total        int     `json:"total"`
	Healthy      int     `json:"healthy"`
	Failed       int     `json:"failed"`
	AvgLatencyMs float64 `json:"avgLatencyMs"`
}

type healthModelRunCheck struct {
	GeneratedAt  time.Time      `json:"generatedAt"`
	ModelID      string         `json:"modelId"`
	Protocol     string         `json:"protocol"`
	Method       string         `json:"method"`
	Endpoint     string         `json:"endpoint"`
	Success      bool           `json:"success"`
	HTTPStatus   int            `json:"httpStatus"`
	DurationMs   int64          `json:"durationMs"`
	StatusLabel  string         `json:"statusLabel"`
	Detail       string         `json:"detail"`
	AttemptCount int            `json:"attemptCount"`
	Meta         map[string]any `json:"meta,omitempty"`
}

type healthModelRunResult struct {
	GeneratedAt     time.Time                 `json:"generatedAt"`
	Scope           string                    `json:"scope"`
	Protocol        string                    `json:"protocol"`
	SelectedModelID string                    `json:"selectedModelId"`
	Summary         healthModelRunSummary     `json:"summary"`
	Checks          []healthModelRunCheck     `json:"checks"`
	LatencyChart    []healthLatencyChartPoint `json:"latencyChart"`
}

type healthReport struct {
	GeneratedAt       time.Time                `json:"generatedAt"`
	UpstreamBaseURL   string                   `json:"upstreamBaseURL"`
	ProbeKeyName      string                   `json:"probeKeyName"`
	ProbeKeyDedicated bool                     `json:"probeKeyDedicated"`
	ProbeTimeoutSec   int                      `json:"probeTimeoutSecond"`
	Summary           healthSummary            `json:"summary"`
	SchedulerStats    *scheduler.Stats         `json:"schedulerStats"`
	Keys              []healthKeySnapshot      `json:"keys"`
	Checks            []healthCheckResult      `json:"checks"`
	History           []healthHistoryPoint     `json:"history"`
	Recommendations   []string                 `json:"recommendations"`
	ModelCatalog      []healthModelCatalogItem `json:"modelCatalog"`
	ActiveRun         *healthModelRunResult    `json:"activeRun,omitempty"`
	FullSweep         *healthModelRunResult    `json:"fullSweep,omitempty"`
}

type upstreamModelsResponse struct {
	GeneratedAt  time.Time                `json:"generatedAt"`
	ProbeKeyName string                   `json:"probeKeyName"`
	Models       []healthModelCatalogItem `json:"models"`
}

type healthRunRequest struct {
	Scope    string `json:"scope"`
	ModelID  string `json:"modelId"`
	Protocol string `json:"protocol"`
}

type healthProbeSelection struct {
	Name       string
	Plaintext  string
	Dedicated  bool
	Configured bool
}

type persistedHealthState struct {
	Latest       *healthReport            `json:"latest,omitempty"`
	History      []healthHistoryPoint     `json:"history,omitempty"`
	ActiveRun    *healthModelRunResult    `json:"activeRun,omitempty"`
	FullSweep    *healthModelRunResult    `json:"fullSweep,omitempty"`
	ModelCatalog []healthModelCatalogItem `json:"modelCatalog,omitempty"`
}

type healthReportStore struct {
	mu           sync.Mutex
	latest       *healthReport
	history      []healthHistoryPoint
	activeRun    *healthModelRunResult
	fullSweep    *healthModelRunResult
	modelCatalog []healthModelCatalogItem
}

var systemHealthStore = &healthReportStore{}

func GetHealthReport(sched *scheduler.Scheduler) fiber.Handler {
	return func(c *fiber.Ctx) error {
		systemHealthStore.ensureHydrated()
		if latest := systemHealthStore.getLatest(); latest != nil {
			return c.JSON(latest)
		}
		report, err := buildPassiveHealthReport(context.Background(), sched)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(report)
	}
}

func RunHealthReport(sched *scheduler.Scheduler) fiber.Handler {
	return func(c *fiber.Ctx) error {
		systemHealthStore.ensureHydrated()
		req := healthRunRequest{}
		if len(c.Body()) > 0 {
			if err := c.BodyParser(&req); err != nil {
				return c.Status(400).JSON(fiber.Map{"error": "请求体格式无效"})
			}
		}
		normalized, err := normalizeHealthRunRequest(req)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		report, err := buildSystemHealthReport(context.Background(), sched, normalized)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		systemHealthStore.put(report)
		return c.JSON(report)
	}
}

func GetUpstreamModels() fiber.Handler {
	return func(c *fiber.Ctx) error {
		systemHealthStore.ensureHydrated()
		catalog, probeKeyName, err := loadUpstreamModelCatalog(context.Background())
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		systemHealthStore.setModelCatalog(catalog)
		return c.JSON(upstreamModelsResponse{
			GeneratedAt:  time.Now(),
			ProbeKeyName: probeKeyName,
			Models:       catalog,
		})
	}
}

func normalizeHealthRunRequest(req healthRunRequest) (healthRunRequest, error) {
	scope := strings.ToLower(strings.TrimSpace(req.Scope))
	if scope == "" {
		scope = "all"
	}
	if scope != "all" && scope != "single" {
		return healthRunRequest{}, fmt.Errorf("scope 只能是 all 或 single")
	}
	protocol := strings.ToLower(strings.TrimSpace(req.Protocol))
	if protocol == "" {
		protocol = "auto"
	}
	if protocol != "auto" && protocol != "chat" && protocol != "embeddings" {
		return healthRunRequest{}, fmt.Errorf("protocol 只能是 auto、chat 或 embeddings")
	}
	modelID := strings.TrimSpace(req.ModelID)
	if scope == "single" && modelID == "" {
		return healthRunRequest{}, fmt.Errorf("单模型检查时必须提供 modelId")
	}
	return healthRunRequest{Scope: scope, ModelID: modelID, Protocol: protocol}, nil
}

func buildPassiveHealthReport(ctx context.Context, sched *scheduler.Scheduler) (*healthReport, error) {
	store, err := db.ReadStore()
	if err != nil {
		return nil, fmt.Errorf("load store: %w", err)
	}
	cfg := models.NormalizeSystemConfig(store.SystemConfig)
	stats, err := sched.Stats(ctx)
	if err != nil {
		stats = &scheduler.Stats{}
	}
	keys := buildHealthKeySnapshots(store.APIKeys)
	catalog := systemHealthStore.modelCatalogSnapshot()
	checks := make([]healthCheckResult, 0)
	recommendations := []string{"当前页面只展示缓存结果，不会在打开时主动探测上游。点击“立即检查”后，才会真实访问 NVIDIA 官方 API。"}
	if len(catalog) == 0 {
		recommendations = append(recommendations, "尚未缓存模型目录；如需单模型检测，请先手动执行一次健康检查。")
	}
	if !hasDedicatedHealthProbeKey(store.APIKeys) {
		recommendations = append(recommendations, "当前尚未配置“健康检查专用”上游 Key。为了避免健康检查占用业务流量，建议至少准备一个专用探测 Key，并在 Key 管理页开启“健康专用”。")
	}
	return &healthReport{
		GeneratedAt:       time.Now(),
		UpstreamBaseURL:   cfg.UpstreamBaseURL,
		ProbeKeyName:      "",
		ProbeKeyDedicated: false,
		ProbeTimeoutSec:   cfg.HealthProbeTimeoutSec,
		Summary:           buildHealthSummary(keys, checks),
		SchedulerStats:    stats,
		Keys:              keys,
		Checks:            checks,
		History:           systemHealthStore.historySnapshot(),
		Recommendations:   recommendations,
		ModelCatalog:      catalog,
		ActiveRun:         systemHealthStore.activeRunSnapshot(),
		FullSweep:         systemHealthStore.fullSweepSnapshot(),
	}, nil
}

func buildSystemHealthReport(ctx context.Context, sched *scheduler.Scheduler, runReq healthRunRequest) (*healthReport, error) {
	store, err := db.ReadStore()
	if err != nil {
		return nil, fmt.Errorf("load store: %w", err)
	}
	cfg := models.NormalizeSystemConfig(store.SystemConfig)
	stats, err := sched.Stats(ctx)
	if err != nil {
		stats = &scheduler.Stats{}
	}
	keys := buildHealthKeySnapshots(store.APIKeys)
	probeSelection, err := selectHealthProbeKey(store.APIKeys)
	checks := make([]healthCheckResult, 0, 3)
	catalog := systemHealthStore.modelCatalogSnapshot()
	if err != nil {
		checks = append(checks, healthCheckResult{
			ID:          "probe_key",
			Title:       "上游探测密钥",
			Method:      "INTERNAL",
			Endpoint:    cfg.UpstreamBaseURL,
			Success:     false,
			HTTPStatus:  0,
			DurationMs:  0,
			StatusLabel: "NoKey",
			Detail:      err.Error(),
		})
		report := &healthReport{
			GeneratedAt:       time.Now(),
			UpstreamBaseURL:   cfg.UpstreamBaseURL,
			ProbeKeyName:      "",
			ProbeKeyDedicated: false,
			ProbeTimeoutSec:   cfg.HealthProbeTimeoutSec,
			Summary:           buildHealthSummary(keys, checks),
			SchedulerStats:    stats,
			Keys:              keys,
			Checks:            checks,
			History:           systemHealthStore.historySnapshot(),
			Recommendations:   buildHealthRecommendations(keys, checks, nil),
			ModelCatalog:      catalog,
			ActiveRun:         systemHealthStore.activeRunSnapshot(),
			FullSweep:         systemHealthStore.fullSweepSnapshot(),
		}
		return report, nil
	}

	modelsCheck, availableModels := runUpstreamModelsCheck(ctx, cfg, probeSelection.Plaintext)
	checks = append(checks, modelsCheck)
	checks = append(checks, runUpstreamChatCheck(ctx, cfg, probeSelection.Plaintext, availableModels))
	checks = append(checks, runUpstreamEmbeddingsCheck(ctx, cfg, probeSelection.Plaintext, availableModels))
	if len(availableModels) > 0 {
		catalog = buildModelCatalog(availableModels)
	}

	report := &healthReport{
		GeneratedAt:       time.Now(),
		UpstreamBaseURL:   cfg.UpstreamBaseURL,
		ProbeKeyName:      probeSelection.Name,
		ProbeKeyDedicated: probeSelection.Dedicated,
		ProbeTimeoutSec:   cfg.HealthProbeTimeoutSec,
		Summary:           buildHealthSummary(keys, checks),
		SchedulerStats:    stats,
		Keys:              keys,
		Checks:            checks,
		History:           systemHealthStore.historySnapshot(),
		Recommendations:   buildHealthRecommendations(keys, checks, &probeSelection),
		ModelCatalog:      catalog,
		ActiveRun:         systemHealthStore.activeRunSnapshot(),
		FullSweep:         systemHealthStore.fullSweepSnapshot(),
	}

	if runReq.Scope != "" && len(catalog) > 0 {
		runResult := executeModelHealthRun(ctx, cfg, probeSelection.Plaintext, catalog, runReq)
		report.ActiveRun = cloneHealthModelRun(runResult)
		if runReq.Scope == "all" {
			report.FullSweep = cloneHealthModelRun(runResult)
		}
	}

	return report, nil
}

func buildHealthKeySnapshots(keys []models.APIKey) []healthKeySnapshot {
	items := make([]healthKeySnapshot, 0, len(keys))
	for _, key := range keys {
		items = append(items, healthKeySnapshot{
			ID:        key.ID,
			Name:      key.Name,
			Weight:    key.Weight,
			Status:    key.Status,
			UpdatedAt: key.UpdatedAt,
		})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

func hasDedicatedHealthProbeKey(keys []models.APIKey) bool {
	for _, key := range keys {
		if key.ProbeOnly {
			return true
		}
	}
	return false
}

func selectHealthProbeKey(keys []models.APIKey) (healthProbeSelection, error) {
	secret := strings.TrimSpace(utils.GetEncryptionKey())
	if secret == "" {
		return healthProbeSelection{}, fmt.Errorf("missing ENCRYPTION_KEY")
	}
	probeConfigured := hasDedicatedHealthProbeKey(keys)
	decryptCandidates := func(candidates []models.APIKey, dedicated bool) (healthProbeSelection, error) {
		for _, key := range candidates {
			plaintext, err := utils.Decrypt(key.Key, secret)
			if err != nil {
				continue
			}
			return healthProbeSelection{Name: key.Name, Plaintext: plaintext, Dedicated: dedicated, Configured: probeConfigured}, nil
		}
		if dedicated {
			return healthProbeSelection{}, fmt.Errorf("健康检查专用 Key 无法解密或不可用")
		}
		return healthProbeSelection{}, fmt.Errorf("no active NVIDIA upstream key available")
	}

	dedicatedActive := make([]models.APIKey, 0)
	sharedActive := make([]models.APIKey, 0)
	for _, key := range keys {
		if key.Status != APIKeyStatusActive {
			continue
		}
		if key.ProbeOnly {
			dedicatedActive = append(dedicatedActive, key)
			continue
		}
		sharedActive = append(sharedActive, key)
	}
	if len(dedicatedActive) > 0 {
		return decryptCandidates(dedicatedActive, true)
	}
	if probeConfigured {
		return healthProbeSelection{}, fmt.Errorf("已配置健康检查专用 Key，但当前没有 Active 状态的健康检查专用 Key")
	}
	if len(sharedActive) > 0 {
		return decryptCandidates(sharedActive, false)
	}
	return healthProbeSelection{}, fmt.Errorf("no active NVIDIA upstream key available")
}

func loadUpstreamModelCatalog(ctx context.Context) ([]healthModelCatalogItem, string, error) {
	store, err := db.ReadStore()
	if err != nil {
		return nil, "", fmt.Errorf("load store: %w", err)
	}
	cfg := models.NormalizeSystemConfig(store.SystemConfig)
	probeSelection, err := selectHealthProbeKey(store.APIKeys)
	if err != nil {
		return nil, "", err
	}
	modelsCheck, availableModels := runUpstreamModelsCheck(ctx, cfg, probeSelection.Plaintext)
	if !modelsCheck.Success {
		return nil, probeSelection.Name, errors.New(modelsCheck.Detail)
	}
	return buildModelCatalog(availableModels), probeSelection.Name, nil
}

func buildModelCatalog(modelIDs []string) []healthModelCatalogItem {
	items := make([]healthModelCatalogItem, 0, len(modelIDs))
	seen := make(map[string]struct{}, len(modelIDs))
	for _, raw := range modelIDs {
		modelID := strings.TrimSpace(raw)
		if modelID == "" {
			continue
		}
		if _, ok := seen[modelID]; ok {
			continue
		}
		seen[modelID] = struct{}{}
		lower := strings.ToLower(modelID)
		embeddings := strings.Contains(lower, "embed") || strings.Contains(lower, "embedding") || strings.Contains(lower, "e5") || strings.Contains(lower, "bge") || strings.Contains(lower, "rerank")
		items = append(items, healthModelCatalogItem{
			ID:                          modelID,
			SupportsChatCandidate:       true,
			SupportsEmbeddingsCandidate: embeddings,
		})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

func executeModelHealthRun(ctx context.Context, cfg models.SystemConfig, apiKey string, catalog []healthModelCatalogItem, req healthRunRequest) *healthModelRunResult {
	tasks := buildModelRunTasks(catalog, req)
	checks := runModelRunTasks(ctx, cfg, apiKey, tasks)
	run := &healthModelRunResult{
		GeneratedAt:     time.Now(),
		Scope:           req.Scope,
		Protocol:        req.Protocol,
		SelectedModelID: req.ModelID,
		Checks:          checks,
	}
	latencySum := int64(0)
	latencyCount := 0
	for _, check := range checks {
		run.Summary.Total++
		if check.Success {
			run.Summary.Healthy++
			latencySum += check.DurationMs
			latencyCount++
		} else {
			run.Summary.Failed++
		}
		run.LatencyChart = append(run.LatencyChart, healthLatencyChartPoint{Label: check.ModelID, Value: check.DurationMs, Meta: check.StatusLabel})
	}
	if latencyCount > 0 {
		run.Summary.AvgLatencyMs = float64(latencySum) / float64(latencyCount)
	}
	return run
}

type modelRunTask struct {
	ModelID  string
	Protocol string
}

func buildModelRunTasks(catalog []healthModelCatalogItem, req healthRunRequest) []modelRunTask {
	tasks := make([]modelRunTask, 0, len(catalog))
	appendTask := func(item healthModelCatalogItem) {
		protocol := req.Protocol
		if protocol == "auto" {
			if item.SupportsEmbeddingsCandidate {
				protocol = "embeddings"
			} else {
				protocol = "chat"
			}
		}
		tasks = append(tasks, modelRunTask{ModelID: item.ID, Protocol: protocol})
	}
	if req.Scope == "single" {
		for _, item := range catalog {
			if item.ID == req.ModelID {
				appendTask(item)
				break
			}
		}
		return tasks
	}
	for _, item := range catalog {
		appendTask(item)
	}
	return tasks
}

func runModelRunTasks(ctx context.Context, cfg models.SystemConfig, apiKey string, tasks []modelRunTask) []healthModelRunCheck {
	if len(tasks) == 0 {
		return nil
	}
	concurrency := 1
	results := make([]healthModelRunCheck, len(tasks))
	indexCh := make(chan int)
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range indexCh {
				task := tasks[idx]
				if task.Protocol == "embeddings" {
					results[idx] = runSingleModelEmbeddingsCheck(ctx, cfg, apiKey, task.ModelID)
				} else {
					results[idx] = runSingleModelChatCheck(ctx, cfg, apiKey, task.ModelID)
				}
			}
		}()
	}
	for idx := range tasks {
		indexCh <- idx
	}
	close(indexCh)
	wg.Wait()
	sort.SliceStable(results, func(i, j int) bool { return results[i].ModelID < results[j].ModelID })
	return results
}

func runSingleModelChatCheck(ctx context.Context, cfg models.SystemConfig, apiKey, modelID string) healthModelRunCheck {
	result := healthModelRunCheck{
		GeneratedAt:  time.Now(),
		ModelID:      modelID,
		Protocol:     "chat",
		Method:       http.MethodPost,
		Endpoint:     buildUpstreamURL(cfg, "chat/completions"),
		AttemptCount: 1,
		Meta:         map[string]any{"model": modelID, "probeMode": "stream_first_chunk"},
	}
	httpStatus, durationMs, detail, err := doHealthChatFirstChunkRequest(ctx, cfg, apiKey, modelID)
	result.DurationMs = durationMs
	result.HTTPStatus = httpStatus
	if err != nil {
		result.StatusLabel = "Error"
		result.Detail = err.Error()
		return result
	}
	if httpStatus < 200 || httpStatus >= 300 {
		result.StatusLabel = "Failed"
		result.Detail = detail
		return result
	}
	result.Success = true
	result.StatusLabel = "Healthy"
	result.Detail = detail
	return result
}

func runSingleModelEmbeddingsCheck(ctx context.Context, cfg models.SystemConfig, apiKey, modelID string) healthModelRunCheck {
	result := healthModelRunCheck{
		GeneratedAt:  time.Now(),
		ModelID:      modelID,
		Protocol:     "embeddings",
		Method:       http.MethodPost,
		Endpoint:     buildUpstreamURL(cfg, "embeddings"),
		AttemptCount: 1,
		Meta:         map[string]any{"model": modelID},
	}
	payload := map[string]any{
		"model": modelID,
		"input": []string{"NVIDIA", "gateway health check"},
	}
	body, _ := json.Marshal(payload)
	startedAt := time.Now()
	resp, err := doHealthRequest(ctx, cfg, apiKey, http.MethodPost, "embeddings", body)
	result.DurationMs = time.Since(startedAt).Milliseconds()
	if err != nil {
		result.StatusLabel = "Error"
		result.Detail = err.Error()
		return result
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	result.HTTPStatus = resp.StatusCode
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.StatusLabel = "Failed"
		result.Detail = parseUpstreamError(respBody, "embeddings request failed")
		return result
	}
	dim, dataCount, parsedModel, parseErr := inspectEmbeddingResponse(respBody)
	if parseErr != nil {
		result.StatusLabel = "Failed"
		result.Detail = "embeddings response parse failed"
		result.Meta["responsePreview"] = summarizeResponsePreview(respBody, 320)
		result.Meta["parseError"] = parseErr.Error()
		return result
	}
	result.Meta["dataCount"] = dataCount
	result.Success = dim > 0
	if result.Success {
		result.StatusLabel = "Healthy"
		result.Detail = fmt.Sprintf("embedding_dim=%d", dim)
		result.Meta["dimension"] = dim
		result.Meta["model"] = firstNonEmpty(parsedModel, modelID)
		return result
	}
	result.StatusLabel = "Failed"
	result.Detail = fmt.Sprintf("embeddings response returned empty vector (data_count=%d)", dataCount)
	result.Meta["responsePreview"] = summarizeResponsePreview(respBody, 320)
	result.Meta["model"] = firstNonEmpty(parsedModel, modelID)
	return result
}

func inspectEmbeddingResponse(respBody []byte) (int, int, string, error) {
	var parsed struct {
		Model string `json:"model"`
		Data  []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return 0, 0, "", err
	}
	maxDim := 0
	for _, item := range parsed.Data {
		if dim := len(item.Embedding); dim > maxDim {
			maxDim = dim
		}
	}
	return maxDim, len(parsed.Data), parsed.Model, nil
}

func summarizeResponsePreview(body []byte, limit int) string {
	if limit <= 0 {
		limit = 240
	}
	trimmed := strings.TrimSpace(string(body))
	trimmed = strings.ReplaceAll(trimmed, "\n", " ")
	trimmed = strings.ReplaceAll(trimmed, "\r", " ")
	if len(trimmed) <= limit {
		return trimmed
	}
	return trimmed[:limit] + "..."
}

func runUpstreamModelsCheck(ctx context.Context, cfg models.SystemConfig, apiKey string) (healthCheckResult, []string) {
	startedAt := time.Now()
	result := healthCheckResult{
		ID:       "nvidia_models",
		Title:    "NVIDIA 官方 /models",
		Method:   http.MethodGet,
		Endpoint: buildUpstreamURL(cfg, "models"),
		Meta:     map[string]any{},
	}
	resp, err := doHealthRequest(ctx, cfg, apiKey, http.MethodGet, "models", nil)
	result.DurationMs = time.Since(startedAt).Milliseconds()
	if err != nil {
		result.StatusLabel = "Error"
		result.Detail = err.Error()
		return result, nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	result.HTTPStatus = resp.StatusCode
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.StatusLabel = "Failed"
		result.Detail = parseUpstreamError(body, "models request failed")
		return result, nil
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(body, &payload)
	availableModels := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		if strings.TrimSpace(item.ID) != "" {
			availableModels = append(availableModels, item.ID)
		}
	}
	result.Success = true
	result.StatusLabel = "Healthy"
	result.Detail = fmt.Sprintf("models=%d", len(availableModels))
	result.Meta["modelCount"] = len(availableModels)
	if len(availableModels) > 0 {
		result.Meta["sampleModel"] = availableModels[0]
	}
	return result, availableModels
}

func runUpstreamChatCheck(ctx context.Context, cfg models.SystemConfig, apiKey string, availableModels []string) healthCheckResult {
	model := chooseChatHealthModel(availableModels)
	result := healthCheckResult{
		ID:       "nvidia_chat",
		Title:    "NVIDIA 官方 /chat/completions",
		Method:   http.MethodPost,
		Endpoint: buildUpstreamURL(cfg, "chat/completions"),
		Meta:     map[string]any{"model": model, "probeMode": "stream_first_chunk"},
	}
	if model == "" {
		result.StatusLabel = "Skipped"
		result.Detail = "no suitable chat model discovered from /models"
		return result
	}
	httpStatus, durationMs, detail, err := doHealthChatFirstChunkRequest(ctx, cfg, apiKey, model)
	result.DurationMs = durationMs
	result.HTTPStatus = httpStatus
	if err != nil {
		result.StatusLabel = "Error"
		result.Detail = err.Error()
		return result
	}
	if httpStatus < 200 || httpStatus >= 300 {
		result.StatusLabel = "Failed"
		result.Detail = detail
		return result
	}
	result.Success = true
	result.StatusLabel = "Healthy"
	result.Detail = detail
	return result
}

func runUpstreamEmbeddingsCheck(ctx context.Context, cfg models.SystemConfig, apiKey string, availableModels []string) healthCheckResult {
	model := chooseEmbeddingHealthModel(availableModels)
	result := healthCheckResult{
		ID:       "nvidia_embeddings",
		Title:    "NVIDIA 官方 /embeddings",
		Method:   http.MethodPost,
		Endpoint: buildUpstreamURL(cfg, "embeddings"),
		Meta:     map[string]any{"model": model},
	}
	if model == "" {
		result.StatusLabel = "Skipped"
		result.Detail = "no suitable embedding model discovered from /models"
		return result
	}
	payload := map[string]any{
		"model": model,
		"input": []string{"NVIDIA", "gateway health check"},
	}
	body, _ := json.Marshal(payload)
	startedAt := time.Now()
	resp, err := doHealthRequest(ctx, cfg, apiKey, http.MethodPost, "embeddings", body)
	result.DurationMs = time.Since(startedAt).Milliseconds()
	if err != nil {
		result.StatusLabel = "Error"
		result.Detail = err.Error()
		return result
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	result.HTTPStatus = resp.StatusCode
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.StatusLabel = "Failed"
		result.Detail = parseUpstreamError(respBody, "embeddings request failed")
		return result
	}
	dim, dataCount, parsedModel, parseErr := inspectEmbeddingResponse(respBody)
	if parseErr != nil {
		result.StatusLabel = "Failed"
		result.Detail = "embeddings response parse failed"
		result.Meta["responsePreview"] = summarizeResponsePreview(respBody, 320)
		result.Meta["parseError"] = parseErr.Error()
		return result
	}
	result.Meta["dataCount"] = dataCount
	result.Success = dim > 0
	if result.Success {
		result.StatusLabel = "Healthy"
		result.Detail = fmt.Sprintf("embedding_dim=%d", dim)
		result.Meta["dimension"] = dim
		result.Meta["model"] = firstNonEmpty(parsedModel, model)
		return result
	}
	result.StatusLabel = "Failed"
	result.Detail = fmt.Sprintf("embeddings response returned empty vector (data_count=%d)", dataCount)
	result.Meta["responsePreview"] = summarizeResponsePreview(respBody, 320)
	result.Meta["model"] = firstNonEmpty(parsedModel, model)
	return result
}

func newHealthProbeContext(ctx context.Context, cfg models.SystemConfig) (context.Context, context.CancelFunc) {
	timeout := time.Duration(cfg.HealthProbeTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = time.Duration(models.DefaultHealthProbeTimeoutSec) * time.Second
	}
	return context.WithTimeout(ctx, timeout)
}

func doHealthRequest(ctx context.Context, cfg models.SystemConfig, apiKey, method, endpoint string, body []byte) (*http.Response, error) {
	client := newHTTPClientForAPIKey(cfg, apiKey)
	bodyReader := bytes.NewReader(body)
	probeCtx, cancel := newHealthProbeContext(ctx, cfg)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, method, buildUpstreamURL(cfg, endpoint), bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	return client.Do(req)
}

func doHealthChatFirstChunkRequest(ctx context.Context, cfg models.SystemConfig, apiKey, modelID string) (int, int64, string, error) {
	payload := map[string]any{
		"model": modelID,
		"messages": []map[string]any{{
			"role":    "user",
			"content": "Reply with OK only.",
		}},
		"stream":      true,
		"max_tokens":  1,
		"temperature": 0,
	}
	body, _ := json.Marshal(payload)
	client := newHTTPClientForAPIKey(cfg, apiKey)
	probeCtx, cancel := newHealthProbeContext(ctx, cfg)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodPost, buildUpstreamURL(cfg, "chat/completions"), bytes.NewReader(body))
	if err != nil {
		return 0, 0, "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	startedAt := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, time.Since(startedAt).Milliseconds(), "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, time.Since(startedAt).Milliseconds(), parseUpstreamError(respBody, "chat request failed"), nil
	}
	buf := make([]byte, 1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			return resp.StatusCode, time.Since(startedAt).Milliseconds(), "已收到首个流式输出块", nil
		}
		if readErr != nil {
			if readErr == io.EOF {
				return resp.StatusCode, time.Since(startedAt).Milliseconds(), "chat 流式接口没有返回任何输出", nil
			}
			return resp.StatusCode, time.Since(startedAt).Milliseconds(), "", readErr
		}
	}
}

func chooseChatHealthModel(availableModels []string) string {
	preferred := []string{
		"nvidia/nemotron-mini-4b-instruct",
		"google/gemma-3-4b-it",
		"meta/llama-3.1-8b-instruct",
		"meta/llama-3.1-70b-instruct",
	}
	return choosePreferredModel(availableModels, preferred, false)
}

func chooseEmbeddingHealthModel(availableModels []string) string {
	preferred := []string{
		"nvidia/nv-embed-v1",
		"nvidia/nv-embedqa-e5-v5",
		"baai/bge-m3",
	}
	return choosePreferredModel(availableModels, preferred, true)
}

func choosePreferredModel(availableModels, preferred []string, allowContainsEmbed bool) string {
	set := make(map[string]struct{}, len(availableModels))
	for _, model := range availableModels {
		set[model] = struct{}{}
	}
	for _, candidate := range preferred {
		if _, ok := set[candidate]; ok {
			return candidate
		}
	}
	if allowContainsEmbed {
		for _, model := range availableModels {
			if strings.Contains(strings.ToLower(model), "embed") {
				return model
			}
		}
	}
	return ""
}

func buildHealthSummary(keys []healthKeySnapshot, checks []healthCheckResult) healthSummary {
	summary := healthSummary{OverallStatus: "healthy"}
	latencySum := int64(0)
	latencyCount := 0
	for _, key := range keys {
		summary.TotalKeys++
		switch key.Status {
		case APIKeyStatusActive:
			summary.ActiveKeys++
		case APIKeyStatusCooling:
			summary.CoolingKeys++
		case APIKeyStatusDead:
			summary.DeadKeys++
		case APIKeyStatusDisabled:
			summary.DisabledKeys++
		}
	}
	for _, check := range checks {
		if check.Success {
			summary.HealthyChecks++
			if check.DurationMs > 0 {
				latencySum += check.DurationMs
				latencyCount++
			}
		} else {
			summary.UnhealthyChecks++
		}
	}
	if latencyCount > 0 {
		summary.AvgLatencyMs = float64(latencySum) / float64(latencyCount)
	}
	switch {
	case summary.HealthyChecks == 0 && summary.UnhealthyChecks > 0:
		summary.OverallStatus = "critical"
	case summary.UnhealthyChecks > 0:
		summary.OverallStatus = "degraded"
	}
	return summary
}

func buildHealthRecommendations(keys []healthKeySnapshot, checks []healthCheckResult, probeSelection *healthProbeSelection) []string {
	recommendations := make([]string, 0, 6)
	activeKeys := 0
	slowChecks := make([]string, 0)
	for _, key := range keys {
		if key.Status == APIKeyStatusActive {
			activeKeys++
		}
	}
	if activeKeys == 0 {
		recommendations = append(recommendations, "当前没有 Active 状态的 NVIDIA key，请先录入并启用至少一个上游 key。")
	}
	if probeSelection == nil {
		recommendations = append(recommendations, "当前还没有执行实时健康检查，因此页面只展示缓存结果。需要刷新探测数据时，请手动点击“立即检查”。")
	} else if !probeSelection.Dedicated {
		recommendations = append(recommendations, "当前健康检查仍在复用业务上游 key。建议新增一个健康检查专用 key，并在 Key 管理页开启“健康专用”，把探测流量和正常用户流量隔离开。")
	}
	for _, check := range checks {
		if check.Success && check.DurationMs >= 15000 {
			slowChecks = append(slowChecks, check.Title)
		}
		if check.Success {
			continue
		}
		switch check.ID {
		case "nvidia_models":
			recommendations = append(recommendations, "官方 /models 探测失败，优先检查提供的 nvapi key 是否有效，以及出口网络是否能访问 integrate.api.nvidia.com。")
		case "nvidia_chat":
			recommendations = append(recommendations, "官方 /chat/completions 探测失败，建议检查默认聊天模型映射是否命中可用模型，并观察是否长期卡在首包阶段。")
		case "nvidia_embeddings":
			recommendations = append(recommendations, "官方 /embeddings 探测失败，建议确认是否映射到可用 embedding 模型（如 nvidia/nv-embed-v1）。")
		case "probe_key":
			recommendations = append(recommendations, check.Detail)
		}
	}
	if len(slowChecks) > 0 {
		recommendations = append(recommendations, fmt.Sprintf("这些基线接口延迟偏高：%s。若不是模型本身慢，优先排查出口网络、代理/VPN、TLS 握手或跨区链路；同时可在系统设置里缩短健康探测超时，避免手动检查长时间占用。", strings.Join(slowChecks, "、")))
	}
	if len(recommendations) == 0 {
		recommendations = append(recommendations, "当前健康检查全部通过，可以继续在调试页验证 Responses / Gemini / Claude 的业务链路。")
	}
	return recommendations
}

func truncateForHealth(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len([]rune(value)) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit]) + "…"
}

func (s *healthReportStore) put(report *healthReport) {
	if s == nil || report == nil {
		return
	}
	s.mu.Lock()
	if report.ActiveRun != nil {
		s.activeRun = cloneHealthModelRun(report.ActiveRun)
	}
	if report.FullSweep != nil {
		s.fullSweep = cloneHealthModelRun(report.FullSweep)
	}
	if len(report.ModelCatalog) > 0 {
		s.modelCatalog = cloneHealthModelCatalog(report.ModelCatalog)
	}
	s.latest = cloneHealthReport(report)
	s.history = append(s.history, healthHistoryPoint{
		GeneratedAt:     report.GeneratedAt,
		Label:           report.GeneratedAt.Format("15:04:05"),
		AvgLatencyMs:    report.Summary.AvgLatencyMs,
		HealthyChecks:   report.Summary.HealthyChecks,
		UnhealthyChecks: report.Summary.UnhealthyChecks,
		OverallStatus:   report.Summary.OverallStatus,
	})
	if len(s.history) > 20 {
		s.history = append([]healthHistoryPoint(nil), s.history[len(s.history)-20:]...)
	}
	s.latest.History = append([]healthHistoryPoint(nil), s.history...)
	s.latest.ModelCatalog = cloneHealthModelCatalog(s.modelCatalog)
	s.latest.ActiveRun = cloneHealthModelRun(s.activeRun)
	s.latest.FullSweep = cloneHealthModelRun(s.fullSweep)
	state := s.snapshotLocked()
	s.mu.Unlock()
	persistHealthState(state)
}

func (s *healthReportStore) getLatest() *healthReport {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.latest == nil {
		return nil
	}
	copy := cloneHealthReport(s.latest)
	copy.History = append([]healthHistoryPoint(nil), s.history...)
	copy.ModelCatalog = cloneHealthModelCatalog(s.modelCatalog)
	copy.ActiveRun = cloneHealthModelRun(s.activeRun)
	copy.FullSweep = cloneHealthModelRun(s.fullSweep)
	return copy
}

func (s *healthReportStore) historySnapshot() []healthHistoryPoint {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]healthHistoryPoint(nil), s.history...)
}

func (s *healthReportStore) activeRunSnapshot() *healthModelRunResult {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneHealthModelRun(s.activeRun)
}

func (s *healthReportStore) fullSweepSnapshot() *healthModelRunResult {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneHealthModelRun(s.fullSweep)
}

func (s *healthReportStore) modelCatalogSnapshot() []healthModelCatalogItem {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneHealthModelCatalog(s.modelCatalog)
}

func (s *healthReportStore) setModelCatalog(catalog []healthModelCatalogItem) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.modelCatalog = cloneHealthModelCatalog(catalog)
	if s.latest != nil {
		s.latest.ModelCatalog = cloneHealthModelCatalog(catalog)
	}
	state := s.snapshotLocked()
	s.mu.Unlock()
	persistHealthState(state)
}

func (s *healthReportStore) ensureHydrated() {
	if s == nil {
		return
	}
	state, err := loadPersistedHealthState()
	if err != nil || state == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.latest == nil {
		s.latest = cloneHealthReport(state.Latest)
	}
	if len(s.history) == 0 {
		s.history = append([]healthHistoryPoint(nil), state.History...)
	}
	if s.activeRun == nil {
		s.activeRun = cloneHealthModelRun(state.ActiveRun)
	}
	if s.fullSweep == nil {
		s.fullSweep = cloneHealthModelRun(state.FullSweep)
	}
	if len(s.modelCatalog) == 0 {
		s.modelCatalog = cloneHealthModelCatalog(state.ModelCatalog)
	}
}

func (s *healthReportStore) snapshotLocked() *persistedHealthState {
	if s == nil {
		return nil
	}
	return &persistedHealthState{
		Latest:       cloneHealthReport(s.latest),
		History:      append([]healthHistoryPoint(nil), s.history...),
		ActiveRun:    cloneHealthModelRun(s.activeRun),
		FullSweep:    cloneHealthModelRun(s.fullSweep),
		ModelCatalog: cloneHealthModelCatalog(s.modelCatalog),
	}
}

func loadPersistedHealthState() (*persistedHealthState, error) {
	store, err := db.ReadStore()
	if err != nil {
		return nil, err
	}
	if len(store.HealthState) == 0 {
		return nil, nil
	}
	var state persistedHealthState
	if err := json.Unmarshal(store.HealthState, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func persistHealthState(state *persistedHealthState) {
	if state == nil {
		return
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return
	}
	_ = db.UpdateStore(func(store *db.Store) error {
		store.HealthState = encoded
		return nil
	})
}

func cloneHealthReport(report *healthReport) *healthReport {
	if report == nil {
		return nil
	}
	copy := *report
	copy.Keys = append([]healthKeySnapshot(nil), report.Keys...)
	copy.Checks = append([]healthCheckResult(nil), report.Checks...)
	copy.History = append([]healthHistoryPoint(nil), report.History...)
	copy.Recommendations = append([]string(nil), report.Recommendations...)
	copy.ModelCatalog = cloneHealthModelCatalog(report.ModelCatalog)
	copy.ActiveRun = cloneHealthModelRun(report.ActiveRun)
	copy.FullSweep = cloneHealthModelRun(report.FullSweep)
	return &copy
}

func cloneHealthModelRun(run *healthModelRunResult) *healthModelRunResult {
	if run == nil {
		return nil
	}
	copy := *run
	copy.Checks = append([]healthModelRunCheck(nil), run.Checks...)
	copy.LatencyChart = append([]healthLatencyChartPoint(nil), run.LatencyChart...)
	return &copy
}

func cloneHealthModelCatalog(catalog []healthModelCatalogItem) []healthModelCatalogItem {
	return append([]healthModelCatalogItem(nil), catalog...)
}
