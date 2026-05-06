package middleware

import (
	"context"
	"fmt"
	"time"

	"nvidia-api-gateway/pkg/models"

	"github.com/redis/go-redis/v9"
)

type UsageTracker struct {
	client *redis.Client
}

func NewUsageTracker(client *redis.Client) *UsageTracker {
	return &UsageTracker{client: client}
}

func (t *UsageTracker) Check(ctx context.Context, masterKey *models.MasterKey, tokenCost int) error {
	if masterKey == nil {
		return nil
	}
	if t == nil || t.client == nil {
		if masterKey.Quota != -1 && masterKey.UsedQuota+int64(tokenCost) > masterKey.Quota {
			return fmt.Errorf("Quota exceeded")
		}
		return nil
	}
	if masterKey.RPM > 0 {
		count, err := t.client.Get(ctx, masterRPMKey(masterKey.ID)).Int()
		if err != nil && err != redis.Nil {
			return err
		}
		if count >= masterKey.RPM {
			return fmt.Errorf("RPM exceeded")
		}
	}
	if masterKey.TPM > 0 {
		used, err := t.client.Get(ctx, masterTPMKey(masterKey.ID)).Int()
		if err != nil && err != redis.Nil {
			return err
		}
		if used+tokenCost > masterKey.TPM {
			return fmt.Errorf("TPM exceeded")
		}
	}
	if masterKey.Quota != -1 && masterKey.UsedQuota+int64(tokenCost) > masterKey.Quota {
		return fmt.Errorf("Quota exceeded")
	}
	return nil
}

func (t *UsageTracker) Record(ctx context.Context, masterKey *models.MasterKey, tokenCost int) error {
	if masterKey == nil || t == nil || t.client == nil {
		return nil
	}

	minuteTTL := ttlUntilNextMinute()
	if err := t.client.Incr(ctx, masterRPMKey(masterKey.ID)).Err(); err != nil {
		return err
	}
	if err := t.client.Expire(ctx, masterRPMKey(masterKey.ID), minuteTTL).Err(); err != nil {
		return err
	}
	if err := t.client.IncrBy(ctx, masterTPMKey(masterKey.ID), int64(tokenCost)).Err(); err != nil {
		return err
	}
	if err := t.client.Expire(ctx, masterTPMKey(masterKey.ID), minuteTTL).Err(); err != nil {
		return err
	}
	return nil
}

func masterRPMKey(id uint) string {
	return fmt.Sprintf("master:%d:rpm", id)
}

func masterTPMKey(id uint) string {
	return fmt.Sprintf("master:%d:tpm", id)
}

func ttlUntilNextMinute() time.Duration {
	now := time.Now()
	next := now.Truncate(time.Minute).Add(time.Minute)
	return time.Until(next)
}
