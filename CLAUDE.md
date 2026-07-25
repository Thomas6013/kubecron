# KubeCron — CLAUDE.md

Context file for Claude Code. Covers architecture, commands, conventions, and definition of done.

---

## Project Overview

KubeCron is a **single-binary Go application** that monitors Kubernetes CronJob executions across multiple clusters. It provides live log streaming, run history, resource tracking, and CronJob control (suspend/resume/trigger) via a server-rendered web UI.

- **Language**: Go 1.26, stdlib `net/http` router (Go 1.22+ enhanced routing), `log/slog` structured logging
- **UI rendering**: Raw HTML via `fmt.Fprintf` in `internal/api/html.go` + `html_components.go` — no template engine, no Node.js build step
- **Database**: SQLite via `modernc.org/sqlite` (pure Go, no CGO, WAL mode, embedded migrations)
- **Kubernetes**: `k8s.io/client-go` informers (CronJob, Job, Pod), MetricsClient for resource sampling
- **Frontend**: HTMX 2.x (CDN) + custom CSS (`internal/ui/static/app.css`, embedded) + Chart.js (CDN) — no Node.js build step
- **Auth**: OIDC optional (`coreos/go-oidc/v3` + `golang.org/x/oauth2`); HMAC-signed session cookie, 24 h TTL
- **Metrics**: Prometheus via `prometheus/client_golang`; 6 wired metrics exposed at `/metrics`
- **Infra**: Two-stage Dockerfile (`golang:1.26-alpine` build → `gcr.io/distroless/static:nonroot`), CI with golangci-lint + SBOM + cosign; images currently `linux/amd64` only (arm64 planned — AUDIT INFRA-3)

---

## Repository Layout

```
cmd/kubecron/
  main.go                     # Bootstrap, config parsing (caarlos0/env), service wiring, graceful shutdown

internal/
  api/
    server.go                 # HTTP server setup, route registration
    middleware.go             # Logging, recovery, auth middleware
    csrf.go                   # CSRF double-submit cookie protection
    rate_limiter.go           # Fixed-window rate limiter (login, trigger)
    handlers_cluster.go       # Dashboard, ClusterDetail, NamespaceDetail pages
    handlers_cronjob.go       # Suspend/resume/trigger endpoints
    handlers_runs.go          # Run list, run detail, SSE stream, log download
    handlers_sse.go           # GET /api/runs/{id}/stream (SSE log streaming)
    html.go                   # Shared HTML head/foot, nav, breadcrumb, countdown, statusBadge
    html_components.go        # Reusable CronJob row, run row, action buttons, table headers
    html_log.go               # logSearchBar, logSearchJS
    html_chart.go             # sparklineSVG, heatmapHTML
  auth/
    auth.go                   # OIDC discovery, login/callback/logout, session management
  cluster/
    manager.go                # Load kubeconfigs, build per-cluster clients
    client.go                 # ClusterClient (Clientset + InformerFactory + MetricsClient)
    registry.go               # Thread-safe cluster registry
  watcher/
    controller.go             # Per-cluster informer controller
    cronjob.go                # CronJob informer event handlers
    job.go                    # Job informer event handlers (run tracking)
    pod.go                    # Pod informer event handlers (logs, metrics, status)
    run_index.go              # Thread-safe jobName→runID map (O(1) lookup for PodHandler)
  streamer/
    logstream.go              # Stream pod logs via client-go
    broadcaster.go            # In-memory pub/sub for SSE (run_id → subscribers)
  sampler/
    metrics_probe.go          # Probe Metrics API availability at startup + re-probe every 5 min
    resource_sampler.go       # Poll PodMetrics every METRICS_SAMPLE_INTERVAL s
  storage/
    db.go                     # SQLite connection, WAL mode, migrations
    models.go                 # Go structs for DB tables
    queries.go                # All SQL operations (upsert, list, update, delete)
    retention.go              # Background cleanup (delete runs older than RETENTION_DAYS)
  metrics/
    metrics.go                # Prometheus collectors (wired: runs_total, duration, last_status, suspended, next_run)
  schedule/
    next.go                   # Compute next N runs from cron expression
  ui/
    static/                   # Embedded CSS (app.css)

docs/
  AUDIT.md                    # Engineering audit, pass history and finding ids
  PRODUCT.md                  # Product strategy: dependency levers, phase plan, anti-goals

migrations/                   # SQL files embedded in binary via embed.FS
charts/kubecron/              # Helm chart (cluster installs)
  Chart.yaml
  values.yaml
  templates/
dev/kubeconfigs/              # Local dev kubeconfigs (gitignored content, directory committed)
```

---

## Key Commands

```bash
# Build
go build ./...

# Vet & lint
go vet ./...
golangci-lint run

# Test
go test ./...

# Run locally (kubeconfigs in dev/kubeconfigs/)
go run ./cmd/kubecron

# Docker Compose (live reload)
docker compose up --build
```

---

## Environment Variables

See `.env.example`. Key variables:

