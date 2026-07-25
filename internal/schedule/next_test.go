package schedule_test

import (
	"testing"
	"time"
	_ "time/tzdata" // named zones must resolve without host zoneinfo

	"github.com/kubecron/kubecron/internal/schedule"
)

// base is a fixed Monday at 10:00 UTC used across all test cases.
var base = time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

func TestNextRun(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		tz      string
		after   time.Time
		want    time.Time
		wantErr bool
	}{
		{
			name:  "every minute",
			expr:  "* * * * *",
			after: base,
			want:  time.Date(2024, 1, 15, 10, 1, 0, 0, time.UTC),
		},
		{
			name:  "every hour on the hour",
			expr:  "0 * * * *",
			after: base,
			want:  time.Date(2024, 1, 15, 11, 0, 0, 0, time.UTC),
		},
		{
			name:  "daily at midnight",
			expr:  "0 0 * * *",
			after: base,
			want:  time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "every 5 minutes",
			expr:  "*/5 * * * *",
			after: base,
			want:  time.Date(2024, 1, 15, 10, 5, 0, 0, time.UTC),
		},
		{
			name:  "exact on boundary returns next occurrence",
			expr:  "0 10 * * *",
			after: base, // exactly 10:00 — next should be tomorrow
			want:  time.Date(2024, 1, 16, 10, 0, 0, 0, time.UTC),
		},
		{
			name:    "invalid expression",
			expr:    "not-a-cron",
			after:   base,
			wantErr: true,
		},

		// ── spec.timeZone (DOM-1) ────────────────────────────────────────────
		{
			// No zone: the expression resolves in the zone of `after`, which is
			// the 04:00 UTC tick. This is the answer KubeCron used to give for
			// every CronJob, including those declaring a zone — five hours off
			// for this one.
			name:  "no zone resolves in the caller's zone",
			expr:  "0 4 * * *",
			after: base,
			want:  time.Date(2024, 1, 16, 4, 0, 0, 0, time.UTC),
		},
		{
			// Winter: New York is UTC-5, so 04:00 local is 09:00 UTC.
			name:  "named zone in winter (EST, UTC-5)",
			expr:  "0 4 * * *",
			tz:    "America/New_York",
			after: base,
			want:  time.Date(2024, 1, 16, 9, 0, 0, 0, time.UTC),
		},
		{
			// Summer: DST puts New York at UTC-4, so the same expression fires
			// an hour earlier in UTC. A fixed offset would get this wrong.
			name:  "named zone in summer honours DST (EDT, UTC-4)",
			expr:  "0 4 * * *",
			tz:    "America/New_York",
			after: time.Date(2024, 7, 15, 10, 0, 0, 0, time.UTC),
			want:  time.Date(2024, 7, 16, 8, 0, 0, 0, time.UTC),
		},
		{
			name:  "zone east of UTC",
			expr:  "30 9 * * *",
			tz:    "Asia/Tokyo",
			after: base,
			want:  time.Date(2024, 1, 16, 0, 30, 0, 0, time.UTC), // 09:30 JST = 00:30 UTC
		},
		{
			name:    "unknown zone is an error, not a silent fallback",
			expr:    "0 4 * * *",
			tz:      "Mars/Olympus_Mons",
			after:   base,
			wantErr: true,
		},
		{
			// Interval schedules carry no wall-clock, so a zone is accepted and
			// simply has no effect.
			name:  "interval schedule ignores the zone",
			expr:  "@every 1h",
			tz:    "America/New_York",
			after: base,
			want:  time.Date(2024, 1, 15, 11, 0, 0, 0, time.UTC),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := schedule.NextRun(tc.expr, tc.tz, tc.after)
			if tc.wantErr {
				if err == nil {
					t.Errorf("NextRun(%q, %q) expected error, got %v", tc.expr, tc.tz, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NextRun(%q, %q) unexpected error: %v", tc.expr, tc.tz, err)
			}
			if !got.Equal(tc.want) {
				t.Errorf("NextRun(%q, %q) = %v, want %v", tc.expr, tc.tz, got.UTC(), tc.want)
			}
		})
	}
}

