# KubeCron — Full Audit

Date: 2026-05-21
Scope: source code (`cmd/`, `internal/`, `migrations/`), Helm chart, CI/CD, Dockerfile.
Axes: security, bugs, dead code, maintainability, factorisation, coverage, feature coverage.

Severities:
- 🔴 **Critical** — must fix before prod / data loss / exploitable vulnerability
- 🟠 **Important** — fix in the short term
- 🟡 **Minor** — fix when possible

---

## 🔒 Security

### ✅ SEC-1 — XSS via logs in SSE history replay *(fixed 2026-05-21)*
**File:** `internal/api/handlers_sse.go:113-116`

The history replay for *running* runs did not call `html.EscapeString` (unlike the "completed" branch and the live broadcast path).

```go
hist, _ := h.store.GetLogLinesTail(ctx, id, sseLogLimit)
for _, l := range hist {
    fmt.Fprintf(w, "data: %s\n\n", l.Line)   // ❌ no escaping, no <div> wrapper
}
```

An application log containing `<script>alert(1)</script>` was injected as-is into the DOM.

**Fix applied:**
```go
esc := html.EscapeString(l.Line)
fmt.Fprintf(w, "data: <div class=\"ll\" data-raw=\"%s\">%s</div>\n\n", esc, esc)
```

---

### ✅ SEC-2 — Open redirect on `/auth/login?redirect=` *(fixed 2026-05-21)*
**File:** `internal/auth/auth.go`

The `redirect` parameter was base64-encoded in the OIDC state and used raw in `http.Redirect` at callback. No validation that it was a same-origin relative URL.

An attacker could craft `https://kubecron/auth/login?redirect=https://attacker.com` → after login the user was redirected to the malicious site.

**Fix applied:** added `safeRedirect(raw, fallback)` helper that validates the redirect starts with `/` and not `//`. Applied in both `HandleLogin` and `HandleCallback`. Covered by `TestSafeRedirect` in `auth_test.go`.

---

### ✅ SEC-3 — `UpsertJobRun INSERT OR REPLACE` overwrites columns *(fixed 2026-05-21)*
**File:** `internal/storage/queries.go:124-131`

```sql
INSERT OR REPLACE INTO job_runs(id, cronjob_id, pod_name, trigger, started_at, status) VALUES (?,?,?,?,?,?)
```

`INSERT OR REPLACE` deletes then re-inserts the row. All unlisted columns (`finished_at`, `exit_code`, `log_size_bytes`, `node_name`, `container_image`, `avg_cpu_millicores`, `max_cpu_millicores`, etc.) revert to NULL/0.

If the Job informer called `UpsertJobRun` on a run that already had content, **all history for that run was lost**.

**Fix applied:** switched to `INSERT … ON CONFLICT(id) DO UPDATE SET …` targeting only the intended fields.

---

### ✅ SEC-4 — Session cookie missing `Secure` flag *(fixed 2026-05-21)*
**File:** `internal/auth/auth.go`

The session cookie was set without the `Secure` flag.

**Fix applied:** added `secure bool` field to `Authenticator`, derived from `strings.HasPrefix(cfg.RedirectURL, "https://")`. Applied to all `SetCookie` calls (session, state, logout). Also added an explicit `slog.Warn` at boot when `OIDC_SESSION_KEY` is empty (SEC-12).

---

### ✅ SEC-5 — Global CORS `*` on all routes *(fixed 2026-05-21)*
**File:** `internal/api/middleware.go`

**Fix applied:** removed the `CORS` middleware entirely from the middleware chain (`server.go`). KubeCron has no public cross-origin API.

---

### ✅ SEC-6 — No CSRF token on POST endpoints *(fixed 2026-05-21)*
**Files:** `internal/api/csrf.go`, `internal/api/server.go`, `internal/api/html.go`

**Fix applied:** added double-submit cookie pattern:
- `EnsureCSRFCookie` middleware sets a `csrf_token` cookie (SameSite=Strict, not HttpOnly) on every response.
- `CSRFProtect` middleware validates `X-CSRF-Token` header matches the cookie on every POST.
- HTMX `configRequest` event handler in `htmlHead` reads the cookie and attaches the header automatically.

