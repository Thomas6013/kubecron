package storage

import (
	"context"
	"log/slog"
	"time"
)

// StartRetention runs a background goroutine that:
//   - deletes log lines for runs older than logRetentionDays (keeps run metadata)
//   - deletes full run records (+ cascading log lines) older than retentionDays
//
// Both operations run hourly. It stops when ctx is cancelled.
func StartRetention(ctx context.Context, store Store, retentionDays, logRetentionDays int) {
	ticker := time.NewTicker(time.Hour)
	go func() {
		defer ticker.Stop()

		runRetention(ctx, store, retentionDays, logRetentionDays)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runRetention(ctx, store, retentionDays, logRetentionDays)
			}
		}
	}()
}

func runRetention(ctx context.Context, store Store, retentionDays, logRetentionDays int) {
	logBefore := time.Now().AddDate(0, 0, -logRetentionDays)
	if err := store.DeleteOldLogLines(ctx, logBefore); err != nil {
		slog.ErrorContext(ctx, "retention: failed to delete old log lines",
			"error", err, "before", logBefore)
	} else {
		slog.DebugContext(ctx, "retention: deleted old log lines",
			"before", logBefore, "log_retention_days", logRetentionDays)
	}

	before := time.Now().AddDate(0, 0, -retentionDays)
	if err := store.DeleteOldData(ctx, before); err != nil {
		slog.ErrorContext(ctx, "retention: failed to delete old data",
			"error", err, "before", before)
	} else {
		slog.DebugContext(ctx, "retention: deleted old data",
			"before", before, "retention_days", retentionDays)
	}

	// Soft-deleted CronJobs are kept as long as they still have runs, so their
	// history stays readable; once DeleteOldData has aged those runs out, the
	// row itself can go.
	if err := store.PurgeDeletedCronJobs(ctx, before); err != nil {
		slog.ErrorContext(ctx, "retention: failed to purge deleted cronjobs",
			"error", err, "before", before)
	} else {
		slog.DebugContext(ctx, "retention: purged deleted cronjobs", "before", before)
	}
}
