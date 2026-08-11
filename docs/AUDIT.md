# KubeCron — Engineering Audit

> Current audit pass: **2026-08-11**
> Previous audit pass: **2026-07-08**
> Framework: full-tree pass across security, correctness, maintainability, DRY, dead code, altitude, performance, testing, docs compliance, supply chain, domain correctness, observability, and Kubernetes-native shape — each weighted by relevance to this project.

<!-- Historical header from the bootstrap pass, kept for provenance: -->
> Bootstrap pass: **2026-07-08**
> Previous audit pass: _none in this document_ — an earlier audit existed (finding IDs `SEC-1..11+`, `BUG-1..14+` are referenced in `CHANGELOG.md` and code comments) but its document was never committed. This file re-bootstraps the audit trail; prior IDs are treated as retired and **never reused** — new findings start at `SEC-20` / `BUG-20`, other families at 1.
> Framework: full-tree pass across security, correctness, maintainability, DRY, dead code, altitude, performance, testing, docs compliance, supply chain, domain correctness, observability, and Kubernetes-native shape — each weighted by relevance to this project.

## Run History

| Pass date | Model | Score | HIGH open | New findings | Closures | Notes |
|---|---|---|---|---|---|---|
| 2026-07-08 | Claude Fable 5 | 7.5/10 | 0 | 26 (0 H / 6 M / 13 L / 7 I) | 6 prior-pass verified fixed | Baseline document; prior audit doc lost, carry-overs reconstructed from CHANGELOG/CLAUDE.md |
| 2026-08-11 | Claude Opus 5 | 7.5/10 | **1** | 6 (1 H / 3 M / 1 L / 1 I) | 4 in-pass (OBS-3, OBS-4, INFRA-4, MAINT-2) + 3 verified closed from v0.2.0 (DOM-1, BUG-20, PERF-2) | First HIGH in this document's history (SEC-28, chart-level). `govulncheck` run for the first time. Overview/metrics feature reviewed as part of the pass. |

## Severity Classification Rubric

| Severity | Definition | Response |
|---|---|---|
| **CRITICAL** | Active exploit possible now: RCE, full account takeover, mass data exfiltration, leaked prod credentials, ransomable data path. | Block all merges; hotfix within 24 h. |
| **HIGH** | Single-step exploit with meaningful impact, or a single fault crashes the service for all users; cross-tenant leak. | Block release; fix within 1 week. |
| **MEDIUM** | Self-inflicted DoS by one actor, partial same-scope info leak, missing rate limit, real-but-low-probability race, missing observability that will hide a future incident. | Fix within 1 sprint. |
| **LOW** | Best-practice violation with no exploitable impact today, minor info disclosure, deprecated-but-present API. | Address opportunistically; carry across passes. |
| **INFO** | Style, factorisation, future-proofing, architectural opinion. | Track only, no SLA. |

**Heuristic when in doubt:** single unauthenticated request exploits it → CRITICAL; one authenticated tenant against another → HIGH; one actor degrades service for others without a bug → MEDIUM; needs collusion/insider/physical access → LOW/INFO.

## Overall Assessment

### Pass 2026-08-11

**Score: 7.5/10 (unchanged — but the composition moved).** Two of the previous pass's most damaging MEDIUMs are gone for good (DOM-1, BUG-20, both closed in v0.2.0 and re-verified here), the observability story improved materially this pass, and lint is finally deterministic. Against that, this pass opens the **first HIGH in this document's history** — not in the Go code, which remains sound, but in the Helm chart's exposure model. Net: same number, healthier core, one blocking item.

**The HIGH (SEC-28).** `oidc.enabled` and `ingress.enabled` both default to `false`, so a default install is safe (ClusterIP, port-forward only). But enabling the Ingress is the documented way to actually use the UI, and nothing — not a chart `fail`, not `NOTES.txt`, not a startup log — objects when it is turned on with OIDC still off. The result is an internet-reachable endpoint where a single unauthenticated `POST` suspends or triggers CronJobs across **every connected cluster**. The authorization code is correct; the packaging simply never forces the operator to notice that they have turned it off. `NOTES.txt` even prints the public URL with no caveat.

**Observability was the weakest dimension and is now the most improved.** OBS-3 (found and fixed in this pass) was the significant one: every gauge-valued series was written only by live watcher events, so after each restart `kubecron_last_run_status` and friends had *no series at all* until each CronJob next happened to fire — up to 24 h for a nightly backup, precisely when a restart makes alerting most valuable. Verified empirically: a fresh process exposed 2 of 6 metric families. A DB-derived state collector now makes those gauges a function of stored state rather than of process uptime, and the previously-unexported "missed run" verdict — the single best signal that a schedule silently stopped firing — is now a metric.

**Supply chain got its first real look (SUP-1).** `govulncheck` had never been run against this repo. It reports 14 reachable stdlib advisories at the pinned-minimum toolchain (`go 1.26.0`). The *released* image is likely unaffected because `golang:1.26-alpine` floats to a patched 1.26.x — which makes INFRA-1 (pin base images by digest) a trap rather than a plain win: pinning a digest with no CVE gate in CI would freeze a vulnerable stdlib into every future release. These two findings must be resolved together, gate first.

**Dimensions n/a for this project** (unchanged): multi-tenant isolation (single-tenant ops tool by design); public-production/GDPR (no end-user PII beyond operator emails); microservice decomposition (the monolith is correct and justified in-chart).

**Biggest forward risk:** shifted from doc drift to **packaging-vs-code drift**. The Go code enforces a careful operator/viewer split that the chart lets you switch off wholesale without a word. Doc drift, last pass's top risk, is materially better — CLAUDE.md now matches the code on timezone handling, soft deletes and indexing.

### Pass 2026-07-08 (bootstrap)

**Score: 7.5/10 (baseline for this document).** KubeCron is a well-shaped single-binary Go service: clean package boundaries (`api`/`auth`/`watcher`/`streamer`/`sampler`/`storage`), a justified monolith (one scaling axis, one SQLite writer), disciplined SQL in one file, migrations embedded and transactional, and a previous audit round that demonstrably landed (CSRF, rate limiting, open-redirect fix, XSS fix in SSE replay, upsert data-loss fix — all verified in code this pass). Zero CRITICAL/HIGH findings.

What keeps it off 8+: a cluster of MEDIUMs — no HTTP server timeouts (Slowloris), raw Kubernetes errors returned to clients in violation of the repo's own convention, all frontend assets loaded from third-party CDNs without SRI (a CDN compromise is full XSS on an authenticated ops tool, and air-gapped clusters render unstyled/broken), deleted CronJobs/clusters ghosting in the UI and Prometheus forever, `spec.timeZone` silently ignored (wrong next-run/missed verdicts — the product's output of record), and a multi-arch claim in the docs that the release workflow does not implement.

