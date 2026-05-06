package gateway

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"nvidia-api-gateway/pkg/db"
	"nvidia-api-gateway/pkg/models"
	"nvidia-api-gateway/pkg/scheduler"
	"nvidia-api-gateway/pkg/utils"

	"github.com/gofiber/fiber/v2"
)

type apiKeyResponse struct {
	ID         uint      `json:"id"`
	Name       string    `json:"name"`
	Weight     float64   `json:"weight"`
	Status     string    `json:"status"`
	ProbeOnly  bool      `json:"probeOnly"`
	ProxyID    uint      `json:"proxyId,omitempty"`
	ProxyName  string    `json:"proxyName,omitempty"`
	ProxyGroup string    `json:"proxyGroup,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type apiKeysResponse struct {
	Keys []apiKeyResponse `json:"keys"`
}

type createAPIKeyRequest struct {
	Key       string  `json:"key"`
	Name      string  `json:"name"`
	Weight    float64 `json:"weight"`
	ProbeOnly bool    `json:"probeOnly"`
	ProxyID   *uint   `json:"proxyId,omitempty"`
}

type updateAPIKeyRequest struct {
	Key       string   `json:"key"`
	Name      *string  `json:"name"`
	Weight    *float64 `json:"weight"`
	ProbeOnly *bool    `json:"probeOnly"`
	ProxyID   *uint    `json:"proxyId,omitempty"`
}

type updateAPIKeyStatusRequest struct {
	Status string `json:"status"`
}

type systemConfigResponse struct {
	UpstreamBaseURL       string `json:"upstreamBaseURL"`
	SchedulerStrategy     string `json:"schedulerStrategy"`
	MaxRetries            int    `json:"maxRetries"`
	MaxConcurrency        int    `json:"maxConcurrency"`
	RequestTimeoutSecond  int    `json:"requestTimeoutSecond"`
	UpstreamProxyURL      string `json:"upstreamProxyURL"`
	GatewayBaseURL        string `json:"gatewayBaseURL"`
	FirstByteTimeoutMs    int    `json:"firstByteTimeoutMs"`
	HealthProbeTimeoutSec int    `json:"healthProbeTimeoutSecond"`
	EnableOpenAI          bool   `json:"enableOpenAI"`
	EnableClaude          bool   `json:"enableClaude"`
	EnableGemini          bool   `json:"enableGemini"`
	AnonymousAccess       bool   `json:"anonymousAccess"`
}

type updateSystemConfigRequest struct {
	UpstreamBaseURL       *string `json:"upstreamBaseURL"`
	SchedulerStrategy     *string `json:"schedulerStrategy"`
	MaxRetries            *int    `json:"maxRetries"`
	MaxConcurrency        *int    `json:"maxConcurrency"`
	RequestTimeoutSecond  *int    `json:"requestTimeoutSecond"`
	UpstreamProxyURL      *string `json:"upstreamProxyURL"`
	FirstByteTimeoutMs    *int    `json:"firstByteTimeoutMs"`
	HealthProbeTimeoutSec *int    `json:"healthProbeTimeoutSecond"`
	EnableOpenAI          *bool   `json:"enableOpenAI"`
	EnableClaude          *bool   `json:"enableClaude"`
	EnableGemini          *bool   `json:"enableGemini"`
	AnonymousAccess       *bool   `json:"anonymousAccess"`
}

func AddAPIKey(sched *scheduler.Scheduler) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req createAPIKeyRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "\u8bf7\u6c42\u4f53\u683c\u5f0f\u65e0\u6548"})
		}

		rootKey := utils.GetEncryptionKey()
		if rootKey == "" || len(rootKey) != 32 {
			return c.Status(500).JSON(fiber.Map{"error": "\u670d\u52a1\u7aef\u7f3a\u5c11\u5408\u6cd5\u7684 32 \u4f4d ENCRYPTION_KEY"})
		}

		name, weight, err := validateAPIKeyCreate(req)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}

		encryptedKey, err := utils.Encrypt(strings.TrimSpace(req.Key), rootKey)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "\u52a0\u5bc6\u4e0a\u6e38 Key \u5931\u8d25"})
		}

		now := time.Now()
		apiKey := models.APIKey{
			Key:       encryptedKey,
			Name:      name,
			Weight:    weight,
			Status:    APIKeyStatusActive,
			ProbeOnly: req.ProbeOnly,
			CreatedAt: now,
			UpdatedAt: now,
		}

		if err := db.UpdateStore(func(store *db.Store) error {
			if req.ProxyID != nil {
				if err := validateAPIKeyProxyReference(store, *req.ProxyID); err != nil {
					return err
				}
				apiKey.ProxyID = *req.ProxyID
			}
			apiKey.ID = store.NextAPIID
			store.NextAPIID++
			store.APIKeys = append(store.APIKeys, apiKey)
			return nil
		}); err != nil {
			status := 500
			if err.Error() == "所选代理不存在" {
				status = 400
			}
			return c.Status(status).JSON(fiber.Map{"error": err.Error()})
		}

		if err := LoadActiveKeys(context.Background(), sched); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "\u4e0a\u6e38 Key \u5df2\u4fdd\u5b58\uff0c\u4f46\u91cd\u8f7d\u8c03\u5ea6\u5668\u5931\u8d25"})
		}

		return c.JSON(fiber.Map{
			"message": "\u4e0a\u6e38 Key \u6dfb\u52a0\u6210\u529f",
			"key":     newAPIKeyResponse(apiKey),
		})
	}
}

func GetAPIKeys(c *fiber.Ctx) error {
	store, err := db.ReadStore()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "\u8bfb\u53d6\u4e0a\u6e38 Key \u5931\u8d25"})
	}
	return c.JSON(apiKeysResponse{Keys: buildAPIKeyResponsesWithProxies(store.APIKeys, store.Proxies)})
}

func UpdateAPIKey(sched *scheduler.Scheduler) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id, err := parseAPIKeyID(c)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}

		var req updateAPIKeyRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "\u8bf7\u6c42\u4f53\u683c\u5f0f\u65e0\u6548"})
		}

		rootKey := utils.GetEncryptionKey()
		if rootKey == "" || len(rootKey) != 32 {
			return c.Status(500).JSON(fiber.Map{"error": "\u670d\u52a1\u7aef\u7f3a\u5c11\u5408\u6cd5\u7684 32 \u4f4d ENCRYPTION_KEY"})
		}

		updatedKey, err := mutateAPIKeyWithStore(id, func(store *db.Store, key *models.APIKey) error {
			if req.Name != nil {
				name := strings.TrimSpace(*req.Name)
				if name == "" {
					return errors.New("\u540d\u79f0\u4e0d\u80fd\u4e3a\u7a7a")
				}
				key.Name = name
			}
			if req.Weight != nil {
				if *req.Weight <= 0 {
					return errors.New("\u6743\u91cd\u5fc5\u987b\u5927\u4e8e 0")
				}
				key.Weight = *req.Weight
			}
			if strings.TrimSpace(req.Key) != "" {
				encryptedKey, encryptErr := utils.Encrypt(strings.TrimSpace(req.Key), rootKey)
				if encryptErr != nil {
					return errors.New("\u52a0\u5bc6\u4e0a\u6e38 Key \u5931\u8d25")
				}
				key.Key = encryptedKey
			}
			if req.ProbeOnly != nil {
				key.ProbeOnly = *req.ProbeOnly
			}
			if req.ProxyID != nil {
				if err := validateAPIKeyProxyReference(store, *req.ProxyID); err != nil {
					return err
				}
				key.ProxyID = *req.ProxyID
			}
			key.UpdatedAt = time.Now()
			return nil
		})
		if err != nil {
			status := 500
			if errors.Is(err, errAPIKeyNotFound) {
				status = 404
			} else if err.Error() == "\u540d\u79f0\u4e0d\u80fd\u4e3a\u7a7a" || err.Error() == "\u6743\u91cd\u5fc5\u987b\u5927\u4e8e 0" || err.Error() == "所选代理不存在" {
				status = 400
			}
			return c.Status(status).JSON(fiber.Map{"error": err.Error()})
		}

		if err := LoadActiveKeys(context.Background(), sched); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "\u4e0a\u6e38 Key \u5df2\u66f4\u65b0\uff0c\u4f46\u91cd\u8f7d\u8c03\u5ea6\u5668\u5931\u8d25"})
		}

		return c.JSON(fiber.Map{
			"message": "\u4e0a\u6e38 Key \u66f4\u65b0\u6210\u529f",
			"key":     newAPIKeyResponse(updatedKey),
		})
	}
}

func DeleteAPIKey(sched *scheduler.Scheduler) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id, err := parseAPIKeyID(c)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}

		if err := deleteAPIKey(id); err != nil {
			status := 500
			if errors.Is(err, errAPIKeyNotFound) {
				status = 404
			}
			return c.Status(status).JSON(fiber.Map{"error": err.Error()})
		}

		if err := LoadActiveKeys(context.Background(), sched); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "\u4e0a\u6e38 Key \u5df2\u5220\u9664\uff0c\u4f46\u91cd\u8f7d\u8c03\u5ea6\u5668\u5931\u8d25"})
		}

		return c.JSON(fiber.Map{"message": "\u4e0a\u6e38 Key \u5220\u9664\u6210\u529f"})
	}
}

func UpdateAPIKeyStatus(sched *scheduler.Scheduler) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id, err := parseAPIKeyID(c)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}

		var req updateAPIKeyStatusRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "\u8bf7\u6c42\u4f53\u683c\u5f0f\u65e0\u6548"})
		}

		status, err := normalizeManualStatus(req.Status)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}

		updatedKey, err := mutateAPIKey(id, func(key *models.APIKey) error {
			key.Status = status
			key.UpdatedAt = time.Now()
			return nil
		})
		if err != nil {
			code := 500
			if errors.Is(err, errAPIKeyNotFound) {
				code = 404
			}
			return c.Status(code).JSON(fiber.Map{"error": err.Error()})
		}

		if err := LoadActiveKeys(context.Background(), sched); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "\u72b6\u6001\u5df2\u66f4\u65b0\uff0c\u4f46\u91cd\u8f7d\u8c03\u5ea6\u5668\u5931\u8d25"})
		}

		return c.JSON(fiber.Map{
			"message": "\u4e0a\u6e38 Key \u72b6\u6001\u66f4\u65b0\u6210\u529f",
			"key":     newAPIKeyResponse(updatedKey),
		})
	}
}

func ProbeAPIKey(sched *scheduler.Scheduler) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id, err := parseAPIKeyID(c)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}

		rootKey := utils.GetEncryptionKey()
		if rootKey == "" || len(rootKey) != 32 {
			return c.Status(500).JSON(fiber.Map{"error": "\u670d\u52a1\u7aef\u7f3a\u5c11\u5408\u6cd5\u7684 32 \u4f4d ENCRYPTION_KEY"})
		}

		store, err := db.ReadStore()
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "\u8bfb\u53d6\u4e0a\u6e38 Key \u5931\u8d25"})
		}

		var current *models.APIKey
		for i := range store.APIKeys {
			if store.APIKeys[i].ID == id {
				copy := store.APIKeys[i]
				current = &copy
				break
			}
		}
		if current == nil {
			return c.Status(404).JSON(fiber.Map{"error": "\u4e0a\u6e38 Key \u4e0d\u5b58\u5728"})
		}

		plaintext, err := utils.Decrypt(current.Key, rootKey)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "\u89e3\u5bc6\u4e0a\u6e38 Key \u5931\u8d25"})
		}

		probe, ok := probeKeyStatus(context.Background(), plaintext)
		if !ok || probe == nil {
			return c.Status(502).JSON(fiber.Map{"error": "\u63a2\u6d4b\u4e0a\u6e38 Key \u72b6\u6001\u5931\u8d25"})
		}

		updatedKey, err := mutateAPIKey(id, func(key *models.APIKey) error {
			key.Status = probe.Status
			key.UpdatedAt = time.Now()
			return nil
		})
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}

		if err := LoadActiveKeys(context.Background(), sched); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "\u4e0a\u6e38 Key \u63a2\u6d4b\u6210\u529f\uff0c\u4f46\u91cd\u8f7d\u8c03\u5ea6\u5668\u5931\u8d25"})
		}

		return c.JSON(fiber.Map{
			"message": "\u4e0a\u6e38 Key \u63a2\u6d4b\u5b8c\u6210",
			"key":     newAPIKeyResponse(updatedKey),
			"probe": fiber.Map{
				"endpoint":   probe.Endpoint,
				"method":     probe.Method,
				"httpStatus": probe.HTTPStatus,
				"durationMs": probe.DurationMs,
				"detail":     probe.Detail,
			},
		})
	}
}

func ReloadSystem(sched *scheduler.Scheduler) fiber.Handler {
	return func(c *fiber.Ctx) error {
		ctx := context.Background()
		if err := LoadActiveKeys(ctx, sched); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}

		stats, err := sched.Stats(ctx)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}

		return c.JSON(fiber.Map{
			"message": "\u8c03\u5ea6\u5668\u70ed\u91cd\u8f7d\u5b8c\u6210",
			"stats":   stats,
		})
	}
}

func SchedulerStats(sched *scheduler.Scheduler) fiber.Handler {
	return func(c *fiber.Ctx) error {
		store, err := db.ReadStore()
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "\u8bfb\u53d6\u8c03\u5ea6\u7edf\u8ba1\u5931\u8d25"})
		}

		stats, err := sched.Stats(context.Background())
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		if stats.Active == 0 {
			for _, key := range store.APIKeys {
				switch key.Status {
				case APIKeyStatusActive:
					stats.Active++
				case APIKeyStatusCooling:
					stats.Cooling++
				case APIKeyStatusDead, APIKeyStatusDisabled:
					stats.Dead++
				}
			}
		}
		return c.JSON(stats)
	}
}

func GetSystemConfig(c *fiber.Ctx) error {
	store, err := db.ReadStore()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "\u8bfb\u53d6\u7cfb\u7edf\u914d\u7f6e\u5931\u8d25"})
	}
	return c.JSON(newSystemConfigResponse(store.SystemConfig))
}

func UpdateSystemConfig(sched *scheduler.Scheduler) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req updateSystemConfigRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "\u8bf7\u6c42\u4f53\u683c\u5f0f\u65e0\u6548"})
		}

		var normalizedProxyURL *string
		if req.UpstreamProxyURL != nil {
			trimmed := strings.TrimSpace(*req.UpstreamProxyURL)
			if err := validateUpstreamProxySetting(trimmed); err != nil {
				return c.Status(400).JSON(fiber.Map{"error": "\u4e0a\u6e38\u4ee3\u7406\u914d\u7f6e\u65e0\u6548: " + err.Error()})
			}
			normalizedProxyURL = &trimmed
		}

		var updatedConfig models.SystemConfig
		err := db.UpdateStore(func(store *db.Store) error {
			cfg := store.SystemConfig
			if req.UpstreamBaseURL != nil {
				cfg.UpstreamBaseURL = strings.TrimSpace(*req.UpstreamBaseURL)
			}
			if req.SchedulerStrategy != nil {
				cfg.SchedulerStrategy = strings.TrimSpace(*req.SchedulerStrategy)
			}
			if req.MaxRetries != nil {
				cfg.MaxRetries = *req.MaxRetries
			}
			if req.MaxConcurrency != nil {
				cfg.MaxConcurrency = *req.MaxConcurrency
			}
			if req.RequestTimeoutSecond != nil {
				cfg.RequestTimeoutSecond = *req.RequestTimeoutSecond
			}
			if normalizedProxyURL != nil {
				cfg.UpstreamProxyURL = *normalizedProxyURL
			}
			if req.FirstByteTimeoutMs != nil {
				cfg.FirstByteTimeoutMs = *req.FirstByteTimeoutMs
			}
			if req.HealthProbeTimeoutSec != nil {
				cfg.HealthProbeTimeoutSec = *req.HealthProbeTimeoutSec
			}
			if req.EnableOpenAI != nil {
				cfg.EnableOpenAI = *req.EnableOpenAI
			}
			if req.EnableClaude != nil {
				cfg.EnableClaude = *req.EnableClaude
			}
			if req.EnableGemini != nil {
				cfg.EnableGemini = *req.EnableGemini
			}
			if req.AnonymousAccess != nil {
				cfg.AnonymousAccess = *req.AnonymousAccess
			}
			cfg = models.NormalizeSystemConfig(cfg)
			store.SystemConfig = cfg
			updatedConfig = cfg
			return nil
		})
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "\u66f4\u65b0\u7cfb\u7edf\u914d\u7f6e\u5931\u8d25"})
		}

		if err := LoadActiveKeys(context.Background(), sched); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "\u7cfb\u7edf\u914d\u7f6e\u5df2\u4fdd\u5b58\uff0c\u4f46\u91cd\u8f7d\u8c03\u5ea6\u5668\u5931\u8d25"})
		}

		return c.JSON(fiber.Map{
			"message": "\u7cfb\u7edf\u914d\u7f6e\u66f4\u65b0\u6210\u529f",
			"config":  newSystemConfigResponse(updatedConfig),
		})
	}
}

func newSystemConfigResponse(cfg models.SystemConfig) systemConfigResponse {
	cfg = models.NormalizeSystemConfig(cfg)
	return systemConfigResponse{
		UpstreamBaseURL:       cfg.UpstreamBaseURL,
		SchedulerStrategy:     cfg.SchedulerStrategy,
		MaxRetries:            cfg.MaxRetries,
		MaxConcurrency:        cfg.MaxConcurrency,
		RequestTimeoutSecond:  cfg.RequestTimeoutSecond,
		UpstreamProxyURL:      cfg.UpstreamProxyURL,
		GatewayBaseURL:        gatewayBaseURL(),
		FirstByteTimeoutMs:    cfg.FirstByteTimeoutMs,
		HealthProbeTimeoutSec: cfg.HealthProbeTimeoutSec,
		EnableOpenAI:          cfg.EnableOpenAI,
		EnableClaude:          cfg.EnableClaude,
		EnableGemini:          cfg.EnableGemini,
		AnonymousAccess:       cfg.AnonymousAccess,
	}
}

var errAPIKeyNotFound = errors.New("\u4e0a\u6e38 Key \u4e0d\u5b58\u5728")

func validateAPIKeyCreate(req createAPIKeyRequest) (string, float64, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return "", 0, errors.New("\u540d\u79f0\u4e0d\u80fd\u4e3a\u7a7a")
	}
	if strings.TrimSpace(req.Key) == "" {
		return "", 0, errors.New("Key \u4e0d\u80fd\u4e3a\u7a7a")
	}
	if req.Weight <= 0 {
		return "", 0, errors.New("\u6743\u91cd\u5fc5\u987b\u5927\u4e8e 0")
	}
	return name, req.Weight, nil
}

func normalizeManualStatus(status string) (string, error) {
	switch strings.TrimSpace(status) {
	case APIKeyStatusActive:
		return APIKeyStatusActive, nil
	case APIKeyStatusDisabled:
		return APIKeyStatusDisabled, nil
	default:
		return "", errors.New("\u72b6\u6001\u53ea\u80fd\u662f Active \u6216 Disabled")
	}
}

func parseAPIKeyID(c *fiber.Ctx) (uint, error) {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil || id == 0 {
		return 0, errors.New("\u65e0\u6548\u7684\u4e0a\u6e38 Key ID")
	}
	return uint(id), nil
}

func mutateAPIKey(id uint, mutator func(*models.APIKey) error) (models.APIKey, error) {
	return mutateAPIKeyWithStore(id, func(_ *db.Store, key *models.APIKey) error {
		return mutator(key)
	})
}

func mutateAPIKeyWithStore(id uint, mutator func(*db.Store, *models.APIKey) error) (models.APIKey, error) {
	var updated models.APIKey
	err := db.UpdateStore(func(store *db.Store) error {
		for i := range store.APIKeys {
			if store.APIKeys[i].ID != id {
				continue
			}
			if err := mutator(store, &store.APIKeys[i]); err != nil {
				return err
			}
			updated = store.APIKeys[i]
			return nil
		}
		return errAPIKeyNotFound
	})
	return updated, err
}

func deleteAPIKey(id uint) error {
	return db.UpdateStore(func(store *db.Store) error {
		for i := range store.APIKeys {
			if store.APIKeys[i].ID != id {
				continue
			}
			store.APIKeys = append(store.APIKeys[:i], store.APIKeys[i+1:]...)
			return nil
		}
		return errAPIKeyNotFound
	})
}

func buildAPIKeyResponses(keys []models.APIKey) []apiKeyResponse {
	return buildAPIKeyResponsesWithProxies(keys, nil)
}

func buildAPIKeyResponsesWithProxies(keys []models.APIKey, proxies []models.UpstreamProxy) []apiKeyResponse {
	proxyIndex := buildProxyReferenceIndex(proxies)
	items := make([]apiKeyResponse, 0, len(keys))
	for _, key := range keys {
		items = append(items, newAPIKeyResponseWithProxies(key, proxyIndex))
	}
	return items
}

func newAPIKeyResponse(key models.APIKey) apiKeyResponse {
	return newAPIKeyResponseWithProxies(key, nil)
}

func newAPIKeyResponseWithProxies(key models.APIKey, proxyIndex map[uint]models.UpstreamProxy) apiKeyResponse {
	resp := apiKeyResponse{
		ID:        key.ID,
		Name:      key.Name,
		Weight:    key.Weight,
		Status:    key.Status,
		ProbeOnly: key.ProbeOnly,
		ProxyID:   key.ProxyID,
		CreatedAt: key.CreatedAt,
		UpdatedAt: key.UpdatedAt,
	}
	if proxyIndex != nil {
		if proxy, ok := proxyIndex[key.ProxyID]; ok {
			resp.ProxyName = proxy.Name
			resp.ProxyGroup = proxy.Group
		}
	}
	return resp
}
