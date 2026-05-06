package scheduler

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	LuaAcquireKey = redis.NewScript(`
local active_keys = KEYS[1]
local dead_keys = KEYS[2]
local current_weights = KEYS[3]

local max_concurrency = tonumber(ARGV[1])
local lock_ttl = tonumber(ARGV[2])
local now = tonumber(ARGV[3])

local weighted = redis.call("ZRANGE", active_keys, 0, -1, "WITHSCORES")
local selected_key = ""
local selected_weight = nil
local total_weight = 0

for i = 1, #weighted, 2 do
	local key = weighted[i]
	local weight = tonumber(weighted[i + 1]) or 0
	if redis.call("SISMEMBER", dead_keys, key) == 0 then
		local cool_until = redis.call("HGET", "key_cooling", key)
		if (not cool_until) or tonumber(cool_until) < now then
			local c_key = "concurrency:" .. key
			local current = tonumber(redis.call("GET", c_key) or "0")
			if current < max_concurrency then
				local new_weight = tonumber(redis.call("HGET", current_weights, key) or "0") + weight
				redis.call("HSET", current_weights, key, new_weight)
				total_weight = total_weight + weight
				if (not selected_weight) or new_weight > selected_weight then
					selected_key = key
					selected_weight = new_weight
				end
			end
		end
	end
end

if selected_key == "" then
	return ""
end

redis.call("HINCRBYFLOAT", current_weights, selected_key, -total_weight)
local concurrency_key = "concurrency:" .. selected_key
redis.call("INCR", concurrency_key)
redis.call("EXPIRE", concurrency_key, lock_ttl)
return selected_key
		`)

	LuaReleaseKey = redis.NewScript(`
local c_key = "concurrency:" .. KEYS[1]
local current = tonumber(redis.call("GET", c_key) or "0")
if current > 0 then
	redis.call("DECR", c_key)
end
return 1
		`)
)

type Stats struct {
	Active  int `json:"active"`
	Cooling int `json:"cooling"`
	Dead    int `json:"dead"`
}

type localKey struct {
	key           string
	weight        float64
	currentWeight float64
}

type Scheduler struct {
	client      *redis.Client
	mu          sync.Mutex
	active      []localKey
	cooling     map[string]int64
	dead        map[string]struct{}
	concurrency map[string]int
}

func NewScheduler(client *redis.Client) *Scheduler {
	return &Scheduler{
		client:      client,
		cooling:     make(map[string]int64),
		dead:        make(map[string]struct{}),
		concurrency: make(map[string]int),
	}
}

func (s *Scheduler) Client() *redis.Client {
	return s.client
}

func (s *Scheduler) AddKey(ctx context.Context, key string, weight float64) error {
	if s == nil {
		return nil
	}
	if s.client == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		for i := range s.active {
			if s.active[i].key == key {
				s.active[i].weight = weight
				return nil
			}
		}
		s.active = append(s.active, localKey{key: key, weight: weight})
		return nil
	}
	return s.client.ZAdd(ctx, "nvidia:keys:active", redis.Z{Score: weight, Member: key}).Err()
}

func (s *Scheduler) AcquireKey(ctx context.Context, maxConcurrency int) (string, error) {
	if s == nil {
		return "", nil
	}
	if s.client == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		now := time.Now().Unix()
		for key, until := range s.cooling {
			if until < now {
				delete(s.cooling, key)
			}
		}

		selectedIndex := -1
		selectedWeight := 0.0
		totalWeight := 0.0
		for i := range s.active {
			item := &s.active[i]
			if _, dead := s.dead[item.key]; dead {
				continue
			}
			if until, cooling := s.cooling[item.key]; cooling && until >= now {
				continue
			}
			if s.concurrency[item.key] >= maxConcurrency {
				continue
			}
			item.currentWeight += item.weight
			totalWeight += item.weight
			if selectedIndex == -1 || item.currentWeight > selectedWeight {
				selectedIndex = i
				selectedWeight = item.currentWeight
			}
		}
		if selectedIndex == -1 {
			return "", nil
		}
		s.active[selectedIndex].currentWeight -= totalWeight
		s.concurrency[s.active[selectedIndex].key]++
		return s.active[selectedIndex].key, nil
	}
	res, err := LuaAcquireKey.Run(ctx, s.client,
		[]string{"nvidia:keys:active", "nvidia:keys:dead", "key_current_weight"},
		maxConcurrency, 60, time.Now().Unix()).Result()
	if err == redis.Nil {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	str, ok := res.(string)
	if !ok || str == "" {
		return "", nil
	}
	return str, nil
}

