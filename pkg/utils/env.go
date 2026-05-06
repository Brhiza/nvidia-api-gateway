package utils

import (
	"os"
	"strings"
)

const DefaultBackendPort = "18080"

func GetEncryptionKey() string {
	return strings.TrimSpace(os.Getenv("ENCRYPTION_KEY"))
}

func ResolveBackendPort() string {
	port := strings.TrimSpace(os.Getenv("BACKEND_PORT"))
	if port == "" {
		port = strings.TrimSpace(os.Getenv("PORT"))
	}
	if port == "" {
		port = DefaultBackendPort
	}
	return port
}

func ResolvePublicGatewayBaseURL() string {
	if value := strings.TrimSpace(os.Getenv("PUBLIC_GATEWAY_BASE_URL")); value != "" {
		return strings.TrimRight(value, "/")
	}
	return "http://127.0.0.1:" + ResolveBackendPort()
}
