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

	"github.com/gofiber/fiber/v2"
)

type proxyTestRecordResponse struct {
	Success      bool      `json:"success"`
	StatusCode   int       `json:"statusCode,omitempty"`
	ResponseTime int64     `json:"responseTime,omitempty"`
	Message      string    `json:"message,omitempty"`
	Target       string    `json:"target,omitempty"`
	TestedAt     time.Time `json:"testedAt"`
	Summary      string    `json:"summary"`
}

type upstreamProxyResponse struct {
	ID            uint                      `json:"id"`
	Name          string                    `json:"name"`
	Group         string                    `json:"group,omitempty"`
	Type          string                    `json:"type"`
	Status        string                    `json:"status"`
	Host          string                    `json:"host"`
	Port          int                       `json:"port"`
	Username      string                    `json:"username,omitempty"`
	HasPassword   bool                      `json:"hasPassword"`
	BoundKeyCount int                       `json:"boundKeyCount"`
	URLPreview    string                    `json:"urlPreview"`
	LastTest      *proxyTestRecordResponse  `json:"lastTest,omitempty"`
	TestHistory   []proxyTestRecordResponse `json:"testHistory,omitempty"`
	CreatedAt     time.Time                 `json:"createdAt"`
	UpdatedAt     time.Time                 `json:"updatedAt"`
}

type upstreamProxiesResponse struct {
	Proxies []upstreamProxyResponse `json:"proxies"`
}

