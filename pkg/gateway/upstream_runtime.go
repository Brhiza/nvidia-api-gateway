package gateway

import (
	"context"
	"strings"
	"sync"
	"time"

	"nvidia-api-gateway/pkg/db"
	"nvidia-api-gateway/pkg/scheduler"
	"nvidia-api-gateway/pkg/utils"

	"github.com/gofiber/fiber/v2"
)

type upstreamRuntimeSummary struct {
	TotalKeys      int              `json:"totalKeys"`
	ActiveKeys     int              `json:"activeKeys"`
	CoolingKeys    int              `json:"coolingKeys"`
	DeadKeys       int              `json:"deadKeys"`
	DisabledKeys   int              `json:"disabledKeys"`
	SchedulerStats *scheduler.Stats `json:"schedulerStats"`
}

type upstreamRuntimeEvent struct {
	At              time.Time `json:"at"`
	Operation       string    `json:"operation"`
	OperationLabel  string    `json:"operationLabel,omitempty"`
	Stage           string    `json:"stage"`
	StageLabel      string    `json:"stageLabel,omitempty"`
	UpstreamKeyName string    `json:"upstreamKeyName,omitempty"`
	Success         bool      `json:"success"`
	HTTPStatus      int       `json:"httpStatus,omitempty"`
	Detail          string    `json:"detail,omitempty"`
}

type upstreamRuntimeSnapshot struct {
	GeneratedAt  time.Time              `json:"generatedAt"`
	Summary      upstreamRuntimeSummary `json:"summary"`
	LastEvent    *upstreamRuntimeEvent  `json:"lastEvent,omitempty"`
	RecentEvents []upstreamRuntimeEvent `json:"recentEvents"`
}

type upstreamRuntimeStore struct {
	mu     sync.Mutex
	events []upstreamRuntimeEvent
}

var systemUpstreamRuntimeStore = &upstreamRuntimeStore{}

func GetUpstreamRuntime(sched *scheduler.Scheduler) fiber.Handler {
	return func(c *fiber.Ctx) error {
		return c.JSON(systemUpstreamRuntimeStore.snapshot(sched))
	}
}

func recordUpstreamRuntimeEvent(operation, stage, plaintextKey string, success bool, httpStatus int, detail string) {
	if strings.TrimSpace(operation) == "" {
		operation = "unknown"
	}
	event := upstreamRuntimeEvent{
		At:              time.Now(),
		Operation:       strings.TrimSpace(operation),
		OperationLabel:  upstreamOperationLabel(operation),
		Stage:           strings.TrimSpace(stage),
		StageLabel:      upstreamStageLabel(stage),
		UpstreamKeyName: lookupUpstreamKeyNameByPlaintext(plaintextKey),
		Success:         success,
		HTTPStatus:      httpStatus,
		Detail:          strings.TrimSpace(detail),
	}
	systemUpstreamRuntimeStore.record(event)
}

func upstreamOperationLabel(operation string) string {
	switch strings.TrimSpace(operation) {
	case "models.list":
		return "上游模型列表"
	case "chat.nonstream":
		return "OpenAI 对话（非流式）"
	case "chat.stream":
		return "OpenAI 对话（流式）"
	case "chat/completions":
		return "上游 Chat Completions"
	case "embeddings":
		return "上游 Embeddings"
	case "responses.stream":
		return "OpenAI Responses（流式）"
	case "claude.stream":
		return "Claude Messages（流式）"
	case "gemini.stream":
		return "Gemini Generate Content（流式）"
	case "unknown":
		return "未知操作"
	default:
		if strings.HasSuffix(operation, ".stream") {
			return operation + "（流式）"
		}
		if strings.HasSuffix(operation, ".nonstream") {
			return operation + "（非流式）"
		}
		return operation
	}
}

func upstreamStageLabel(stage string) string {
	switch strings.TrimSpace(stage) {
	case "scheduler_error":
		return "调度器出错"
	case "key_selected":
		return "已选中上游 Key"
	case "restore_attempt":
		return "尝试恢复上游 Key 状态"
	case "request_cancelled":
		return "请求已取消"
	case "queue_timeout":
		return "队列等待超时"
	case "first_byte_timeout":
		return "等待首包超时"
	case "first_chunk_timeout":
		return "等待首个 chunk 超时"
	case "upstream_error":
		return "上游请求出错"
	case "rate_limited":
		return "上游限流"
	case "auth_rejected":
		return "上游鉴权被拒"
	case "upstream_failed":
		return "上游响应失败"
	case "upstream_ok":
		return "上游响应成功"
	default:
		if strings.TrimSpace(stage) == "" {
			return ""
		}
		return stage
	}
}

func lookupUpstreamKeyNameByPlaintext(plaintext string) string {
	plaintext = strings.TrimSpace(plaintext)
	if plaintext == "" {
		return ""
	}
	secret := strings.TrimSpace(utils.GetEncryptionKey())
	if secret == "" {
		return ""
	}
	store, err := db.ReadStore()
	if err != nil {
		return ""
	}
	for _, key := range store.APIKeys {
		decrypted, decErr := utils.Decrypt(key.Key, secret)
		if decErr != nil {
			continue
		}
		if decrypted == plaintext {
			return key.Name
		}
	}
	return ""
}

func (s *upstreamRuntimeStore) record(event upstreamRuntimeEvent) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	if len(s.events) > 20 {
		s.events = append([]upstreamRuntimeEvent(nil), s.events[len(s.events)-20:]...)
	}
}

func (s *upstreamRuntimeStore) snapshot(sched *scheduler.Scheduler) upstreamRuntimeSnapshot {
	summary := buildUpstreamRuntimeSummary(context.Background(), sched)
	snapshot := upstreamRuntimeSnapshot{
		GeneratedAt:  time.Now(),
		Summary:      summary,
		RecentEvents: make([]upstreamRuntimeEvent, 0),
	}
	if s == nil {
		return snapshot
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.events) > 0 {
		last := s.events[len(s.events)-1]
		snapshot.LastEvent = &last
		snapshot.RecentEvents = append([]upstreamRuntimeEvent(nil), s.events...)
	}
	return snapshot
}

func buildUpstreamRuntimeSummary(ctx context.Context, sched *scheduler.Scheduler) upstreamRuntimeSummary {
	summary := upstreamRuntimeSummary{SchedulerStats: &scheduler.Stats{}}
	store, err := db.ReadStore()
	if err == nil {
		for _, key := range store.APIKeys {
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
	}
	if sched != nil {
		if stats, statsErr := sched.Stats(ctx); statsErr == nil && stats != nil {
			summary.SchedulerStats = stats
		}
	}
	return summary
}