**Dimensions n/a for this project:** multi-tenant isolation (single-tenant ops tool; all logged-in users see all clusters by design — the operator-email split is the only privilege boundary and it works); public-production/GDPR (no end-user PII beyond operator emails in OIDC sessions); microservice decomposition (monolith is the right call and the deployment justifies Recreate strategy in a comment).

**Biggest forward risk:** doc drift. The CI/CD reality (branch-push images, amd64-only, race detector present) has diverged from CLAUDE.md/ROADMAP in at least five places this pass — an agent-driven repo whose agent instructions are wrong compounds errors.

## 1. Audit Tracking Table

Prior-audit IDs (document lost) reconstructed from CHANGELOG/code references, then verified at cited locations this pass. New IDs start at SEC-20/BUG-20 to guarantee no collision with retired numbers.

| ID | Pass found | Severity | Area | Title | Status 2026-07-08 | Test covering fix? |
|---|---|---|---|---|---|---|
| SEC-5 | pre-2026-07 | MED | api | Global CORS `*` middleware | FIXED — no CORS code in `middleware.go` | — |
| SEC-6 | pre-2026-07 | MED | api | No CSRF protection on POSTs | FIXED — `csrf.go` double-submit + HTMX hook `html.go:71` | `handlers_test.go` |
| SEC-11 | pre-2026-07 | MED | api | No rate limit on login/trigger | FIXED — `rate_limiter.go`, wired `server.go:60` | — |
| BUG-4 | pre-2026-07 | MED | watcher | `findRunID` O(N×M) scan | FIXED — `run_index.go`, fallback `pod.go:192` | `job_test.go` |
| BUG-5 | pre-2026-07 | LOW | cluster | `Manager.Load` errored on dotfiles | FIXED — skip logic `manager.go:55-62` | — |
| BUG-14 | pre-2026-07 | MED | streamer | Scanner 64 KB token cap killed stream | FIXED — `bufio.Reader` `logstream.go:125` | — |
| PERF-1 | pre-2026-07 | MED | storage | `log_lines` unbounded growth in SQLite | OPEN — S3 backend planned (ROADMAP) | — |
| INFRA-1 | pre-2026-07 | LOW | docker | Floating base-image tags (no digest pin) | OPEN — `Dockerfile:2,10` | — |
| INFRA-2 | pre-2026-07 | LOW | helm | `seccompProfile: RuntimeDefault` missing | FIXED 2026-07-09 — `values.yaml` podSecurityContext | helm lint |
| TEST-1 | pre-2026-07 | MED | tests | Partial test coverage | PARTIAL — api/storage/auth/broadcaster/watcher(Job)/schedule covered; `-race` now in CI (`ci.yml`); missing: CronJob/Pod handlers, sampler, Streamer, cluster.Manager; no coverage threshold | n/a |
| SEC-20 | 2026-07-08 | MED | api | No `http.Server` timeouts (Slowloris) | FIXED 2026-07-09 — `server.go` ReadHeaderTimeout/IdleTimeout | no |
| SEC-21 | 2026-07-08 | MED | api | Raw K8s error returned by Suspend/Resume | FIXED 2026-07-09 — `handlers_cronjob.go` logs + generic msg, 404 on unknown cluster | `TestSuspend_ClusterNotFound` |
| SEC-22 | 2026-07-08 | MED | ui | CDN scripts/fonts without SRI, breaks air-gapped | OPEN | no |
| SEC-23 | 2026-07-08 | LOW | api | No security headers (CSP/XCTO/XFO/HSTS) | PARTIAL 2026-07-09 — nosniff/XFO/Referrer-Policy via `SecurityHeaders`; CSP (needs nonce refactor) + HSTS deferred | no |
| SEC-24 | 2026-07-08 | LOW | api | Rate limiter: proxy-blind IP key + unbounded map | OPEN | no |
| SEC-25 | 2026-07-08 | LOW | auth | CSRF cookie not `Secure`; logout via GET | FIXED 2026-07-09 — `EnsureCSRFCookie(secure)`, `POST /auth/logout` + HX-Redirect | no |
| SEC-26 | 2026-07-08 | LOW | helm | ClusterRole broader than documented minimum | FIXED 2026-07-09 — split rules in `clusterrole.yaml` | helm lint |
| SEC-27 | 2026-07-08 | INFO | ui | HTML-escaping used in JS string contexts | OPEN | no |
| BUG-20 | 2026-07-08 | MED | watcher | Deleted CronJobs/clusters never cleaned (UI + Prometheus ghosts) | FIXED 2026-07-25 — `deleted_at` (migration 000005), `CronJobHandler.OnDelete` + tombstones + startup `Reconcile`, `MarkClustersDeletedExcept`, `metrics.DeleteCronJobSeries`, retention purge | `cronjob_test.go`, `summaries_test.go` |
| BUG-21 | 2026-07-08 | LOW | storage | Mixed timestamp formats in SQL comparisons; cursor same-second skip | OPEN | no |
| BUG-22 | 2026-07-08 | LOW | main | Server bind failure doesn't exit the process | OPEN | no |
| BUG-23 | 2026-07-08 | LOW | sampler | `metrics_enabled` sticky-true; probe has no disable path | OPEN | no |
| BUG-24 | 2026-07-08 | INFO | watcher | Backfilled failed runs get `exit_code=0` on pod-lookup failure | OPEN | no |
| BUG-25 | 2026-07-08 | LOW | api | DownloadLogs: no existence check, whole log in RAM, raw filename | OPEN | no |
| DOM-1 | 2026-07-08 | MED | schedule | CronJob `spec.timeZone` ignored → wrong next-run & false "missed" | FIXED 2026-07-25 — zone persisted (migration 000004), `schedule.{Parse,NextRun,PrevRun}` take an IANA zone, `time/tzdata` embedded, `unresolved` shown instead of a wrong countdown | `next_test.go` (incl. DST), `cronjob_row_test.go`, `cronjob_test.go` |
| PERF-2 | 2026-07-08 | LOW | api | N+1 (3 queries/CronJob) per page render × 10 s HTMX poll; PrevRun tick-scan | FIXED 2026-07-25 — composite index `job_runs(cronjob_id, started_at DESC)` (migration 000006) removes all three temp-B-tree sorts; ~154→38 ms at 500 CJ × 500 runs. Reads gathered behind `GetCronJobSummaries`; batching via window functions measured 4–16× *slower* and rejected. PrevRun tick-scan unchanged | `summaries_test.go` parity + `BenchmarkGetCronJobSummaries` |
| PERF-3 | 2026-07-08 | INFO | api/storage | SSE 1 Hz DB poll per viewer; `datetime(started_at)` defeats index | OPEN | no |
| OBS-1 | 2026-07-08 | LOW | api | Auth middleware outermost → unauthenticated requests unlogged; no request ID | OPEN | no |
| OBS-2 | 2026-07-08 | LOW | watcher | No informer-health signal — dead watch looks like "no runs" | OPEN | no |
| INFRA-3 | 2026-07-08 | MED | ci | Releases build `linux/amd64` only — multi-arch claim not implemented | OPEN | no |
| INFRA-4 | 2026-07-08 | LOW | ci | golangci-lint `@latest` unpinned; no `.golangci.yml` | OPEN | no |
| INFRA-5 | 2026-07-08 | LOW | ci | CI skipped entirely for `renovate[bot]` → dep PRs untested | OPEN | no |
| MAINT-1 | 2026-07-08 | INFO | ui | `htmlHead` title param discarded — every page titled "KubeCron" | OPEN | no |
| DOC-1 | 2026-07-08 | LOW | docs | "Images publish only on tag push" false — workflow also pushes on `main` | FIXED 2026-07-08 — CLAUDE.md corrected this pass | n/a |
| DOC-2 | 2026-07-08 | LOW | docs | Multi-arch (arm64) claimed in CLAUDE.md/ROADMAP, not built | PARTIAL — docs annotated; resolve via INFRA-3 (implement or unclaim) | n/a |
| DOC-3 | 2026-07-08 | LOW | docs | CLAUDE.md: "Tailwind CDN" (none used), "single-stage Dockerfile" (two stages) | FIXED 2026-07-08 — CLAUDE.md corrected this pass | n/a |
| DOC-4 | 2026-07-08 | LOW | docs | CLAUDE.md claimed `go test -race` missing — present in `ci.yml` | FIXED 2026-07-08 — CLAUDE.md corrected this pass | n/a |
| DOC-5 | 2026-07-08 | LOW | docs | `docker-compose.yaml` comments in French (house rule: English); README says `:8080`, compose exposes `:8082` | OPEN | n/a |
| SEC-28 | 2026-08-11 | **HIGH** | helm | `ingress.enabled=true` + `oidc.enabled=false` exposes unauthenticated suspend/resume/trigger across every cluster; no chart guard, no NOTES warning, no startup log | **OPEN** | no |
| SUP-1 | 2026-08-11 | MED | ci | No CVE gate (`govulncheck`/image scan) in CI; 14 reachable stdlib advisories at pinned-minimum toolchain. Must be fixed **before** INFRA-1 digest-pinning | OPEN | no |
| SEC-29 | 2026-08-11 | LOW | api | `/metrics` is auth-exempt (`auth.go:172`) and now exposes full cluster/namespace/CronJob inventory, schedules, run outcomes and resource usage | OPEN | no |
| OBS-3 | 2026-08-11 | MED | metrics | Gauge series written only by live watcher events → absent after every restart until each CronJob next fires (verified: 2 of 6 families on a fresh process) | FIXED 2026-08-11 — `metrics.StateCollector` derives them from stored state every 30 s; counters/histogram stay event-driven | `fleet_test.go`, `missed_test.go` (rule shared with UI) |
| OBS-4 | 2026-08-11 | MED | metrics | No series for in-flight runs, missed schedules, last-run duration/resources, per-cluster inventory, Metrics-API health, build info, or HTTP traffic | FIXED 2026-08-11 — 8 new families in `metrics.go`; HTTP labelled by route pattern, not path, to bound cardinality | `nav_test.go` (helpers), manual scrape verified |
| INFRA-4 | 2026-07-08 | LOW | ci | golangci-lint `@latest` unpinned; no `.golangci.yml` | FIXED 2026-08-11 — root cause was the **v1 module path**: `@latest` there can never resolve v2, so CI silently froze on the last v1 while local v2 reported 15 findings CI never saw. `.golangci.yml` (v2, explicit exclusion presets) + CI installs `/v2/` | lint gate now 0 issues |
| MAINT-2 | 2026-08-11 | INFO | api | Missed-run rule duplicated between the UI row builder and any future consumer | FIXED 2026-08-11 — extracted to `schedule.IsMissed`, now shared by the UI badge and `kubecron_cronjob_missed` | `missed_test.go` |

