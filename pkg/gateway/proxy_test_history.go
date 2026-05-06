package gateway

import (
	"fmt"
	"time"

	"nvidia-api-gateway/pkg/db"
	"nvidia-api-gateway/pkg/models"
)

const maxProxyTestHistory = 10

func buildProxyTestRecordFromResult(result map[string]any) models.ProxyTestRecord {
	record := models.ProxyTestRecord{TestedAt: time.Now()}
	if value, ok := result["success"].(bool); ok {
		record.Success = value
	}
	record.StatusCode = anyToInt(result["status_code"])
	record.ResponseTime = anyToInt64(result["response_time"])
	record.Message, _ = result["message"].(string)
	record.Target, _ = result["target"].(string)
	return record
}

func mergeProxyTestHistory(existing []models.ProxyTestRecord, record models.ProxyTestRecord, limit int) []models.ProxyTestRecord {
	if limit <= 0 {
		limit = maxProxyTestHistory
	}
	merged := make([]models.ProxyTestRecord, 0, minInt(limit, len(existing)+1))
	merged = append(merged, record)
	for _, item := range existing {
		if len(merged) >= limit {
			break
		}
		merged = append(merged, item)
	}
	return merged
}

func recordProxyTestResult(proxyID uint, record models.ProxyTestRecord) error {
	if proxyID == 0 {
		return nil
	}
	return db.UpdateStore(func(store *db.Store) error {
		for i := range store.Proxies {
			if store.Proxies[i].ID != proxyID {
				continue
			}
			copyRecord := record
			store.Proxies[i].LastTest = &copyRecord
			store.Proxies[i].TestHistory = mergeProxyTestHistory(store.Proxies[i].TestHistory, record, maxProxyTestHistory)
			return nil
		}
		return errProxyNotFound
	})
}

func anyToInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	default:
		return 0
	}
}

func anyToInt64(value any) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case float32:
		return int64(v)
	default:
		return 0
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func formatProxyHistoryLabel(record models.ProxyTestRecord) string {
	status := "失败"
	if record.Success {
		status = "成功"
	}
	if record.StatusCode > 0 {
		return fmt.Sprintf("%s · HTTP %d", status, record.StatusCode)
	}
	return status
}