type createUpstreamProxyRequest struct {
	Name     string `json:"name"`
	Group    string `json:"group,omitempty"`
	Type     string `json:"type"`
	Status   string `json:"status,omitempty"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type updateUpstreamProxyRequest struct {
	Name     *string `json:"name"`
	Group    *string `json:"group,omitempty"`
	Type     *string `json:"type"`
	Status   *string `json:"status,omitempty"`
	Host     *string `json:"host"`
	Port     *int    `json:"port"`
	Username *string `json:"username,omitempty"`
	Password *string `json:"password,omitempty"`
}

type testUpstreamProxyRequest struct {
	ProxyID  *uint  `json:"proxyId,omitempty"`
	Name     string `json:"name,omitempty"`
	Type     string `json:"type,omitempty"`
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type updateUpstreamProxyStatusRequest struct {
	Status string `json:"status"`
}

var errProxyNotFound = errors.New("代理不存在")

func GetUpstreamProxies(c *fiber.Ctx) error {
	store, err := db.ReadStore()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "读取代理失败"})
	}
	return c.JSON(upstreamProxiesResponse{Proxies: buildProxyResponses(store.Proxies, store.APIKeys)})
}

func AddUpstreamProxy(sched *scheduler.Scheduler) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req createUpstreamProxyRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "请求体格式无效"})
		}
		password, err := encryptProxyPassword(req.Password)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		now := time.Now()
		proxyCfg := models.NormalizeUpstreamProxy(models.UpstreamProxy{
			Name:        req.Name,
			Group:       req.Group,
			Type:        req.Type,
			Status:      req.Status,
			Host:        req.Host,
			Port:        req.Port,
			Username:    req.Username,
			Password:    password,
			TestHistory: make([]models.ProxyTestRecord, 0),
			CreatedAt:   now,
			UpdatedAt:   now,
		})
		if err := validateUpstreamProxyModel(proxyCfg); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		if err := db.UpdateStore(func(store *db.Store) error {
			proxyCfg.ID = store.NextProxyID
			store.NextProxyID++
			store.Proxies = append(store.Proxies, proxyCfg)
			return nil
		}); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "保存代理失败"})
		}
		if err := LoadActiveKeys(context.Background(), sched); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "代理已保存，但刷新运行时失败"})
		}
		return c.JSON(fiber.Map{"message": "代理添加成功", "proxy": newProxyResponse(proxyCfg, 0)})
	}
}

func UpdateUpstreamProxy(sched *scheduler.Scheduler) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id, err := parseProxyID(c)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		var req updateUpstreamProxyRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "请求体格式无效"})
		}
		updated, err := mutateProxy(id, func(proxyCfg *models.UpstreamProxy) error {
			if req.Name != nil {
				proxyCfg.Name = strings.TrimSpace(*req.Name)
			}
			if req.Group != nil {
				proxyCfg.Group = strings.TrimSpace(*req.Group)
			}
			if req.Type != nil {
				proxyCfg.Type = strings.TrimSpace(*req.Type)
			}
			if req.Status != nil {
				proxyCfg.Status = strings.TrimSpace(*req.Status)
			}
			if req.Host != nil {
				proxyCfg.Host = strings.TrimSpace(*req.Host)
			}
			if req.Port != nil {
				proxyCfg.Port = *req.Port
			}
			if req.Username != nil {
				proxyCfg.Username = strings.TrimSpace(*req.Username)
			}
			if req.Password != nil && strings.TrimSpace(*req.Password) != "" {
				encrypted, encryptErr := encryptProxyPassword(*req.Password)
				if encryptErr != nil {
					return encryptErr
				}
				proxyCfg.Password = encrypted
			}
			*proxyCfg = models.NormalizeUpstreamProxy(*proxyCfg)
			if err := validateUpstreamProxyModel(*proxyCfg); err != nil {
				return err
			}
			proxyCfg.UpdatedAt = time.Now()
			return nil
		})
		if err != nil {
			status := 500
			if errors.Is(err, errProxyNotFound) {
				status = 404
			} else {
				status = 400
			}
			return c.Status(status).JSON(fiber.Map{"error": err.Error()})
		}
		if err := LoadActiveKeys(context.Background(), sched); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "代理已更新，但刷新运行时失败"})
		}
		return c.JSON(fiber.Map{"message": "代理更新成功", "proxy": newProxyResponse(updated, 0)})
	}
}

func UpdateUpstreamProxyStatus(sched *scheduler.Scheduler) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id, err := parseProxyID(c)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		var req updateUpstreamProxyStatusRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "???????"})
		}
		status := strings.TrimSpace(req.Status)
		if err := validateUpstreamProxyStatus(status); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		updated, err := mutateProxy(id, func(proxyCfg *models.UpstreamProxy) error {
			proxyCfg.Status = status
			proxyCfg.UpdatedAt = time.Now()
			return nil
		})
		if err != nil {
			code := 500
			if errors.Is(err, errProxyNotFound) {
				code = 404
			}
			return c.Status(code).JSON(fiber.Map{"error": err.Error()})
		}
		if err := LoadActiveKeys(context.Background(), sched); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "????????????????"})
		}
		return c.JSON(fiber.Map{"message": "????????", "proxy": newProxyResponse(updated, 0)})
	}
}

func DeleteUpstreamProxy(sched *scheduler.Scheduler) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id, err := parseProxyID(c)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		if err := db.UpdateStore(func(store *db.Store) error {
			for i := range store.Proxies {
				if store.Proxies[i].ID != id {
					continue
				}
				store.Proxies = append(store.Proxies[:i], store.Proxies[i+1:]...)
				for j := range store.APIKeys {
					if store.APIKeys[j].ProxyID == id {
						store.APIKeys[j].ProxyID = 0
						store.APIKeys[j].UpdatedAt = time.Now()
					}
				}
				return nil
			}
			return errProxyNotFound
		}); err != nil {
			status := 500
			if errors.Is(err, errProxyNotFound) {
				status = 404
			}
			return c.Status(status).JSON(fiber.Map{"error": err.Error()})
		}
		if err := LoadActiveKeys(context.Background(), sched); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "代理已删除，但刷新运行时失败"})
		}
		return c.JSON(fiber.Map{"message": "代理删除成功，相关上游 key 已自动解绑"})
	}
}

func TestUpstreamProxy(c *fiber.Ctx) error {
	var req testUpstreamProxyRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "请求体格式无效"})
	}
	var proxyCfg models.UpstreamProxy
	persistToStore := uint(0)
	if req.ProxyID != nil && *req.ProxyID > 0 {
		store, err := db.ReadStore()
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "读取代理失败"})
		}
		found := false
		for _, item := range store.Proxies {
			if item.ID == *req.ProxyID {
				proxyCfg = item
				persistToStore = item.ID
				found = true
				break
			}
		}
		if !found {
			return c.Status(404).JSON(fiber.Map{"error": errProxyNotFound.Error()})
		}
	} else {
		password, err := encryptProxyPassword(req.Password)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		proxyCfg = models.NormalizeUpstreamProxy(models.UpstreamProxy{
			Name:     req.Name,
			Group:    "",
			Type:     req.Type,
			Status:   models.ProxyStatusEnabled,
			Host:     req.Host,
			Port:     req.Port,
			Username: req.Username,
			Password: password,
		})
	}
	if err := validateUpstreamProxyModel(proxyCfg); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	result := testUpstreamProxyConnectivity(context.Background(), proxyCfg)
	if persistToStore > 0 {
		record := buildProxyTestRecordFromResult(result)
		if err := recordProxyTestResult(persistToStore, record); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "代理测试完成，但保存测试历史失败: " + err.Error()})
		}
	}
	return c.JSON(result)
}

func parseProxyID(c *fiber.Ctx) (uint, error) {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil || id == 0 {
		return 0, errors.New("无效的代理 ID")
	}
	return uint(id), nil
}

func mutateProxy(id uint, mutator func(*models.UpstreamProxy) error) (models.UpstreamProxy, error) {
	var updated models.UpstreamProxy
	err := db.UpdateStore(func(store *db.Store) error {
		for i := range store.Proxies {
			if store.Proxies[i].ID != id {
				continue
			}
			if err := mutator(&store.Proxies[i]); err != nil {
				return err
			}
			updated = store.Proxies[i]
			return nil
		}
		return errProxyNotFound
	})
	return updated, err
}

func buildProxyResponses(proxies []models.UpstreamProxy, keys []models.APIKey) []upstreamProxyResponse {
	counts := countAPIKeyProxyUsage(keys)
	items := make([]upstreamProxyResponse, 0, len(proxies))
	for _, proxyCfg := range proxies {
		items = append(items, newProxyResponse(proxyCfg, counts[proxyCfg.ID]))
	}
	return items
}

func buildProxyTestResponse(record *models.ProxyTestRecord) *proxyTestRecordResponse {
	if record == nil {
		return nil
	}
	return &proxyTestRecordResponse{
		Success:      record.Success,
		StatusCode:   record.StatusCode,
		ResponseTime: record.ResponseTime,
		Message:      record.Message,
		Target:       record.Target,
		TestedAt:     record.TestedAt,
		Summary:      formatProxyHistoryLabel(*record),
	}
}

func buildProxyTestHistoryResponses(history []models.ProxyTestRecord) []proxyTestRecordResponse {
	items := make([]proxyTestRecordResponse, 0, len(history))
	for _, item := range history {
		copyRecord := item
		resp := buildProxyTestResponse(&copyRecord)
		if resp != nil {
			items = append(items, *resp)
		}
	}
	return items
}

func newProxyResponse(proxyCfg models.UpstreamProxy, boundCount int) upstreamProxyResponse {
	proxyCfg = models.NormalizeUpstreamProxy(proxyCfg)
	return upstreamProxyResponse{
		ID:            proxyCfg.ID,
		Name:          proxyCfg.Name,
		Group:         proxyCfg.Group,
		Type:          proxyCfg.Type,
		Status:        proxyCfg.Status,
		Host:          proxyCfg.Host,
		Port:          proxyCfg.Port,
		Username:      proxyCfg.Username,
		HasPassword:   strings.TrimSpace(proxyCfg.Password) != "",
		BoundKeyCount: boundCount,
		URLPreview:    buildProxyPreviewFromModel(proxyCfg),
		LastTest:      buildProxyTestResponse(proxyCfg.LastTest),
		TestHistory:   buildProxyTestHistoryResponses(proxyCfg.TestHistory),
		CreatedAt:     proxyCfg.CreatedAt,
		UpdatedAt:     proxyCfg.UpdatedAt,
	}
}
