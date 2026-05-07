# Roadmap

Living checklist of potential next steps for KubeCron, roughly ordered by priority.
Items are not committed to any timeline — this is a solo side project.

## Infrastructure & Distribution

- [x] **Multi-arch Docker builds** — `linux/amd64` + `linux/arm64` _(v0.0.1)_
- [x] **Distroless runtime image** — `gcr.io/distroless/static:nonroot` _(v0.0.1)_
- [x] **Helm chart** — parameterized chart with ingress, TLS, resource limits, OIDC secrets, retention _(v0.0.1)_
- [ ] **Publish Helm chart to OCI / Artifact Hub** — push `charts/kubecron` to GHCR OCI registry and submit to Artifact Hub
- [ ] **GitHub Container Registry visibility** — ensure GHCR images are public once the repo is public
- [ ] **Automated GitHub Releases** — workflow that creates a GitHub Release from CHANGELOG entries on tag push
- [ ] **Pin base image digests** — `golang:1.26@sha256:...` and `gcr.io/distroless/static:nonroot@sha256:...` in Dockerfile

## Features

- [x] **Live log streaming** — SSE-based real-time pod log viewer _(v0.0.1)_
- [x] **Run history** — every execution recorded with status, duration, exit code _(v0.0.1)_
- [x] **Resource tracking** — CPU/memory sampled via metrics-server, avg/max per run _(v0.0.1)_
- [x] **Suspend / Resume / Trigger** — CronJob control from the dashboard _(v0.0.1)_
- [x] **OIDC authentication** _(v0.0.1)_
- [x] **Prometheus metrics** _(v0.0.1)_
- [x] **Missed run detection** — `missed` badge when a CronJob skipped a scheduled tick _(v0.1.0)_
- [x] **Concurrent run detection** — `⚠ concurrent` badge when multiple runs are active simultaneously _(v0.1.0)_
- [x] **Log level colorization** — FATAL/ERROR/WARN/INFO/DEBUG coloring in the log terminal _(v0.1.0)_
- [x] **Log search** — regex filter with match highlighting and line count _(v0.1.0)_
- [x] **Duration sparkline** — 20-run SVG sparkline alongside the 7-day success ratio _(v0.1.0)_
- [x] **Log download** — plain-text `.log` download for any run _(v0.1.0)_
- [x] **Calendar heatmap** — 90-day per-day success/failure heatmap on the run list page _(v0.1.0)_
- [ ] **S3 log storage** — store log lines in S3/object storage instead of SQLite for unlimited retention
- [ ] **Alerting** — webhook or email notification when a CronJob fails or is missing
- [ ] **Grafana dashboard** — pre-built dashboard JSON for `kubecron_*` metrics
- [ ] **Pagination** — cursor-based pagination for large run history lists
- [ ] **CronJob annotations** — display description, owner, runbook link from K8s annotations
- [ ] **Dark / light mode toggle**

## Code Quality

- [x] **golangci-lint in CI** _(v0.0.1)_
- [x] **helm lint in CI** _(v0.0.1)_
- [ ] **Unit tests** — resource parsing, schedule computation, storage queries
- [ ] **Integration tests** — informer event handling with fake client
- [ ] **Renovate** — automated dependency updates for Go modules and GitHub Actions
- [ ] **seccompProfile: RuntimeDefault** — add to pod security context in k8s/ and Helm chart

## Documentation

- [ ] **Screenshot in README** — dashboard screenshot
- [ ] **OIDC setup guide** — `docs/oidc.md` with Keycloak, Dex, and Google examples
- [ ] **Grafana integration guide** — how to import the bundled dashboard