---

### 🟠 SEC-7 — Dockerfile uses floating image tags
**File:** `Dockerfile`

Base images use floating tags: `golang:1.26-alpine`, `gcr.io/distroless/static:nonroot`.

**Fix:** pin with `image@sha256:<digest>`.

---

### 🟠 SEC-8 — GitHub Actions pinned by major tag only
**File:** `.github/workflows/ci.yml`

```yaml
- uses: actions/checkout@v6
- uses: actions/setup-go@v6
- uses: azure/setup-helm@v5
```

Major tags can be moved or compromised by a maintainer. Pin by commit SHA.

---

### 🟠 SEC-9 — CI tools installed via `@latest`
**File:** `.github/workflows/ci.yml`

Non-reproducible builds and supply-chain risk. The Dockerfile pins `templ@v0.3.1020` but CI uses `@latest` → version drift.

**Fix:** use a single pinned version in both places.

---

### 🟠 SEC-10 — Missing `seccompProfile: RuntimeDefault`
**File:** `charts/kubecron/templates/deployment.yaml`

Already noted in CLAUDE.md Known Issues.

**Fix:** add to `podSecurityContext`:
```yaml
seccompProfile:
  type: RuntimeDefault
```

---

### 🟠 SEC-11 — No rate limiting
No rate limit on `/auth/login` or `/api/.../trigger`. An authenticated account could DoS the cluster by triggering runs in a loop.

---

### 🟠 SEC-12 — `OIDC_SESSION_KEY` empty → random key on each restart
**File:** `internal/auth/auth.go:66-71`

If the variable is empty a random key is generated silently → all sessions invalidated on restart. Documented as "acceptable single-instance" but should at least **log an explicit WARN at boot**.

---

## 🐛 Bugs

### ✅ BUG-1 — Sampler and streamer inactive for pods *Running* at boot *(fixed 2026-05-21)*
**File:** `internal/watcher/pod.go:48-58`

```go
func (h *PodHandler) OnAdd(obj interface{}, isInInitialList bool) {
    phase := pod.Status.Phase
    if phase != corev1.PodSucceeded && phase != corev1.PodFailed {
        return  // ❌ returns for PodRunning
    }
    h.OnUpdate(nil, obj)
}
```

At KubeCron startup, pods already *Running*:
1. `OnAdd` returned immediately
2. `OnUpdate` only fired on the next state change (completion)
3. `factory := informers.NewSharedInformerFactory(clientset, 0)` → resyncPeriod=0 → no re-tick

**Consequence:** for a pod that was Running when KubeCron started, **no logs or CPU/RAM samples were captured** until it finished.

**Fix applied:** extended `OnAdd` to also handle `PodRunning`.

---

### ✅ BUG-2 — `backfillRun` picks `pods.Items[0]` *(fixed 2026-05-21)*
**File:** `internal/watcher/job.go`

Non-deterministic pod selection for Jobs with retries or parallelism.

**Fix applied:** added `selectBestPod(pods []corev1.Pod)` that prefers the most recently created terminal pod (Succeeded/Failed); falls back to the newest pod overall.

---

### ✅ BUG-3 — Race between OnAdd Pod and Job at startup *(fixed 2026-05-21)*
**File:** `internal/watcher/pod.go`

During the initial list, `Pod.OnAdd` called `findRunID(jobName)` before the Job handler had created the run record → logs and samples missed for pods already Running at boot.

**Fix applied:** for `PodRunning` pods during the initial list, `OnAdd` now spawns a `retryOnAdd` goroutine that retries `findRunID` up to 10 times (500 ms apart, 5 s total) before calling `OnUpdate`. Terminal pods at startup continue to rely on `backfillRun` in the Job handler.

---

### ✅ BUG-4 — `findRunID` is O(N×M) *(fixed 2026-05-21)*
**Files:** `internal/watcher/run_index.go`, `internal/watcher/pod.go`, `internal/watcher/job.go`, `internal/watcher/controller.go`

**Fix applied:** introduced `RunIndex` — a thread-safe `jobName → runID` map (RWMutex). `JobHandler.OnAdd` populates it on every new job; `PodHandler.findRunID` checks it first (O(1)) before falling back to the DB scan. Both `PodHandler.OnUpdate` (on terminal pod) and `JobHandler.OnDelete` remove stale entries.

