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

// maxLookback bounds how far PrevRun will search. A schedule whose last
// occurrence is over a year old is not a late run, it is a schedule that fires
// on a date this year does not contain — "0 0 29 2 *" outside a leap year — and
// reporting it as overdue every day for four years would be noise.
const maxLookback = 400 * 24 * time.Hour

// maxProbes bounds the walk inside one window, so a per-minute schedule cannot
// turn a page render into 500 000 calls if the window estimate is ever wrong.
const maxProbes = 2000

// PrevRun returns the most recent scheduled time for expr, evaluated in tz,
// that is ≤ now.
//
// robfig/cron only walks forwards, so "previous" is a search: start somewhere in
// the past and step until the next step would pass now. The whole difficulty is
// choosing where to start.
//
// This used to be a fixed 25 hours, and that was wrong in a way nothing caught
// (BUG-22). Any schedule sparser than daily has no occurrence in a 25-hour
// window most of the time — a weekly CronJob on six days out of seven, a monthly
// one on twenty-nine days out of thirty — so PrevRun returned an error, IsMissed
// read that as "cannot say" and answered **false**. Missed-run detection was
// therefore silently switched off for the nightly-weekly backup and the monthly
// invoice run: the jobs where a missed run matters most. Worse, it gave the same
// answer for a healthy monthly job and a dead one, so nothing looked broken.
//
// The window is derived from the schedule instead. Two future occurrences give
// its cadence; twice that is a window containing at least one occurrence for any
// regular schedule, and expanding covers the irregular ones (`0 9 * * 1-5` steps
// three days across a weekend) without ever making the per-minute case
// expensive — a fixed 25-hour walk cost ~1 500 Next() calls per CronJob per
// render, which the cluster and namespace pages pay once per row.
func PrevRun(expr, tz string, now time.Time) (time.Time, error) {
	s, err := Parse(expr, tz)
	if err != nil {
		return time.Time{}, err
	}

	window := 2 * estimateCadence(s, now)
	if window < time.Hour {
		window = time.Hour
	}

	// Clamped, and the loop runs at least once: a yearly schedule's estimated
	// cadence already exceeds the cap, and a `window <= maxLookback` guard
	// evaluated first would skip the search entirely and report a perfectly
	// ordinary @yearly CronJob as unresolvable.
	for {
		if window > maxLookback {
			window = maxLookback
		}
		if prev, ok := walk(s, now.Add(-window), now); ok {
			return prev, nil
		}
		if window >= maxLookback {
			break
		}
		window *= 4
	}
	return time.Time{}, fmt.Errorf("schedule: no occurrence of %q in the last %d days", expr, int(maxLookback.Hours()/24))
}

// estimateCadence measures the gap between the next two occurrences. It is an
// estimate and not a period: an irregular schedule has no period, which is why
// the caller expands the window rather than trusting this.
func estimateCadence(s cron.Schedule, now time.Time) time.Duration {
	first := s.Next(now)
	if first.IsZero() {
		return maxLookback
	}
	second := s.Next(first)
	if second.IsZero() {
		return maxLookback
	}
	return second.Sub(first)
}

// walk returns the last occurrence in (from, to], and whether there was one.
func walk(s cron.Schedule, from, to time.Time) (time.Time, bool) {
	var prev time.Time
	var found bool
	t := from
	for range maxProbes {
		next := s.Next(t)
		if next.IsZero() || next.After(to) {
			break
		}
		prev, found = next, true
		t = next
	}
	return prev, found
}

// MissedGracePeriod is how long after a scheduled fire time a run may still
// appear before the schedule counts as missed. It absorbs the normal lag
// between the Kubernetes CronJob controller firing and KubeCron's informer
// recording the resulting Job.
const MissedGracePeriod = 5 * time.Minute

// LastRun is the minimum a caller must know about the most recent recorded run
// for IsMissed to judge a schedule. It is a plain value type so that the
// schedule package stays free of any storage dependency.
type LastRun struct {
	StartedAt time.Time
	// Running marks a run that has not finished. A run still in flight proves
	// the schedule fired, so it clears the missed state regardless of when it
	// started.
	Running bool
}

// IsMissed reports whether expr's most recent scheduled fire time — evaluated
// in tz — passed without a corresponding run being recorded. last may be nil,
// meaning no run has ever been seen.
//
// It returns false rather than true whenever the answer cannot be established:
// a suspended CronJob is not expected to fire, and a schedule or zone that does
// not resolve yields no fire time to compare against. Reporting a missed run on
// an unresolvable schedule would page somebody about a job that is running
// perfectly well (DOM-1).
//
// **A false is therefore two answers wearing one word** — "it ran" and "I could
// not tell" — and callers cannot distinguish them. That is survivable for a
// badge and a gauge, which both have "absent" as their quiet state; it is worth
// knowing before a third caller treats false as proof of health. KubeDeck's port
// of this returns a four-valued verdict for that reason.
func IsMissed(expr, tz string, suspended bool, last *LastRun, now time.Time) bool {
	if suspended {
		return false
	}
	prev, err := PrevRun(expr, tz, now)
	if err != nil {
		return false
	}

	// Due within the grace period, so that occurrence cannot be judged yet.
	// Step back to the one before it rather than answering false (BUG-22).
	//
	// Answering false outright is the trap: a per-minute CronJob is inside the
	// grace period of *some* occurrence at every instant, so a dead one would be
	// reported healthy forever. Even an hourly job asked exactly on the hour came
	// back "not missed" however many days it had been silent.
	//
	// **Only when there is a run to compare against**, and that condition is the
	// whole of the correctness here. With last == nil there is no evidence in
	// either direction, and the earlier occurrence may well predate the CronJob
	// itself — so stepping back would report a CronJob created three minutes ago
	// as having missed a tick from before it existed. Withholding the verdict for
	// the length of the grace period costs a genuinely dead job five minutes of
	// silence and saves every newly-created one from a false alarm.
	if now.Sub(prev) <= MissedGracePeriod {
		if last == nil {
			return false
		}
		earlier, err := PrevRun(expr, tz, prev.Add(-time.Nanosecond))
		if err != nil {
			// Its first-ever occurrence is the one in flight; nothing earlier
			// exists to have been missed.
			return false
		}
		prev = earlier
	}

	if last == nil {
		return true
	}
	return !last.Running && last.StartedAt.Before(prev)
}
