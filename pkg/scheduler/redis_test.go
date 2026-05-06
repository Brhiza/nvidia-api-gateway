package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestScheduler(t *testing.T) {
	// Skip if Redis is not running locally.
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Skip("Redis not running, skipping test")
	}
	defer client.FlushAll(context.Background())

	sched := NewScheduler(client)
	ctx := context.Background()

	// Add 2 keys
	err := sched.AddKey(ctx, "key1", 10.0)
	if err != nil {
		t.Fatalf("AddKey failed: %v", err)
	}
	err = sched.AddKey(ctx, "key2", 5.0)
	if err != nil {
		t.Fatalf("AddKey failed: %v", err)
	}

	// Acquire key, should get key1 (highest weight)
	acquired, err := sched.AcquireKey(ctx, 1)
	if err != nil {
		t.Fatalf("AcquireKey failed: %v", err)
	}
	if acquired != "key1" {
		t.Errorf("Expected key1, got %v", acquired)
	}

	// Acquire again with maxConcurrency 1, should get key2 since key1 is full
	acquired2, err := sched.AcquireKey(ctx, 1)
	if err != nil {
		t.Fatalf("AcquireKey failed: %v", err)
	}
	if acquired2 != "key2" {
		t.Errorf("Expected key2, got %v", acquired2)
	}

	// Release key1
	err = sched.ReleaseKey(ctx, "key1")
	if err != nil {
		t.Fatalf("ReleaseKey failed: %v", err)
	}

	// Mark key1 cooling
	err = sched.MarkCooling(ctx, "key1", 10*time.Second)
	if err != nil {
		t.Fatalf("MarkCooling failed: %v", err)
	}

	// Acquire again, should get key2 because key1 is cooling and key2 is full (but we increase maxConcurrency to 2)
	sched.ReleaseKey(ctx, "key2") // Reset key2 concurrency to 0
	acquired3, err := sched.AcquireKey(ctx, 2)
	if err != nil {
		t.Fatalf("AcquireKey failed: %v", err)
	}
	if acquired3 != "key2" {
		t.Errorf("Expected key2, got %v", acquired3)
	}
}
