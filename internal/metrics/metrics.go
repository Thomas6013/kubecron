package metrics

import (
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// JobRunsTotal counts completed job runs by outcome and trigger type.
	JobRunsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "kubecron_job_runs_total",
		Help: "Total number of completed job runs.",
	}, []string{"cluster", "namespace", "cronjob", "status", "trigger"})

	// JobDurationSeconds records job run durations.
	JobDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "kubecron_job_duration_seconds",
		Help:    "Duration of job runs in seconds.",
		Buckets: []float64{1, 5, 15, 30, 60, 300, 600, 1800},
	}, []string{"cluster", "namespace", "cronjob"})

	// LastRunTimestamp is the Unix timestamp of the most recent run start.
	LastRunTimestamp = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "kubecron_last_run_timestamp",
		Help: "Unix timestamp of the last run start for the CronJob.",
	}, []string{"cluster", "namespace", "cronjob"})

	// LastRunStatus is 0 for success, 1 for failure of the most recent run.
	LastRunStatus = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "kubecron_last_run_status",
		Help: "Status of the last run: 0 = success, 1 = failure.",
	}, []string{"cluster", "namespace", "cronjob"})

	// NextRunTimestamp is the Unix timestamp of the next scheduled run.
	NextRunTimestamp = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "kubecron_next_run_timestamp",
		Help: "Unix timestamp of the next scheduled run for the CronJob.",
	}, []string{"cluster", "namespace", "cronjob"})

	// CronJobSuspended is 1 when a CronJob is suspended, 0 otherwise.
	CronJobSuspended = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "kubecron_cronjob_suspended",
		Help: "Whether the CronJob is suspended (1) or active (0).",
	}, []string{"cluster", "namespace", "cronjob"})
)

// RecordCompletion updates all run-completion metrics.
// cronJobID must have the form "clusterID/namespace/name".
func RecordCompletion(clusterID, cronJobID, status, trigger string, startedAt time.Time, finishedAt time.Time) {
	parts := strings.SplitN(cronJobID, "/", 3)
	if len(parts) != 3 {
		return
	}
	namespace, name := parts[1], parts[2]

	JobRunsTotal.WithLabelValues(clusterID, namespace, name, status, trigger).Inc()
	JobDurationSeconds.WithLabelValues(clusterID, namespace, name).Observe(finishedAt.Sub(startedAt).Seconds())
	LastRunTimestamp.WithLabelValues(clusterID, namespace, name).Set(float64(startedAt.Unix()))

	failedVal := 0.0
	if status == "failed" {
		failedVal = 1.0
	}
	LastRunStatus.WithLabelValues(clusterID, namespace, name).Set(failedVal)
}
