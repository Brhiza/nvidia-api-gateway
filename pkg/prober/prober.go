package prober

import (
	"context"
	"time"

	"nvidia-api-gateway/pkg/scheduler"

	"github.com/redis/go-redis/v9"
)

type Prober struct {
	redis     *redis.Client
	scheduler *scheduler.Scheduler
}

func NewProber(redisClient *redis.Client, sched *scheduler.Scheduler) *Prober {
	return &Prober{
		redis:     redisClient,
		scheduler: sched,
	}
}

func (p *Prober) Start(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.probeRecoverableKeys(ctx)
		}
	}
}
