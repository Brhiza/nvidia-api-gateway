package gateway

import (
	"encoding/base64"
	"strconv"
	"strings"
)

type upstreamAttemptDiagnostics struct {
	Operation        string
	AttemptCount     int
	SelectedNames    []string
	LastSelectedName string
	Switched         bool
	LastRetryCause   string
}

func newUpstreamAttemptDiagnostics(operation string) *upstreamAttemptDiagnostics {
	return &upstreamAttemptDiagnostics{Operation: strings.TrimSpace(operation)}
}

func (d *upstreamAttemptDiagnostics) noteSelectedKey(plaintextKey string) {
	if d == nil {
		return
	}
	d.AttemptCount++
	name := lookupUpstreamKeyNameByPlaintext(plaintextKey)
	if name == "" {
		name = "(unknown)"
	}
	d.SelectedNames = append(d.SelectedNames, name)
	if d.LastSelectedName != "" && d.LastSelectedName != name {
		d.Switched = true
	}
	d.LastSelectedName = name
}

func (d *upstreamAttemptDiagnostics) noteRetry(cause string) {
	if d == nil {
		return
	}
	d.LastRetryCause = strings.TrimSpace(cause)
}

func encodeDebugHeaderValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func (d *upstreamAttemptDiagnostics) headers() map[string]string {
	if d == nil {
		return nil
	}
	selected := ""
	chain := ""
	if len(d.SelectedNames) > 0 {
		selected = d.SelectedNames[len(d.SelectedNames)-1]
		chain = strings.Join(d.SelectedNames, " -> ")
	}
	return map[string]string{
		"X-Gateway-Upstream-Operation":      d.Operation,
		"X-Gateway-Upstream-Key-Name":       selected,
		"X-Gateway-Upstream-Key-Name-B64":   encodeDebugHeaderValue(selected),
		"X-Gateway-Upstream-Key-Chain":      chain,
		"X-Gateway-Upstream-Key-Chain-B64":  encodeDebugHeaderValue(chain),
		"X-Gateway-Upstream-Attempts":       strconv.Itoa(d.AttemptCount),
		"X-Gateway-Upstream-Switched":       strconv.FormatBool(d.Switched),
		"X-Gateway-Upstream-Last-Error":     d.LastRetryCause,
		"X-Gateway-Upstream-Last-Error-B64": encodeDebugHeaderValue(d.LastRetryCause),
	}
}

func applyProxyHeaders(result *proxyResult, headers map[string]string) {
	if result == nil || len(headers) == 0 {
		return
	}
	if result.Headers == nil {
		result.Headers = map[string]string{}
	}
	for key, value := range headers {
		if strings.TrimSpace(value) == "" {
			continue
		}
		result.Headers[key] = value
	}
}

func applyResponseHeaders(headersSink interface{ Set(string, string) }, headers map[string]string) {
	if headersSink == nil || len(headers) == 0 {
		return
	}
	for key, value := range headers {
		if strings.TrimSpace(value) == "" {
			continue
		}
		headersSink.Set(key, value)
	}
}