## 1.B Audit Coverage Matrix

| Path / area | Status |
|---|---|
| `cmd/kubecron/main.go` | reviewed 2026-07-08 |
| `internal/api/server.go`, `middleware.go`, `csrf.go`, `rate_limiter.go` | reviewed 2026-07-08 |
| `internal/api/handlers_cluster.go`, `handlers_cronjob.go`, `handlers_runs.go`, `handlers_sse.go` | reviewed 2026-07-08 |
| `internal/api/html.go`, `html_components.go`, `html_log.go`, `html_chart.go` | reviewed 2026-07-08 |
| `internal/auth/auth.go` | reviewed 2026-07-08 |
| `internal/cluster/manager.go`, `client.go`, `registry.go` | reviewed 2026-07-08 |
| `internal/watcher/controller.go`, `cronjob.go`, `job.go`, `pod.go`, `run_index.go` | reviewed 2026-07-08 |
| `internal/streamer/logstream.go`, `broadcaster.go` | reviewed 2026-07-08 |
| `internal/sampler/metrics_probe.go`, `resource_sampler.go` | reviewed 2026-07-08 |
| `internal/storage/db.go`, `queries.go`, `retention.go`, `models.go` (via scans) | reviewed 2026-07-08 |
| `internal/metrics/metrics.go`, `internal/schedule/next.go` | reviewed 2026-07-08 |
| `migrations/*.sql`, embed files | reviewed 2026-07-08 |
| `charts/kubecron/` (deployment, clusterrole, values; others by listing) | reviewed 2026-07-08 |
| `.github/workflows/ci.yml`, `docker-publish.yml` | reviewed 2026-07-08 |
| `Dockerfile`, `docker-compose.yaml`, `go.mod` | reviewed 2026-07-08 |
| Docs: `README.md`, `CLAUDE.md`, `ROADMAP.md`, `CHANGELOG.md`, `.env.example` | reviewed 2026-07-08 (harmonised same day, pre-audit) |
| `internal/ui/static/app.css`, favicons | skipped — cosmetic CSS, no logic |
| `*_test.go` (6 files, 1396 lines) | skimmed for coverage mapping only, not line-reviewed |
| Helm templates: `ingress.yaml`, `pvc.yaml`, `service*.yaml`, `secret-oidc.yaml`, `_helpers.tpl`, `NOTES.txt` | reviewed 2026-08-11 — **closed last pass's gap; this is where SEC-28 was found** |
| `internal/metrics/collector.go` (new) | reviewed 2026-08-11 |
| `internal/api/html_overview.go` (new), `handlers_cluster.go` overview path | reviewed 2026-08-11 |
| `internal/schedule/next.go` — `IsMissed` extraction | reviewed 2026-08-11 |
| `.golangci.yml` (new) | reviewed 2026-08-11 |
| Supply chain — `govulncheck ./...` | reviewed 2026-08-11 — first run; see SUP-1 |
| `internal/ui/static/app.css`, favicons | skipped — presentation only, no logic (re-confirmed 2026-08-11) |
| `*_test.go` (10 files) | skimmed for coverage mapping 2026-08-11; still not line-reviewed for assertion quality |

