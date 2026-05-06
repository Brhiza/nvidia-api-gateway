package middleware

import (
	"strings"

	"nvidia-api-gateway/pkg/db"
	"nvidia-api-gateway/pkg/models"

	"github.com/gofiber/fiber/v2"
)

func MasterAuthMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		store, err := db.ReadStore()
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Failed to load gateway credentials",
			})
		}

		activeMasterKeys := activeMasterKeys(store.MasterKeys)
		if store.SystemConfig.AnonymousAccess || len(activeMasterKeys) == 0 {
			return c.Next()
		}

		keyStr := extractGatewayToken(c)
		if keyStr == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Missing gateway access token",
			})
		}

		var masterKey *models.MasterKey
		for _, key := range activeMasterKeys {
			if key.Key == keyStr {
				copy := key
				masterKey = &copy
				break
			}
		}
		if masterKey == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Invalid or revoked gateway access token",
			})
		}

		if masterKey.Quota != -1 && masterKey.UsedQuota >= masterKey.Quota {
			return c.Status(fiber.StatusPaymentRequired).JSON(fiber.Map{
				"error": "Quota exceeded",
			})
		}

		c.Locals("masterKey", masterKey)
		return c.Next()
	}
}

func activeMasterKeys(keys []models.MasterKey) []models.MasterKey {
	active := make([]models.MasterKey, 0, len(keys))
	for _, key := range keys {
		if strings.EqualFold(strings.TrimSpace(key.Status), "Active") {
			active = append(active, key)
		}
	}
	return active
}

func extractGatewayToken(c *fiber.Ctx) string {
	if c == nil {
		return ""
	}
	if authHeader := strings.TrimSpace(c.Get("Authorization")); authHeader != "" {
		if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
			return strings.TrimSpace(authHeader[7:])
		}
	}
	if token := strings.TrimSpace(c.Get("x-api-key")); token != "" {
		return token
	}
	if token := strings.TrimSpace(c.Get("x-goog-api-key")); token != "" {
		return token
	}
	if token := strings.TrimSpace(c.Query("key")); token != "" {
		return token
	}
	if token := strings.TrimSpace(c.Query("api_key")); token != "" {
		return token
	}
	return ""
}
