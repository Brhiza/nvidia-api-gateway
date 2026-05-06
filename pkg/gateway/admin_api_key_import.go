package gateway

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"nvidia-api-gateway/pkg/db"
	"nvidia-api-gateway/pkg/models"
	"nvidia-api-gateway/pkg/scheduler"
	"nvidia-api-gateway/pkg/utils"

	"github.com/gofiber/fiber/v2"
)

type importAPIKeysRequest struct {
	Payload       string   `json:"payload"`
	DefaultWeight *float64 `json:"defaultWeight"`
}

type importAPIKeysResponse struct {
	AddedCount   int              `json:"addedCount"`
	SkippedCount int              `json:"skippedCount"`
	Added        []apiKeyResponse `json:"added"`
	Skipped      []string         `json:"skipped"`
}

func ImportAPIKeys(sched *scheduler.Scheduler) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req importAPIKeysRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "\u8bf7\u6c42\u4f53\u683c\u5f0f\u65e0\u6548"})
		}

		payload := strings.TrimSpace(req.Payload)
		if payload == "" {
			return c.Status(400).JSON(fiber.Map{"error": "\u5bfc\u5165\u5185\u5bb9\u4e0d\u80fd\u4e3a\u7a7a"})
		}

		rootKey := utils.GetEncryptionKey()
		if rootKey == "" || len(rootKey) != 32 {
			return c.Status(500).JSON(fiber.Map{"error": "\u670d\u52a1\u7aef\u7f3a\u5c11\u5408\u6cd5\u7684 32 \u4f4d ENCRYPTION_KEY"})
		}

		defaultWeight := 1.0
		if req.DefaultWeight != nil && *req.DefaultWeight > 0 {
			defaultWeight = *req.DefaultWeight
		}

		var added []models.APIKey
		skipped := make([]string, 0)
		err := db.UpdateStore(func(store *db.Store) error {
			existing := make(map[string]struct{}, len(store.APIKeys))
			for _, apiKey := range store.APIKeys {
				plaintext, err := utils.Decrypt(apiKey.Key, rootKey)
				if err != nil {
					return fmt.Errorf("\u89e3\u5bc6\u4e0a\u6e38 Key %d \u5931\u8d25: %w", apiKey.ID, err)
				}
				existing[plaintext] = struct{}{}
			}

			for _, line := range splitImportPayload(payload) {
				name, plaintextKey, weight, err := parseImportedAPIKeyLine(line, store.NextAPIID, defaultWeight)
				if err != nil {
					skipped = append(skipped, fmt.Sprintf("%s -> %s", line, err.Error()))
					continue
				}
				if _, exists := existing[plaintextKey]; exists {
					skipped = append(skipped, fmt.Sprintf("%s -> \u91cd\u590d Key", name))
					continue
				}
				encryptedKey, err := utils.Encrypt(plaintextKey, rootKey)
				if err != nil {
					return fmt.Errorf("\u52a0\u5bc6\u5bfc\u5165\u7684\u4e0a\u6e38 Key %s \u5931\u8d25: %w", name, err)
				}
				now := time.Now()
				apiKey := models.APIKey{
					ID:        store.NextAPIID,
					Key:       encryptedKey,
					Name:      name,
					Weight:    weight,
					Status:    APIKeyStatusActive,
					CreatedAt: now,
					UpdatedAt: now,
				}
				store.NextAPIID++
				store.APIKeys = append(store.APIKeys, apiKey)
				existing[plaintextKey] = struct{}{}
				added = append(added, apiKey)
			}
			return nil
		})
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}

		if len(added) > 0 {
			if err := LoadActiveKeys(context.Background(), sched); err != nil {
				return c.Status(500).JSON(fiber.Map{"error": "\u4e0a\u6e38 Key \u5df2\u5bfc\u5165\uff0c\u4f46\u91cd\u8f7d\u8c03\u5ea6\u5668\u5931\u8d25"})
			}
		}

		return c.JSON(fiber.Map{
			"message": fmt.Sprintf("\u5df2\u5bfc\u5165 %d \u4e2a\u4e0a\u6e38 Key\uff0c\u8df3\u8fc7 %d \u4e2a", len(added), len(skipped)),
			"result": importAPIKeysResponse{
				AddedCount:   len(added),
				SkippedCount: len(skipped),
				Added:        buildAPIKeyResponses(added),
				Skipped:      skipped,
			},
		})
	}
}

func splitImportPayload(payload string) []string {
	lines := strings.Split(strings.ReplaceAll(payload, "\r\n", "\n"), "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		result = append(result, line)
	}
	return result
}

func parseImportedAPIKeyLine(line string, nextID uint, defaultWeight float64) (string, string, float64, error) {
	parts := splitImportFields(line)
	if len(parts) == 0 {
		return "", "", 0, errors.New("\u7a7a\u884c")
	}
	defaultName := fmt.Sprintf("NVIDIA-%04d", nextID)

	switch len(parts) {
	case 1:
		if !looksLikeAPIKey(parts[0]) {
			return "", "", 0, errors.New("\u65e0\u6cd5\u8bc6\u522b Key")
		}
		return defaultName, parts[0], defaultWeight, nil
	case 2:
		firstIsKey := looksLikeAPIKey(parts[0])
		secondIsKey := looksLikeAPIKey(parts[1])
		switch {
		case firstIsKey && !secondIsKey:
			if weight, ok := tryParsePositiveFloat(parts[1]); ok {
				return defaultName, parts[0], weight, nil
			}
			return parts[1], parts[0], defaultWeight, nil
		case !firstIsKey && secondIsKey:
			return parts[0], parts[1], defaultWeight, nil
		default:
			return "", "", 0, errors.New("\u5e94\u4e3a `key`\u3001`name,key` \u6216 `key,weight` \u683c\u5f0f")
		}
	default:
		firstIsKey := looksLikeAPIKey(parts[0])
		secondIsKey := looksLikeAPIKey(parts[1])
		weight := defaultWeight
		if parsed, ok := tryParsePositiveFloat(parts[2]); ok {
			weight = parsed
		}
		switch {
		case firstIsKey && !secondIsKey:
			return parts[1], parts[0], weight, nil
		case !firstIsKey && secondIsKey:
			return parts[0], parts[1], weight, nil
		default:
			return "", "", 0, errors.New("\u5e94\u4e3a `name,key,weight` \u6216 `key,name,weight` \u683c\u5f0f")
		}
	}
}

func splitImportFields(line string) []string {
	var raw []string
	switch {
	case strings.Contains(line, "|"):
		raw = strings.Split(line, "|")
	case strings.Contains(line, "\t"):
		raw = strings.Split(line, "\t")
	case strings.Contains(line, ","):
		raw = strings.Split(line, ",")
	default:
		raw = []string{line}
	}
	parts := make([]string, 0, len(raw))
	for _, item := range raw {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return parts
}

func looksLikeAPIKey(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "nvapi-") || (len(value) >= 24 && !strings.Contains(value, " "))
}

func tryParsePositiveFloat(value string) (float64, bool) {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || parsed <= 0 {
		return 0, false
	}
	return parsed, true
}