**Coverage gaps the next pass must close:** line-review the test files for assertion quality (not just existence) — carried from the previous pass and still open; run an **image** CVE scan (Trivy/Grype) against the published image, which `govulncheck` does not cover; line-review `internal/ui/static/app.css` once it stops being purely presentational.

## 2. New findings (2026-08-11) — detail

### Highlights since the last pass

`v0.2.0` closed the two findings that made the dashboard *lie* — DOM-1 (timezone-aware schedules) and BUG-20 (soft-deleted CronJobs/clusters) — plus PERF-2 by indexing. All three re-verified at their cited locations this pass. This pass adds the fleet/cluster summary views, ten metric families, and the state collector; and closes INFRA-4 after diagnosing why it had never actually bitten.

### HIGH

**SEC-28 — Ingress without OIDC is an unauthenticated control plane, and nothing says so.**
`charts/kubecron/values.yaml:23` (`oidc.enabled: false`) and `:71` (`ingress.enabled: false`) are both safe defaults in isolation: with no Ingress, the Service is ClusterIP and `NOTES.txt` tells the operator to `port-forward`. The failure is the *combination*, which is also the documented path to actually using the product — README lists `ingress.enabled` as the way to "Expose via Ingress" (`README.md:80`) with no mention of authentication.

When `authenticator` is nil, `server.go:81` makes the operator gate a pass-through by design:

```go
operator := func(next http.Handler) http.Handler { return next }
if s.authenticator != nil {
    operator = s.authenticator.RequireOperator
}
```

so `POST /api/clusters/{id}/cronjobs/{ns}/{name}/suspend|resume|trigger` are reachable by anyone who can reach the Ingress. A single unauthenticated request can suspend a backup CronJob — or trigger one repeatedly — **on every cluster whose kubeconfig is mounted**. `auth.Middleware` is never installed at all in this mode, so there is no redirect-to-login either. Grep-verified: no `slog.Warn` anywhere fires when OIDC is disabled, and `NOTES.txt` prints `https://{{ .Values.ingress.host }}` unconditionally.

Aggravating: `ingress.tls` defaults to `[]`, so the template emits no `tls:` block — the default Ingress is plaintext HTTP.

Fix (all three, they are independent defences):
1. Guard the chart — refuse the combination unless explicitly acknowledged:
   ```yaml
   {{- if and .Values.ingress.enabled (not .Values.oidc.enabled) (not .Values.ingress.acknowledgeInsecure) }}
   {{- fail "ingress.enabled=true with oidc.enabled=false exposes unauthenticated suspend/resume/trigger on every connected cluster. Set oidc.enabled=true, or ingress.acknowledgeInsecure=true if the Ingress is on a trusted network." }}
   {{- end }}
   ```
2. Log it at startup in `cmd/kubecron/main.go`, once, at `Warn`: OIDC disabled → all endpoints including mutating ones are unauthenticated.
3. Say it in `NOTES.txt` and in the README's ingress row.

### MEDIUM

**SUP-1 — No vulnerability gate anywhere in CI, and INFRA-1 would make that worse.**
`.github/workflows/` contains no `govulncheck`, Trivy, Grype or Snyk step (grep-verified). Run manually this pass, `govulncheck ./...` reports **14 stdlib advisories with reachable call paths**, against the `go 1.26.0` floor — including `GO-2026-4918` (infinite loop in the HTTP/2 transport on a malformed `SETTINGS_MAX_FRAME_SIZE`, fixed in 1.26.3) and several `crypto/x509` DoS paths, all reached through `api.Server.Start`.

The released image is *probably* fine: `Dockerfile:2` uses `golang:1.26-alpine`, a floating tag that resolves to a patched 1.26.x at build time. That is luck, not policy — and it means **INFRA-1 (pin base images by digest) is a trap if done first**: pinning a digest without a CVE gate freezes whatever stdlib that digest carries into every subsequent release, converting today's accidental safety into a permanent exposure. Sequence the fixes: add the gate, then pin, then let Renovate move the digest.

Fix: add a `govulncheck ./...` step to `ci.yml` (fails the build on reachable advisories) and an image scan to `docker-publish.yml`; only then pin digests.

**OBS-3 — Gauge metrics did not survive a restart.** *(found and fixed this pass)*
Before this pass every gauge was written exclusively from live events — `RecordCompletion` on pod completion (`pod.go:170`) and the CronJob informer (`cronjob.go:152,164`). Nothing repopulated them from the database. Verified empirically against a live cluster: a freshly started process exposed only `kubecron_cronjob_suspended` and `kubecron_next_run_timestamp` (the two the informer refires at cache sync). `kubecron_last_run_status`, `kubecron_last_run_timestamp`, `kubecron_job_runs_total` and `kubecron_job_duration_seconds` had **no series at all**.

The consequence is worst exactly when it matters: an alert like `kubecron_last_run_status == 1` silently stops evaluating after a restart, and `absent()` rules either fire spuriously or mask a genuine failure, for as long as it takes each CronJob to next fire — up to a day for a nightly backup.

Fixed by `internal/metrics/collector.go`: a 30 s ticker derives every gauge from stored state, making them a function of the database rather than of process uptime. Counters and the histogram deliberately stay event-driven (rebuilding them each pass would double-count). A run still in flight keeps its previous values rather than being overwritten with zeroes, and a CronJob that has never run publishes no `last_run_status` at all rather than a `0` that would claim a success that never happened.

**OBS-4 — The metric set had real gaps.** *(found and fixed this pass)*
Six families existed; none covered in-flight runs (so a hung or overlapping run was unalertable), missed schedules (the single best "my backup stopped firing" signal — computed for the UI badge since the first pass but never exported), last-run duration as a gauge (only a histogram, which alert rules cannot express against), last-run CPU/memory (the product samples resources and exported none of it), per-cluster inventory or Metrics-API health (so a resource gauge going flat was indistinguishable from a dead metrics-server), build info, or any HTTP traffic signal.