func TestNextRuns(t *testing.T) {
	runs, err := schedule.NextRuns("*/5 * * * *", "", base, 3)
	if err != nil {
		t.Fatalf("NextRuns: %v", err)
	}
	if len(runs) != 3 {
		t.Fatalf("NextRuns: got %d runs, want 3", len(runs))
	}
	want := []time.Time{
		time.Date(2024, 1, 15, 10, 5, 0, 0, time.UTC),
		time.Date(2024, 1, 15, 10, 10, 0, 0, time.UTC),
		time.Date(2024, 1, 15, 10, 15, 0, 0, time.UTC),
	}
	for i, w := range want {
		if !runs[i].Equal(w) {
			t.Errorf("NextRuns[%d] = %v, want %v", i, runs[i], w)
		}
	}
}

func TestNextRunsAcrossDSTTransition(t *testing.T) {
	// US spring-forward is 2024-03-10 02:00 EST → 03:00 EDT. A daily 04:00 job
	// keeps firing at 04:00 wall-clock, so its UTC instant shifts by exactly one
	// hour on that day and stays there.
	after := time.Date(2024, 3, 8, 10, 0, 0, 0, time.UTC) // 05:00 EST
	runs, err := schedule.NextRuns("0 4 * * *", "America/New_York", after, 3)
	if err != nil {
		t.Fatalf("NextRuns: %v", err)
	}
	want := []time.Time{
		time.Date(2024, 3, 9, 9, 0, 0, 0, time.UTC),  // 04:00 EST
		time.Date(2024, 3, 10, 8, 0, 0, 0, time.UTC), // 04:00 EDT — one hour earlier in UTC
		time.Date(2024, 3, 11, 8, 0, 0, 0, time.UTC), // 04:00 EDT
	}
	for i, w := range want {
		if !runs[i].Equal(w) {
			t.Errorf("NextRuns[%d] = %v, want %v", i, runs[i].UTC(), w)
		}
	}
}

func TestPrevRun(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		tz      string
		now     time.Time
		want    time.Time
		wantErr bool
	}{
		{
			name: "7 minutes past — prev is 5m mark",
			expr: "*/5 * * * *",
			now:  time.Date(2024, 1, 15, 10, 7, 0, 0, time.UTC),
			want: time.Date(2024, 1, 15, 10, 5, 0, 0, time.UTC),
		},
		{
			name: "exactly on boundary — returns that boundary",
			expr: "*/5 * * * *",
			now:  time.Date(2024, 1, 15, 10, 5, 0, 0, time.UTC),
			want: time.Date(2024, 1, 15, 10, 5, 0, 0, time.UTC),
		},
		{
			name: "hourly — just past the hour",
			expr: "0 * * * *",
			now:  time.Date(2024, 1, 15, 10, 3, 0, 0, time.UTC),
			want: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
		},
		{
			// Missed-run detection compares the last run against this tick. At
			// 09:04 UTC the 04:00 New York job fired four minutes ago (09:00
			// UTC); resolving the schedule in UTC instead would place the last
			// tick at 04:00 UTC, five hours earlier, and flag a healthy CronJob
			// as "missed".
			name: "named zone puts the previous tick in the right place",
			expr: "0 4 * * *",
			tz:   "America/New_York",
			now:  time.Date(2024, 1, 15, 9, 4, 0, 0, time.UTC),
			want: time.Date(2024, 1, 15, 9, 0, 0, 0, time.UTC),
		},
		{
			name:    "invalid expression",
			expr:    "bad-expr",
			now:     base,
			wantErr: true,
		},
		{
			name:    "unknown zone",
			expr:    "0 4 * * *",
			tz:      "Nowhere/Special",
			now:     base,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := schedule.PrevRun(tc.expr, tc.tz, tc.now)
			if tc.wantErr {
				if err == nil {
					t.Errorf("PrevRun(%q, %q) expected error, got %v", tc.expr, tc.tz, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("PrevRun(%q, %q) unexpected error: %v", tc.expr, tc.tz, err)
			}
			if !got.Equal(tc.want) {
				t.Errorf("PrevRun(%q, %q) = %v, want %v", tc.expr, tc.tz, got.UTC(), tc.want)
			}
		})
	}
}