---

### ✅ BUG-5 — `Manager.Load` tries to parse all files *(fixed 2026-05-21)*
**File:** `internal/cluster/manager.go`

**Fix applied:** the file loop now skips dotfiles (`strings.HasPrefix(name, ".")`) and only accepts extensions `.yaml`, `.yml`, or no extension.

---

### ✅ BUG-6 — Trigger handler inserts a duplicate run *(resolved by SEC-3)*
**File:** `internal/api/handlers_cronjob.go`

The pre-insert was safe once SEC-3 (`INSERT OR REPLACE` → `ON CONFLICT DO UPDATE`) was applied; the double-upsert now only updates the explicitly listed fields. No data loss possible. Pre-insert retained for UI responsiveness (avoids a 404 on immediate navigation to the run detail page).

---

### 🟠 BUG-7 — `MarkRunFailed` leaves partial log state
**File:** `internal/storage/queries.go:322-329`

Marks a run "failed" with `exit_code = -1` but leaves `log_size_bytes` unchanged. The UI shows a failed run with its partial logs — ambiguous UX rather than a strict bug.

---

### 🟡 BUG-8 — Logs lost for runs with retries
**File:** `internal/watcher/pod.go:104-107`

```go
if run, err := h.store.GetJobRun(ctx, runID); err == nil && run != nil && run.LogSizeBytes == 0 {
    h.streamer.Stream(...)
}
```

If Pod 1 fails (logs captured), Pod 2 retries → `LogSizeBytes > 0` → logs from the retry are never captured.

---

### 🟡 BUG-9 — Heatmap in UTC only
**File:** `internal/api/html.go` (heatmapHTML)

No user timezone support. For operators not in UTC, day buckets are shifted.

---

### 🟡 BUG-10 — No log capture retry for `succeeded` runs with empty logs
If `backfillRun` could not retrieve logs (pod already gone) and marked the run `succeeded` with `log_size_bytes=0`, the run is permanently log-less (the early-return on `succeeded` prevents any retry).

---

## 🧹 Dead code

### ✅ DEAD-1 — Prometheus metrics were declared but never incremented *(wired 2026-05-21)*
**File:** `internal/metrics/metrics.go`

All collectors were **declared and exposed at `/metrics` but never incremented**. The helpers `RecordRunCompleted` / `RecordCronJobState` were called nowhere.

**Consequence:** the Prometheus endpoint returned 12+ collectors all at zero. Operators could configure alerts that would never fire.

**Fix applied:** trimmed the package to only the metrics that are now fully wired:
- `kubecron_job_runs_total{cluster,namespace,cronjob,status,trigger}` — incremented in `pod.go` on every run completion
- `kubecron_job_duration_seconds{cluster,namespace,cronjob}` — observed in `pod.go` on every run completion
- `kubecron_last_run_timestamp{cluster,namespace,cronjob}` — set in `pod.go` on run completion (startedAt)
- `kubecron_last_run_status{cluster,namespace,cronjob}` — set in `pod.go` (0=success, 1=failure)
- `kubecron_cronjob_suspended{cluster,namespace,cronjob}` — set in `cronjob.go` on every Add/Update
- `kubecron_next_run_timestamp{cluster,namespace,cronjob}` — set in `cronjob.go` from schedule computation

---

### ✅ DEAD-2 — Package `internal/ui/templates/` (~1357 lines) *(removed 2026-05-21)*
**Files:** `internal/ui/templates/{layout,dashboard,stats,cronjob_list,run_detail}.templ` + their generated `_templ.go` files.

**No file in the app imported `kubecron/internal/ui/templates`.** All UI rendering went through `internal/api/html.go` (string concatenation).

CLAUDE.md claimed "Templates: `a-h/templ` v0.3 — type-safe compiled HTML templates" — **this was false**.

**Consequences:**
- `templ generate` in `Dockerfile`, CI → was regenerating dead code
- `templ install` was adding build time for nothing
- Confusion for any future contributor looking for the nav template