Added in `internal/metrics/metrics.go` (ten families, 6 → 16): `runs_active`, `cronjob_missed`, `last_run_duration_seconds`, `last_run_cpu_millicores`, `last_run_memory_bytes`, `cluster_cronjobs`, `cluster_metrics_api_available`, `build_info`, `http_requests_total`, `http_request_duration_seconds`. The HTTP pair is labelled by **`r.Pattern`, the matched ServeMux route**, never `r.URL.Path` — paths embed cluster/namespace/CronJob/run identifiers and would mint a series per object; unrouted requests collapse to a single `unmatched` label since that path is attacker-controlled.

### LOW

**SEC-29 — `/metrics` is unauthenticated and now says considerably more.**
`auth.go:172` exempts `/metrics` from the session check alongside `/healthz` and `/readyz`. That is conventional — Prometheus needs to scrape it — but the endpoint now discloses, to anyone who can reach the pod: every cluster name, every namespace, every CronJob name, each one's next scheduled fire time, its last outcome, its duration, and its peak CPU/memory. That is a fairly complete map of an organisation's batch infrastructure and its backup windows.

Not raised higher because it exposes no credentials and the service is ClusterIP by default — but note it compounds SEC-28: with an Ingress and no OIDC, `/metrics` is public too.

Fix: document that `/metrics` must be reached via a NetworkPolicy-scoped scrape rather than the public Ingress; optionally support a bearer token for the scrape endpoint, or a separate listener port that the Ingress does not route.

### Carry-overs re-verified this pass

- **DOM-1** — FIXED, confirmed: `schedule.Parse/NextRun/PrevRun` all take an IANA zone; `time/tzdata` embedded at `main.go:13` with the distroless rationale written down.
- **BUG-20** — FIXED, confirmed: `deleted_at` filters in `ListCronJobs`/`ListClusters`, `metrics.DeleteCronJobSeries` on delete. Extended this pass — the new fleet aggregates join `cronjobs` so a deleted CronJob's surviving run rows stay out of the totals (`fleet_test.go:TestGetFleetStats_ExcludesDeletedCronJobs`).
- **PERF-2** — FIXED, confirmed; the comment above `GetCronJobSummaries` still warns off the window-function rewrite with its measurement.
- **SEC-22** (CDN, no SRI) — **still OPEN**, unchanged: `html.go:110` unpkg htmx, `handlers_runs.go:317` jsdelivr Chart.js, neither with `integrity=`.
- **INFRA-3** (amd64-only) — **still OPEN**, unchanged: `platforms: linux/amd64` at `docker-publish.yml:56,71`, still no QEMU step.
- **INFRA-5** (CI skipped for Renovate) — **still OPEN**: `ci.yml:12` `if: github.actor != 'renovate[bot]'`. Now materially riskier — Renovate has produced nine branches and this pass consolidated eight dependency bumps that, merged as Renovate PRs, would have run zero tests.
- **MAINT-1** (`htmlHead` discards `title`) — **still OPEN**: `html.go:98` `_ = title`.
- **SEC-24, BUG-21/22/23/24/25, PERF-1/3, OBS-1/2, DOC-5, TEST-1** — carried unchanged; not re-investigated in depth this pass.

### Positive notes

- The `deleted_at` discipline held under extension: the new fleet queries were written against it correctly and have a regression test, rather than quietly re-introducing ghost rows into the new totals.
- `GetTopCronJobs` interpolates its ordering expression from a **closed map** keyed by an unexported-value type (`storage.RankMetric`), with the `clusterID` filter left as a bound parameter — the one place in the codebase where SQL text is assembled, and it is fenced. Covered by `TestGetTopCronJobs_RejectsUnknownMetric`.
- Single-cluster installs now redirect `/` to the cluster view instead of rendering a second, identical dashboard — the kind of duplication that usually calcifies into two divergent code paths.

## 2.B Findings from the bootstrap pass (2026-07-08) — detail

### MEDIUM

**SEC-20 — No `http.Server` timeouts.** `internal/api/server.go:109-112` builds `http.Server` with only `Addr`/`Handler`. No `ReadHeaderTimeout` → a handful of slow-header (Slowloris) connections exhausts the server. Fix: set `ReadHeaderTimeout: 10 * time.Second` and `IdleTimeout: 120 * time.Second` (leave `WriteTimeout` at 0 — SSE streams are long-lived).

**SEC-21 — Raw Kubernetes error to HTTP clients.** `internal/api/handlers_cronjob.go:93,103` do `writeError(w, 500, err.Error())` with the error from `patchSuspend` — a raw client-go error (API server URLs, RBAC details). This violates the repo's own convention ("never return raw K8s API or DB errors to HTTP clients", CLAUDE.md). Fix: `slog.Error(...)` the real error, return a generic `"failed to update cronjob"`.

**SEC-22 — All frontend assets from third-party CDNs, no SRI.** htmx from unpkg (`html.go:50`), Chart.js from jsdelivr (`handlers_runs.go:318`), Google Fonts (`html.go:45-46`, `auth.go:277-278`). A CDN compromise executes arbitrary JS in an authenticated session that can suspend/trigger workloads across every connected cluster. Also breaks entirely in air-gapped clusters — a core audience for a K8s ops tool. Fix: vendor htmx + Chart.js into `internal/ui/static/` (embed.FS already exists); at minimum add `integrity=` + `crossorigin` attributes.

**BUG-20 — Deleted CronJobs and clusters ghost forever.** `CronJobHandler.OnDelete` is a deliberate no-op (`cronjob.go:47`) and nothing marks deletion: a CronJob removed from the cluster stays in every list view with a live countdown, accumulates a false `missed` badge (`handlers_cluster.go:344-352`), and its `kubecron_cronjob_suspended`/`kubecron_next_run_timestamp` series export stale values forever. Same for clusters whose kubeconfig is removed (`manager.go` only upserts). Fix: add a `deleted_at` column (keeps history), set it in `OnDelete`, filter active views on it, and call `metrics.CronJobSuspended.DeleteLabelValues(...)` / `NextRunTimestamp.DeleteLabelValues(...)`.

**DOM-1 — `spec.timeZone` ignored.** Zero references to `TimeZone` in the tree; `schedule.NextRun/PrevRun` (`next.go:13,39`) evaluate the cron expression in the server's TZ. For a CronJob with `spec.timeZone: America/New_York` the dashboard shows a wrong next-run countdown and raises false `missed` badges (or misses real ones) — the exact numbers this product exists to get right. Fix: persist `cj.Spec.TimeZone` in `cronjobs` (migration 000004), and in `schedule` load the location and evaluate `s.Next(t.In(loc))`.

