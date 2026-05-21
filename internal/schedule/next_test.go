package schedule_test

import (
	"testing"
	"time"

	"github.com/kubecron/kubecron/internal/schedule"
)

// base is a fixed Monday at 10:00 UTC used across all test cases.
var base = time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

func TestNextRun(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
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
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := schedule.NextRun(tc.expr, tc.after)
			if tc.wantErr {
				if err == nil {
					t.Errorf("NextRun(%q) expected error, got %v", tc.expr, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NextRun(%q) unexpected error: %v", tc.expr, err)
			}
			if !got.Equal(tc.want) {
				t.Errorf("NextRun(%q) = %v, want %v", tc.expr, got, tc.want)
			}
		})
	}
}

func TestNextRuns(t *testing.T) {
	runs, err := schedule.NextRuns("*/5 * * * *", base, 3)
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

func TestPrevRun(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
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
			name:    "invalid expression",
			expr:    "bad-expr",
			now:     base,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := schedule.PrevRun(tc.expr, tc.now)
			if tc.wantErr {
				if err == nil {
					t.Errorf("PrevRun(%q) expected error, got %v", tc.expr, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("PrevRun(%q) unexpected error: %v", tc.expr, err)
			}
			if !got.Equal(tc.want) {
				t.Errorf("PrevRun(%q) = %v, want %v", tc.expr, got, tc.want)
			}
		})
	}
}
