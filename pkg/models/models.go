package models

import (
	"fmt"
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
	ProxyStatusEnabled           = "Enabled"
	ProxyStatusDisabled          = "Disabled"
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
	ID          uint              `json:"id"`
	Name        string            `json:"name"`
	Group       string            `json:"group,omitempty"`
	Type        string            `json:"type"`
	Status      string            `json:"status"`
	Host        string            `json:"host"`
	Port        int               `json:"port"`
	Username    string            `json:"username,omitempty"`
	Password    string            `json:"password,omitempty"`
	LastTest    *ProxyTestRecord  `json:"last_test,omitempty"`
	TestHistory []ProxyTestRecord `json:"test_history,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
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
	EnableOpenAI          bool   `json:"enable_openai"`
	EnableClaude          bool   `json:"enable_claude"`
	EnableGemini          bool   `json:"enable_gemini"`
	AnonymousAccess       bool   `json:"anonymous_access"`
	FirstByteTimeoutMs    int    `json:"first_byte_timeout_ms"`
	HealthProbeTimeoutSec int    `json:"health_probe_timeout_second"`
}

func DefaultSystemConfig() SystemConfig {
	return SystemConfig{
		UpstreamBaseURL:       DefaultUpstreamBaseURL,
		SchedulerStrategy:     DefaultSchedulerStrategy,
		MaxRetries:            DefaultMaxRetries,
		MaxConcurrency:        DefaultMaxConcurrency,
		RequestTimeoutSecond:  DefaultRequestTimeoutSecond,
		UpstreamProxyURL:      "",
		EnableOpenAI:          true,
		EnableClaude:          true,
		EnableGemini:          true,
		AnonymousAccess:       false,
		FirstByteTimeoutMs:    DefaultFirstByteTimeoutMs,
		HealthProbeTimeoutSec: DefaultHealthProbeTimeoutSec,
	}
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
	if cfg.MaxRetries <= 0 {
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

	if !cfg.EnableOpenAI && !cfg.EnableClaude && !cfg.EnableGemini {
		cfg.EnableOpenAI = defaults.EnableOpenAI
		cfg.EnableClaude = defaults.EnableClaude
		cfg.EnableGemini = defaults.EnableGemini
	}

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
	p.Host = strings.TrimSpace(p.Host)
	p.Username = strings.TrimSpace(p.Username)
	p.Password = strings.TrimSpace(p.Password)
	if p.Name == "" && p.Host != "" && p.Port > 0 {
		p.Name = fmt.Sprintf("%s://%s:%d", p.Type, p.Host, p.Port)
	}
	return p
}
