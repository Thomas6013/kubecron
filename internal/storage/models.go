package storage

import "time"

type Cluster struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	MetricsEnabled bool      `json:"metrics_enabled"`
	CreatedAt      time.Time `json:"created_at"`
}

type CronJob struct {
	ID                 string     `json:"id"`
	ClusterID          string     `json:"cluster_id"`
	Namespace          string     `json:"namespace"`
	Name               string     `json:"name"`
	Schedule           string     `json:"schedule"`
	Suspended          bool       `json:"suspended"`
	CPURequest         *string    `json:"cpu_request,omitempty"`
	CPULimit           *string    `json:"cpu_limit,omitempty"`
	MemoryRequest      *string    `json:"memory_request,omitempty"`
	MemoryLimit        *string    `json:"memory_limit,omitempty"`
	LastSuccessfulTime *time.Time `json:"last_successful_time,omitempty"`
	UpdatedAt          time.Time  `json:"updated_at"`
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

// DailyRunStat holds aggregated run statistics for a single calendar day.
type DailyRunStat struct {
	Day       string `json:"day"`
	Total     int    `json:"total"`
	Succeeded int    `json:"succeeded"`
	Running   int    `json:"running"`
}
