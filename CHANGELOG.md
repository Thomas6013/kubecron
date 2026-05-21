# Changelog

All notable changes to KubeCron are documented here.

---

## [Unreleased]

### Security
- **Remove global CORS `*`** — `Access-Control-Allow-Origin: *` middleware removed entirely; KubeCron has no public cross-origin API (SEC-5)
- **Add CSRF double-submit cookie protection** — `EnsureCSRFCookie` sets a `csrf_token` cookie; `CSRFProtect` validates `X-CSRF-Token` header on every POST; HTMX auto-attaches via `htmx:configRequest` event (SEC-6)
- **Rate limit `/auth/login` and `trigger`** — fixed-window rate limiter (10 req/min for login, 20 req/min for trigger) per source IP (SEC-11)
- **Fix open redirect on `/auth/login?redirect=`** — validate that redirect targets are same-origin relative paths; covered by `TestSafeRedirect`
- **Add `Secure` flag to session and state cookies** — derived from `OIDC_REDIRECT_URL` starting with `https://`
- **Fix XSS in SSE log replay** — history replay for running runs was missing `html.EscapeString` and the `<div class="ll">` wrapper
- **Fix `UpsertJobRun` data loss** — `INSERT OR REPLACE` wiped `finished_at`, `exit_code`, resource columns on re-upsert; replaced with targeted `ON CONFLICT DO UPDATE`; covered by `TestUpsertJobRun_DoesNotOverwriteExistingFields`
- **Warn at boot when `OIDC_SESSION_KEY` is empty** — previously a random key was generated silently, invalidating all sessions on restart

### Fixed
- **`findRunID` O(N×M) scan replaced with O(1) RunIndex** — `JobHandler` populates an in-memory `jobName → runID` map on every new Job; `PodHandler.findRunID` checks it first and falls back to DB only at startup (BUG-4)
- **`Manager.Load` no longer errors on `.gitkeep` and dotfiles** — file loop now skips dot-prefixed names and non-`.yaml`/`.yml` extensions (BUG-5)
- **Pod log/metric capture for Running pods at startup** — `PodHandler.OnAdd` now handles `PodRunning`; `retryOnAdd` goroutine works around the Job/Pod informer ordering race at boot
- **`backfillRun` non-deterministic pod selection** — `selectBestPod()` prefers the most recently created terminal pod instead of `pods.Items[0]`
- **Log triplication on multi-node/restart** — `backfillRun` skips log streaming when `log_size_bytes > 0` already

### Changed
- **Prometheus metrics now wired** — 6 collectors are fully populated: `kubecron_job_runs_total`, `kubecron_job_duration_seconds`, `kubecron_last_run_timestamp`, `kubecron_last_run_status`, `kubecron_cronjob_suspended`, `kubecron_next_run_timestamp`
- **Removed `internal/ui/templates/`** — the `templ` package was unused; removed from `go.mod`, Dockerfile, and CI
- **Simplified Dockerfile** — single-stage Go build (removed `templ-gen` stage)
- **HTML component extraction** — `html_components.go` adds `renderRunRow` + `runTableHeader`; `html.go` split into `html_log.go` (log search) and `html_chart.go` (sparkline, heatmap); `buildCronJobRow` method eliminates duplicated row computation between `ClusterDetail` and `NamespaceDetail`
- **SQL deduplication** — `const jobRunCols` consolidates the 17-column `job_runs` SELECT list used by `GetJobRun`, `ListJobRuns`, `GetLastJobRun`, `GetRunningRuns`
- **Removed trivial wrappers** — `scanCronJobRow`, `scanJobRunRows`, `errorf`/`stringError` deleted; callers use `scanCronJob`, `scanJobRun`, `fmt.Errorf` directly
- **Auth logout flow** — redirects to an intermediate `/auth/logged-out` page; nav shows user email and logout button when OIDC is enabled
- **Log truncation** — `GetLogLinesTail(5000)` replaces unbounded `GetLogLines`; truncation warning with full-log download link shown when limit is hit
- **SQLite performance** — `PRAGMA synchronous=NORMAL`, `PRAGMA cache_size=-65536` (64 MB), log batch flush 200 ms / 200 lines

### Added
- **Test coverage extended** — broadcaster pub/sub + concurrent stress test (`internal/streamer`); watcher `JobHandler` OnAdd/OnDelete tests with fake K8s client (`internal/watcher`); 5 suites total, `go test ./...` passes

---

## [0.1.0] - 2026-05-12

### Added

- **Missed run detection** — CronJobs that should have run but did not are flagged with a `missed` badge in the cluster and namespace views; detection is based on the previous scheduled tick (≤25h lookback, 5-minute grace)
- **Concurrent run detection** — when multiple runs of the same CronJob are simultaneously active, a `⚠ concurrent` badge appears next to the last status
- **Log level colorization** — log lines are automatically colorized by severity (FATAL/CRITICAL, ERROR, WARN, INFO, DEBUG) using a MutationObserver that handles both static and live-streamed logs
- **Regex log search** — a search bar above the log terminal accepts a regular expression, highlights matches with `<mark>`, shows a filtered line count, and hides non-matching lines
- **Duration sparkline** — a 60×16 SVG sparkline of the last 20 completed run durations (oldest→newest) is displayed alongside the 7-day success ratio in both cluster and namespace views
- **Log download button** — a `⬇ .log` link in the log toolbar triggers a plain-text download via `GET /api/runs/{id}/logs.txt`
- **Calendar heatmap** — a 90-day SVG heatmap on the run list page shows per-day success/failure/partial status; green = all succeeded, yellow = partial, red = all failed, surface2 = no runs

---

## [0.0.1] - 2026-05-07

### Added

- **Multi-cluster CronJob monitoring** — load one kubeconfig per cluster from `KUBECONFIG_DIR`; all clusters shown in a unified dashboard
- **Live log streaming** — SSE endpoint streams pod logs in real time as CronJob pods execute; replays from DB when run is finished
- **Run history** — every CronJob execution recorded with status, duration, exit code, retry count, and log size
- **Resource sampling** — CPU and memory sampled every 15 s via metrics-server when available; avg/max computed per run and stored in DB
- **7-day statistics** — success rate, average duration, and p95 duration computed per CronJob
- **Next-run countdown** — computed from cron expression using `robfig/cron/v3`, updated live in the browser via HTMX polling
- **Suspend / Resume / Trigger** — control CronJobs directly from the dashboard; trigger creates a Job with label `kubecron/trigger=manual`
- **OIDC authentication** — optional SSO via any OIDC provider (Keycloak, Dex, Google, etc.)
- **Prometheus metrics** — `/metrics` endpoint exposing run counts, durations, status, resource usage, and cluster state
- **SQLite persistence** — embedded single-file database with WAL mode, automatic migrations, and configurable retention (default: 7 days)
- **Helm chart** — `charts/kubecron/` with values for ingress, TLS, OIDC secrets, resource limits, retention, and persistence
- **Raw Kubernetes manifests** — `k8s/` directory with namespace, ClusterRole (minimal RBAC), ServiceAccount, Deployment, PVC, Service
- **Distroless runtime image** — `gcr.io/distroless/static:nonroot` with no shell and no package manager
- **Multi-arch Docker builds** — `linux/amd64` + `linux/arm64`
- **Health endpoints** — `/healthz` (always 200) and `/readyz` (200 when informer caches synced)
- **SBOM + image signing** — SPDX SBOM generated per release; images signed with cosign (keyless)
