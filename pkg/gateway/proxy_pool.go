package gateway

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"nvidia-api-gateway/pkg/db"
	"nvidia-api-gateway/pkg/models"
	"nvidia-api-gateway/pkg/utils"
)

type upstreamKeyRuntimeInfo struct {
	KeyID     uint
	KeyName   string
	ProxyID   uint
	ProxyName string
	ProxyURL  string
}

type upstreamProxyRuntimeInfo struct {
	ID       uint
	Name     string
	Type     string
	URL      string
	HostPort string
	Username string
}

var (
	upstreamRuntimeMu      sync.RWMutex
	upstreamRuntimeByKey   = map[string]upstreamKeyRuntimeInfo{}
	upstreamRuntimeByProxy = map[uint]upstreamProxyRuntimeInfo{}
)

func normalizeProxyType(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func validateProxyType(value string) error {
	switch normalizeProxyType(value) {
	case "http", "https", "socks5", "socks5h":
		return nil
	default:
		return fmt.Errorf("unsupported proxy type: %s", strings.TrimSpace(value))
	}
}

func validateUpstreamProxyStatus(value string) error {
	switch strings.TrimSpace(value) {
	case models.ProxyStatusEnabled, models.ProxyStatusDisabled:
		return nil
	default:
		return fmt.Errorf("unsupported proxy status: %s", strings.TrimSpace(value))
	}
}

func validateUpstreamProxyModel(proxy models.UpstreamProxy) error {
	proxy = models.NormalizeUpstreamProxy(proxy)
	if proxy.Name == "" {
		return fmt.Errorf("代理名称不能为空")
	}
	if err := validateProxyType(proxy.Type); err != nil {
		return err
	}
	if proxy.Host == "" {
		return fmt.Errorf("代理主机不能为空")
	}
	if proxy.Port <= 0 || proxy.Port > 65535 {
		return fmt.Errorf("代理端口必须在 1-65535 之间")
	}
	return nil
}

func encryptProxyPassword(raw string) (string, error) {
	secret := strings.TrimSpace(utils.GetEncryptionKey())
	if secret == "" || len(secret) != 32 {
		return "", fmt.Errorf("missing ENCRYPTION_KEY")
	}
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	return utils.Encrypt(strings.TrimSpace(raw), secret)
}

func decryptProxyPassword(raw string) (string, error) {
	secret := strings.TrimSpace(utils.GetEncryptionKey())
	if secret == "" || len(secret) != 32 {
		return "", fmt.Errorf("missing ENCRYPTION_KEY")
	}
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	return utils.Decrypt(strings.TrimSpace(raw), secret)
}

func buildProxyURLFromModel(proxy models.UpstreamProxy) (string, error) {
	proxy = models.NormalizeUpstreamProxy(proxy)
	if err := validateUpstreamProxyModel(proxy); err != nil {
		return "", err
	}
	password, err := decryptProxyPassword(proxy.Password)
	if err != nil {
		return "", err
	}
	userInfo := ""
	if proxy.Username != "" || password != "" {
		if password != "" {
			userInfo = fmt.Sprintf("%s:%s@", proxy.Username, password)
		} else {
			userInfo = fmt.Sprintf("%s@", proxy.Username)
		}
	}
	return fmt.Sprintf("%s://%s%s:%d", proxy.Type, userInfo, proxy.Host, proxy.Port), nil
}

func buildProxyPreviewFromModel(proxy models.UpstreamProxy) string {
	proxy = models.NormalizeUpstreamProxy(proxy)
	userInfo := ""
	if proxy.Username != "" {
		userInfo = proxy.Username + "@"
	}
	return fmt.Sprintf("%s://%s%s:%d", proxy.Type, userInfo, proxy.Host, proxy.Port)
}

func buildProxyRuntimeIndex(proxies []models.UpstreamProxy) map[uint]upstreamProxyRuntimeInfo {
	items := make(map[uint]upstreamProxyRuntimeInfo, len(proxies))
	for _, proxy := range proxies {
		proxy = models.NormalizeUpstreamProxy(proxy)
		if proxy.ID == 0 || !isProxyEnabled(proxy) {
			continue
		}
		url, err := buildProxyURLFromModel(proxy)
		if err != nil {
			continue
		}
		items[proxy.ID] = upstreamProxyRuntimeInfo{
			ID:       proxy.ID,
			Name:     proxy.Name,
			Type:     proxy.Type,
			URL:      url,
			HostPort: fmt.Sprintf("%s:%d", proxy.Host, proxy.Port),
			Username: proxy.Username,
		}
	}
	return items
}

func rebuildUpstreamRuntime(store *db.Store) error {
	secret := strings.TrimSpace(utils.GetEncryptionKey())
	if secret == "" || len(secret) != 32 {
		upstreamRuntimeMu.Lock()
		upstreamRuntimeByKey = map[string]upstreamKeyRuntimeInfo{}
		upstreamRuntimeByProxy = map[uint]upstreamProxyRuntimeInfo{}
		upstreamRuntimeMu.Unlock()
		return fmt.Errorf("missing ENCRYPTION_KEY")
	}
	proxyIndex := buildProxyRuntimeIndex(store.Proxies)
	keyIndex := make(map[string]upstreamKeyRuntimeInfo, len(store.APIKeys))
	for _, key := range store.APIKeys {
		plaintext, err := utils.Decrypt(key.Key, secret)
		if err != nil || strings.TrimSpace(plaintext) == "" {
			continue
		}
		info := upstreamKeyRuntimeInfo{
			KeyID:   key.ID,
			KeyName: key.Name,
			ProxyID: key.ProxyID,
		}
		if proxyInfo, ok := proxyIndex[key.ProxyID]; ok {
			info.ProxyName = proxyInfo.Name
			info.ProxyURL = proxyInfo.URL
		}
		keyIndex[plaintext] = info
	}
	upstreamRuntimeMu.Lock()
	upstreamRuntimeByKey = keyIndex
	upstreamRuntimeByProxy = proxyIndex
	upstreamRuntimeMu.Unlock()
	return nil
}

func resolveProxyOverrideForPlaintextKey(plaintextKey string) (string, bool) {
	plaintextKey = strings.TrimSpace(plaintextKey)
	if plaintextKey == "" {
		return "", false
	}
	upstreamRuntimeMu.RLock()
	info, ok := upstreamRuntimeByKey[plaintextKey]
	upstreamRuntimeMu.RUnlock()
	if ok && info.ProxyURL != "" {
		return info.ProxyURL, true
	}
	return resolveProxyOverrideForPlaintextKeyFromStore(plaintextKey)
}

func resolveProxyOverrideForPlaintextKeyFromStore(plaintextKey string) (string, bool) {
	secret := strings.TrimSpace(utils.GetEncryptionKey())
	if secret == "" || len(secret) != 32 {
		return "", false
	}
	store, err := db.ReadStore()
	if err != nil {
		return "", false
	}
	proxyIndex := buildProxyRuntimeIndex(store.Proxies)
	for _, key := range store.APIKeys {
		plaintext, decryptErr := utils.Decrypt(key.Key, secret)
		if decryptErr != nil || plaintext != plaintextKey {
			continue
		}
		proxyInfo, ok := proxyIndex[key.ProxyID]
		if !ok || proxyInfo.URL == "" {
			return "", false
		}
		return proxyInfo.URL, true
	}
	return "", false
}

func lookupKeyRuntimeInfo(plaintextKey string) (upstreamKeyRuntimeInfo, bool) {
	plaintextKey = strings.TrimSpace(plaintextKey)
	if plaintextKey == "" {
		return upstreamKeyRuntimeInfo{}, false
	}
	upstreamRuntimeMu.RLock()
	info, ok := upstreamRuntimeByKey[plaintextKey]
	upstreamRuntimeMu.RUnlock()
	if ok {
		return info, true
	}
	return upstreamKeyRuntimeInfo{}, false
}

func buildProxyReferenceIndex(proxies []models.UpstreamProxy) map[uint]models.UpstreamProxy {
	items := make(map[uint]models.UpstreamProxy, len(proxies))
	for _, proxy := range proxies {
		proxy = models.NormalizeUpstreamProxy(proxy)
		if proxy.ID == 0 {
			continue
		}
		items[proxy.ID] = proxy
	}
	return items
}

func countAPIKeyProxyUsage(keys []models.APIKey) map[uint]int {
	counts := make(map[uint]int)
	for _, key := range keys {
		if key.ProxyID == 0 {
			continue
		}
		counts[key.ProxyID]++
	}
	return counts
}

func validateAPIKeyProxyReference(store *db.Store, proxyID uint) error {
	if proxyID == 0 {
		return nil
	}
	for _, proxy := range store.Proxies {
		if proxy.ID == proxyID {
			return nil
		}
	}
	return fmt.Errorf("所选代理不存在")
}

func newHTTPClientWithProxyOverride(cfg models.SystemConfig, overrideProxyURL *string) *http.Client {
	timeout := time.Duration(cfg.RequestTimeoutSecond) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	transport := cloneDefaultTransport()
	effectiveProxyURL := cfg.UpstreamProxyURL
	if overrideProxyURL != nil {
		effectiveProxyURL = strings.TrimSpace(*overrideProxyURL)
	}
	if proxyTransport, _, err := buildUpstreamProxyTransport(effectiveProxyURL); err == nil && proxyTransport != nil {
		transport = proxyTransport
	}
	return &http.Client{Timeout: timeout, Transport: transport}
}

func newHTTPClientForAPIKey(cfg models.SystemConfig, plaintextKey string) *http.Client {
	if proxyURL, ok := resolveProxyOverrideForPlaintextKey(plaintextKey); ok && strings.TrimSpace(proxyURL) != "" {
		return newHTTPClientWithProxyOverride(cfg, &proxyURL)
	}
	return newHTTPClient(cfg)
}

func testUpstreamProxyConnectivity(ctx context.Context, proxyCfg models.UpstreamProxy) map[string]any {
	cfg := loadSystemConfig()
	proxyURL, err := buildProxyURLFromModel(proxyCfg)
	if err != nil {
		return map[string]any{
			"success": false,
			"message": err.Error(),
		}
	}
	client := newHTTPClientWithProxyOverride(cfg, &proxyURL)
	startedAt := time.Now()
	requestURL := buildUpstreamURL(cfg, "models")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return map[string]any{"success": false, "message": err.Error()}
	}
	resp, err := client.Do(req)
	durationMs := time.Since(startedAt).Milliseconds()
	if err != nil {
		return map[string]any{
			"success":       false,
			"proxy_id":      proxyCfg.ID,
			"proxy_type":    proxyCfg.Type,
			"response_time": durationMs,
			"target":        requestURL,
			"message":       err.Error(),
		}
	}
	defer resp.Body.Close()
	success := resp.StatusCode >= 200 && resp.StatusCode < 500
	message := fmt.Sprintf("代理可达，上游返回 HTTP %d", resp.StatusCode)
	if !success {
		message = fmt.Sprintf("代理建立成功，但上游返回 HTTP %d", resp.StatusCode)
	}
	return map[string]any{
		"success":       success,
		"proxy_id":      proxyCfg.ID,
		"proxy_type":    proxyCfg.Type,
		"response_time": durationMs,
		"status_code":   resp.StatusCode,
		"target":        requestURL,
		"message":       message,
	}
}
