package gateway

import (
	"os"
	"testing"
	"time"

	"nvidia-api-gateway/pkg/db"
	"nvidia-api-gateway/pkg/models"
	"nvidia-api-gateway/pkg/utils"
)

func TestBuildProxyURLFromModel(t *testing.T) {
	old := os.Getenv("ENCRYPTION_KEY")
	_ = os.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")
	defer func() { _ = os.Setenv("ENCRYPTION_KEY", old) }()

	encrypted, err := encryptProxyPassword("secret")
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}
	proxyCfg := models.UpstreamProxy{
		ID:       1,
		Name:     "SG Node",
		Type:     "socks5h",
		Host:     "127.0.0.1",
		Port:     1080,
		Username: "alice",
		Password: encrypted,
	}
	url, err := buildProxyURLFromModel(proxyCfg)
	if err != nil {
		t.Fatalf("build proxy URL: %v", err)
	}
	if url != "socks5h://alice:secret@127.0.0.1:1080" {
		t.Fatalf("url = %q", url)
	}
}

func TestRebuildUpstreamRuntimeResolvesPerKeyProxy(t *testing.T) {
	old := os.Getenv("ENCRYPTION_KEY")
	_ = os.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")
	defer func() { _ = os.Setenv("ENCRYPTION_KEY", old) }()

	encryptedProxyPassword, err := encryptProxyPassword("secret")
	if err != nil {
		t.Fatalf("encrypt proxy password: %v", err)
	}
	encryptedKey, err := utils.Encrypt("nvapi-test", os.Getenv("ENCRYPTION_KEY"))
	if err != nil {
		t.Fatalf("encrypt api key: %v", err)
	}
	store := &db.Store{
		APIKeys: []models.APIKey{{
			ID:        1,
			Name:      "NVIDIA-01",
			Key:       encryptedKey,
			Status:    APIKeyStatusActive,
			ProxyID:   7,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}},
		Proxies: []models.UpstreamProxy{{
			ID:        7,
			Name:      "SG Node",
			Type:      "http",
			Host:      "10.0.0.2",
			Port:      7890,
			Username:  "alice",
			Password:  encryptedProxyPassword,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}},
	}
	if err := rebuildUpstreamRuntime(store); err != nil {
		t.Fatalf("rebuild runtime: %v", err)
	}
	proxyURL, ok := resolveProxyOverrideForPlaintextKey("nvapi-test")
	if !ok {
		t.Fatal("expected override proxy URL")
	}
	if proxyURL != "http://alice:secret@10.0.0.2:7890" {
		t.Fatalf("proxyURL = %q", proxyURL)
	}
	info, ok := lookupKeyRuntimeInfo("nvapi-test")
	if !ok {
		t.Fatal("expected key runtime info")
	}
	if info.ProxyName != "SG Node" {
		t.Fatalf("proxy name = %q", info.ProxyName)
	}
}
