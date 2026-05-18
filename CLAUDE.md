# KubeCron — CLAUDE.md

Context file for Claude Code. Covers architecture, commands, conventions, and definition of done.

---

## Project Overview

KubeCron is a **single-binary Go application** that monitors Kubernetes CronJob executions across multiple clusters. It provides live log streaming, run history, resource tracking, and CronJob control (suspend/resume/trigger) via a server-rendered web UI.

- **Language**: Go 1.26, stdlib `net/http` router (Go 1.22+ enhanced routing), `log/slog` structured logging
- **Templates**: `a-h/templ` v0.3 — type-safe compiled HTML templates (no runtime parsing)
- **Database**: SQLite via `modernc.org/sqlite` (pure Go, no CGO, WAL mode, embedded migrations)
- **Kubernetes**: `k8s.io/client-go` informers (CronJob, Job, Pod), MetricsClient for resource sampling
- **Frontend**: HTMX 2.x + Tailwind CDN + Chart.js — no Node.js build step
- **Auth**: OIDC optional (`coreos/go-oidc/v3` + `golang.org/x/oauth2`)
- **Infra**: Multi-stage Dockerfile → `gcr.io/distroless/static:nonroot`, multi-arch (amd64+arm64), CI with golangci-lint + SBOM + cosign

---

## Repository Layout

```
cmd/kubecron/
  main.go                     # Bootstrap, config parsing (caarlos0/env), service wiring, graceful shutdown

internal/
  api/
    server.go                 # HTTP server setup, route registration
    middleware.go             # Logging, recovery, CORS, auth middleware
    handlers_cluster.go       # GET /api/clusters
    handlers_cronjob.go       # Suspend/resume/trigger endpoints
    handlers_runs.go          # Run list, stats endpoints
    handlers_sse.go           # GET /api/runs/{id}/stream (SSE log streaming)
    html.go                   # Server-rendered HTML page handlers
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
    metrics.go                # Prometheus collectors
  schedule/
    next.go                   # Compute next N runs from cron expression
  ui/
    templates/                # *.templ source files + generated *_templ.go (both committed)
    static/                   # Embedded CSS (app.css)

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
# Generate templ files (required after any *.templ change)
templ generate

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

---

## Database Schema

Tables: `clusters`, `cronjobs`, `job_runs`, `resource_samples`, `log_lines`.

- `clusters` — one row per kubeconfig file; `metrics_enabled` flag set at startup and re-probed every 5 min
- `cronjobs` — synced from informer; stores schedule, suspended state, resource requests/limits
- `job_runs` — one row per CronJob execution; status: `running` → `succeeded`/`failed`; stores avg/max CPU+RAM
- `resource_samples` — raw PodMetrics samples (15 s interval) linked to a `job_run`
- `log_lines` — streamed pod log lines linked to a `job_run`

Migrations are embedded SQL files in `migrations/` applied at startup via `embed.FS`.

---

## Security Model

- **Service Account RBAC** — `get`/`list`/`watch` on CronJobs, Jobs, Pods; `patch` on CronJobs; `create` on Jobs. No Secrets access.
- **Distroless image** — `gcr.io/distroless/static:nonroot`; runs as non-root.
- **No CGO** — `CGO_ENABLED=0`; pure Go binary.
- **OIDC session** — signed session cookie; session key stored in K8s Secret; never logged.
- **Kubeconfig data** — never logged server-side.

---

## Known Issues & Backlog

### Performance — Medium Priority

- **No pagination on run history** — large run lists sent in one response. Fix: cursor-based pagination.
- **Log lines stored in SQLite** — for high-volume CronJobs, the `log_lines` table can grow large before retention kicks in. Planned: S3 log storage backend.

### Testing — Medium Priority

- **No unit tests** — storage queries, schedule computation, resource parsing untested. Fix: add table-driven tests.
- **No integration tests** — informer event handling untested without a real cluster. Fix: fake client-go client.

### Security — Low Priority

- **Base images use floating tags** — `golang:1.26` and `gcr.io/distroless/static:nonroot` in Dockerfile. Fix: pin with `@sha256:...`.
- **Missing `seccompProfile: RuntimeDefault`** in `charts/kubecron/templates/deployment.yaml`. Fix: add to pod security context.

---

## Code Conventions

- **No CGO**: always build with `CGO_ENABLED=0`.
- **templ**: always run `templ generate` after editing `.templ` files. Commit both `*.templ` and `*_templ.go`.
- **SQL**: all queries in `internal/storage/queries.go`. New tables = new migration file in `migrations/`.
- **Error handling**: never return raw K8s API or DB errors to HTTP clients. Log with `slog.Error()`, return 500.
- **Secrets**: never log kubeconfig paths or content, OIDC secrets, or session keys.
- **Versioning**: update `CHANGELOG.md` and the `Version` constant in `internal/version/version.go` on every release.
- **RBAC**: any new K8s API access must be added to `charts/kubecron/templates/clusterrole.yaml`.
- **Docker images**: published only on `*.*.*` tag push — `git tag 0.2.0 && git push origin 0.2.0`.

---

## CI/CD Notes

- `ci.yml` runs on push/PR to `main`: `templ generate`, `go build`, `go vet`, `go test`, `golangci-lint`. Skipped for `renovate[bot]`.
- `docker-publish.yml` builds and pushes to `ghcr.io/thomas6013/kubecron` on `*.*.*` tag push only.
- Image tags: `latest`, `<git-tag>`, `<commit-sha>`.
- Multi-arch: `linux/amd64` + `linux/arm64` via QEMU + buildx.
- SBOM generated per image with `anchore/sbom-action` (SPDX format).
- Images signed with `sigstore/cosign` (keyless, OIDC-based).

---

## Definition of Done (Release Checklist)

### Build & quality
- [ ] `templ generate` — no errors, generated files committed
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
- Forgetting `templ generate` — CI catches it but costs a round-trip
- Docker images publish **only on tag push** — the tag must exactly match the version bump
- `helm lint` is now in CI (`helm-lint` job) — run it locally first to avoid a wasted CI run
