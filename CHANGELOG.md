# Changelog

All notable changes to KubeCron are documented here.

---

## [Unreleased]

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
