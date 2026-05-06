package gateway

import (
	"context"
	"fmt"

	"nvidia-api-gateway/pkg/scheduler"
)

func RestoreRecoverableStatuses(ctx context.Context, sched *scheduler.Scheduler) error {
	if err := restoreAPIKeyStatuses(ctx); err != nil {
		return fmt.Errorf("restore database statuses: %w", err)
	}
	if err := LoadActiveKeys(ctx, sched); err != nil {
		return fmt.Errorf("reload scheduler state: %w", err)
	}
	return nil
}
