package scheduler

import (
	"context"
	"testing"
)

func TestLocalSchedulerUsesWeightedRoundRobin(t *testing.T) {
	sched := NewScheduler(nil)
	ctx := context.Background()

	if err := sched.AddKey(ctx, "key1", 2); err != nil {
		t.Fatalf("AddKey key1 failed: %v", err)
	}
	if err := sched.AddKey(ctx, "key2", 1); err != nil {
		t.Fatalf("AddKey key2 failed: %v", err)
	}

	first, err := sched.AcquireKey(ctx, 1)
	if err != nil {
		t.Fatalf("AcquireKey first failed: %v", err)
	}
	if first != "key1" {
		t.Fatalf("expected first key to be key1, got %s", first)
	}
	if err := sched.ReleaseKey(ctx, first); err != nil {
		t.Fatalf("ReleaseKey first failed: %v", err)
	}

	second, err := sched.AcquireKey(ctx, 1)
	if err != nil {
		t.Fatalf("AcquireKey second failed: %v", err)
	}
	if second != "key2" {
		t.Fatalf("expected second key to be key2, got %s", second)
	}
	if err := sched.ReleaseKey(ctx, second); err != nil {
		t.Fatalf("ReleaseKey second failed: %v", err)
	}

	third, err := sched.AcquireKey(ctx, 1)
	if err != nil {
		t.Fatalf("AcquireKey third failed: %v", err)
	}
	if third != "key1" {
		t.Fatalf("expected third key to be key1, got %s", third)
	}
}
