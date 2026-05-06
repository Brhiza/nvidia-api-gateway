package gateway

import (
	"net/url"
	"path"
	"strings"

	"nvidia-api-gateway/pkg/db"
	"nvidia-api-gateway/pkg/models"
	"nvidia-api-gateway/pkg/utils"
)

func loadSystemConfig() models.SystemConfig {
	store, err := db.ReadStore()
	if err != nil {
		return models.DefaultSystemConfig()
	}
	return models.NormalizeSystemConfig(store.SystemConfig)
}

func buildUpstreamURL(cfg models.SystemConfig, endpointPath string) string {
	base := strings.TrimSpace(cfg.UpstreamBaseURL)
	if base == "" {
		base = models.DefaultUpstreamBaseURL
	}
	u, err := url.Parse(base)
	if err != nil {
		return models.DefaultUpstreamBaseURL + endpointPath
	}
	u.Path = path.Join(u.Path, endpointPath)
	if !strings.HasPrefix(u.Path, "/") {
		u.Path = "/" + u.Path
	}
	return u.String()
}

func protocolEnabled(cfg models.SystemConfig, protocol string) bool {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "openai":
		return cfg.EnableOpenAI
	case "claude":
		return cfg.EnableClaude
	case "gemini":
		return cfg.EnableGemini
	default:
		return false
	}
}

func gatewayBaseURL() string {
	return utils.ResolvePublicGatewayBaseURL()
}