**INFRA-3 — Releases are amd64-only; docs claim multi-arch.** `docker-publish.yml` has `platforms: linux/amd64` on both branch and release builds, and no QEMU setup step. CLAUDE.md and ROADMAP claim `linux/amd64 + linux/arm64 via QEMU + buildx` (ROADMAP even ticks it as shipped in v0.0.1). arm64 users (Graviton, Ampere, RPi) get no image. Fix: add `docker/setup-qemu-action` and `platforms: linux/amd64,linux/arm64` on the release build — or remove the claim.

### LOW

**SEC-23 — No security headers.** No CSP, `X-Content-Type-Options: nosniff`, `X-Frame-Options`/`frame-ancestors`, or HSTS anywhere (`middleware.go`). The inline-script-heavy UI needs a permissive CSP, but even `nosniff` + `frame-ancestors 'none'` + HSTS-behind-TLS is free hardening. Add a headers middleware in the chain (`server.go:103`).

**SEC-24 — Rate limiter is proxy-blind and unbounded.** `rate_limiter.go:53` keys on `r.RemoteAddr`; behind the Helm chart's own ingress every user shares the LB/ingress IP → 10 logins/min for the whole org (collective lockout one attacker can trigger). And `buckets` grows one entry per IP ever seen, never pruned. Fix: optional trusted-proxy `X-Forwarded-For` parsing + periodic sweep of expired buckets.

