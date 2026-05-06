package gateway

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"nvidia-api-gateway/pkg/db"
	"nvidia-api-gateway/pkg/scheduler"

	"github.com/gofiber/fiber/v2"
)

type bindAPIKeysProxyRequest struct {
	KeyIDs     []uint  `json:"keyIds"`
	ProxyID    *uint   `json:"proxyId,omitempty"`
	ProxyGroup *string `json:"proxyGroup,omitempty"`
}

type bindAPIKeysProxyResponse struct {
	UpdatedCount int    `json:"updatedCount"`
	MissingIDs   []uint `json:"missingIds,omitempty"`
	ProxyID      uint   `json:"proxyId,omitempty"`
	ProxyName    string `json:"proxyName,omitempty"`
	ProxyGroup   string `json:"proxyGroup,omitempty"`
	KeyIDs       []uint `json:"keyIds"`
}

func BindAPIKeysProxy(sched *scheduler.Scheduler) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req bindAPIKeysProxyRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "请求体格式无效"})
		}
		keyIDs := uniqueUintList(req.KeyIDs)
		if len(keyIDs) == 0 {
			return c.Status(400).JSON(fiber.Map{"error": "至少要选择一个上游 key"})
		}
		proxyID := uint(0)
		if req.ProxyID != nil {
			proxyID = *req.ProxyID
		}
		proxyGroup := ""
		if req.ProxyGroup != nil {
			proxyGroup = strings.TrimSpace(*req.ProxyGroup)
		}
		updated, missing, proxyName, normalizedGroup, err := applyBulkAPIKeyProxyBinding(keyIDs, proxyID, proxyGroup)
		if err != nil {
			status := 500
			if err.Error() == "所选代理不存在" || err.Error() == "所选代理分组不存在" {
				status = 400
			}
			return c.Status(status).JSON(fiber.Map{"error": err.Error()})
		}
		if err := LoadActiveKeys(context.Background(), sched); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "批量绑定已完成，但刷新运行时失败"})
		}
		message := "已批量清空所选 key 的代理绑定"
		if normalizedGroup != "" {
			message = "已按代理分组批量绑定所选 key"
		} else if proxyID > 0 {
			message = "已批量绑定所选 key 到代理池"
		}
		return c.JSON(fiber.Map{
			"message": message,
			"result": bindAPIKeysProxyResponse{
				UpdatedCount: updated,
				MissingIDs:   missing,
				ProxyID:      proxyID,
				ProxyName:    proxyName,
				ProxyGroup:   normalizedGroup,
				KeyIDs:       keyIDs,
			},
		})
	}
}

func applyBulkAPIKeyProxyBinding(keyIDs []uint, proxyID uint, proxyGroup string) (int, []uint, string, string, error) {
	var updatedCount int
	var missing []uint
	var proxyName string
	var normalizedGroup string
	err := db.UpdateStore(func(store *db.Store) error {
		var innerErr error
		updatedCount, missing, proxyName, normalizedGroup, innerErr = applyBulkAPIKeyProxyBindingToStore(store, keyIDs, proxyID, proxyGroup)
		return innerErr
	})
	if err != nil {
		return 0, nil, "", "", err
	}
	return updatedCount, missing, proxyName, normalizedGroup, nil
}

func applyBulkAPIKeyProxyBindingToStore(store *db.Store, keyIDs []uint, proxyID uint, proxyGroup string) (int, []uint, string, string, error) {
	keyIDs = uniqueUintList(keyIDs)
	if len(keyIDs) == 0 {
		return 0, nil, "", "", nil
	}
	proxyGroup = strings.TrimSpace(proxyGroup)
	keyIDSet := make(map[uint]struct{}, len(keyIDs))
	for _, id := range keyIDs {
		keyIDSet[id] = struct{}{}
	}

	proxyName := ""
	normalizedGroup := ""
	proxyAssignments := map[uint]uint{}

	switch {
	case proxyGroup != "":
		members := sortedProxyGroupMembers(store.Proxies, proxyGroup)
		if len(members) == 0 {
			return 0, nil, "", "", errors.New("所选代理分组不存在或没有启用代理")
		}
		normalizedGroup = members[0].Group
		proxyAssignments = healthPreferredAssignProxyIDs(keyIDs, members)
	case proxyID > 0:
		if err := validateAPIKeyProxyReference(store, proxyID); err != nil {
			return 0, nil, "", "", err
		}
		for _, proxyCfg := range store.Proxies {
			if proxyCfg.ID == proxyID {
				proxyName = proxyCfg.Name
				break
			}
		}
		for _, id := range keyIDs {
			proxyAssignments[id] = proxyID
		}
	default:
		for _, id := range keyIDs {
			proxyAssignments[id] = 0
		}
	}

	seen := make(map[uint]struct{})
	updatedCount := 0
	for i := range store.APIKeys {
		assignedProxyID, ok := proxyAssignments[store.APIKeys[i].ID]
		if !ok {
			continue
		}
		store.APIKeys[i].ProxyID = assignedProxyID
		store.APIKeys[i].UpdatedAt = time.Now()
		seen[store.APIKeys[i].ID] = struct{}{}
		updatedCount++
	}
	missing := make([]uint, 0)
	for _, id := range keyIDs {
		if _, ok := seen[id]; !ok {
			missing = append(missing, id)
		}
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i] < missing[j] })
	return updatedCount, missing, proxyName, normalizedGroup, nil
}

func uniqueUintList(items []uint) []uint {
	seen := make(map[uint]struct{}, len(items))
	result := make([]uint, 0, len(items))
	for _, item := range items {
		if item == 0 {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