| Variable | Default | Description |
|---|---|---|
| `KUBECONFIG_DIR` | `/etc/kubecron/kubeconfigs` | One kubeconfig file per cluster |
| `DB_PATH` | `/data/kubecron.db` | SQLite database file |
| `PORT` | `8080` | HTTP listen port |
| `RETENTION_DAYS` | `90` | Days of run history (job_runs) to keep |
| `LOG_RETENTION_DAYS` | `14` | Days of raw log lines to keep (run metadata kept until RETENTION_DAYS) |
| `METRICS_SAMPLE_INTERVAL` | `15` | Resource sampling interval (seconds) |
| `OIDC_ENABLED` | `false` | Enable OIDC/SSO |
| `OIDC_ISSUER_URL` | _(empty)_ | OIDC provider issuer URL |
| `OIDC_CLIENT_ID` | _(empty)_ | OIDC client ID |
| `OIDC_CLIENT_SECRET` | _(empty)_ | OIDC client secret (store in K8s Secret) |
| `OIDC_REDIRECT_URL` | _(empty)_ | `https://<host>/auth/callback` — must be HTTPS |
| `OIDC_SESSION_KEY` | _(empty)_ | Any string ≥32 chars; SHA-256 derived internally (store in K8s Secret) |
| `OIDC_ALLOWED_EMAILS` | _(empty)_ | Comma-separated allow-list of emails permitted to log in (empty = any account) |
| `OIDC_OPERATOR_EMAILS` | _(empty)_ | Comma-separated emails allowed to suspend/resume/trigger; others read-only (empty = all operators) |

---

## Database Schema

Tables: `clusters`, `cronjobs`, `job_runs`, `resource_samples`, `log_lines`.

- `clusters` — one row per kubeconfig file; `metrics_enabled` flag set at startup and re-probed every 5 min; `deleted_at` set when the kubeconfig is removed
- `cronjobs` — synced from informer; stores schedule, `time_zone` (from `spec.timeZone`), suspended state, resource requests/limits, `deleted_at`
- `job_runs` — one row per CronJob execution; status: `running` → `succeeded`/`failed`; stores avg/max CPU+RAM
- `resource_samples` — raw PodMetrics samples (15 s interval) linked to a `job_run`
- `log_lines` — streamed pod log lines linked to a `job_run`

Migrations are embedded SQL files in `migrations/` applied at startup via `embed.FS`.

**Soft delete:** listings (`ListCronJobs`, `ListClusters`) filter on `deleted_at IS NULL`; `GetCronJobByName` deliberately does not, so existing links to the run history of a deleted CronJob keep working. Any new listing query must filter too.

**Indexes:** the CronJob-row read path (`GetLastJobRun`, `GetRunStats7d`, `GetRecentDurations`) depends on `idx_job_runs_cronjob_started(cronjob_id, started_at DESC)` to avoid a temp-B-tree sort per query. Check `EXPLAIN QUERY PLAN` before changing those queries' `WHERE`/`ORDER BY`.

---

## Security Model

- **Service Account RBAC** — `get`/`list`/`watch` on CronJobs, Jobs, Pods; `patch` on CronJobs; `create` on Jobs. No Secrets access.
- **Distroless image** — `gcr.io/distroless/static:nonroot`; runs as non-root.
- **No CGO** — `CGO_ENABLED=0`; pure Go binary.
- **OIDC session** — signed session cookie; session key stored in K8s Secret; never logged.
- **Authorization** — optional login allow-list (`OIDC_ALLOWED_EMAILS`) and operator role (`OIDC_OPERATOR_EMAILS`) restricting suspend/resume/trigger; others read-only.
- **CSRF & rate limiting** — double-submit cookie validated on every POST; fixed-window rate limits on `/auth/login` (10/min) and trigger (20/min) per source IP.
- **Kubeconfig data** — never logged server-side.

---

## Known Issues & Backlog

Full detail, evidence, and history: `docs/AUDIT.md` (IDs below reference it).

### Security — Medium Priority

- **SEC-22** — htmx/Chart.js/fonts loaded from CDNs without SRI; breaks air-gapped installs. Fix: vendor into `internal/ui/static/`.

### Security — Low Priority

- **SEC-23 (partial)** — nosniff/XFO/Referrer-Policy shipped; CSP (needs inline-script nonce refactor) and HSTS still missing.
- **SEC-24** — rate limiter proxy-blind (`RemoteAddr` behind ingress = collective lockout) with unbounded bucket map.
- **INFRA-1** — base images use floating tags. Fix: pin with `@sha256:...`.

### Infra & CI — Medium Priority

- **INFRA-3** — release images are `linux/amd64` only; the arm64 claim is not implemented in `docker-publish.yml` (no QEMU step). Implement or drop the claim.
- **INFRA-5** — CI is skipped entirely for `renovate[bot]`; once Renovate is enabled its PRs would merge untested.

### Performance — Medium Priority

- **PERF-1** — log lines stored in SQLite; `log_lines` can grow large before retention. Planned: S3 log storage backend.

### Testing — Medium Priority

