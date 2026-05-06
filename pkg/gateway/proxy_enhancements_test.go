package gateway

import (
	"testing"
	"time"

	"nvidia-api-gateway/pkg/db"
	"nvidia-api-gateway/pkg/models"
)

func TestMergeProxyTestHistoryKeepsNewestAndCaps(t *testing.T) {
	existing := []models.ProxyTestRecord{
		{StatusCode: 201, TestedAt: time.Unix(2, 0)},
		{StatusCode: 202, TestedAt: time.Unix(1, 0)},
	}
	record := models.ProxyTestRecord{StatusCode: 200, TestedAt: time.Unix(3, 0)}
	merged := mergeProxyTestHistory(existing, record, 2)
	if len(merged) != 2 {
		t.Fatalf("len = %d, want 2", len(merged))
	}
	if merged[0].StatusCode != 200 || merged[1].StatusCode != 201 {
		t.Fatalf("unexpected order: %#v", merged)
	}
}

func TestApplyBulkAPIKeyProxyBindingToStore(t *testing.T) {
	store := &db.Store{
		APIKeys: []models.APIKey{{ID: 1, Name: "A"}, {ID: 2, Name: "B"}},
		Proxies: []models.UpstreamProxy{{ID: 7, Name: "SG", Type: "http", Status: models.ProxyStatusEnabled, Host: "1.1.1.1", Port: 7890}},
	}
	updated, missing, proxyName, proxyGroup, err := applyBulkAPIKeyProxyBindingToStore(store, []uint{2, 1, 9, 1}, 7, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated != 2 {
		t.Fatalf("updated = %d, want 2", updated)
	}
	if proxyName != "SG" {
		t.Fatalf("proxyName = %q, want SG", proxyName)
	}
	if proxyGroup != "" {
		t.Fatalf("proxyGroup = %q, want empty", proxyGroup)
	}
	if len(missing) != 1 || missing[0] != 9 {
		t.Fatalf("missing = %#v, want [9]", missing)
	}
	if store.APIKeys[0].ProxyID != 7 || store.APIKeys[1].ProxyID != 7 {
		t.Fatalf("proxy binding not applied: %#v", store.APIKeys)
	}
}

func TestApplyBulkAPIKeyProxyBindingToStoreByGroupRoundRobin(t *testing.T) {
	store := &db.Store{
		APIKeys: []models.APIKey{{ID: 1, Name: "A"}, {ID: 2, Name: "B"}, {ID: 3, Name: "C"}},
		Proxies: []models.UpstreamProxy{
			{ID: 11, Name: "SG-1", Group: "sg", Type: "http", Status: models.ProxyStatusEnabled, Host: "1.1.1.1", Port: 7890, LastTest: &models.ProxyTestRecord{Success: true, ResponseTime: 120, TestedAt: time.Unix(10, 0)}},
			{ID: 12, Name: "SG-2", Group: "sg", Type: "http", Status: models.ProxyStatusEnabled, Host: "1.1.1.2", Port: 7890, LastTest: &models.ProxyTestRecord{Success: true, ResponseTime: 80, TestedAt: time.Unix(11, 0)}},
			{ID: 13, Name: "HK-1", Group: "hk", Type: "http", Status: models.ProxyStatusEnabled, Host: "2.2.2.2", Port: 7890},
		},
	}
	updated, missing, proxyName, proxyGroup, err := applyBulkAPIKeyProxyBindingToStore(store, []uint{1, 2, 3}, 0, "sg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated != 3 {
		t.Fatalf("updated = %d, want 3", updated)
	}
	if proxyName != "" {
		t.Fatalf("proxyName = %q, want empty", proxyName)
	}
	if proxyGroup != "sg" {
		t.Fatalf("proxyGroup = %q, want sg", proxyGroup)
	}
	if len(missing) != 0 {
		t.Fatalf("missing = %#v, want none", missing)
	}
	if store.APIKeys[0].ProxyID != 12 || store.APIKeys[1].ProxyID != 12 || store.APIKeys[2].ProxyID != 12 {
		t.Fatalf("unexpected group health-preferred assignment: %#v", store.APIKeys)
	}
}

func TestSortedProxyGroupMembersSkipsDisabledProxy(t *testing.T) {
	items := sortedProxyGroupMembers([]models.UpstreamProxy{
		{ID: 1, Name: "SG-1", Group: "sg", Type: "http", Status: models.ProxyStatusDisabled, Host: "1.1.1.1", Port: 7890},
		{ID: 2, Name: "SG-2", Group: "sg", Type: "http", Status: models.ProxyStatusEnabled, Host: "1.1.1.2", Port: 7890},
	}, "sg")
	if len(items) != 1 || items[0].ID != 2 {
		t.Fatalf("unexpected enabled group members: %#v", items)
	}
}
