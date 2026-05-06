package gateway

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"nvidia-api-gateway/pkg/db"
	"nvidia-api-gateway/pkg/models"

	"github.com/gofiber/fiber/v2"
)

type masterKeyResponse struct {
	ID        uint      `json:"id"`
	Name      string    `json:"name"`
	MaskedKey string    `json:"maskedKey"`
	RPM       int       `json:"rpm"`
	TPM       int       `json:"tpm"`
	Quota     int64     `json:"quota"`
	UsedQuota int64     `json:"usedQuota"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type masterKeysResponse struct {
	Keys []masterKeyResponse `json:"keys"`
}

type createMasterKeyRequest struct {
	Name   string `json:"name"`
	Key    string `json:"key"`
	RPM    int    `json:"rpm"`
	TPM    int    `json:"tpm"`
	Quota  int64  `json:"quota"`
	Status string `json:"status"`
}

type updateMasterKeyRequest struct {
	Name   *string `json:"name"`
	Key    string  `json:"key"`
	RPM    *int    `json:"rpm"`
	TPM    *int    `json:"tpm"`
	Quota  *int64  `json:"quota"`
	Status *string `json:"status"`
}

type updateMasterKeyStatusRequest struct {
	Status string `json:"status"`
}

func GetMasterKeys(c *fiber.Ctx) error {
	store, err := db.ReadStore()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "\u8bfb\u53d6\u81ea\u5b9a\u4e49 API Key \u5217\u8868\u5931\u8d25"})
	}
	return c.JSON(masterKeysResponse{Keys: buildMasterKeyResponses(store.MasterKeys)})
}

func AddMasterKey(c *fiber.Ctx) error {
	var req createMasterKeyRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "\u8bf7\u6c42\u4f53\u683c\u5f0f\u65e0\u6548"})
	}

	name, keyValue, status, err := normalizeMasterKeyCreate(req)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}

	now := time.Now()
	masterKey := models.MasterKey{
		Name:      name,
		Key:       keyValue,
		RPM:       req.RPM,
		TPM:       req.TPM,
		Quota:     req.Quota,
		UsedQuota: 0,
		Status:    status,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := db.UpdateStore(func(store *db.Store) error {
		for _, existing := range store.MasterKeys {
			if existing.Key == keyValue {
				return errors.New("\u81ea\u5b9a\u4e49 API Key \u5df2\u5b58\u5728")
			}
		}
		masterKey.ID = store.NextMKID
		store.NextMKID++
		store.MasterKeys = append(store.MasterKeys, masterKey)
		return nil
	}); err != nil {
		statusCode := 500
		if err.Error() == "\u81ea\u5b9a\u4e49 API Key \u5df2\u5b58\u5728" {
			statusCode = 409
		}
		return c.Status(statusCode).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{
		"message":  "\u81ea\u5b9a\u4e49 API Key \u521b\u5efa\u6210\u529f",
		"key":      newMasterKeyResponse(masterKey),
		"plainKey": keyValue,
	})
}

func UpdateMasterKey(c *fiber.Ctx) error {
	id, err := parseMasterKeyID(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}

	var req updateMasterKeyRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "\u8bf7\u6c42\u4f53\u683c\u5f0f\u65e0\u6548"})
	}

	var plainKey string
	updatedKey, err := mutateMasterKey(id, func(key *models.MasterKey, store *db.Store) error {
		if req.Name != nil {
			name := strings.TrimSpace(*req.Name)
			if name == "" {
				return errors.New("\u540d\u79f0\u4e0d\u80fd\u4e3a\u7a7a")
			}
			key.Name = name
		}
		if req.RPM != nil {
			if *req.RPM < 0 {
				return errors.New("RPM \u4e0d\u80fd\u5c0f\u4e8e 0")
			}
			key.RPM = *req.RPM
		}
		if req.TPM != nil {
			if *req.TPM < 0 {
				return errors.New("TPM \u4e0d\u80fd\u5c0f\u4e8e 0")
			}
			key.TPM = *req.TPM
		}
		if req.Quota != nil {
			if *req.Quota < -1 {
				return errors.New("\u914d\u989d\u53ea\u80fd\u586b\u5199 -1 \u6216\u66f4\u5927\u7684\u6570\u5b57")
			}
			key.Quota = *req.Quota
		}
		if req.Status != nil {
			status, err := normalizeMasterKeyStatus(*req.Status)
			if err != nil {
				return err
			}
			key.Status = status
		}
		if strings.TrimSpace(req.Key) != "" {
			for _, existing := range store.MasterKeys {
				if existing.ID != key.ID && existing.Key == strings.TrimSpace(req.Key) {
					return errors.New("\u81ea\u5b9a\u4e49 API Key \u5df2\u5b58\u5728")
				}
			}
			key.Key = strings.TrimSpace(req.Key)
			plainKey = key.Key
		}
		key.UpdatedAt = time.Now()
		return nil
	})
	if err != nil {
		statusCode := 500
		switch {
		case errors.Is(err, errMasterKeyNotFound):
			statusCode = 404
		case err.Error() == "\u540d\u79f0\u4e0d\u80fd\u4e3a\u7a7a" || err.Error() == "RPM \u4e0d\u80fd\u5c0f\u4e8e 0" || err.Error() == "TPM \u4e0d\u80fd\u5c0f\u4e8e 0" || err.Error() == "\u914d\u989d\u53ea\u80fd\u586b\u5199 -1 \u6216\u66f4\u5927\u7684\u6570\u5b57" || err.Error() == "\u72b6\u6001\u53ea\u80fd\u662f Active \u6216 Disabled":
			statusCode = 400
		case err.Error() == "\u81ea\u5b9a\u4e49 API Key \u5df2\u5b58\u5728":
			statusCode = 409
		}
		return c.Status(statusCode).JSON(fiber.Map{"error": err.Error()})
	}

	response := fiber.Map{
		"message": "\u81ea\u5b9a\u4e49 API Key \u66f4\u65b0\u6210\u529f",
		"key":     newMasterKeyResponse(updatedKey),
	}
	if plainKey != "" {
		response["plainKey"] = plainKey
	}
	return c.JSON(response)
}

func DeleteMasterKey(c *fiber.Ctx) error {
	id, err := parseMasterKeyID(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	if err := deleteMasterKey(id); err != nil {
		statusCode := 500
		if errors.Is(err, errMasterKeyNotFound) {
			statusCode = 404
		}
		return c.Status(statusCode).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"message": "\u81ea\u5b9a\u4e49 API Key \u5220\u9664\u6210\u529f"})
}

func RotateMasterKey(c *fiber.Ctx) error {
	id, err := parseMasterKeyID(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}

	var rotated models.MasterKey
	var plainKey string
	err = db.UpdateStore(func(store *db.Store) error {
		index := -1
		for i := range store.MasterKeys {
			if store.MasterKeys[i].ID == id {
				index = i
				break
			}
		}
		if index == -1 {
			return errMasterKeyNotFound
		}
		for {
			candidate, genErr := generateMasterKeyValue()
			if genErr != nil {
				return genErr
			}
			duplicate := false
			for _, existing := range store.MasterKeys {
				if existing.ID != id && existing.Key == candidate {
					duplicate = true
					break
				}
			}
			if duplicate {
				continue
			}
			store.MasterKeys[index].Key = candidate
			store.MasterKeys[index].UpdatedAt = time.Now()
			rotated = store.MasterKeys[index]
			plainKey = candidate
			return nil
		}
	})
	if err != nil {
		statusCode := 500
		if errors.Is(err, errMasterKeyNotFound) {
			statusCode = 404
		}
		return c.Status(statusCode).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{
		"message":  "\u81ea\u5b9a\u4e49 API Key \u5df2\u8f6e\u6362",
		"key":      newMasterKeyResponse(rotated),
		"plainKey": plainKey,
	})
}

func RevealMasterKey(c *fiber.Ctx) error {
	id, err := parseMasterKeyID(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	store, err := db.ReadStore()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "\u8bfb\u53d6\u81ea\u5b9a\u4e49 API Key \u5931\u8d25"})
	}
	for _, key := range store.MasterKeys {
		if key.ID == id {
			return c.JSON(fiber.Map{
				"id":       key.ID,
				"name":     key.Name,
				"plainKey": key.Key,
			})
		}
	}
	return c.Status(404).JSON(fiber.Map{"error": errMasterKeyNotFound.Error()})
}

func UpdateMasterKeyStatus(c *fiber.Ctx) error {
	id, err := parseMasterKeyID(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	var req updateMasterKeyStatusRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "\u8bf7\u6c42\u4f53\u683c\u5f0f\u65e0\u6548"})
	}
	status, err := normalizeMasterKeyStatus(req.Status)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	updatedKey, err := mutateMasterKey(id, func(key *models.MasterKey, _ *db.Store) error {
		key.Status = status
		key.UpdatedAt = time.Now()
		return nil
	})
	if err != nil {
		statusCode := 500
		if errors.Is(err, errMasterKeyNotFound) {
			statusCode = 404
		}
		return c.Status(statusCode).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{
		"message": "\u81ea\u5b9a\u4e49 API Key \u72b6\u6001\u66f4\u65b0\u6210\u529f",
		"key":     newMasterKeyResponse(updatedKey),
	})
}

var errMasterKeyNotFound = errors.New("\u81ea\u5b9a\u4e49 API Key \u4e0d\u5b58\u5728")

func normalizeMasterKeyCreate(req createMasterKeyRequest) (string, string, string, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return "", "", "", errors.New("\u540d\u79f0\u4e0d\u80fd\u4e3a\u7a7a")
	}
	if req.RPM < 0 {
		return "", "", "", errors.New("RPM \u4e0d\u80fd\u5c0f\u4e8e 0")
	}
	if req.TPM < 0 {
		return "", "", "", errors.New("TPM \u4e0d\u80fd\u5c0f\u4e8e 0")
	}
	if req.Quota < -1 {
		return "", "", "", errors.New("\u914d\u989d\u53ea\u80fd\u586b\u5199 -1 \u6216\u66f4\u5927\u7684\u6570\u5b57")
	}
	status := req.Status
	if strings.TrimSpace(status) == "" {
		status = APIKeyStatusActive
	}
	normalizedStatus, err := normalizeMasterKeyStatus(status)
	if err != nil {
		return "", "", "", err
	}
	keyValue := strings.TrimSpace(req.Key)
	if keyValue == "" {
		generated, err := generateMasterKeyValue()
		if err != nil {
			return "", "", "", err
		}
		keyValue = generated
	}
	return name, keyValue, normalizedStatus, nil
}

func normalizeMasterKeyStatus(status string) (string, error) {
	switch strings.TrimSpace(status) {
	case APIKeyStatusActive:
		return APIKeyStatusActive, nil
	case APIKeyStatusDisabled:
		return APIKeyStatusDisabled, nil
	default:
		return "", errors.New("\u72b6\u6001\u53ea\u80fd\u662f Active \u6216 Disabled")
	}
}

func generateMasterKeyValue() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("\u751f\u6210\u81ea\u5b9a\u4e49 API Key \u5931\u8d25: %w", err)
	}
	return "sk-" + base64.RawURLEncoding.EncodeToString(buf), nil
}

func maskSecret(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 12 {
		if len(value) <= 4 {
			return value
		}
		return value[:2] + strings.Repeat("*", len(value)-4) + value[len(value)-2:]
	}
	return value[:6] + strings.Repeat("*", len(value)-12) + value[len(value)-6:]
}

func parseMasterKeyID(c *fiber.Ctx) (uint, error) {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil || id == 0 {
		return 0, errors.New("\u65e0\u6548\u7684\u81ea\u5b9a\u4e49 API Key ID")
	}
	return uint(id), nil
}

func mutateMasterKey(id uint, mutator func(*models.MasterKey, *db.Store) error) (models.MasterKey, error) {
	var updated models.MasterKey
	err := db.UpdateStore(func(store *db.Store) error {
		for i := range store.MasterKeys {
			if store.MasterKeys[i].ID != id {
				continue
			}
			if err := mutator(&store.MasterKeys[i], store); err != nil {
				return err
			}
			updated = store.MasterKeys[i]
			return nil
		}
		return errMasterKeyNotFound
	})
	return updated, err
}

func deleteMasterKey(id uint) error {
	return db.UpdateStore(func(store *db.Store) error {
		for i := range store.MasterKeys {
			if store.MasterKeys[i].ID != id {
				continue
			}
			store.MasterKeys = append(store.MasterKeys[:i], store.MasterKeys[i+1:]...)
			return nil
		}
		return errMasterKeyNotFound
	})
}

func buildMasterKeyResponses(keys []models.MasterKey) []masterKeyResponse {
	items := make([]masterKeyResponse, 0, len(keys))
	for _, key := range keys {
		items = append(items, newMasterKeyResponse(key))
	}
	return items
}

func newMasterKeyResponse(key models.MasterKey) masterKeyResponse {
	return masterKeyResponse{
		ID:        key.ID,
		Name:      key.Name,
		MaskedKey: maskSecret(key.Key),
		RPM:       key.RPM,
		TPM:       key.TPM,
		Quota:     key.Quota,
		UsedQuota: key.UsedQuota,
		Status:    key.Status,
		CreatedAt: key.CreatedAt,
		UpdatedAt: key.UpdatedAt,
	}
}
