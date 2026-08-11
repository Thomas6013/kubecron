# Changelog

All notable changes to KubeCron are documented here.

---

## [Unreleased]

_Nothing yet._

---

## [0.3.0] - 2026-08-11

The "know where to look" release: a summary that ranks what actually needs
attention, and metrics that keep telling you so after a restart.

### Added

- **Fleet and cluster summary views** — every view now leads with the numbers
  that decide where to look: CronJobs, success rate, failures (and how many
  distinct CronJobs they came from), runs in flight, and suspended jobs. Below
  them, four rankings — most failures, longest mean duration, highest peak CPU,
  highest peak memory — each linking straight to the CronJob's run history, with
  bars scaled against the leader so the list reads as a distribution. A
  24 h / 7 d / 30 d switch rescopes the whole block.
- **Cluster-scoped summary** — the same block heads the cluster view, restricted
  to that cluster, so "where do I focus" is answerable per cluster as well as
  fleet-wide. Both views share one code path and one pair of queries.
- **Cluster control in the nav** — a picker when there are several clusters, a
  plain label naming the cluster when there is exactly one, nothing when there
  are none. A single-option dropdown is a control that cannot do anything.
- **Ten new Prometheus metrics** — `kubecron_runs_active` (hung or overlapping
  runs), `kubecron_cronjob_missed` (a schedule that silently stopped firing —
  computed for the UI badge since the beginning but never exported),
  `kubecron_last_run_duration_seconds` (a gauge; alert rules cannot express
  themselves against the existing histogram), `kubecron_last_run_cpu_millicores`
  and `kubecron_last_run_memory_bytes`, `kubecron_cluster_cronjobs`,
  `kubecron_cluster_metrics_api_available` (so a flat resource gauge is
  distinguishable from a dead metrics-server), and `kubecron_build_info`.
- **HTTP request metrics** — `kubecron_http_requests_total` and
  `kubecron_http_request_duration_seconds`, labelled by matched route pattern
  rather than raw path, so per-CronJob URLs cannot inflate cardinality.

### Fixed

- **Gauge metrics now survive a restart (OBS-3)** — every gauge was previously
  written only by live watcher events, so after each restart
  `kubecron_last_run_status`, `kubecron_last_run_timestamp` and the run counters
  had **no series at all** until each CronJob next happened to fire — up to a
  full day for a nightly backup, exactly when a restart makes alerting most
  valuable. Alert rules written against them silently stopped evaluating. A
  state collector now derives them from stored state every 30 s, making them a
  function of the database rather than of process uptime. Counters and the
  duration histogram stay event-driven, since rebuilding them each pass would
  double-count. A run still in flight keeps its previous values instead of being
  zeroed, and a CronJob that has never run publishes no `last_run_status` rather
  than a `0` claiming a success that never happened.
- **Single-cluster installs no longer render the same dashboard twice** — with
  one cluster the fleet summary and that cluster's own summary are the same
  numbers, so `/` now redirects to the cluster view (which also lists the
  CronJobs), carrying the selected window across. The Overview nav link appears
  only when there is more than one cluster to compare.
- **Deterministic lint (INFRA-4)** — CI installed golangci-lint from the **v1**
  module path, where `@latest` can never resolve v2 (v2 moved to
  `.../v2/cmd/golangci-lint`). CI was therefore frozen on the final v1 release
  and never picked up a new check again, while anyone running a current v2
  locally saw 15 findings CI had never reported. A committed `.golangci.yml`
  now fixes the semantics and CI installs v2.

### Changed

- **Missed-run detection extracted to `schedule.IsMissed`** — the UI badge and
  the new `kubecron_cronjob_missed` metric now share one definition instead of
  two that could drift. Covered by tests including DST and unresolvable zones.
- **Dependencies** — `k8s.io/{api,apimachinery,client-go,metrics}` 0.36.2 →
  0.36.3, `github.com/coreos/go-oidc/v3` 3.19.0 → 3.20.0,
  `github.com/prometheus/client_golang` 1.23.2 → 1.24.1, `modernc.org/sqlite`
  1.53.0 → 1.56.0, `actions/setup-go` v6 → v7.

### Security

- **Audit pass 2026-08-11 opened SEC-28 (HIGH), not yet fixed** — the Helm chart
  allows `ingress.enabled=true` with `oidc.enabled=false` (the default), which
  exposes `suspend`/`resume`/`trigger` unauthenticated on every connected
  cluster, with no warning from the chart, `NOTES.txt`, or the startup log. Until
  it is fixed, **do not enable the Ingress without also enabling OIDC.** See
  `docs/AUDIT.md`.

