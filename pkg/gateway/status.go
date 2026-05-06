package gateway

import (
	"context"
	"strings"
	"time"

	"nvidia-api-gateway/pkg/db"
	"nvidia-api-gateway/pkg/utils"
)

func updateAPIKeyStatusByPlaintext(plaintextKey, status string) {
	secret := strings.TrimSpace(utils.GetEncryptionKey())
	if secret == "" {
		return
	}

	_ = db.UpdateStore(func(store *db.Store) error {
		for i, key := range store.APIKeys {
			decrypted, err := utils.Decrypt(key.Key, secret)
			if err != nil {
				continue
			}
			if decrypted == plaintextKey {
				store.APIKeys[i].Status = status
				store.APIKeys[i].UpdatedAt = time.Now()
				return nil
			}
		}
		return nil
	})
}

func setAPIKeyStatusByEncryptedID(id uint, status string) {
	_ = db.UpdateStore(func(store *db.Store) error {
		for i, key := range store.APIKeys {
			if key.ID == id {
				store.APIKeys[i].Status = status
				store.APIKeys[i].UpdatedAt = time.Now()
				return nil
			}
		}
		return nil
	})
}

func restoreAPIKeyStatuses(ctx context.Context) error {
	secret := strings.TrimSpace(utils.GetEncryptionKey())
	if secret == "" {
		return nil
	}

	store, err := db.ReadStore()
	if err != nil {
		return err
	}

	for _, key := range store.APIKeys {
		if key.Status != APIKeyStatusCooling && key.Status != APIKeyStatusDead {
			continue
		}
		plaintext, err := utils.Decrypt(key.Key, secret)
		if err != nil {
			continue
		}
		if probe, ok := probeKeyStatus(ctx, plaintext); ok && probe != nil {
			setAPIKeyStatusByEncryptedID(key.ID, probe.Status)
		}
	}

	return nil
}

func incrementMasterKeyQuota(id uint, tokenCost int) {
	if id == 0 || tokenCost <= 0 {
		return
	}
	_ = db.UpdateStore(func(store *db.Store) error {
		for i, key := range store.MasterKeys {
			if key.ID == id {
				store.MasterKeys[i].UsedQuota += int64(tokenCost)
				store.MasterKeys[i].UpdatedAt = time.Now()
				return nil
			}
		}
		return nil
	})
}
