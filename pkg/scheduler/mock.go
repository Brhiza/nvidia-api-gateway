package scheduler

import (
	"sync"

	"github.com/redis/go-redis/v9"
)

// Mock Redis client for testing when real Redis is unavailable
type MockRedisClient struct {
	*redis.Client
	mu    sync.Mutex
	data  map[string]string
	sets  map[string]map[string]bool
	zsets map[string]map[string]float64
}

func NewMockRedisClient() *MockRedisClient {
	return &MockRedisClient{
		data:  make(map[string]string),
		sets:  make(map[string]map[string]bool),
		zsets: make(map[string]map[string]float64),
	}
}
