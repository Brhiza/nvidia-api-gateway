package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"nvidia-api-gateway/pkg/models"
)

type Store struct {
	APIKeys      []models.APIKey        `json:"api_keys"`
	Proxies      []models.UpstreamProxy `json:"proxies,omitempty"`
	MasterKeys   []models.MasterKey     `json:"master_keys"`
	SystemConfig models.SystemConfig    `json:"system_config"`
	HealthState  json.RawMessage        `json:"health_state,omitempty"`
	NextAPIID    uint                   `json:"next_api_id"`
	NextProxyID  uint                   `json:"next_proxy_id,omitempty"`
	NextMKID     uint                   `json:"next_master_key_id"`
}

var (
	storePath string
	storeMu   sync.Mutex
)

func InitDB(dsn string) {
	if strings.TrimSpace(dsn) == "" {
		dsn = "gateway.json"
	}
	storePath = dsn

	if err := ensureStoreFile(storePath); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	if _, err := ReadStore(); err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	log.Printf("Database initialized successfully: %s", storePath)
}

func ensureStoreFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if len(data) == 0 {
			return writeStore(defaultStore())
		}
		var probe json.RawMessage
		if err := json.Unmarshal(data, &probe); err != nil {
			return fmt.Errorf("%s is not a JSON store file; rename or remove it first", path)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if legacyPath := legacyStorePath(path); legacyPath != "" {
		if _, err := os.Stat(legacyPath); err == nil {
			backupPath := legacyPath + ".bak"
			if _, backupErr := os.Stat(backupPath); errors.Is(backupErr, os.ErrNotExist) {
				if renameErr := os.Rename(legacyPath, backupPath); renameErr != nil {
					return fmt.Errorf("found legacy sqlite file %s and failed to rename it to %s: %w", legacyPath, backupPath, renameErr)
				}
				log.Printf("Legacy sqlite file %s renamed to %s", legacyPath, backupPath)
			} else if backupErr == nil {
				return fmt.Errorf("found legacy sqlite file %s; remove it because backup %s already exists", legacyPath, backupPath)
			} else {
				return backupErr
			}
		}
	}

	return writeStore(defaultStore())
}

func legacyStorePath(path string) string {
	if filepath.Base(path) == "gateway.json" {
		return filepath.Join(filepath.Dir(path), "gateway.db")
	}
	return ""
}

func ReadStore() (*Store, error) {
	storeMu.Lock()
	defer storeMu.Unlock()
	return readStoreUnlocked()
}

func UpdateStore(mutator func(*Store) error) error {
	storeMu.Lock()
	defer storeMu.Unlock()

	store, err := readStoreUnlocked()
	if err != nil {
		return err
	}
	if err := mutator(store); err != nil {
		return err
	}
	return writeStoreUnlocked(normalizeStore(store))
}

func readStoreUnlocked() (*Store, error) {
	data, err := os.ReadFile(storePath)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return defaultStore(), nil
	}

	var store Store
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, err
	}
	return normalizeStore(&store), nil
}

func writeStore(store *Store) error {
	storeMu.Lock()
	defer storeMu.Unlock()
	return writeStoreUnlocked(normalizeStore(store))
}

func writeStoreUnlocked(store *Store) error {
	data, err := json.MarshalIndent(normalizeStore(store), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(storePath, data, 0o644)
}

func defaultStore() *Store {
	return &Store{
		APIKeys:      make([]models.APIKey, 0),
		Proxies:      make([]models.UpstreamProxy, 0),
		MasterKeys:   make([]models.MasterKey, 0),
		SystemConfig: models.DefaultSystemConfig(),
		NextAPIID:    1,
		NextProxyID:  1,
		NextMKID:     1,
	}
}

func normalizeStore(store *Store) *Store {
	if store == nil {
		return defaultStore()
	}
	if store.APIKeys == nil {
		store.APIKeys = make([]models.APIKey, 0)
	}
	if store.Proxies == nil {
		store.Proxies = make([]models.UpstreamProxy, 0)
	}
	if store.MasterKeys == nil {
		store.MasterKeys = make([]models.MasterKey, 0)
	}
	for i := range store.Proxies {
		store.Proxies[i] = models.NormalizeUpstreamProxy(store.Proxies[i])
		if store.Proxies[i].TestHistory == nil {
			store.Proxies[i].TestHistory = make([]models.ProxyTestRecord, 0)
		}
	}
	store.SystemConfig = models.NormalizeSystemConfig(store.SystemConfig)
	if store.NextAPIID == 0 {
		store.NextAPIID = nextAPIID(store.APIKeys)
	}
	if store.NextProxyID == 0 {
		store.NextProxyID = nextProxyID(store.Proxies)
	}
	if store.NextMKID == 0 {
		store.NextMKID = nextMasterKeyID(store.MasterKeys)
	}
	return store
}

func nextAPIID(keys []models.APIKey) uint {
	var maxID uint
	for _, key := range keys {
		if key.ID > maxID {
			maxID = key.ID
		}
	}
	return maxID + 1
}

func nextProxyID(proxies []models.UpstreamProxy) uint {
	var maxID uint
	for _, proxy := range proxies {
		if proxy.ID > maxID {
			maxID = proxy.ID
		}
	}
	return maxID + 1
}

func nextMasterKeyID(keys []models.MasterKey) uint {
	var maxID uint
	for _, key := range keys {
		if key.ID > maxID {
			maxID = key.ID
		}
	}
	return maxID + 1
}
