package schedule

import (
	"time"

	"github.com/robfig/cron/v3"
)

// NextRuns returns the next n scheduled times for the cron expression expr
// that fall strictly after after. It uses the standard five-field cron
// syntax (minute, hour, day-of-month, month, day-of-week).
func NextRuns(expr string, after time.Time, n int) ([]time.Time, error) {
	s, err := cron.ParseStandard(expr)
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

// NextRun returns the single next scheduled time for expr after after.
func NextRun(expr string, after time.Time) (time.Time, error) {
	runs, err := NextRuns(expr, after, 1)
	if err != nil {
		return time.Time{}, err
	}
	return runs[0], nil
}