---

## [0.2.0] - 2026-07-25

The "stop being wrong" release: the three findings that made the dashboard
disagree with the cluster, plus the read-path work needed to hold up at scale.
See [`docs/PRODUCT.md`](docs/PRODUCT.md) for where the product goes from here.

### Security

- **HTTP server timeouts** — `ReadHeaderTimeout` 10 s + `IdleTimeout` 120 s on the listener (Slowloris hardening); no `WriteTimeout` so SSE streams stay long-lived (SEC-20)
- **Generic errors on suspend/resume** — raw Kubernetes errors are now logged server-side and never returned to HTTP clients; unknown cluster returns 404 instead of 500 (SEC-21)
- **Security headers** — `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: same-origin` on every response (SEC-23, partial — CSP/HSTS deferred)
- **CSRF cookie `Secure` flag** — set when the app is served over HTTPS (derived from `OIDC_REDIRECT_URL`); logout switched from GET to CSRF-protected POST (SEC-25)
- **ClusterRole least-privilege split** — `patch` granted on CronJobs only, `create` on Jobs only (previously both verbs on both resources) (SEC-26)
- **`seccompProfile: RuntimeDefault`** — added to the Helm pod security context (INFRA-2)
- **Remove global CORS `*`** — `Access-Control-Allow-Origin: *` middleware removed entirely; KubeCron has no public cross-origin API (SEC-5)
- **Add CSRF double-submit cookie protection** — `EnsureCSRFCookie` sets a `csrf_token` cookie; `CSRFProtect` validates `X-CSRF-Token` header on every POST; HTMX auto-attaches via `htmx:configRequest` event (SEC-6)
- **Rate limit `/auth/login` and `trigger`** — fixed-window rate limiter (10 req/min for login, 20 req/min for trigger) per source IP (SEC-11)
- **Fix open redirect on `/auth/login?redirect=`** — validate that redirect targets are same-origin relative paths; covered by `TestSafeRedirect`
- **Add `Secure` flag to session and state cookies** — derived from `OIDC_REDIRECT_URL` starting with `https://`
- **Fix XSS in SSE log replay** — history replay for running runs was missing `html.EscapeString` and the `<div class="ll">` wrapper
- **Fix `UpsertJobRun` data loss** — `INSERT OR REPLACE` wiped `finished_at`, `exit_code`, resource columns on re-upsert; replaced with targeted `ON CONFLICT DO UPDATE`; covered by `TestUpsertJobRun_DoesNotOverwriteExistingFields`
- **Warn at boot when `OIDC_SESSION_KEY` is empty** — previously a random key was generated silently, invalidating all sessions on restart

### Fixed

- **CronJob `spec.timeZone` is now honoured (DOM-1)** — next-run countdowns and
  missed-run detection are evaluated in the CronJob's own zone instead of the
  server's. A `spec.timeZone: America/New_York` job was previously shown with a
  countdown up to five hours off and could be flagged `missed` while perfectly
  healthy. `spec.timeZone` is persisted on `cronjobs` (migration 000004), the
  `schedule` package takes an IANA zone on every entry point, and the binary
  embeds `time/tzdata` because the distroless image ships no zoneinfo. When a
  schedule or zone cannot be resolved the row now shows `unresolved` and claims
  no missed run, rather than a confidently wrong countdown. DST transitions are
  covered by tests.
- **Deleted CronJobs and clusters no longer ghost forever (BUG-20)** — both are
  soft-deleted (migration 000005): they disappear from listings and their
  Prometheus series are dropped via `metrics.DeleteCronJobSeries`, while their
  run history is preserved and stays reachable by direct link. Deletions are
  caught three ways: the informer's `OnDelete` (including
  `DeletedFinalStateUnknown` tombstones), a startup reconciliation against the
  informer cache for CronJobs removed while KubeCron was down, and
  `MarkClustersDeletedExcept` for kubeconfigs removed from `KUBECONFIG_DIR`. A
  CronJob or cluster that reappears is revived. The retention job purges
  soft-deleted CronJob rows once their runs have aged out.
