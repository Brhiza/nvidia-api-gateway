package gateway

import (
	"sync"
	"time"
)

type storedGatewayResponse struct {
	payload   []byte
	expiresAt time.Time
}

type gatewayResponseStore struct {
	mu    sync.Mutex
	items map[string]storedGatewayResponse
	ttl   time.Duration
}

func newGatewayResponseStore(ttl time.Duration) *gatewayResponseStore {
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	return &gatewayResponseStore{
		items: make(map[string]storedGatewayResponse),
		ttl:   ttl,
	}
}

func (s *gatewayResponseStore) put(id string, payload []byte) {
	if s == nil || id == "" || len(payload) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(time.Now())
	s.items[id] = storedGatewayResponse{
		payload:   append([]byte(nil), payload...),
		expiresAt: time.Now().Add(s.ttl),
	}
}

func (s *gatewayResponseStore) get(id string) ([]byte, bool) {
	if s == nil || id == "" {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.cleanupLocked(now)
	item, ok := s.items[id]
	if !ok || item.expiresAt.Before(now) {
		if ok {
			delete(s.items, id)
		}
		return nil, false
	}
	return append([]byte(nil), item.payload...), true
}

func (s *gatewayResponseStore) cleanupLocked(now time.Time) {
	for id, item := range s.items {
		if item.expiresAt.Before(now) {
			delete(s.items, id)
		}
	}
}

var responsesStore = newGatewayResponseStore(30 * time.Minute)