**SEC-25 — CSRF cookie missing `Secure`; logout is a GET.** `csrf.go:20-26` never sets `Secure` (session cookie does, `auth.go:256`). `GET /auth/logout` (`server.go:67`) is state-changing and CSRF-protectable by nothing (CSRFProtect only covers POST) — any page can force-logout users via an `<img>` tag. Make logout a POST (it's behind the HTMX CSRF hook for free) and thread the `secure` flag into the CSRF middleware.

**SEC-26 — ClusterRole over-grants.** `clusterrole.yaml` gives `patch` + `create` on **both** `cronjobs` and `jobs`; the app needs `patch` on cronjobs only (`handlers_cronjob.go:126`) and `create` on jobs only (`handlers_cronjob.go:169`). CLAUDE.md/README document the narrower set. Split into two rules.

**BUG-21 — Timestamp format mismatches in SQL.** The Go driver stores `started_at` as RFC3339 (`2026-07-08T14:03:02Z`); SQLite's `datetime('now', '-7 days')` yields `2026-07-01 14:03:02`. `queries.go:202` and `:258` compare them **lexicographically** — `'T' > ' '` makes boundary-day rows misclassify in the 7-day stats and heatmap window. The paged cursor (`queries.go:291`) normalises via `datetime()` (correct) but strict `<` at second precision skips runs sharing the cursor's second, and the function call defeats `idx_job_runs_started`. Fix: normalise both sides (`datetime(started_at) > datetime('now','-7 days')`) or store a canonical format; page on `(started_at, id)` tuple.

**BUG-22 — Bind failure leaves a headless process.** `main.go:129-134`: if `ListenAndServe` returns immediately (port in use), the goroutine logs and exits but `main` blocks on `<-ctx.Done()` — the pod keeps running with informers but no HTTP, so no `/healthz` for the liveness probe to fail fast on. Propagate the error to a channel selected alongside `ctx.Done()` and exit non-zero.

**BUG-23 — `metrics_enabled` can only ever become true.** `probe()` (`metrics_probe.go:20-32`) sets the flag on success and does nothing on failure; `UpsertCluster` (`queries.go:19`) preserves the old value on conflict. Once true — even in a previous process lifetime — a vanished metrics-server means a `Warn` every 15 s per running pod, forever, and the UI dot stays green. Give the probe a disable path (`SetClusterMetricsEnabled(false)` + `cc.SetMetricsEnabled(false)` after N consecutive failures).

**BUG-25 — DownloadLogs corner-cutting.** `handlers_runs.go:408-420`: swallowed query error and no run-existence check (any ID → 200 with an empty file), the entire log loaded into memory (`GetLogLines`) — a multi-GB log OOMs the pod within its 512 Mi limit — and the `Content-Disposition` filename embeds 8 raw chars of the path param. Check existence (404), stream rows to the response, sanitise the filename.

**PERF-2 — Per-row query fan-out on hot pages.** `buildCronJobRow` (`handlers_cluster.go:337-354`) issues `GetLastJobRun` + `GetRunStats7d` + `GetRecentDurations` per CronJob — 3N queries per render, re-run **every 10 s per open browser tab** by the HTMX poll (`html_components.go:130`), plus `PrevRun`'s tick-scan (`next.go:39`) which walks up to ~1 500 iterations for minutely schedules. Fine at 30 CronJobs; painful at 500 with 5 viewers. Batch the three into single grouped queries when it starts to hurt.

**OBS-1 — Blind spots in request logging.** Auth middleware is prepended outermost (`server.go:105`), so 401s/redirects never reach `Logger` — failed auth probes are invisible in access logs (only the OIDC callback denial logs). No request ID correlates log lines. Also `/healthz` clutter at Info every probe period. Reorder chain (Logger outermost), skip health paths, add a request ID.

**OBS-2 — Informer death is silent.** No metric or health signal tracks watch/list errors or last-event age (`controller.go`). A cluster whose kubeconfig expired keeps serving stale data indefinitely — `/readyz` only covers the *initial* sync. Export `kubecron_informer_healthy{cluster}` or last-sync-age and alert on it.

**INFRA-4 — Unpinned lint toolchain.** `ci.yml` does `go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest` — unpinned (non-reproducible; also the v1 module path no longer tracks current releases, v2 moved to `/v2/`). No `.golangci.yml`, so implicit default linters. Pin a version (or use the official action) and commit a config.

**INFRA-5 — `renovate[bot]` bypasses CI.** `ci.yml` job condition `github.actor != 'renovate[bot]'` means once Renovate is enabled (it's on the roadmap), its PRs run **zero** build/test/lint. That inverts the point of dependency bots. Drop the condition or restrict it to non-PR events.

**DOC-5 — Compose file drift.** `docker-compose.yaml` header comments are in French (house rule: all file content in English), and it maps `8082:8080` while README's compose section says "Open http://localhost:8080".

### INFO

- **SEC-27** — `esc()` (HTML entity escaping) is used inside JS string contexts (`onclick="window.location='/clusters/%s/…'"`, `html_components.go:71,156`; heatmap `html_chart.go:145`). Safe today only because K8s DNS-1123 names can't contain quotes; a future non-K8s-constrained value in these slots is XSS. Prefer data-attributes + a delegated click handler.
- **BUG-24** — `backfillRun` first writes `exit_code=0` even for `failed` runs and only corrects it if the pod lookup succeeds (`job.go:142,175`); a failed backfilled run can display `exit 0`.
- **PERF-3** — each SSE viewer polls `GetJobRun` at 1 Hz (`handlers_sse.go:145`); fine single-instance, worth a shared cache if viewer counts grow.
- **MAINT-1** — `htmlHead(title, …)` discards `title` (`html.go:38`, `_ = title`); every tab reads "KubeCron", hurting navigation with multiple pages open. Either use it or drop the param.
- Log `size` accounting counts the raw line including the stripped K8s timestamp prefix (`logstream.go:156`), overstating `log_size_bytes` by ~31 bytes/line.
- Broadcaster drops lines for slow SSE consumers silently (`broadcaster.go:70-72`) — acceptable design, but the viewer gets no "lines dropped" marker.
- No `VACUUM`/`PRAGMA incremental_vacuum` after retention deletes (`retention.go`) — the SQLite file never shrinks.

### Carry-overs verified this pass

All six pre-2026-07 fixes re-verified at their cited locations (see tracking table): CORS removal, CSRF double-submit, login/trigger rate limits, RunIndex, dotfile skip in `Manager.Load`, `bufio.Reader` log streaming. `UpsertJobRun` targeted upsert confirmed at `queries.go:132-145` with test `TestUpsertJobRun_DoesNotOverwriteExistingFields`. XSS fix in SSE replay confirmed (`handlers_sse.go:101-102` escapes + wraps).

## 3. What's done right

- **Right-sized architecture.** One binary, one writer, informer-driven ingestion, in-memory pub/sub for SSE — every piece earns its place; no premature microservices. The Helm deployment even documents *why* `Recreate` (RWO PVC + single-writer SQLite).
- **Prior audit landed.** Every referenced SEC/BUG fix from the lost previous pass is verifiably in the code, several with regression tests.
- **Careful concurrency at the hot spots.** Broadcaster's publish-under-RLock design is documented and correct; `Streamer.Stream` is idempotent per run; `RunIndex` is a clean lock-scoped map; `atomic.Bool` for the probe flag.
- **Subscribe-before-replay SSE ordering** (`handlers_sse.go:109-119`) with the DB-insert-before-publish invariant — the no-gap/no-dup reasoning is written down where it matters.
- **Storage discipline.** All SQL in `queries.go`, shared `jobRunCols`, FKs with `ON DELETE CASCADE` + `PRAGMA foreign_keys=ON`, transactional embedded migrations, WAL + busy-timeout + single-conn choices explained in comments.
- **Sane security baseline.** HMAC-signed sessions with derived key, state-bound OIDC flow with safe-redirect validation, operator-vs-viewer split, distroless non-root image with read-only rootfs and dropped capabilities, secrets via K8s Secrets.
- **CI covers build/vet/test/race/lint/helm-lint**, releases get SBOM + cosign keyless signing.

## 4. Audit Checklist — applied every pass

### Security
- [x] Every mutating route behind auth when OIDC enabled; operator gate on suspend/resume/trigger (`server.go:84-86`)
- [x] CSRF on all POSTs (`csrf.go`) — but see SEC-25 (Secure flag, GET logout)
- [x] Session cookies HttpOnly/SameSite/Secure-when-HTTPS (`auth.go:254-257`)
- [x] Parametrised queries only — zero string-built SQL (`queries.go` reviewed exhaustively)
- [x] Secrets never logged (session key, kubeconfig contents — grep-verified)
- [x] Open-redirect guard (`safeRedirect`, `auth.go:337`)
- [x] HTTP server timeouts (SEC-20 — fixed 2026-07-09, `server.go`)
- [ ] Security headers: nosniff/XFO/Referrer-Policy done 2026-07-09; CSP + HSTS remain (SEC-23)
- [ ] No third-party runtime assets, or SRI-pinned (SEC-22)
- [x] Raw upstream errors never reach clients (SEC-21 — fixed 2026-07-09)
- [ ] Rate limiter proxy-aware and bounded (SEC-24)
- [x] RBAC minimal per verb/resource pair (SEC-26 — fixed 2026-07-09)
- [ ] **Packaging cannot silently ship an unauthenticated deployment** — chart refuses or loudly warns on `ingress.enabled && !oidc.enabled` (SEC-28) *(added 2026-08-11)*
- [ ] Default Ingress requires TLS, or says why not (SEC-28 aggravator)
- [ ] `/metrics` exposure scoped — not routed by the public Ingress (SEC-29) *(added 2026-08-11)*
- [x] Any SQL text assembled in Go comes from a closed allow-list, never a caller string (`rankValueExpr`, `queries.go`) *(added 2026-08-11)*

### Correctness & concurrency
- [x] Check-then-act sites locked (`Streamer.Stream`, `Sampler.Start`, broadcaster)
- [x] Send-on-closed-channel impossible by lock design (`broadcaster.go:54-74`)
- [x] Upsert keys stable; targeted `ON CONFLICT DO UPDATE` (no data loss)
- [x] Startup recovery for stale 'running' runs (`main.go:67-76`)
- [ ] Consistent timestamp format/TZ across Go and SQL comparisons (BUG-21)
- [x] `spec.timeZone` respected in schedule math (DOM-1)
- [x] Deleted upstream objects reflected in DB/UI/metrics (BUG-20)
- [ ] Fatal startup errors terminate the process (BUG-22)

### Maintainability / DRY / dead code
- [x] SQL centralised; shared column list; scan helpers deduped
- [x] HTML components extracted (`html_components.go`); no template drift
- [x] No orphaned files or ghost endpoints found this pass
- [ ] No dead params (`htmlHead` title, MAINT-1)

### Kubernetes / infra
- [x] Probes, resources, Recreate strategy, non-root, read-only rootfs, capabilities dropped
- [x] RBAC documented and chart-managed; no Secrets access
- [x] seccompProfile RuntimeDefault (INFRA-2 — fixed 2026-07-09, `values.yaml`)
- [ ] Base images digest-pinned (INFRA-1)
- [ ] Published platforms match documented platforms (INFRA-3)

### Performance
- [x] Dashboard running-count N+1 already fixed via single `GetRunningRuns` (`handlers_cluster.go:313`)
- [x] Log writes batched (200 ms / 200 lines); log reads tail-limited (5 000)
- [x] Cluster-page per-row fan-out made index-served (PERF-2 — measured; batching was the wrong fix)
- [ ] Pagination cursor index-friendly and gap-free (BUG-21)

### Observability
- [x] Structured slog everywhere; 16 Prometheus collector families wired *(6 → 16, 2026-08-11)*
- [x] `/readyz` gates on initial informer sync
- [ ] Informer liveness signal after initial sync (OBS-2)
- [ ] Unauthenticated/denied requests visible in access logs (OBS-1)
- [x] Stale metric series deleted with their objects (BUG-20)
- [x] **Gauge series survive a process restart** — derived from stored state, not only from live events (OBS-3) *(added 2026-08-11)*
- [x] Background-job failure is alertable without the UI: in-flight, missed-schedule, last-outcome and resource series all exported (OBS-4) *(added 2026-08-11)*
- [x] HTTP metrics labelled by route pattern, never raw path — no per-object cardinality *(added 2026-08-11)*
- [x] A metric is never published as a plausible-looking default: a never-run CronJob emits no `last_run_status` rather than `0` *(added 2026-08-11)*

### Testing
- [x] `go test` + `-race` + golangci-lint + helm lint in CI
- [x] Regression tests for prior audit fixes (upsert, safeRedirect, broadcaster stress)
- [ ] CronJobHandler/PodHandler/sampler/Streamer/Manager coverage (TEST-1)
- [ ] Coverage measured with a threshold in CI
- [ ] Renovate PRs run CI (INFRA-5)
- [x] Lint behaviour is deterministic and identical locally and in CI (INFRA-4) *(added 2026-08-11)*
- [ ] Test files reviewed for assertion quality, not just existence — carried gap

### Supply chain
- [ ] `govulncheck` gates merges (SUP-1) *(added 2026-08-11)*
- [ ] Published image CVE-scanned (Trivy/Grype) *(added 2026-08-11)*
- [ ] Base images digest-pinned — **only after** the CVE gate exists, or the pin freezes a vulnerable stdlib (INFRA-1 × SUP-1) *(added 2026-08-11)*
- [x] SBOM generated and release images cosign-signed

### Docs
- [x] README/env/CLAUDE.md/ROADMAP harmonised 2026-07-08 (pre-audit pass)
- [ ] CI/CD claims match workflows exactly (DOC-2 residual via INFRA-3; DOC-5)
- [ ] All file content in English incl. compose comments (DOC-5)

### Domain correctness (next-run / missed / stats are the product's output of record)
- [x] Missed detection guards: suspension check, 5-min grace, running-run exemption (`handlers_cluster.go:344-352`)
- [x] Duration never negative (`durationFromStart`, `scanJobRun`)
- [x] Timezone-aware schedule evaluation (DOM-1)
- [ ] Boundary-exact 7-day/daily windows (BUG-21)

## 5. Verdict

### Pass 2026-08-11

**Ship-readiness: yes for `helm install` defaults, no for an Ingress deployment until SEC-28 is addressed.** The Go code is in its best shape yet — the two findings that made the dashboard lie are closed, observability went from the weakest dimension to a genuine strength, and the lint gate is deterministic for the first time. The blocker is packaging, not code: the chart will happily publish an unauthenticated control plane for every connected cluster and never mention it.

**Remaining blockers:** SEC-28 (HIGH). Nothing else blocks.

**Highest-leverage next moves:**
1. **SEC-28 — three small edits, one HIGH closed.** A `fail` guard in the chart, a `slog.Warn` at startup when OIDC is off, and a sentence each in `NOTES.txt` and the README ingress row. None of it is more than an hour, and it is the only thing standing between this repo and a clean HIGH column.
2. **SUP-1 before INFRA-1.** Add `govulncheck` to `ci.yml` and an image scan to `docker-publish.yml` *first*; pinning base-image digests before the gate exists would convert today's accidental patching into a permanent freeze. Doing these in the wrong order is worse than doing neither.
3. **INFRA-5 — drop the `renovate[bot]` CI skip.** Nine Renovate branches are already open; this pass merged eight bumps by hand precisely because their PRs would have been untested. The condition inverts the point of a dependency bot.
4. **SEC-22 — vendor htmx and Chart.js.** Unchanged from the last two passes' recommendation, and now the oldest open MEDIUM. `embed.FS` is already wired; this is mostly a download and two `<script>` edits, and it closes the CDN supply-chain path while making air-gapped installs work.

**Audit-pass progress summary:**

| Pass | Score | OPEN HIGH | OPEN MED | Closures that pass |
|---|---|---|---|---|
| 2026-07-08 | 7.5/10 | 0 | 8 | 6 verified from prior lost pass + 3 DOC fixed in-pass |
| 2026-08-11 | 7.5/10 | **1** (SEC-28) | 5 (SEC-22, INFRA-3, PERF-1, TEST-1 partial, SUP-1) | DOM-1, BUG-20, PERF-2 verified closed in v0.2.0; OBS-3, OBS-4, INFRA-4, MAINT-2 found-and-fixed in-pass |

### Pass 2026-07-08 (bootstrap)

**Ship-readiness: yes for its stated positioning** ("alpha, personal project" per README) — no HIGH/CRITICAL, sound architecture, real test momentum. For a 0.2.0 tag the six MEDIUMs are not blockers, but SEC-20/SEC-21 are each ~10-line fixes that should ride in the release.

**Highest-leverage next moves:**
1. ~~**Quick security batch:** SEC-20, SEC-21, SEC-25, SEC-26, INFRA-2~~ — **done 2026-07-09** (plus nosniff/XFO/Referrer-Policy headers); tag 0.2.0 next.
2. **Vendor the frontend assets** (SEC-22) — kills the CDN supply-chain exposure and makes air-gapped installs work; `embed.FS` is already wired.
3. ~~**DOM-1 + BUG-20 together** — timezone-aware scheduling and deleted-object cleanup are the two findings that make the dashboard *lie*~~ — **done 2026-07-25** (v0.2.0), together with PERF-2; tag 0.2.0 next.
4. **Decide INFRA-3** — either add QEMU+arm64 to the release build or strike the multi-arch claim from CLAUDE.md/ROADMAP; today the docs promise what CI doesn't do.

**Audit-pass progress summary:**

| Pass | Score | OPEN HIGH | OPEN MED | Closures that pass |
|---|---|---|---|---|
| 2026-07-08 | 7.5/10 | 0 | 8 (SEC-20/21/22, BUG-20, DOM-1, INFRA-3, PERF-1, TEST-1 partial) | 6 verified from prior lost pass + 3 DOC fixed in-pass |
