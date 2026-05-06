package gateway

import (
	"sort"
	"strings"
	"time"

	"nvidia-api-gateway/pkg/models"
)

func normalizeProxyGroup(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func isProxyEnabled(proxy models.UpstreamProxy) bool {
	proxy = models.NormalizeUpstreamProxy(proxy)
	return proxy.Status != models.ProxyStatusDisabled
}

func compareProxyHealth(a, b models.UpstreamProxy) int {
	aRank := proxyHealthRank(a)
	bRank := proxyHealthRank(b)
	if aRank != bRank {
		if aRank < bRank {
			return -1
		}
		return 1
	}
	aLatency := proxyLatencyValue(a)
	bLatency := proxyLatencyValue(b)
	if aLatency != bLatency {
		if aLatency < bLatency {
			return -1
		}
		return 1
	}
	aTime := proxyLastTestUnix(a)
	bTime := proxyLastTestUnix(b)
	if aTime != bTime {
		if aTime > bTime {
			return -1
		}
		return 1
	}
	aBound := countRecentSuccesses(a)
	bBound := countRecentSuccesses(b)
	if aBound != bBound {
		if aBound > bBound {
			return -1
		}
		return 1
	}
	if normalizeProxyGroup(a.Group) != normalizeProxyGroup(b.Group) {
		return strings.Compare(normalizeProxyGroup(a.Group), normalizeProxyGroup(b.Group))
	}
	return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
}

func proxyHealthRank(proxy models.UpstreamProxy) int {
	if !isProxyEnabled(proxy) {
		return 3
	}
	if proxy.LastTest == nil {
		return 2
	}
	if proxy.LastTest.Success {
		return 0
	}
	return 1
}

func proxyLatencyValue(proxy models.UpstreamProxy) int64 {
	if proxy.LastTest == nil || proxy.LastTest.ResponseTime <= 0 {
		return 1<<62 - 1
	}
	return proxy.LastTest.ResponseTime
}

func proxyLastTestUnix(proxy models.UpstreamProxy) int64 {
	if proxy.LastTest == nil {
		return 0
	}
	return proxy.LastTest.TestedAt.Unix()
}

func countRecentSuccesses(proxy models.UpstreamProxy) int {
	count := 0
	for _, item := range proxy.TestHistory {
		if item.Success {
			count++
		}
	}
	return count
}

func sortedProxyGroupMembers(proxies []models.UpstreamProxy, group string) []models.UpstreamProxy {
	targetGroup := normalizeProxyGroup(group)
	items := make([]models.UpstreamProxy, 0)
	for _, proxy := range proxies {
		proxy = models.NormalizeUpstreamProxy(proxy)
		if normalizeProxyGroup(proxy.Group) != targetGroup {
			continue
		}
		if !isProxyEnabled(proxy) {
			continue
		}
		items = append(items, proxy)
	}
	sort.Slice(items, func(i, j int) bool {
		return compareProxyHealth(items[i], items[j]) < 0
	})
	return items
}

func proxyGroupNames(proxies []models.UpstreamProxy) []string {
	seen := map[string]string{}
	for _, proxy := range proxies {
		group := strings.TrimSpace(proxy.Group)
		if group == "" {
			continue
		}
		key := normalizeProxyGroup(group)
		if _, ok := seen[key]; !ok {
			seen[key] = group
		}
	}
	items := make([]string, 0, len(seen))
	for _, value := range seen {
		items = append(items, value)
	}
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i]) < strings.ToLower(items[j])
	})
	return items
}

func healthPreferredAssignProxyIDs(keyIDs []uint, proxies []models.UpstreamProxy) map[uint]uint {
	assigned := make(map[uint]uint, len(keyIDs))
	if len(proxies) == 0 {
		return assigned
	}
	weighted := make([]uint, 0, len(proxies)*3)
	for _, proxy := range proxies {
		weight := proxyPreferenceWeight(proxy)
		for i := 0; i < weight; i++ {
			weighted = append(weighted, proxy.ID)
		}
	}
	if len(weighted) == 0 {
		return assigned
	}
	for idx, keyID := range keyIDs {
		assigned[keyID] = weighted[idx%len(weighted)]
	}
	return assigned
}

func proxyPreferenceWeight(proxy models.UpstreamProxy) int {
	if !isProxyEnabled(proxy) {
		return 0
	}
	if proxy.LastTest == nil {
		return 1
	}
	if !proxy.LastTest.Success {
		return 1
	}
	switch {
	case proxy.LastTest.ResponseTime > 0 && proxy.LastTest.ResponseTime <= 100:
		return 4
	case proxy.LastTest.ResponseTime > 0 && proxy.LastTest.ResponseTime <= 300:
		return 3
	default:
		return 2
	}
}

func proxyTestRecency(proxy models.UpstreamProxy) time.Time {
	if proxy.LastTest == nil {
		return time.Time{}
	}
	return proxy.LastTest.TestedAt
}