**Decision applied:** deleted the package, removed `templ` from the Dockerfile and CI pipeline, removed `github.com/a-h/templ` from `go.mod`.

---

### 🔴 DEAD-3 — Page `/stats` was never routed
`stats.templ` existed but no `/stats` route was registered in `server.go`. Linked to DEAD-2 (now resolved).

---

### 🟡 DEAD-4 — No-op handlers
- `CronJobHandler.OnDelete` (`internal/watcher/cronjob.go:45`)
- `JobHandler.OnUpdate` (`internal/watcher/job.go:173`)
- `PodHandler.OnDelete` (`internal/watcher/pod.go:170`)

Acceptable if intentional (no reaction on delete/update), but should be clearly documented.

---

### ✅ DEAD-5 — Trivial wrapper functions *(fixed 2026-05-21)*
- Deleted `scanCronJobRow` — callers now call `scanCronJob` directly
- Deleted `scanJobRunRows` — callers now call `scanJobRun` directly
- Deleted `errorf` + `stringError` — replaced with `fmt.Errorf`

---

## 🏗 Maintainability

### ✅ MAINT-1 — Two parallel UI rendering systems *(resolved by DEAD-2 fix)*
Linked to DEAD-2. Generated confusion and unnecessary build complexity.

---

### ✅ MAINT-2 — HTML/JS/CSS inlined in `fmt.Fprintf` calls *(further addressed 2026-05-21)*
**Files:** `internal/api/html_components.go`, `handlers_runs.go`

**Additional fix applied:**
- `renderRunRow(run, clusterID, ns, name)` — run table `<tr>` extracted from `RunsList` handler; `runTableHeader` const added
- `RunsList` loop now two lines instead of 30+

Full migration of remaining inline HTML (run detail metadata card, dashboard cluster cards) to named helpers remains open.

---

### ✅ MAINT-3 — Misleading CLAUDE.md *(resolved by DEAD-2 fix)*
The "Project Overview" section advertised `templ` as the rendering system. A future contributor would have wasted time. Corrected after DEAD-2 cleanup.

---

### ✅ MAINT-4 — `html.go` is a catch-all *(fixed 2026-05-21)*
**Fix applied:** split into thematic files:
- `html_log.go` — `logSearchBar`, `logSearchJS`
- `html_chart.go` — `sparklineSVG`, `heatmapHTML`
- `html.go` — head/foot, nav, breadcrumb, countdown, statusBadge, fmtDuration, esc

---

### ✅ MAINT-5 — Monolithic UI handlers *(partially fixed 2026-05-21)*
**Fix applied:** extracted `Handler.buildCronJobRow` method that consolidates missed/concurrent detection + all store calls for a single CronJob row. Both `ClusterDetail` and `NamespaceDetail` call it, eliminating ~30 lines of duplicated computation. Run detail metadata card remains inline.

---

### ✅ MAINT-6 — Repeated SQL column lists in `job_runs` queries *(fixed 2026-05-21)*
**Fix applied:** `const jobRunCols` declared in `queries.go`. All four queries (`GetJobRun`, `ListJobRuns`, `GetLastJobRun`, `GetRunningRuns`) now reference it.

---

### 🟡 MAINT-7 — Duplicate suspend/resume/trigger button rendering
The HTML rendering block for these buttons is copy-pasted identically in `ClusterDetail` and `NamespaceDetail`. Extract as a helper.

---

## 📦 Factorisation opportunities

1. **`const jobRunCols`** — repeated SELECT columns (see MAINT-6)
2. **`renderActionsCell(clusterID, ns, name, suspended)`** — deduplicate button rendering (see MAINT-7)
3. **`renderPage(w, title, userEmail, body func())`** — uniform head/foot wrapping
4. **Centralised pagination** — currently absent everywhere (already in CLAUDE.md backlog)
5. **Date format `"2006-01-02 15:04:05"`** — repeated constant, extract as `const`

---

## 🧪 Coverage

### ✅ COV-1 — Test coverage *(further extended 2026-05-21)*
**Previously zero `*_test.go` files.** `go test ./...` passed trivially.