- **TEST-1** — schedule (incl. timezone/DST), auth, storage (incl. the migration upgrade path), broadcaster, watcher (Job + CronJob handlers), HTTP handlers covered; `go test -race` runs in CI (`ci.yml`). Still missing: PodHandler, sampler, Streamer, cluster.Manager; no coverage threshold.

### Resolved (archived)

- **DOM-1** — `spec.timeZone` now honoured end to end _(v0.2.0)_. `schedule.{Parse,NextRun,PrevRun}` take an IANA zone; `cmd/kubecron` imports `time/tzdata` because distroless ships no zoneinfo. An unresolvable schedule or zone renders `unresolved` rather than a wrong countdown.
- **BUG-20** — deleted CronJobs/clusters are soft-deleted _(v0.2.0)_: hidden from listings, Prometheus series dropped, history preserved, revived on recreation. Caught by `OnDelete`, by a post-cache-sync `Reconcile` (for deletions that happened while KubeCron was down), and by `MarkClustersDeletedExcept` on kubeconfig load.
- **PERF-2** — fixed by indexing, not batching _(v0.2.0)_. Migration 000006's `job_runs(cronjob_id, started_at DESC)` removes the temp-B-tree sort behind each row read. A window-function batch was measured at 4–16× slower and rejected; see the comment above `GetCronJobSummaries` before attempting it again.

---

## Code Conventions

- **No CGO**: always build with `CGO_ENABLED=0`.
- **UI rendering**: all HTML is generated in `internal/api/html.go`, `html_components.go`, `html_log.go`, and `html_chart.go` via `fmt.Fprintf`. No template engine. When adding a new UI component, add a helper to `html_components.go` (reusable row/card) or the appropriate thematic file rather than inlining HTML in handlers.
- **SQL**: all queries in `internal/storage/queries.go`. New tables = new migration file in `migrations/`.
- **Schedules**: never evaluate a cron expression without its zone. Every `schedule` entry point takes an IANA zone name; pass `cj.TZ()`. On error, show nothing rather than a wrong time — a wrong countdown is worse than an absent one.
- **Error handling**: never return raw K8s API or DB errors to HTTP clients. Log with `slog.Error()`, return 500.
- **Secrets**: never log kubeconfig paths or content, OIDC secrets, or session keys.
- **Versioning**: update `CHANGELOG.md` and the `Version` constant in `internal/version/version.go` on every release.
- **RBAC**: any new K8s API access must be added to `charts/kubecron/templates/clusterrole.yaml`.
- **Docker images**: published only on `*.*.*` tag push — `git tag 0.2.0 && git push origin 0.2.0`.

---

## CI/CD Notes

- `ci.yml` runs on push/PR to `main`: `go build`, `go vet`, `go test` (+ `-race`), `golangci-lint`, `helm lint`. Skipped for `renovate[bot]` (see AUDIT INFRA-5).
- `docker-publish.yml` pushes to `ghcr.io/thomas6013/kubecron` on **every push to `main`** (tags `main`, `<commit-sha>`) and on `*.*.*` tag push (tags `latest`, `<git-tag>`, `<commit-sha>`).
- Platforms: `linux/amd64` only for now (arm64 pending — AUDIT INFRA-3).
- SBOM generated per release image with `anchore/sbom-action` (SPDX); release images signed with `sigstore/cosign` (keyless, OIDC-based).

---

## Definition of Done (Release Checklist)

### Build & quality
- [ ] `go build ./...` — no errors
- [ ] `go vet ./...` — no warnings
- [ ] `go test ./...` — all tests pass
- [ ] `golangci-lint run` — no lint errors
- [ ] `helm lint charts/kubecron` — no errors

### Version bump
- [ ] `internal/version/version.go` — update `Version` constant
- [ ] `charts/kubecron/Chart.yaml` — bump `appVersion` to match

### Helm chart
- [ ] If env vars changed: update `charts/kubecron/values.yaml` + `templates/deployment.yaml`
- [ ] If RBAC changed: update `charts/kubecron/templates/clusterrole.yaml`

### Documentation
- [ ] `CHANGELOG.md` — all changes documented under the new version with today's date
- [ ] `ROADMAP.md` — mark shipped items as `[x]` with the version tag
- [ ] `README.md` — update if user-facing features, env vars, or install steps changed
- [ ] `CLAUDE.md` — move resolved Known Issues to an archived section

### Git workflow
1. All changes committed on a feature branch
2. PR reviewed and merged into `main`
3. Tag pushed from `main`:
   ```bash
   git tag 0.X.Y && git push origin 0.X.Y
   ```
   The `docker-publish.yml` workflow triggers automatically on `*.*.*` tag push.

### Common pitfalls
- Release image tags (`latest`, `<version>`) publish **only on tag push** — the tag must exactly match the version bump (`main`/`<sha>` tags also publish on every main push)
- `helm lint` is now in CI (`helm-lint` job) — run it locally first to avoid a wasted CI run
- New Prometheus metrics must be wired (incremented/set) in the appropriate watcher or handler — declaring them in `metrics.go` is not enough
