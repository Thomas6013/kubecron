// Package schedule computes past and future occurrences of a Kubernetes
// CronJob schedule.
//
// Every entry point takes an IANA time-zone name alongside the cron
// expression. This mirrors CronJob `spec.timeZone`: when it is set the
// Kubernetes controller evaluates the schedule in that zone, so KubeCron must
// do the same or its next-run and missed-run verdicts disagree with reality.
//
// An empty tz leaves the schedule in the zone of the timestamp handed to
// NextRun/PrevRun. Callers pass time.Now(), so that is the server's local zone
// — matching what Kubernetes does for a CronJob that declares no zone.
package schedule

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// Parse parses a standard five-field cron expression (minute, hour,
// day-of-month, month, day-of-week) and resolves it in the IANA location named
// tz. An empty tz leaves the schedule unpinned, in which case robfig/cron
// evaluates it in the location of the timestamp it is given.
//
// Loading a named zone requires tzdata to be present in the binary; cmd/kubecron
// imports time/tzdata because the distroless runtime image ships no
// /usr/share/zoneinfo.
//
// A tz argument takes precedence over any `CRON_TZ=`/`TZ=` prefix inside expr.
// Kubernetes rejects a CronJob that sets both, so in practice only one is ever
// present.
func Parse(expr, tz string) (cron.Schedule, error) {
	sched, err := cron.ParseStandard(expr)
	if err != nil {
		return nil, err
	}
	if tz == "" {
		return sched, nil
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("schedule: unknown time zone %q: %w", tz, err)
	}
	// Spec schedules ("0 4 * * *", "@daily") carry the zone they are evaluated
	// in. Interval schedules ("@every 1h") are zone-independent, so there is
	// nothing to override.
	if spec, ok := sched.(*cron.SpecSchedule); ok {
		spec.Location = loc
	}
	return sched, nil
}

// NextRuns returns the next n scheduled times for expr, evaluated in tz, that
// fall strictly after after.
func NextRuns(expr, tz string, after time.Time, n int) ([]time.Time, error) {
	s, err := Parse(expr, tz)
	if err != nil {
		return nil, err
	}
	times := make([]time.Time, n)
	t := after
	for i := range n {
		t = s.Next(t)
		times[i] = t
	}
	return times, nil
}

// NextRun returns the single next scheduled time for expr, evaluated in tz,
// after after.
func NextRun(expr, tz string, after time.Time) (time.Time, error) {
	runs, err := NextRuns(expr, tz, after, 1)
	if err != nil {
		return time.Time{}, err
	}
	return runs[0], nil
}

// PrevRun returns the most recent scheduled time for expr, evaluated in tz,
// that is ≤ now. It scans forward from now-25h, stepping tick by tick, and
// returns the last tick that does not exceed now.
func PrevRun(expr, tz string, now time.Time) (time.Time, error) {
	s, err := Parse(expr, tz)
	if err != nil {
		return time.Time{}, err
	}
	start := now.Add(-25 * time.Hour)
	var prev time.Time
	t := start
	for {
		next := s.Next(t)
		if next.IsZero() || next.After(now) {
			break
		}
		prev = next
		t = next
	}
	if prev.IsZero() {
		return time.Time{}, fmt.Errorf("schedule: no previous run found for %q in the last 25h", expr)
	}
	return prev, nil
}
