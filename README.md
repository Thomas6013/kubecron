# KubeCron

> Monitor and control Kubernetes CronJobs across multiple clusters — live logs, resource tracking, and a clean web dashboard.

[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26+-00ADD8.svg)](https://golang.org/)
[![Kubernetes](https://img.shields.io/badge/kubernetes-%E2%89%A51.21-326CE5.svg)](https://kubernetes.io/)

---

> **Alpha software** — Personal project maintained by a single developer with the help of an AI coding agent. Expect rough edges. Contributions welcome, response times may vary.

---

## Why?

CronJobs are invisible by default. You define a schedule, deploy it, and hope it runs correctly — but the only way to know is to `kubectl logs` into the right pod at the right time, across each cluster.

**KubeCron** gives you a single pane of glass:

- **Live log streaming** — watch CronJob pod logs in real time as they execute, via SSE
- **Run history** — every execution recorded with status, duration, exit code, and retry count
- **Resource usage** — CPU and memory sampled every 15 s when metrics-server is available; avg/max computed per run
- **7-day statistics** — success rate, average duration, p95 duration per CronJob
- **Next-run countdown** — computed from the cron expression, updated live in the browser
- **Suspend / Resume / Trigger** — control CronJobs directly from the UI without touching kubectl
- **Multi-cluster** — one kubeconfig file per cluster in a directory; all clusters shown in a unified dashboard
- **OIDC authentication** — optional SSO via Keycloak, Dex, Google, or any OIDC provider
- **Prometheus metrics** — `/metrics` endpoint for Grafana integration

---

## Requirements

| Requirement | Minimum version |
|---|---|
| Kubernetes | **1.21** (`batch/v1` CronJobs) |
| metrics-server | any (optional, enables live CPU/RAM tracking) |
| Go | 1.26+ (build only) |

---

## Install

### Helm (recommended)

```bash
# Encode your kubeconfig(s) in base64 — one per cluster
KC=$(kubectl config view --minify --raw | base64 -w0)

helm install kubecron oci://ghcr.io/thomas6013/charts/kubecron \
  --namespace kubecron --create-namespace \
  --set "kubeconfigs.data.my-cluster=$KC"
```

Or from source:

```bash
git clone https://github.com/thomas6013/kubecron.git
cd kubecron

KC=$(kubectl config view --minify --raw | base64 -w0)

helm install kubecron ./charts/kubecron \
  --namespace kubecron --create-namespace \
  --set "kubeconfigs.data.my-cluster=$KC" \
  --set config.retentionDays=30          # keep 30 days of run history
```

Key Helm values:

| Value | Default | Description |
|---|---|---|
| `config.retentionDays` | `7` | Days of run history (logs + metrics) to keep |
| `config.metricsSampleInterval` | `15` | Resource sampling interval (seconds) |
| `persistence.size` | `500Mi` | PVC size for SQLite data |
| `ingress.enabled` | `false` | Expose via Ingress |
| `oidc.enabled` | `false` | Enable OIDC authentication |

Full list of values: [`charts/kubecron/values.yaml`](charts/kubecron/values.yaml).

### Docker Compose (local)

```bash
git clone https://github.com/thomas6013/kubecron.git && cd kubecron

# Place kubeconfig files in dev/kubeconfigs/
cp ~/.kube/config dev/kubeconfigs/local.yaml

docker compose up --build
```

Open http://localhost:8080.

### Local dev

```bash
# Requires: Go 1.26+, templ CLI
go install github.com/a-h/templ/cmd/templ@latest

# Generate templ files then run
templ generate && go run ./cmd/kubecron
```

---

## Configuration

All configuration is via environment variables.

| Variable | Default | Description |
|---|---|---|
| `KUBECONFIG_DIR` | `/etc/kubecron/kubeconfigs` | Directory with one kubeconfig file per cluster |
| `DB_PATH` | `/data/kubecron.db` | SQLite database file path |
| `PORT` | `8080` | HTTP listen port |
| `RETENTION_DAYS` | `7` | How many days of run history to keep |
| `METRICS_SAMPLE_INTERVAL` | `15` | Resource sampling interval in seconds (requires metrics-server) |
| `OIDC_ENABLED` | `false` | Enable OIDC/SSO authentication |
| `OIDC_ISSUER_URL` | _(empty)_ | OIDC provider issuer URL |
| `OIDC_CLIENT_ID` | _(empty)_ | OIDC client ID |
| `OIDC_CLIENT_SECRET` | _(empty)_ | OIDC client secret (store in a K8s Secret) |
| `OIDC_REDIRECT_URL` | _(empty)_ | `https://<host>/auth/callback` — must be HTTPS in production |
| `OIDC_SESSION_KEY` | _(empty)_ | ≥32-char random string for session signing (store in a K8s Secret) |

See `.env.example` for a commented template.

---

## Architecture

```
Browser → Go HTTP server (port 8080)
            ├── Kubernetes API (informers: CronJob, Job, Pod)
            ├── metrics-server (optional — PodMetrics every 15s)
            ├── SQLite (runs, logs, resource samples)
            └── SSE broadcaster (live log streaming to browser)
```

KubeCron is a **single binary**. It connects directly to each Kubernetes cluster via kubeconfig files using `client-go` informers. No sidecar, no separate database process, no frontend build step.

- **Informers** watch CronJob, Job, and Pod events and update the SQLite database in real time.
- **Log streaming** uses `client-go` `GetLogs(Follow=true)` and broadcasts lines via an in-memory pub/sub to SSE subscribers.
- **Resource sampling** polls the Metrics API every `METRICS_SAMPLE_INTERVAL` seconds per running pod.
- **HTML templates** are compiled from `.templ` files at build time — no runtime template parsing.

---

## Security

- **Minimal RBAC** — the ClusterRole grants only `get`/`list`/`watch` on CronJobs, Jobs, and Pods, plus `patch` on CronJobs and `create` on Jobs (for suspend/resume/trigger). No access to Secrets.
- **Distroless runtime image** — `gcr.io/distroless/static:nonroot`, no shell, no package manager.
- **OIDC authentication** — when enabled, all routes require a valid session. The session key is never logged.
- **No token forwarding** — KubeCron uses its own Service Account, not user tokens. RBAC is enforced at the cluster level.
- **Structured logging** — `log/slog` JSON output, no sensitive data ever logged.

---

## API

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/clusters` | List clusters |
| `GET` | `/api/clusters/{id}/cronjobs` | List CronJobs with next-run, last-run, 7-day stats |
| `GET` | `/api/clusters/{id}/cronjobs/{ns}/{name}/runs` | Run history |
| `POST` | `/api/clusters/{id}/cronjobs/{ns}/{name}/suspend` | Suspend a CronJob |
| `POST` | `/api/clusters/{id}/cronjobs/{ns}/{name}/resume` | Resume a CronJob |
| `POST` | `/api/clusters/{id}/cronjobs/{ns}/{name}/trigger` | Trigger a manual run |
| `GET` | `/api/runs/{id}/stream` | SSE stream of live log lines |
| `GET` | `/api/runs/{id}/resources` | Resource sample time-series |
| `GET` | `/metrics` | Prometheus metrics |
| `GET` | `/healthz` | Health check (always 200) |
| `GET` | `/readyz` | Readiness (200 when informer caches synced) |

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Apache 2.0 — see [LICENSE](LICENSE).
