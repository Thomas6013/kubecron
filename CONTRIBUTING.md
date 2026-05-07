# Contributing to KubeCron

Thank you for taking the time to contribute!

## Getting started

1. Fork the repository and clone your fork
2. Create a feature branch: `git checkout -b feat/my-feature`
3. Make your changes (see development setup below)
4. Open a pull request against `main`

## Development setup

### Prerequisites

- Go 1.26+
- [templ CLI](https://templ.guide/quick-start/installation) (`go install github.com/a-h/templ/cmd/templ@latest`)
- Docker + Docker Compose (optional)
- A Kubernetes cluster with at least one CronJob

### Run locally

```bash
# Place kubeconfig(s) in dev/kubeconfigs/
cp ~/.kube/config dev/kubeconfigs/local.yaml

# Generate templ files and run
templ generate
go run ./cmd/kubecron
```

Or with Docker Compose (includes live reload):

```bash
docker compose up --build
```

Open http://localhost:8080.

### Regenerate templ files

Any change to a `*.templ` file requires regeneration:

```bash
templ generate
```

The generated `*_templ.go` files must be committed alongside the `.templ` source.

## Project structure

```
cmd/kubecron/
  main.go               # Bootstrap, config, service wiring, graceful shutdown

internal/
  api/                  # HTTP server, routes, handlers, middleware
  auth/                 # OIDC login/callback/logout, session management
  cluster/              # Kubeconfig loading, per-cluster client, registry
  watcher/              # Kubernetes informers (CronJob, Job, Pod event handlers)
  streamer/             # Pod log streaming + SSE broadcaster
  sampler/              # metrics-server probe + resource sampling
  storage/              # SQLite: migrations, models, queries, retention
  metrics/              # Prometheus collectors
  schedule/             # Next-run computation from cron expressions
  ui/
    templates/          # *.templ source files + generated *_templ.go files
    static/             # Embedded CSS

migrations/             # SQL migration files (embedded in binary)
k8s/                    # Kubernetes manifests (namespace, RBAC, deployment, PVC, service)
```

## Guidelines

- **Single binary**: KubeCron has no frontend build step. UI changes go in `internal/ui/templates/*.templ`.
- **No CGO**: keep `CGO_ENABLED=0`. The SQLite driver (`modernc.org/sqlite`) is pure Go.
- **Database**: all SQL is in `internal/storage/queries.go`. New tables require a migration file in `migrations/`.
- **RBAC**: any new Kubernetes resource access must be added to `k8s/clusterrole.yaml`.
- **Error handling**: never return raw Kubernetes API errors to HTTP clients. Log server-side with `slog`, return generic messages.
- **Secrets**: never log kubeconfig data, OIDC secrets, or session keys.

## Versioning

When bumping a release, update the version constant:

1. `internal/version/version.go` — `Version` constant (displayed in the UI and logs)

Docker images are published via `docker-publish.yml` when a `*.*.*` git tag is pushed:

```bash
git tag 0.2.0 && git push origin 0.2.0
```

## Pull request checklist

- [ ] `go build ./...` passes
- [ ] `go vet ./...` passes
- [ ] `go test ./...` passes
- [ ] `templ generate` was run and generated files are committed
- [ ] New env vars are documented in `README.md`, `.env.example`, and `CLAUDE.md`
- [ ] `CHANGELOG.md` updated (if releasing)
- [ ] `k8s/clusterrole.yaml` updated if new K8s API access is required

## Code of Conduct

This project follows the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md).
Please read it before participating.

## Reporting bugs

Please use the [bug report template](.github/ISSUE_TEMPLATE/bug_report.yml).

## License

By contributing, you agree that your contributions will be licensed under the Apache 2.0 License.