- **`findRunID` O(N×M) scan replaced with O(1) RunIndex** — `JobHandler` populates an in-memory `jobName → runID` map on every new Job; `PodHandler.findRunID` checks it first and falls back to DB only at startup (BUG-4)
- **`Manager.Load` no longer errors on `.gitkeep` and dotfiles** — file loop now skips dot-prefixed names and non-`.yaml`/`.yml` extensions (BUG-5)
- **Pod log/metric capture for Running pods at startup** — `PodHandler.OnAdd` now handles `PodRunning`; `retryOnAdd` goroutine works around the Job/Pod informer ordering race at boot
- **`backfillRun` non-deterministic pod selection** — `selectBestPod()` prefers the most recently created terminal pod instead of `pods.Items[0]`
- **Log triplication on multi-node/restart** — `backfillRun` skips log streaming when `log_size_bytes > 0` already

### Changed

- **Hot read path made index-friendly (PERF-2)** — migration 000006 adds
  `idx_job_runs_cronjob_started ON job_runs(cronjob_id, started_at DESC)`. The
  three reads behind every CronJob row (last run, 7-day stats, recent durations)
  each matched on `cronjob_id` and ordered by `started_at`, so SQLite sorted that
  CronJob's whole run history through a temp B-tree on every one — re-run every
  10 s per open tab by the HTMX poll. All three are now ordered index scans and
  the 7-day aggregate is index-covered. Measured on 500 CronJobs × 500 runs
  (250k rows): a page render's worth of queries drops from ~154 ms to ~38 ms.
  A batched window-function alternative was measured and rejected —
  `ROW_NUMBER() OVER (PARTITION BY … ORDER BY …)` cannot use an index for the
  partition ordering and came out 4–16× slower (~600 ms on the same data).
- **Row rendering no longer touches the store** — handlers gather their reads
  once per render through `Store.GetCronJobSummaries`, and `buildCronJobRow` is
  a pure function of what it is handed (making the DOM-1 regressions testable).
- **`CountRunningRuns`** replaces `GetRunningRuns` on page renders: an aggregate
  in SQLite instead of materialising every running row to count it.
- **CronJob rows show the schedule's time zone** when `spec.timeZone` is set — a
  schedule means nothing without the zone it is read in. `GET /api/clusters/{id}/cronjobs`
  gained a `time_zone` field, and `next_run_at` is computed in that zone.
- **`Store.Close()`** added and called on shutdown, so a clean exit checkpoints
  the WAL instead of leaving `-wal`/`-shm` files for the next start to recover.
- `GET /api/clusters/{id}/cronjobs` now omits `last_run`/`stats_7d` for a CronJob
  with no runs, where it previously emitted `null`.
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

- **`docs/PRODUCT.md`** — product strategy: why a viewer does not create
  dependency, the three levers that would (alerting with log context, a
  searchable archive of dead pods' logs, right-sizing/cost), a phased plan, and
  explicit anti-goals.
- **Test coverage** — `CronJobHandler` is now covered (timezone persistence,
  soft delete, tombstones, revival, reconciliation), closing part of TEST-1;
  plus schedule DST/zone cases, `GetCronJobSummaries` parity against the
  per-CronJob queries it fronts, the soft-delete/purge paths, a
  `BenchmarkGetCronJobSummaries` regression benchmark, and
  `TestOpen_UpgradesExistingDatabase`, which applies only the 0.1.0 migrations
  and then opens the database with current code — the upgrade path every
  existing deployment takes, and one no previous test exercised.
- **OIDC authorization** — `OIDC_ALLOWED_EMAILS` restricts which accounts may log in; `OIDC_OPERATOR_EMAILS` restricts suspend/resume/trigger to operators, everyone else is read-only (both optional, empty = no restriction)
- **Cursor-based pagination on run history** — `RunsList` loads 50 runs per page; "Load more" button appends the next page via HTMX (`beforeend` on `#runs-tbody`) with OOB button update; `ListJobRunsPaged` / `ListJobRunsByDay` added to storage
- **Heatmap "running" indicator** — days with at least one active run now show in blue (`#4299e1`) instead of red; tooltip shows running count; legend updated
- **Heatmap click-to-filter** — clicking any heatmap tile navigates to `?day=YYYY-MM-DD`; a filter chip with a "✕ clear" link appears above the run table; `GET /clusters/.../runs/more` partial endpoint added for pagination
- **Test coverage extended** — broadcaster pub/sub + concurrent stress test (`internal/streamer`); watcher `JobHandler` OnAdd/OnDelete tests with fake K8s client (`internal/watcher`); HTTP handler integration tests (`internal/api`); `go test ./...` passes

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