**Tests added in session 2:**
- `internal/schedule/next_test.go` — table-driven tests for `NextRun`, `NextRuns`, `PrevRun` (6 cases)
- `internal/auth/auth_test.go` — HMAC sign/parse round-trip, expired session, tampered token, wrong key, `safeRedirect` (7 cases including SEC-2 regression)
- `internal/storage/queries_test.go` — cluster CRUD, SEC-3 regression (`UpsertJobRun` ON CONFLICT), `GetLogLinesTail` ordering and limit

**Tests added in session 3:**
- `internal/streamer/broadcaster_test.go` — subscribe/publish, unsub closes channel, Close() removes all subs, concurrent stress test (4 publishers × 4 subscribers)
- `internal/watcher/job_test.go` — `OnAdd` live job creates run + RunIndex entry, `OnAdd` initial-list done job backfills with correct status, `OnDelete` marks run failed + cleans RunIndex

All 5 suites pass (`go test ./...`). `go test -race` not available on Windows (CGO required); run in CI.

**Still untested:**
- `internal/api/` — HTTP handler integration tests

---

## 🎯 Feature Coverage

The product delivers the essentials: multi-cluster monitoring, run history, live logs, suspend/resume/trigger, optional OIDC, Helm chart, single binary without CGO.

| Promise | Reality |
|---|---|
| Prometheus metrics | ~~All at zero~~ → 6 metrics now fully wired (DEAD-1) |
| Type-safe `templ` templates | ~~Present but never used~~ → package removed (DEAD-2) |
| `/stats` page | ~~Route not registered~~ → resolved by DEAD-2 removal |
| `go test ./...` in DoD | No tests exist (COV-1) |

**Functional gaps:**
- No alerting / webhook on run failure
- No pagination on run list (already in CLAUDE.md backlog)
- Heatmap UTC only (BUG-9)
- No log capture retry for runs already marked `succeeded` with empty logs (BUG-10)

---

## 🎬 Top 5 actions — status

| # | Action | Effort | Status |
|---|---|---|---|
| 1 | Fix XSS SSE replay (SEC-1) | 5 min | ✅ Fixed 2026-05-21 |
| 2 | Fix `INSERT OR REPLACE` job_runs (SEC-3) | 10 min | ✅ Fixed 2026-05-21 |
| 3 | Remove `internal/ui/templates/` + `templ` from build (DEAD-2) | 30 min | ✅ Fixed 2026-05-21 |
| 4 | Extend PodHandler.OnAdd for Running pods (BUG-1) | 15 min | ✅ Fixed 2026-05-21 |
| 5 | Wire Prometheus metrics (DEAD-1) | 1h | ✅ Fixed 2026-05-21 |

---

## Findings Summary

| Category | 🔴 | 🟠 | 🟡 | Total |
|---|---|---|---|---|
| Security | 3 | 9 | 0 | 12 |
| Bugs | 1 | 6 | 3 | 10 |
| Dead code | 3 | 0 | 2 | 5 |
| Maintainability | 3 | 2 | 2 | 7 |
| Coverage | 1 | 0 | 0 | 1 |
| **Total** | **11** | **17** | **7** | **35** |

**Session 1 (2026-05-21):** SEC-1, SEC-3, BUG-1, DEAD-2 removed, DEAD-1 wired with 6 real metrics.
**Session 2 (2026-05-21):** SEC-2, SEC-4 (+SEC-12 warn), BUG-2, BUG-3, MAINT-2 partial, COV-1 baseline (3 test suites, 16 test cases).
**Session 3 (2026-05-21):** SEC-5 (removed CORS), SEC-6 (CSRF double-submit cookie), SEC-11 (rate limit login+trigger), BUG-4 (RunIndex O(1)), BUG-5 (Manager.Load filter), BUG-6 (resolved by SEC-3), DEAD-5 (trivial wrappers removed), MAINT-2 (renderRunRow + runTableHeader), MAINT-4 (html.go split), MAINT-5 (buildCronJobRow extracted), MAINT-6 (jobRunCols const), COV-1 (broadcaster + watcher tests).
Remaining open after S3: BUG-7, BUG-8, BUG-9, BUG-10 (minor bugs), SEC-7 (Dockerfile digest pins), SEC-8 (GHA SHA pins), SEC-9 (CI tool version pin), SEC-10 (seccompProfile), MAINT-7, internal/api HTTP handler tests.

