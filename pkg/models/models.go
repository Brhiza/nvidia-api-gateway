package models

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	DefaultUpstreamBaseURL       = "https://integrate.api.nvidia.com/v1"
	DefaultSchedulerStrategy     = "weighted_round_robin"
	DefaultMaxRetries            = 5
	DefaultMaxConcurrency        = 3
	DefaultRequestTimeoutSecond  = 600
	DefaultFirstByteTimeoutMs    = 90000
	DefaultHealthProbeTimeoutSec = 45
	// DefaultStreamIdleTimeoutSec 控制流式响应"两个 chunk 之间最大允许的静默时间"。
	// Claude Code 跑长任务时，上游真实推理 + 工具调用思考很容易超过 90 秒，
	// 默认调大到 600 秒，避免被网关当成"僵尸流"提前发 message_stop 让客户端误判完成。
	DefaultStreamIdleTimeoutSec = 600
	// DefaultStreamKeepAliveSec 控制空闲期 SSE 心跳注释帧的发送间隔；
	// 防止中间代理 / CDN / Cloudflare 因为长时间没有响应字节而 RST 连接。
	DefaultStreamKeepAliveSec        = 15
	DefaultTransportRetryCount       = 2
	DefaultTransportRetryBackoffMs   = 300
	DefaultGoProxyPublicAPIURL       = "https://proxy.amux.ai/api/proxies"
	DefaultGoProxyPublicProxyURL     = "http://proxy.amux.ai:7777"
	DefaultProxyImportMode           = "all"
	DefaultProxyImportGroup          = "自动抓取"
	DefaultProxyImportLimit          = 800
	DefaultProxyImportConcurrency    = 96
	DefaultProxyImportTimeoutSeconds = 4
	DefaultProxyImportRetryCount     = 0
	DefaultProxyImportCleanupLatency = 3000
	ProxyStatusEnabled               = "Enabled"
	ProxyStatusDisabled              = "Disabled"
	ProxySourceManual                = "manual"
	ProxySourceAuto                  = "auto"
)