func (s *Scheduler) ReleaseKey(ctx context.Context, key string) error {
	if s == nil || key == "" {
		return nil
	}
	if s.client == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.concurrency[key] > 0 {
			s.concurrency[key]--
		}
		return nil
	}
	return LuaReleaseKey.Run(ctx, s.client, []string{key}).Err()
}

func (s *Scheduler) MarkCooling(ctx context.Context, key string, duration time.Duration) error {
	if s == nil {
		return nil
	}
	coolUntil := time.Now().Add(duration).Unix()
	if s.client == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.cooling[key] = coolUntil
		return nil
	}
	return s.client.HSet(ctx, "key_cooling", key, coolUntil).Err()
}

func (s *Scheduler) MarkDead(ctx context.Context, key string) error {
	if s == nil {
		return nil
	}
	if s.client == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.dead[key] = struct{}{}
		return nil
	}
	return s.client.SAdd(ctx, "nvidia:keys:dead", key).Err()
}

func (s *Scheduler) Reset(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if s.client == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.active = nil
		s.cooling = make(map[string]int64)
		s.dead = make(map[string]struct{})
		s.concurrency = make(map[string]int)
		return nil
	}
	keys, err := s.client.ZRange(ctx, "nvidia:keys:active", 0, -1).Result()
	if err != nil {
		if isRedisUnavailable(err) {
			return nil
		}
		return err
	}
	redisKeys := make([]string, 0, len(keys)+4)
	redisKeys = append(redisKeys, "nvidia:keys:active", "nvidia:keys:dead", "key_cooling", "key_current_weight")
	for _, key := range keys {
		redisKeys = append(redisKeys, "concurrency:"+key)
	}
	if len(redisKeys) > 0 {
		if err := s.client.Del(ctx, redisKeys...).Err(); err != nil {
			if isRedisUnavailable(err) {
				return nil
			}
			return err
		}
	}
	return nil
}

func (s *Scheduler) Stats(ctx context.Context) (*Stats, error) {
	if s == nil {
		return &Stats{}, nil
	}
	if s.client == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		now := time.Now().Unix()
		cooling := 0
		for key, until := range s.cooling {
			if until >= now {
				cooling++
				continue
			}
			delete(s.cooling, key)
		}
		return &Stats{Active: len(s.active), Cooling: cooling, Dead: len(s.dead)}, nil
	}
	active, err := s.client.ZCard(ctx, "nvidia:keys:active").Result()
	if err != nil {
		if isRedisUnavailable(err) {
			return &Stats{}, nil
		}
		return nil, err
	}
	dead, err := s.client.SCard(ctx, "nvidia:keys:dead").Result()
	if err != nil {
		if isRedisUnavailable(err) {
			return &Stats{}, nil
		}
		return nil, err
	}
	coolingEntries, err := s.client.HGetAll(ctx, "key_cooling").Result()
	if err != nil {
		if isRedisUnavailable(err) {
			return &Stats{}, nil
		}
		return nil, err
	}
	now := time.Now().Unix()
	cooling := 0
	for key, until := range coolingEntries {
		unixTs, convErr := strconv.ParseInt(until, 10, 64)
		if convErr != nil {
			continue
		}
		if unixTs >= now {
			cooling++
			continue
		}
		s.client.HDel(ctx, "key_cooling", key)
	}
	return &Stats{Active: int(active), Cooling: cooling, Dead: int(dead)}, nil
}

func isRedisUnavailable(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "connectex") ||
		strings.Contains(message, "connection refused") ||
		strings.Contains(message, "failed to dial") ||
		strings.Contains(message, "no such host") ||
		strings.Contains(message, "i/o timeout")
}