---

# Session 4 — 2026-06-01 (fresh full re-audit)

Re-reviewed the whole tree against the Session 1–3 fixes (all still in place; `internal/api` handler integration tests now exist — COV improved). Two **new, empirically-confirmed runtime bugs** were found that silently disabled advertised features, plus an authorization gap. The agreed "top 5" actions were implemented this session. Build, `go vet`, `go test ./...`, and `helm lint` all pass.

## 🔴 New confirmed bugs

### ✅ BUG-11 — Resource sampling never ran (CPU/RAM feature dead) *(fixed 2026-06-01)*
**Files:** `cluster/manager.go`, `cluster/client.go`, `cluster/registry.go`, `sampler/metrics_probe.go`, `watcher/controller.go`, `watcher/pod.go`, `cmd/kubecron/main.go`

`registerCluster` hardcoded `MetricsEnabled: false` on every `ClusterClient`. The metrics probe only wrote the flag to the **DB** (`store.SetClusterMetricsEnabled`); `Registry.SetMetricsEnabled` existed but was **never called** (dead code). `PodHandler` captured the static `false` at startup, so `if h.metricsEnabled && …` (`pod.go`) was always false → `resource_samples` was never populated → `FinalizeResourceUsage` averaged an empty table → `avg/max_*` stayed NULL → the run-detail resource charts never rendered. The entire "resource tracking" pillar produced nothing.

**Fix applied:**
- `ClusterClient.metricsEnabled` is now an `atomic.Bool` with `MetricsEnabled()`/`SetMetricsEnabled()` accessors (race-free: probe goroutine writes, informer handlers read).
- `PodHandler.metricsEnabled` is now a `func() bool` read **live** on each pod event, so the 5-minute re-probe takes effect.
- `StartProbe` takes an `onSuccess` callback; `main.go` passes `func(){ cc.SetMetricsEnabled(true) }`.
- Removed dead `Registry.SetMetricsEnabled`.

### ✅ BUG-12 — `duration_ms` generated column always NULL *(fixed 2026-06-01)*
**Files:** `migrations/000003_duration_ms.{up,down}.sql`, `storage/queries.go`, `storage/queries_test.go`

`duration_ms` was a `STORED GENERATED` column computed with `julianday()`, but the SQLite driver's timestamp text format is not parseable by `julianday()`, so the column was **always NULL** (verified empirically). `scanJobRun` has a Go-side fallback so single-run *display* was correct — but SQL aggregates over the column were not:
- `GetRecentDurations` filters `WHERE duration_ms IS NOT NULL` → returned 0 rows → **duration sparklines never rendered**.
- `GetRunStats7d` `AVG/MAX(duration_ms)` → always NULL → **7-day avg/max duration always blank**.

**Fix applied:** migration `000003` drops the generated column and replaces it with a plain `INTEGER` column. `UpdateJobRunStatus` and `MarkRunFailed` now compute the duration in Go (`durationFromStart`, format-independent) and persist it via `COALESCE(?, duration_ms)`. Regression test `TestUpdateJobRunStatus_PopulatesDuration` asserts `GetRecentDurations` and `GetRunStats7d` return data.

## 🔒 Security

### ✅ SEC-13 — No in-app authorization *(implemented 2026-06-01)*
OIDC only authenticated — **any** logged-in user could suspend/trigger CronJobs on **all** clusters.

**Fix applied:** two optional env vars (empty = backwards-compatible open access):
- `OIDC_ALLOWED_EMAILS` — login allow-list, enforced in `HandleCallback` (rejected accounts get 403, no session).
- `OIDC_OPERATOR_EMAILS` — only these may suspend/resume/trigger; everyone else is read-only. Enforced by `Authenticator.RequireOperator` middleware wrapping the three mutating POST routes in `server.go`.

Added `Authenticator.IsAllowed`/`CanOperate` + unit test `TestIsAllowedAndCanOperate`. Exposed in Helm (`oidc.allowedEmails`, `oidc.operatorEmails`) and `.env.example`.
**Follow-up (open):** the UI still renders action buttons for read-only users (they get a 403 toast on click). Hide the buttons when the session lacks operator rights.

