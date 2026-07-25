package storage

import "time"

type Cluster struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	MetricsEnabled bool       `json:"metrics_enabled"`
	CreatedAt      time.Time  `json:"created_at"`
	DeletedAt      *time.Time `json:"deleted_at,omitempty"`
}

type CronJob struct {
	ID        string `json:"id"`
	ClusterID string `json:"cluster_id"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Schedule  string `json:"schedule"`
	// TimeZone mirrors CronJob spec.timeZone (IANA name). nil means the CronJob
	// declares no zone, in which case the schedule is evaluated server-local —
	// the same rule the Kubernetes CronJob controller applies.
	TimeZone           *string    `json:"time_zone,omitempty"`
	Suspended          bool       `json:"suspended"`
	CPURequest         *string    `json:"cpu_request,omitempty"`
	CPULimit           *string    `json:"cpu_limit,omitempty"`
	MemoryRequest      *string    `json:"memory_request,omitempty"`
	MemoryLimit        *string    `json:"memory_limit,omitempty"`
	LastSuccessfulTime *time.Time `json:"last_successful_time,omitempty"`
	UpdatedAt          time.Time  `json:"updated_at"`
	// DeletedAt is set when the object is no longer present in the cluster.
	// Soft-deleted CronJobs are hidden from listings but keep their run history.
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// TZ returns the CronJob's IANA time-zone name, or "" when it declares none.
func (c CronJob) TZ() string {
	if c.TimeZone == nil {
		return ""
	}
	return *c.TimeZone
}

type JobRun struct {
	ID               string     `json:"id"`
	CronJobID        string     `json:"cronjob_id"`
	PodName          string     `json:"pod_name"`
	NodeName         *string    `json:"node_name,omitempty"`
	ContainerImage   *string    `json:"container_image,omitempty"`
	Trigger          string     `json:"trigger"`
	StartedAt        time.Time  `json:"started_at"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
	Status           string     `json:"status"`
	ExitCode         *int       `json:"exit_code,omitempty"`
	RetryCount       int        `json:"retry_count"`
	LogSizeBytes     int64      `json:"log_size_bytes"`
	DurationMs       *int64     `json:"duration_ms,omitempty"`
	AvgCPUMillicores *int64     `json:"avg_cpu_millicores,omitempty"`
	MaxCPUMillicores *int64     `json:"max_cpu_millicores,omitempty"`
	AvgMemoryBytes   *int64     `json:"avg_memory_bytes,omitempty"`
	MaxMemoryBytes   *int64     `json:"max_memory_bytes,omitempty"`
}

type ResourceSample struct {
	ID            int64     `json:"id"`
	RunID         string    `json:"run_id"`
	SampledAt     time.Time `json:"sampled_at"`
	CPUMillicores int64     `json:"cpu_millicores"`
	MemoryBytes   int64     `json:"memory_bytes"`
}

type LogLine struct {
	ID    int64     `json:"id"`
	RunID string    `json:"run_id"`
	Ts    time.Time `json:"ts"`
	Line  string    `json:"line"`
}

type RunStats struct {
	Total            int    `json:"total"`
	Succeeded        int    `json:"succeeded"`
	Failed           int    `json:"failed"`
	AvgDurationMs    *int64 `json:"avg_duration_ms,omitempty"`
	MaxDurationMs    *int64 `json:"max_duration_ms,omitempty"`
	AvgCPUMillicores *int64 `json:"avg_cpu_millicores,omitempty"`
	AvgMemoryBytes   *int64 `json:"avg_memory_bytes,omitempty"`
}

// CronJobSummary is everything a CronJob table row needs beyond the CronJob
// row itself. Rendering a cluster or namespace page used to issue three
// queries per CronJob (last run, 7-day stats, recent durations), re-run every
// 10 s by the HTMX poll of every open tab — 3N queries per render. Summaries
// for a whole cluster are now fetched in a fixed number of queries instead
// (PERF-2).
type CronJobSummary struct {
	LastRun   *JobRun
	Stats7d   *RunStats
	Durations []int64
}

// DailyRunStat holds aggregated run statistics for a single calendar day.
type DailyRunStat struct {
	Day       string `json:"day"`
	Total     int    `json:"total"`
	Succeeded int    `json:"succeeded"`
	Running   int    `json:"running"`
}
