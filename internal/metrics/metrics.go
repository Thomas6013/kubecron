package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ── Counters ─────────────────────────────────────────────────────────────────

// JobRunsTotal counts completed job runs, partitioned by outcome and trigger.
var JobRunsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "kubecron_job_runs_total",
	Help: "Total number of completed job runs.",
}, []string{"cluster", "namespace", "cronjob", "status", "trigger"})

// LogSizeBytesTotal tracks total log bytes ingested per CronJob.
var LogSizeBytesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "kubecron_log_size_bytes_total",
	Help: "Total log bytes ingested per CronJob.",
}, []string{"cluster", "namespace", "cronjob"})

// ── Histograms ────────────────────────────────────────────────────────────────

// JobDurationSeconds records job run durations.
var JobDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "kubecron_job_duration_seconds",
	Help:    "Duration of job runs in seconds.",
	Buckets: []float64{1, 5, 15, 30, 60, 300, 600, 1800},
}, []string{"cluster", "namespace", "cronjob"})

// ── Gauges ────────────────────────────────────────────────────────────────────

// CronJobSuspended is 1 when a CronJob is suspended, 0 otherwise.
var CronJobSuspended = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "kubecron_cronjob_suspended",
	Help: "Whether the CronJob is suspended (1) or active (0).",
}, []string{"cluster", "namespace", "cronjob"})

// LastRunTimestamp is the Unix timestamp of the most recent run start.
var LastRunTimestamp = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "kubecron_last_run_timestamp",
	Help: "Unix timestamp of the last run start for the CronJob.",
}, []string{"cluster", "namespace", "cronjob"})

// LastRunStatus is 0 for success, 1 for failure of the most recent run.
var LastRunStatus = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "kubecron_last_run_status",
	Help: "Status of the last run: 0 = success, 1 = failure.",
}, []string{"cluster", "namespace", "cronjob"})

// NextRunTimestamp is the Unix timestamp of the next scheduled run.
var NextRunTimestamp = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "kubecron_next_run_timestamp",
	Help: "Unix timestamp of the next scheduled run for the CronJob.",
}, []string{"cluster", "namespace", "cronjob"})

// JobCPUAvg is the average CPU usage in millicores for the most recent run.
var JobCPUAvg = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "kubecron_job_cpu_millicores_avg",
	Help: "Average CPU usage in millicores for the most recent completed run.",
}, []string{"cluster", "namespace", "cronjob"})

// JobCPUPeak is the peak CPU usage in millicores for the most recent run.
var JobCPUPeak = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "kubecron_job_cpu_millicores_peak",
	Help: "Peak CPU usage in millicores for the most recent completed run.",
}, []string{"cluster", "namespace", "cronjob"})

// JobMemoryAvg is the average memory usage in bytes for the most recent run.
var JobMemoryAvg = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "kubecron_job_memory_bytes_avg",
	Help: "Average memory usage in bytes for the most recent completed run.",
}, []string{"cluster", "namespace", "cronjob"})

// JobMemoryPeak is the peak memory usage in bytes for the most recent run.
var JobMemoryPeak = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "kubecron_job_memory_bytes_peak",
	Help: "Peak memory usage in bytes for the most recent completed run.",
}, []string{"cluster", "namespace", "cronjob"})

// ActiveStreams is the number of currently open log-streaming goroutines.
var ActiveStreams = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "kubecron_active_streams",
	Help: "Number of currently active log streaming goroutines.",
})

// ClusterMetricsEnabled is 1 when the Metrics API is available on the cluster.
var ClusterMetricsEnabled = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "kubecron_cluster_metrics_enabled",
	Help: "Whether the Metrics API is available on the cluster (1) or not (0).",
}, []string{"cluster"})

// ── Helpers ───────────────────────────────────────────────────────────────────

// RecordRunCompleted increments the run counter and observes the duration
// histogram for a completed job run.
func RecordRunCompleted(clusterID, namespace, cronjob, status, trigger string, durationSeconds float64) {
	JobRunsTotal.WithLabelValues(clusterID, namespace, cronjob, status, trigger).Inc()
	JobDurationSeconds.WithLabelValues(clusterID, namespace, cronjob).Observe(durationSeconds)
}

// RecordCronJobState updates all gauge metrics that describe the current state
// of a CronJob. lastRunFailed should be true if the last run ended with a
// non-success status.
func RecordCronJobState(clusterID, namespace, cronjob string, suspended bool, lastRunTs, nextRunTs float64, lastRunFailed bool) {
	suspendedVal := 0.0
	if suspended {
		suspendedVal = 1.0
	}
	CronJobSuspended.WithLabelValues(clusterID, namespace, cronjob).Set(suspendedVal)

	if lastRunTs > 0 {
		LastRunTimestamp.WithLabelValues(clusterID, namespace, cronjob).Set(lastRunTs)
	}

	if nextRunTs > 0 {
		NextRunTimestamp.WithLabelValues(clusterID, namespace, cronjob).Set(nextRunTs)
	}

	lastStatusVal := 0.0
	if lastRunFailed {
		lastStatusVal = 1.0
	}
	LastRunStatus.WithLabelValues(clusterID, namespace, cronjob).Set(lastStatusVal)
}