### 🟠 SEC-14 — CSRF cookie missing `Secure` flag
**File:** `internal/api/csrf.go` — the session cookie got `Secure` (SEC-4) but the CSRF cookie did not. Add the same https-derived `Secure` flag.

### 🟡 SEC-15 — CDN scripts lack SRI
htmx (unpkg), Google Fonts, Chart.js (jsdelivr) are loaded without `integrity`/`crossorigin`. Add SRI hashes or vendor them into `/static` (CSS is already embedded).

## ⚡ Performance

### ✅ PERF-1 — Dashboard / ListClusters N+1 query storm *(fixed 2026-06-01)*
**File:** `internal/api/handlers_cluster.go` — both handlers looped every cluster → every cronjob → `ListJobRuns` (loading **all** run rows) just to count running runs. Replaced with a single `GetRunningRuns` grouped by cluster (`runningCountByCluster` helper, keyed on the `clusterID/…` prefix of `cronjob_id`).

### 🟡 PERF-2 — HTMX 10 s polling re-computes per cronjob (open)
`cronJobTableBodyPoll` triggers `buildCronJobRow` (4 store calls per cronjob) every 10 s per viewer. Fine for small installs; a per-cluster snapshot cache refreshed on informer events would scale better.

## 📚 Documentation

### ✅ DOC-1 — Stale README *(fixed 2026-06-01)*
README still told contributors to install `templ` and run `templ generate` (templ was removed in S1), claimed `.templ` compiled templates, and advertised non-existent "p95 duration". Corrected the Local-dev steps, Architecture note, and stats wording.

### ✅ DOC-2 — `LOG_RETENTION_DAYS` undocumented / not in chart *(fixed 2026-06-01)*
The var existed in code but was absent from `values.yaml`/`deployment.yaml` and the README table; `RETENTION_DAYS` default also disagreed across docs (90 in code, 7 in Helm/README). Reconciled to the code defaults (90 / 14), exposed `config.logRetentionDays` in the chart, and documented both. *(Note: Helm previously defaulted retention to 7 days; the chart now matches the code default of 90 — set `config.retentionDays` explicitly if a shorter window is desired.)*

## 🎬 Top 5 actions — Session 4 status

| # | Action | Status |
|---|---|---|
| 1 | Fix resource sampling never running (BUG-11) | ✅ Fixed 2026-06-01 |
| 2 | Fix `duration_ms` always NULL + regression test (BUG-12) | ✅ Fixed 2026-06-01 |
| 3 | Add authorization: login allow-list + operator/read-only (SEC-13) | ✅ Implemented 2026-06-01 |
| 4 | Fix stale README + expose `LOG_RETENTION_DAYS` (DOC-1/DOC-2) | ✅ Fixed 2026-06-01 |
| 5 | Collapse Dashboard/ListClusters N+1 into one query (PERF-1) | ✅ Fixed 2026-06-01 |

## Remaining open after Session 4
- SEC-7 (Dockerfile digest pins), SEC-8 (GHA SHA pins), SEC-9 (CI tool version pin), SEC-10 (seccompProfile), SEC-14 (CSRF `Secure`), SEC-15 (CDN SRI).
- SEC-13 UI follow-up (hide action buttons for read-only users).
- BUG-7, BUG-8, BUG-9 (heatmap UTC), BUG-10 (log retry for empty `succeeded` runs).
- PERF-2 (polling snapshot cache).
- MAINT-7 (dedupe trigger button), `htmlHead` unused `title` param, log-line HTML/`data-raw` wrapper helper, repeated date-format constant.
- Tests: `go test -race` in Linux CI; sampler metrics-enable wiring test.

## 🎯 Product / MVP note
The idea is strong and well-differentiated (historical, cross-cluster CronJob visibility — a real gap `kubectl`/k9s/Lens don't fill), and the execution (informers → SQLite → SSE → server-rendered HTML, single binary) is appropriately scoped. With BUG-11/BUG-12 fixed, the two headline features (resource tracking, duration trends) actually work now. The remaining highest-value gap for "usable by a team" is **failure alerting / webhooks** — monitoring without notification still requires someone watching the screen. Recommend that next, ahead of more UI.