type APIKey struct {
	ID        uint      `json:"id"`
	Key       string    `json:"key"`
	Name      string    `json:"name"`
	Weight    float64   `json:"weight"`
	Status    string    `json:"status"`
	ProbeOnly bool      `json:"probe_only,omitempty"`
	ProxyID   uint      `json:"proxy_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ProxyTestRecord struct {
	Success      bool      `json:"success"`
	StatusCode   int       `json:"status_code,omitempty"`
	ResponseTime int64     `json:"response_time,omitempty"`
	Message      string    `json:"message,omitempty"`
	Target       string    `json:"target,omitempty"`
	TestedAt     time.Time `json:"tested_at"`
}

type UpstreamProxy struct {
	ID           uint              `json:"id"`
	Name         string            `json:"name"`
	Group        string            `json:"group,omitempty"`
	Country      string            `json:"country,omitempty"`
	Source       string            `json:"source,omitempty"`
	ManagedBy    string            `json:"managed_by,omitempty"`
	ManagedRefID uint              `json:"managed_ref_id,omitempty"`
	Type         string            `json:"type"`
	Status       string            `json:"status"`
	Host         string            `json:"host"`
	Port         int               `json:"port"`
	Username     string            `json:"username,omitempty"`
	Password     string            `json:"password,omitempty"`
	LastTest     *ProxyTestRecord  `json:"last_test,omitempty"`
	TestHistory  []ProxyTestRecord `json:"test_history,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

type ExternalProxySources struct {
	HTTPTXT    []string `json:"httpTxt,omitempty"`
	HTTPJSON   []string `json:"httpJSON,omitempty"`
	HTTPHTML   []string `json:"httpHTML,omitempty"`
	SOCKS5TXT  []string `json:"socks5Txt,omitempty"`
	SOCKS5JSON []string `json:"socks5JSON,omitempty"`
	SOCKS5HTML []string `json:"socks5HTML,omitempty"`
}

type ProxyImportSchedule struct {
	Enabled                        bool      `json:"enabled"`
	Times                          []string  `json:"times,omitempty"`
	Mode                           string    `json:"mode,omitempty"`
	Group                          string    `json:"group,omitempty"`
	Limit                          int       `json:"limit,omitempty"`
	Concurrency                    int       `json:"concurrency,omitempty"`
	TimeoutSeconds                 int       `json:"timeoutSeconds,omitempty"`
	RetryCount                     int       `json:"retryCount,omitempty"`
	CleanupEnabled                 bool      `json:"cleanupEnabled,omitempty"`
	CleanupMaxLatencyMs            int       `json:"cleanupMaxLatencyMs,omitempty"`
	CleanupDeleteFailedAutoProxies bool      `json:"cleanupDeleteFailedAutoProxies,omitempty"`
	LastRunAt                      time.Time `json:"lastRunAt,omitempty"`
	UpdatedAt                      time.Time `json:"updatedAt,omitempty"`
}

type ProxyImportExecutionLog struct {
	TaskID                         string    `json:"taskId"`
	Trigger                        string    `json:"trigger"`
	Status                         string    `json:"status"`
	Message                        string    `json:"message,omitempty"`
	Mode                           string    `json:"mode,omitempty"`
	Group                          string    `json:"group,omitempty"`
	Limit                          int       `json:"limit,omitempty"`
	Concurrency                    int       `json:"concurrency,omitempty"`
	TimeoutSeconds                 int       `json:"timeoutSeconds,omitempty"`
	RetryCount                     int       `json:"retryCount,omitempty"`
	CleanupEnabled                 bool      `json:"cleanupEnabled,omitempty"`
	CleanupMaxLatencyMs            int       `json:"cleanupMaxLatencyMs,omitempty"`
	CleanupDeleteFailedAutoProxies bool      `json:"cleanupDeleteFailedAutoProxies,omitempty"`
	StartedAt                      time.Time `json:"startedAt,omitempty"`
	FinishedAt                     time.Time `json:"finishedAt,omitempty"`
	CandidateCount                 int       `json:"candidateCount,omitempty"`
	TestedCount                    int       `json:"testedCount,omitempty"`
	AvailableCount                 int       `json:"availableCount,omitempty"`
	FailedCount                    int       `json:"failedCount,omitempty"`
	ImportedCount                  int       `json:"importedCount,omitempty"`
	UpdatedCount                   int       `json:"updatedCount,omitempty"`
	MatchedManualCount             int       `json:"matchedManualCount,omitempty"`
	SourceErrorCount               int       `json:"sourceErrorCount,omitempty"`
	CleanedSlowCount               int       `json:"cleanedSlowCount,omitempty"`
	CleanedFailedCount             int       `json:"cleanedFailedCount,omitempty"`
	CleanupDeletedCount            int       `json:"cleanupDeletedCount,omitempty"`
	UnboundKeyCount                int       `json:"unboundKeyCount,omitempty"`
}

type MasterKey struct {
	ID        uint      `json:"id"`
	Key       string    `json:"key"`
	Name      string    `json:"name"`
	RPM       int       `json:"rpm"`
	TPM       int       `json:"tpm"`
	Quota     int64     `json:"quota"`
	UsedQuota int64     `json:"used_quota"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type SystemConfig struct {
	UpstreamBaseURL       string `json:"upstream_base_url"`
	SchedulerStrategy     string `json:"scheduler_strategy"`
	MaxRetries            int    `json:"max_retries"`
	MaxConcurrency        int    `json:"max_concurrency"`
	RequestTimeoutSecond  int    `json:"request_timeout_second"`
	UpstreamProxyURL      string `json:"upstream_proxy_url"`
	UpstreamProxyID       uint   `json:"upstream_proxy_id,omitempty"`
	EnableOpenAI          bool   `json:"enable_openai"`
	EnableClaude          bool   `json:"enable_claude"`
	EnableGemini          bool   `json:"enable_gemini"`
	AnonymousAccess       bool   `json:"anonymous_access"`
	FirstByteTimeoutMs    int    `json:"first_byte_timeout_ms"`
	HealthProbeTimeoutSec int    `json:"health_probe_timeout_second"`
	// StreamIdleTimeoutSec 控制流式响应中"两个 chunk 之间最大允许的静默时间"，
	// 触发后网关会主动发送正常的 SSE 结束帧（message_stop / [DONE]），并停止重试。
	// 设置为 0 或负数时使用 DefaultStreamIdleTimeoutSec。
	StreamIdleTimeoutSec int `json:"stream_idle_timeout_second,omitempty"`
	// StreamKeepAliveSec 控制流式空闲期注入 SSE 心跳注释帧（": keep-alive\n\n"）的间隔，
	// 防止中间代理 / CDN 因为长时间没有响应字节而 RST。0 表示禁用心跳。
	StreamKeepAliveSec      int `json:"stream_keep_alive_second,omitempty"`
	TransportRetryCount     int `json:"transport_retry_count,omitempty"`
	TransportRetryBackoffMs int `json:"transport_retry_backoff_ms,omitempty"`
	// SilentFallbackOnExhaustion 控制"所有上游 Key 重试耗尽"时的行为：
	//   true（默认）：返回 200 + 一个空 content 的合法响应，调用方不感知失败（保留旧版 UX）。
	//   false：返回 502 + 真实失败原因，便于监控告警拿到准确信号。
	// 无论开关如何，响应头里始终带上 X-Gateway-Upstream-Status / X-Gateway-Upstream-Final-Error，
	// 这样即使开启了静默回退，日志/网关入口侧仍然能观测到。
	SilentFallbackOnExhaustion bool `json:"silent_fallback_on_exhaustion"`
}

func DefaultSystemConfig() SystemConfig {
	return SystemConfig{
		UpstreamBaseURL:         DefaultUpstreamBaseURL,
		SchedulerStrategy:       DefaultSchedulerStrategy,
		MaxRetries:              DefaultMaxRetries,
		MaxConcurrency:          DefaultMaxConcurrency,
		RequestTimeoutSecond:    DefaultRequestTimeoutSecond,
		UpstreamProxyURL:        DefaultGoProxyPublicProxyURL,
		UpstreamProxyID:         0,
		EnableOpenAI:            true,
		EnableClaude:            true,
		EnableGemini:            true,
		AnonymousAccess:         false,
		FirstByteTimeoutMs:      DefaultFirstByteTimeoutMs,
		HealthProbeTimeoutSec:   DefaultHealthProbeTimeoutSec,
		StreamIdleTimeoutSec:    DefaultStreamIdleTimeoutSec,
		StreamKeepAliveSec:      DefaultStreamKeepAliveSec,
		TransportRetryCount:     DefaultTransportRetryCount,
		TransportRetryBackoffMs: DefaultTransportRetryBackoffMs,
		// 默认开启静默回退，保留旧版 UX；运维想要观测可在 admin UI 里关闭。
		SilentFallbackOnExhaustion: true,
	}
}

func DefaultProxyImportSchedule() ProxyImportSchedule {
	return ProxyImportSchedule{
		Enabled:                        false,
		Times:                          make([]string, 0),
		Mode:                           DefaultProxyImportMode,
		Group:                          DefaultProxyImportGroup,
		Limit:                          DefaultProxyImportLimit,
		Concurrency:                    DefaultProxyImportConcurrency,
		TimeoutSeconds:                 DefaultProxyImportTimeoutSeconds,
		RetryCount:                     DefaultProxyImportRetryCount,
		CleanupEnabled:                 false,
		CleanupMaxLatencyMs:            DefaultProxyImportCleanupLatency,
		CleanupDeleteFailedAutoProxies: false,
	}
}

func DefaultExternalProxySources() ExternalProxySources {
	return ExternalProxySources{
		HTTPTXT:    make([]string, 0),
		HTTPJSON:   make([]string, 0),
		HTTPHTML:   make([]string, 0),
		SOCKS5TXT:  make([]string, 0),
		SOCKS5JSON: make([]string, 0),
		SOCKS5HTML: make([]string, 0),
	}
}

func normalizeProxySourceList(items []string) []string {
	if len(items) == 0 {
		return make([]string, 0)
	}
	seen := make(map[string]struct{}, len(items))
	cleaned := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		cleaned = append(cleaned, trimmed)
	}
	sort.Strings(cleaned)
	return cleaned
}

func NormalizeExternalProxySources(cfg ExternalProxySources) ExternalProxySources {
	defaults := DefaultExternalProxySources()
	cfg.HTTPTXT = normalizeProxySourceList(cfg.HTTPTXT)
	cfg.HTTPJSON = normalizeProxySourceList(cfg.HTTPJSON)
	cfg.HTTPHTML = normalizeProxySourceList(cfg.HTTPHTML)
	cfg.SOCKS5TXT = normalizeProxySourceList(cfg.SOCKS5TXT)
	cfg.SOCKS5JSON = normalizeProxySourceList(cfg.SOCKS5JSON)
	cfg.SOCKS5HTML = normalizeProxySourceList(cfg.SOCKS5HTML)
	if cfg.HTTPTXT == nil {
		cfg.HTTPTXT = defaults.HTTPTXT
	}
	if cfg.HTTPJSON == nil {
		cfg.HTTPJSON = defaults.HTTPJSON
	}
	if cfg.HTTPHTML == nil {
		cfg.HTTPHTML = defaults.HTTPHTML
	}
	if cfg.SOCKS5TXT == nil {
		cfg.SOCKS5TXT = defaults.SOCKS5TXT
	}
	if cfg.SOCKS5JSON == nil {
		cfg.SOCKS5JSON = defaults.SOCKS5JSON
	}
	if cfg.SOCKS5HTML == nil {
		cfg.SOCKS5HTML = defaults.SOCKS5HTML
	}
	return cfg
}

func NormalizeSystemConfig(cfg SystemConfig) SystemConfig {
	defaults := DefaultSystemConfig()

	cfg.UpstreamBaseURL = strings.TrimSpace(cfg.UpstreamBaseURL)
	if cfg.UpstreamBaseURL == "" {
		cfg.UpstreamBaseURL = defaults.UpstreamBaseURL
	}
	cfg.SchedulerStrategy = strings.TrimSpace(cfg.SchedulerStrategy)
	if cfg.SchedulerStrategy == "" {
		cfg.SchedulerStrategy = defaults.SchedulerStrategy
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = defaults.MaxRetries
	}
	if cfg.MaxConcurrency <= 0 {
		cfg.MaxConcurrency = defaults.MaxConcurrency
	}
	if cfg.RequestTimeoutSecond <= 0 {
		cfg.RequestTimeoutSecond = defaults.RequestTimeoutSecond
	}
	cfg.UpstreamProxyURL = strings.TrimSpace(cfg.UpstreamProxyURL)
	if cfg.FirstByteTimeoutMs <= 0 {
		cfg.FirstByteTimeoutMs = defaults.FirstByteTimeoutMs
	}
	if cfg.HealthProbeTimeoutSec <= 0 {
		cfg.HealthProbeTimeoutSec = defaults.HealthProbeTimeoutSec
	}
	if cfg.StreamIdleTimeoutSec <= 0 {
		cfg.StreamIdleTimeoutSec = defaults.StreamIdleTimeoutSec
	}
	if cfg.StreamKeepAliveSec == 0 {
		cfg.StreamKeepAliveSec = defaults.StreamKeepAliveSec
	} else if cfg.StreamKeepAliveSec < 0 {
		// 允许显式传 -1 关闭心跳
		cfg.StreamKeepAliveSec = 0
	}
	if cfg.TransportRetryCount <= 0 {
		cfg.TransportRetryCount = defaults.TransportRetryCount
	}
	if cfg.TransportRetryBackoffMs <= 0 {
		cfg.TransportRetryBackoffMs = defaults.TransportRetryBackoffMs
	}

	if !cfg.EnableOpenAI && !cfg.EnableClaude && !cfg.EnableGemini {
		cfg.EnableOpenAI = defaults.EnableOpenAI
		cfg.EnableClaude = defaults.EnableClaude
		cfg.EnableGemini = defaults.EnableGemini
	}

	return cfg
}

func NormalizeProxyImportSchedule(cfg ProxyImportSchedule) ProxyImportSchedule {
	defaults := DefaultProxyImportSchedule()

	cfg.Mode = strings.ToLower(strings.TrimSpace(cfg.Mode))
	if cfg.Mode == "" {
		cfg.Mode = defaults.Mode
	}
	cfg.Group = strings.TrimSpace(cfg.Group)
	if cfg.Group == "" {
		cfg.Group = defaults.Group
	}
	if cfg.Limit <= 0 {
		cfg.Limit = defaults.Limit
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = defaults.Concurrency
	}
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = defaults.TimeoutSeconds
	}
	if cfg.RetryCount < 0 {
		cfg.RetryCount = defaults.RetryCount
	}
	if cfg.CleanupMaxLatencyMs <= 0 {
		cfg.CleanupMaxLatencyMs = defaults.CleanupMaxLatencyMs
	}
	cfg.Times = normalizeScheduleTimes(cfg.Times)
	return cfg
}

func NormalizeUpstreamProxy(p UpstreamProxy) UpstreamProxy {
	p.Name = strings.TrimSpace(p.Name)
	p.Type = strings.ToLower(strings.TrimSpace(p.Type))
	p.Status = strings.TrimSpace(p.Status)
	if p.Status == "" {
		p.Status = ProxyStatusEnabled
	}
	p.Group = strings.TrimSpace(p.Group)
	p.Country = normalizeCountryLabel(p.Country)
	p.Source = strings.ToLower(strings.TrimSpace(p.Source))
	if p.Source == "" {
		p.Source = ProxySourceManual
	}
	p.Host = strings.TrimSpace(p.Host)
	p.Username = strings.TrimSpace(p.Username)
	p.Password = strings.TrimSpace(p.Password)
	if p.Name == "" && p.Host != "" && p.Port > 0 {
		p.Name = fmt.Sprintf("%s://%s:%d", p.Type, p.Host, p.Port)
	}
	return p
}

func normalizeCountryLabel(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) <= 3 {
		return strings.ToUpper(trimmed)
	}
	return trimmed
}

func normalizeScheduleTimes(values []string) []string {
	if len(values) == 0 {
		return make([]string, 0)
	}
	seen := make(map[string]struct{}, len(values))
	items := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || r == ';' || r == '\n' || r == '\r'
		}) {
			trimmed := strings.TrimSpace(part)
			if trimmed == "" {
				continue
			}
			if _, ok := seen[trimmed]; ok {
				continue
			}
			seen[trimmed] = struct{}{}
			items = append(items, trimmed)
		}
	}
	sort.Strings(items)
	return items
}
