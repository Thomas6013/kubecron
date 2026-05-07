package storage

import (
	"context"
	"log/slog"
	"time"
)

// StartRetention runs a background goroutine that deletes data older than
// retentionDays every hour. It stops when ctx is cancelled.
func StartRetention(ctx context.Context, store Store, retentionDays int) {
	ticker := time.NewTicker(time.Hour)
	go func() {
		defer ticker.Stop()

		// Run once immediately at startup so stale data is pruned without
		// waiting for the first tick.
		runRetention(ctx, store, retentionDays)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runRetention(ctx, store, retentionDays)
			}
		}
	}()
}

func runRetention(ctx context.Context, store Store, retentionDays int) {
	before := time.Now().AddDate(0, 0, -retentionDays)
	if err := store.DeleteOldData(ctx, before); err != nil {
		slog.ErrorContext(ctx, "retention: failed to delete old data",
			"error", err,
			"before", before,
		)
	} else {
		slog.DebugContext(ctx, "retention: deleted old data",
			"before", before,
			"retention_days", retentionDays,
		)
	}
}
