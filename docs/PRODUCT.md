# KubeCron — Product Strategy

Where KubeCron stands as a product, what would make teams depend on it, and in
what order to build that. Written 2026-07-25, against v0.1.0 + the 0.2.0 work.

Companion documents: [`AUDIT.md`](AUDIT.md) grades the engineering;
[`../ROADMAP.md`](../ROADMAP.md) is the executable checklist. This file holds the
reasoning that decides what goes on that checklist.

---

## 1. Diagnosis

**KubeCron today is a viewer. Viewers get opened when you already suspect
something is wrong.**

The products teams depend on do one of two things: they **come and find you**, or
they **hold the only copy** of something that matters.

KubeCron already owns half of the second one without exploiting it: pod logs die
with the pod (`ttlSecondsAfterFinished`, node GC), and KubeCron keeps them in
`log_lines`. That is the one thing `kubectl` cannot reproduce after the fact. It
has none of the first: there is no notification path anywhere in the codebase —
no webhook, no mail. `/metrics` delegates alerting to Prometheus, which means the
dependency accrues to Alertmanager, not to KubeCron.

Three concrete symptoms of that positioning:

| Symptom | Where |
|---|---|
| The home page does not say what is wrong | `internal/api/handlers_cluster.go` `Dashboard` renders cluster cards with a CronJob count and a running count. No failure state at all — finding a broken job means clicking cluster → namespace → CronJob → runs. |
| Log search is a toy | `internal/api/html_log.go` `logSearchJS` is a browser-side regex over **one run**, capped at the last 5000 lines. The real question is "since when has this job been printing `connection refused`?" — across runs, across clusters, over 90 days of pods that no longer exist. |
| No notion of *expected* | The schedule package and `job_runs.duration_ms` give a baseline, but nothing anywhere defines *abnormal*. A job running 47 minutes instead of 4 is reported to nobody. |

---

## 2. Prerequisite: stop being wrong (shipped in 0.2.0)

Nobody depends on a dashboard that is wrong, and nothing below matters until it
is right. These were the three findings that made the UI disagree with the
cluster, and they are done:

- **DOM-1** — `spec.timeZone` was ignored, so next-run countdowns and missed-run
  detection were evaluated in the server's zone. A `TZ: America/New_York` job was
  flagged `missed` while perfectly healthy.
- **BUG-20** — deleted CronJobs and clusters were never removed or flagged: ghost
  rows in the UI and Prometheus series frozen forever on their last value.
- **PERF-2** — cluster pages issued three queries per CronJob, re-run every 10 s
  per open tab, each one sorting that CronJob's whole run history through a temp
  B-tree. It broke at exactly the scale where dependency becomes possible.

The remaining trust risk is **architectural, not cosmetic**: one SQLite file on
one PVC with `strategy: Recreate` means no HA, downtime on every deploy, and no
backup or export of the only copy of the logs. That is tolerable for a viewer and
unacceptable for something an on-call rotation relies on. It is what makes the S3
backend (§4.3) a durability decision, not a storage-cost one.

---

## 3. Lever 1 — "it comes and finds me"

The biggest gap and the best return. This is the work that changes what the
product *is*.

### 3.1 Make the home page "what is wrong, everywhere"

Replace the counter cards with a global cross-cluster view ordered by severity:
failures in the last 24 h, missed runs, runs going abnormally long, jobs suspended
for more than a week. Cheap (one aggregate query and a handler) and it makes the
README's "single pane of glass" claim true — today the pane does not show status.

### 3.2 Native alerting with the log attached ← *the feature that makes it hard to replace*

Not an Alertmanager substitute. Alertmanager can say
`kubecron_last_run_status{job=x} == 0`. It cannot attach the last 20 lines of a
dead pod's log. KubeCron can.

> ❌ `billing-export` failed (exit 1) on `prod-eu`. Normally 4 min, failed after
> 3 min 12 s. — followed by the last 15 log lines as a code block, and a link to
> the run.

That message replaces opening four tabs and a `kubectl`. Destinations: generic
webhook, Slack, Teams, SMTP.

### 3.3 Stuck / runaway run detection

The baseline is already in the database. Alert when the current duration exceeds
`max(p95 × 2, median × 3)`. Nothing in the CronJob ecosystem does this simply,
and it is the scenario that costs the most in practice: a job running six hours,
holding a database lock, burning cloud spend.

### 3.4 The daily digest ← *the cheapest habit-former*

One message per morning per team: what failed, what got slower, what is new, what
has stopped running. Passive, recurring, delivered. This installs the product in
a routine far more effectively than any UI feature. Roughly 1–2 days of work on
top of 3.2.

---

## 4. Lever 2 — "it holds the only copy"

The defensible moat.

### 4.1 Full-text search across runs and clusters ← *the killer feature*

SQLite ships **FTS5**: no new dependency, one migration, one virtual table over
`log_lines`. It unlocks questions no other CronJob tool can answer:

- `OutOfMemory` on any job, any cluster, in the last 30 days
- the first occurrence of a given stack trace → the exact date of a regression
- every job that touched `db-replica-3` before Tuesday's incident

This is the product. Live streaming is pleasant, but Lens and k9s do that too; a
searchable archive of logs from destroyed pods is unique.

### 4.2 Failure fingerprinting and grouping

Extract the error signature (last `ERROR` / `panic` / `Traceback` line,
normalised with IDs and timestamps masked) and group on it: "this same failure
occurred 14 times, on 3 clusters, since Tuesday 14:00". Turns logs into
intelligence, and feeds alert deduplication for §3.2.

### 4.3 Object-storage backend for logs (already PERF-1)

Two wins at once: unlimited retention, so the "only copy" argument becomes real
(a year, not 14 days), and the removal of the single point of data loss described
in §2.

### 4.4 The run journal, written by the job itself ← *the deepest lock-in, nearly free*

Have the streamer parse structured markers out of the log stream:

```
kubecron: rows_processed=14203 files=87 revenue_eur=41200
```

One `print()` on the job side. No sidecar, no SDK, no image change. KubeCron
stores those key/values per run and trends them over time. That moves the product
from observing *infrastructure* to holding the **business record**: "yesterday's
export processed 14,203 rows against 87,000 the day before" — a green job that
processed nothing is an incident nobody detects today. And once jobs *write* into
KubeCron, it is no longer optional; it is a system of record.

---

## 5. Lever 3 — "it knows things nobody else knows"

These do not serve day-to-day ops. They give someone a reason to defend the tool
in a budget meeting.

### 5.1 Right-sizing recommendations

`cronjobs` already stores `cpu_request` / `cpu_limit`, and `job_runs` already
stores `max_cpu_millicores` / `max_memory_bytes`. The recommendation is a join:
"37 over-provisioned jobs — `nightly-etl` requests 2 CPU, observed peak 180 m."
The data is there; only a page is missing. Best value-to-effort ratio in this
document.

### 5.2 Cost per run / job / namespace

`resource_samples` × a configurable €/vCPU·h rate. Quantifying waste turns the
tool into a measurable source of savings, which is what turns a side project into
a budget line.

### 5.3 Declarative SLOs and ownership from annotations

Read `kubecron.io/owner`, `/runbook`, `/description`, `/schedule-sla` from
Kubernetes annotations. Two effects: the runbook link makes KubeCron the on-call
entry point, and `owner` becomes the **alert routing key** for §3.2. Then a
declared SLO ("must run before 06:00", "must finish under 10 min") gives the
product a *contract to verify* — and therefore a monthly compliance report
somebody forwards to management. A recurring report is the final form of lock-in.

---

## 6. Phase plan

| Phase | Contents | Why this order |
|---|---|---|
| **0.2** ✅ | DOM-1, BUG-20, PERF-2 | The product must stop being wrong, and hold up under load, before inviting people in |
| **0.3** | Global "what is wrong" home page, alerting (webhook/Slack) **with log excerpt**, stuck-run detection | The viewer → product switch. The release that counts. |
| **0.4** | FTS5 cross-run search, failure fingerprinting, daily digest | The moat, plus the habit |
| **0.5** | Annotations/ownership → alert routing, right-sizing page, cost | The internal sponsor |
| **0.6+** | Object-storage logs, `kubecron:` business markers, SLOs + reports, HA (Postgres option) | Organisational scale |

Two cross-cutting adoption items: **time to first value** (today you base64 one
kubeconfig per cluster, while `rest.InClusterConfig()` already exists in
`internal/cluster/manager.go` — make it the default zero-config path, "helm
install and it already monitors this cluster"), and a **README screenshot** —
nobody installs a UI tool without seeing the UI.

---

## 7. Anti-goals

- **Do not rebuild Grafana.** The advantage is CronJob specificity, not
  generality. Ship a Grafana dashboard JSON and stop there.
- **Do not add React/Vue or a Node build step.** "No build step" is a real
  maintainability asset for a solo project, and HTMX covers everything above.
- **Do not do multi-tenancy before alerting.** Nobody asks for fine-grained RBAC
  on a tool they do not depend on yet.
- **Do not chase arm64 / CSP / HSTS now.** Real debts (INFRA-3, SEC-23) but they
  create no adoption. One exception: **SEC-22, vendoring htmx and Chart.js** — two
  hours, removes a supply-chain exposure on an authenticated tool, and unblocks
  air-gapped installs, which is precisely where CronJobs are business-critical.
- **Do not expand beyond CronJobs too early.** The only natural adjacency is the
  standalone Job (same informers, already running). Argo Workflows and Airflow are
  a different product.

---

## 8. The one-sentence version

KubeCron's only defensible asset is **memory** — it knows what happened after the
pods are gone. Everything that exploits that memory (cross-run search, duration
baselines, failure fingerprints, business history, right-sizing) builds a
dependency nobody can copy without first collecting the same six months of data.
Everything that merely displays the *present* is reproducible by anyone in a
weekend.
