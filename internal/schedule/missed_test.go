package schedule_test

import (
	"testing"
	"time"

	"github.com/kubecron/kubecron/internal/schedule"
)

// The missed rule backs both the UI's "missed" badge and the
// kubecron_cronjob_missed gauge, so these cases pin the behaviour both read.
func TestIsMissed(t *testing.T) {
	// A fixed now keeps the hourly schedule's last fire time unambiguous:
	// 12:00, i.e. 30 minutes before now.
	now := time.Date(2026, 3, 10, 12, 30, 0, 0, time.UTC)
	hourly := "0 * * * *"

	ranAt := func(t time.Time) *schedule.LastRun { return &schedule.LastRun{StartedAt: t} }

	cases := []struct {
		name      string
		expr      string
		tz        string
		suspended bool
		last      *schedule.LastRun
		want      bool
		why       string
	}{
		{
			name: "never ran and a fire time has passed",
			expr: hourly, last: nil, want: true,
			why: "no run has ever been recorded for a schedule that should have fired",
		},
		{
			name: "ran after the last fire time",
			expr: hourly, last: ranAt(now.Add(-25 * time.Minute)), want: false,
			why: "the 12:00 tick produced a run at 12:05",
		},
		{
			name: "last run predates the last fire time",
			expr: hourly, last: ranAt(now.Add(-90 * time.Minute)), want: true,
			why: "the newest run is from the 11:00 tick, so 12:00 was missed",
		},
		{
			name: "suspended",
			expr: hourly, suspended: true, last: ranAt(now.Add(-90 * time.Minute)), want: false,
			why: "a suspended CronJob is not expected to fire",
		},
		{
			name: "run still in flight",
			expr: hourly,
			last: &schedule.LastRun{StartedAt: now.Add(-90 * time.Minute), Running: true},
			want: false,
			why:  "a run in flight proves the schedule fired, whenever it started",
		},
		{
			name: "unparseable schedule",
			expr: "not a cron expression", last: nil, want: false,
			why: "an unresolvable schedule yields no fire time to compare against",
		},
		{
			name: "unknown time zone",
			expr: hourly, tz: "Mars/Olympus_Mons", last: nil, want: false,
			why: "an unresolvable zone must not produce a confidently wrong verdict",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := schedule.IsMissed(tc.expr, tc.tz, tc.suspended, tc.last, now)
			if got != tc.want {
				t.Errorf("IsMissed() = %v, want %v — %s", got, tc.want, tc.why)
			}
		})
	}
}

// A schedule that fired moments ago must not be reported as missed: the
// Kubernetes controller and KubeCron's informer both lag, and without the
// grace period every CronJob would flap into "missed" on each tick.
func TestIsMissed_GracePeriod(t *testing.T) {
	now := time.Date(2026, 3, 10, 12, 2, 0, 0, time.UTC) // 2 min after the 12:00 tick

	if schedule.IsMissed("0 * * * *", "", false, nil, now) {
		t.Error("IsMissed() = true within the grace period, want false")
	}

	// Past the grace period the same state is a genuine miss.
	later := now.Add(schedule.MissedGracePeriod + time.Minute)
	if !schedule.IsMissed("0 * * * *", "", false, nil, later) {
		t.Error("IsMissed() = false past the grace period, want true")
	}
}

// The zone must be honoured, not the server's local time (DOM-1): a schedule
// pinned to a zone fires on that zone's clock.
func TestIsMissed_HonoursTimeZone(t *testing.T) {
	// Fires daily at 09:00 Paris time. At 08:30 UTC on this date Paris is
	// UTC+1, so the local clock reads 09:30 — the 09:00 tick has passed.
	now := time.Date(2026, 2, 10, 8, 30, 0, 0, time.UTC)

	if !schedule.IsMissed("0 9 * * *", "Europe/Paris", false, nil, now) {
		t.Error("IsMissed() = false, want true: 09:00 Paris has passed and nothing ran")
	}

	// A run recorded after that tick clears it.
	last := &schedule.LastRun{StartedAt: now.Add(-20 * time.Minute)} // 09:10 Paris
	if schedule.IsMissed("0 9 * * *", "Europe/Paris", false, last, now) {
		t.Error("IsMissed() = true, want false: a run followed the 09:00 Paris tick")
	}
}

// --- BUG-22 regressions ------------------------------------------------------

