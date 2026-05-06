package gateway

import (
	"encoding/base64"
	"testing"
)

func TestUpstreamDiagnosticsHeadersIncludeBase64Companions(t *testing.T) {
	d := newUpstreamAttemptDiagnostics("chat.nonstream")
	d.SelectedNames = []string{"中文-Key"}
	d.AttemptCount = 2
	d.LastRetryCause = "上游失败"

	headers := d.headers()
	if headers["X-Gateway-Upstream-Key-Name-B64"] == "" {
		t.Fatal("expected encoded key name header")
	}
	decodedKey, err := base64.RawURLEncoding.DecodeString(headers["X-Gateway-Upstream-Key-Name-B64"])
	if err != nil {
		t.Fatalf("decode key name: %v", err)
	}
	if string(decodedKey) != "中文-Key" {
		t.Fatalf("unexpected decoded key name: %q", string(decodedKey))
	}
	decodedErr, err := base64.RawURLEncoding.DecodeString(headers["X-Gateway-Upstream-Last-Error-B64"])
	if err != nil {
		t.Fatalf("decode last error: %v", err)
	}
	if string(decodedErr) != d.LastRetryCause {
		t.Fatalf("unexpected decoded last error: %q", string(decodedErr))
	}
}
