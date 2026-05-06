package prober

import (
	"context"
	"log"

	"nvidia-api-gateway/pkg/gateway"
)

func (p *Prober) probeRecoverableKeys(ctx context.Context) {
	if err := gateway.RestoreRecoverableStatuses(ctx, p.scheduler); err != nil {
		log.Printf("Prober: failed to restore key statuses: %v", err)
	}
}