// TestPrevRun_SparseSchedules is the bug in one table.
//
// PrevRun used to scan back a fixed 25 hours, so anything sparser than daily had
// no occurrence in the window most of the time and returned an error. IsMissed
// read that as "cannot say" and answered false, which switched missed detection
// off for the weekly and monthly jobs — the ones where a missed run matters most.
func TestPrevRun_SparseSchedules(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 34, 0, 0, time.UTC) // a Friday

	cases := []struct {
		name, expr string
		want       time.Time
	}{
		{"weekly on Sunday", "0 0 * * 0", time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)},
		{"monthly on the 1st", "0 0 1 * *", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)},
		{"quarterly", "0 0 1 1,4,7,10 *", time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)},
		{"yearly", "@yearly", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		// The dense ones must keep working, and cheaply.
		{"per minute", "* * * * *", time.Date(2026, 8, 21, 12, 34, 0, 0, time.UTC)},
		{"hourly", "0 * * * *", time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := schedule.PrevRun(tc.expr, "", now)
			if err != nil {
				t.Fatalf("PrevRun(%q): %v", tc.expr, err)
			}
			if !got.Equal(tc.want) {
				t.Errorf("PrevRun(%q) = %s, want %s", tc.expr, got, tc.want)
			}
		})
	}
}

// TestIsMissed_SparseSchedulesAreJudged. Before BUG-22 both halves of this
// answered false — a healthy monthly job and one that had not fired in seventy
// days were indistinguishable, so nothing ever looked wrong.
func TestIsMissed_SparseSchedulesAreJudged(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 30, 0, 0, time.UTC)

	cases := []struct {
		name, expr string
		lastAgo    time.Duration
		want       bool
	}{
		{"monthly, fired on the 1st", "0 0 1 * *", -20 * 24 * time.Hour, false},
		{"monthly, silent since June", "0 0 1 * *", -70 * 24 * time.Hour, true},
		{"weekly, fired on Sunday", "0 0 * * 0", -5 * 24 * time.Hour, false},
		{"weekly, silent for 20 days", "0 0 * * 0", -20 * 24 * time.Hour, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			last := &schedule.LastRun{StartedAt: now.Add(tc.lastAgo)}
			if got := schedule.IsMissed(tc.expr, "", false, last, now); got != tc.want {
				t.Errorf("IsMissed() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestIsMissed_OnTheHourStillJudgesADeadJob is the second half of BUG-22.
//
// The most recent due time is zero seconds old when the question is asked
// exactly on the hour, so it cannot be judged — and returning false there meant
// a job silent for a day reported healthy purely because of when it was asked.
// A per-minute CronJob is inside the grace period of *some* occurrence at every
// instant, so for those the old code could never report a miss at all.
func TestIsMissed_OnTheHourStillJudgesADeadJob(t *testing.T) {
	onTheHour := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)

	dead := &schedule.LastRun{StartedAt: onTheHour.Add(-25 * time.Hour)}
	if !schedule.IsMissed("0 * * * *", "", false, dead, onTheHour) {
		t.Error("IsMissed() = false for a job silent for 25h, asked on the hour; want true")
	}
	fresh := &schedule.LastRun{StartedAt: onTheHour.Add(-time.Minute)}
	if schedule.IsMissed("0 * * * *", "", false, fresh, onTheHour) {
		t.Error("IsMissed() = true for a job that ran a minute ago; want false")
	}

	// A per-minute schedule: every instant is inside some occurrence's grace
	// period, which is what made this invisible.
	perMinute := time.Date(2026, 8, 21, 12, 0, 10, 0, time.UTC)
	deadMinutes := &schedule.LastRun{StartedAt: perMinute.Add(-3 * time.Hour)}
	if !schedule.IsMissed("* * * * *", "", false, deadMinutes, perMinute) {
		t.Error("IsMissed() = false for a per-minute job silent for 3h; want true")
	}
}

// TestIsMissed_ANewCronJobIsNotMissed guards the fix's own failure mode. The
// step-back must not reach an occurrence from before the CronJob existed: with
// no run ever recorded there is no evidence either way, and a CronJob created
// three minutes ago has missed nothing.
func TestIsMissed_ANewCronJobIsNotMissed(t *testing.T) {
	due := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)

	if schedule.IsMissed("0 * * * *", "", false, nil, due.Add(2*time.Minute)) {
		t.Error("IsMissed() = true two minutes after a tick with no run ever recorded; a CronJob created moments ago has missed nothing")
	}
	// Past the grace period the same state is a genuine miss.
	if !schedule.IsMissed("0 * * * *", "", false, nil, due.Add(schedule.MissedGracePeriod+time.Minute)) {
		t.Error("IsMissed() = false past the grace period with no run ever recorded; want true")
	}
}
