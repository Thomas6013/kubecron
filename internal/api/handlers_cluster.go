package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kubecron/kubecron/internal/auth"
	"github.com/kubecron/kubecron/internal/schedule"
	"github.com/kubecron/kubecron/internal/storage"
)

// ── JSON ─────────────────────────────────────────────────────────────────────

type clusterResponse struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	CronJobCount   int    `json:"cronjob_count"`
	RunningCount   int    `json:"running_count"`
	MetricsEnabled bool   `json:"metrics_enabled"`
}

func (h *Handler) ListClusters(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	clusters, err := h.store.ListClusters(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list clusters")
		return
	}
	runningByCluster := h.runningCountByCluster(ctx)

	resp := make([]clusterResponse, 0, len(clusters))
	for _, c := range clusters {
		cronjobs, _ := h.store.ListCronJobs(ctx, c.ID)
		resp = append(resp, clusterResponse{
			ID: c.ID, Name: c.Name,
			CronJobCount: len(cronjobs), RunningCount: runningByCluster[c.ID],
			MetricsEnabled: c.MetricsEnabled,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// ── UI — Dashboard ────────────────────────────────────────────────────────────

func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	type card struct {
		storage.Cluster
		CronJobCount int
		RunningCount int
	}

	clusters, err := h.store.ListClusters(ctx)
	if err != nil {
		slog.Error("dashboard: failed to load clusters", "err", err)
		http.Error(w, "failed to load clusters", http.StatusInternalServerError)
		return
	}

	runningByCluster := h.runningCountByCluster(ctx)

	cards := make([]card, 0, len(clusters))
	for _, c := range clusters {
		cjs, _ := h.store.ListCronJobs(ctx, c.ID)
		cards = append(cards, card{c, len(cjs), runningByCluster[c.ID]})
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, htmlHead("Dashboard", auth.EmailFromContext(ctx)))
	fmt.Fprint(w, `<div class="container">`)
	fmt.Fprint(w, `<h1 style="font-family:var(--font-mono);color:var(--accent);margin-bottom:1.5rem;">[KubeCron]</h1>`)

	if len(cards) == 0 {
		fmt.Fprint(w, `<div class="card" style="text-align:center;padding:3rem;color:var(--muted);font-family:var(--font-mono);">
			No clusters loaded — drop a kubeconfig in <code>dev/kubeconfigs/</code> and restart.
		</div>`)
	} else {
		fmt.Fprint(w, `<div class="grid grid-2">`)
		for _, c := range cards {
			metricsClass := "metrics-dot"
			if c.MetricsEnabled {
				metricsClass = "metrics-dot enabled"
			}
			runningBadge := ""
			if c.RunningCount > 0 {
				runningBadge = fmt.Sprintf(` &nbsp;<span class="badge badge-running">%d running</span>`, c.RunningCount)
			}
			fmt.Fprintf(w, `
<a href="/clusters/%s" style="text-decoration:none;">
  <div class="card" style="cursor:pointer;"
       onmouseover="this.style.borderColor='var(--accent)'"
       onmouseout="this.style.borderColor='var(--border)'">
    <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:.75rem;">
      <span style="font-family:var(--font-mono);color:var(--accent);font-size:1.05rem;">%s</span>
      <span class="%s" title="Metrics API"></span>
    </div>
    <div style="font-family:var(--font-mono);font-size:0.8rem;color:var(--muted);">
      %d cronjob(s)%s
    </div>
  </div>
</a>`,
				esc(c.ID), esc(c.Name), metricsClass, c.CronJobCount, runningBadge)
		}
		fmt.Fprint(w, `</div>`)
	}

	fmt.Fprint(w, `</div>`)
	fmt.Fprint(w, htmlFoot)
}

// ── UI — Cluster detail (CronJobs grouped by namespace) ──────────────────────

func (h *Handler) ClusterDetail(w http.ResponseWriter, r *http.Request) {
	clusterID := r.PathValue("clusterID")
	ctx := r.Context()

	cronjobs, summaries, runningCount, err := h.cronJobRowInputs(ctx, clusterID)
	if err != nil {
		slog.Error("cluster detail: failed to load cronjobs", "cluster", clusterID, "err", err)
		http.Error(w, "failed to load cronjobs", http.StatusInternalServerError)
		return
	}

	type nsGroup struct {
		Namespace string
		Rows      []cronJobRowData
	}

	now := time.Now()
	var groups []nsGroup
	nsIdx := map[string]int{}
	for _, cj := range cronjobs {
		row := buildCronJobRow(clusterID, cj, summaries[cj.ID], runningCount, now)
		if _, ok := nsIdx[cj.Namespace]; !ok {
			nsIdx[cj.Namespace] = len(groups)
			groups = append(groups, nsGroup{Namespace: cj.Namespace})
		}
		i := nsIdx[cj.Namespace]
		groups[i].Rows = append(groups[i].Rows, row)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, htmlHeadSidebar(clusterID, buildNsSidebar(clusterID, cronjobs, ""), auth.EmailFromContext(ctx)))
	fmt.Fprint(w, `<div class="page-content">`)
	fmt.Fprint(w, breadcrumb(
		`<a href="/">clusters</a>`,
		`<span>`+esc(clusterID)+`</span>`,
	))

	if len(groups) == 0 {
		fmt.Fprint(w, `<div class="card" style="text-align:center;padding:3rem;color:var(--muted);font-family:var(--font-mono);">No CronJobs found in this cluster.</div>`)
	} else {
		for _, g := range groups {
			pollURL := fmt.Sprintf("/clusters/%s/cronjobs/%s/rows", esc(clusterID), esc(g.Namespace))
			fmt.Fprintf(w, `<div class="ns-section" id="ns-%s">`, esc(g.Namespace))
			fmt.Fprintf(w, `<div class="ns-header"><span class="ns-tag">namespace</span><span class="ns-name">%s</span><span class="ns-count">%d cron(s)</span></div>`,
				esc(g.Namespace), len(g.Rows))
			fmt.Fprint(w, cronJobTableOpen)
			fmt.Fprint(w, cronJobTableBodyPoll(pollURL))
			for _, row := range g.Rows {
				fmt.Fprint(w, renderCronJobRow(row))
			}
			fmt.Fprint(w, `</tbody></table></div></div>`)
		}
	}

	fmt.Fprint(w, `</div>`)
	fmt.Fprint(w, countdownJS)
	fmt.Fprint(w, htmlFootSidebar)
}

// ── UI — Namespace detail ─────────────────────────────────────────────────────

func (h *Handler) NamespaceDetail(w http.ResponseWriter, r *http.Request) {
	clusterID := r.PathValue("clusterID")
	ns := r.PathValue("ns")
	ctx := r.Context()

	allCronJobs, summaries, runningCount, err := h.cronJobRowInputs(ctx, clusterID)
	if err != nil {
		slog.Error("namespace view: failed to load cronjobs",
			"cluster", clusterID, "namespace", ns, "route", r.Pattern, "err", err)
		http.Error(w, "failed to load cronjobs", http.StatusInternalServerError)
		return
	}

	now := time.Now()
	var rows []cronJobRowData
	for _, cj := range allCronJobs {
		if cj.Namespace != ns {
			continue
		}
		rows = append(rows, buildCronJobRow(clusterID, cj, summaries[cj.ID], runningCount, now))
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, htmlHeadSidebar(clusterID, buildNsSidebar(clusterID, allCronJobs, ns), auth.EmailFromContext(ctx)))
	fmt.Fprint(w, `<div class="page-content">`)
	fmt.Fprint(w, breadcrumb(
		`<a href="/">clusters</a>`,
		`<a href="/clusters/`+esc(clusterID)+`">`+esc(clusterID)+`</a>`,
		`<span>`+esc(ns)+`</span>`,
	))

	if len(rows) == 0 {
		fmt.Fprint(w, `<div class="card" style="text-align:center;padding:3rem;color:var(--muted);font-family:var(--font-mono);">No CronJobs in this namespace.</div>`)
	} else {
		pollURL := fmt.Sprintf("/clusters/%s/cronjobs/%s/rows", esc(clusterID), esc(ns))
		fmt.Fprint(w, cronJobTableOpen)
		fmt.Fprint(w, cronJobTableBodyPoll(pollURL))
		for _, row := range rows {
			fmt.Fprint(w, renderCronJobRow(row))
		}
		fmt.Fprint(w, `</tbody></table></div>`)
	}

	fmt.Fprint(w, `</div>`)
	fmt.Fprint(w, countdownJS)
	fmt.Fprint(w, htmlFootSidebar)
}

// buildNsSidebar returns the sidebar HTML for namespace navigation.
func buildNsSidebar(clusterID string, cronjobs []storage.CronJob, activeNS string) string {
	type nsEntry struct {
		Namespace string
		Count     int
	}
	var entries []nsEntry
	idx := map[string]int{}
	for _, cj := range cronjobs {
		if _, ok := idx[cj.Namespace]; !ok {
			idx[cj.Namespace] = len(entries)
			entries = append(entries, nsEntry{Namespace: cj.Namespace})
		}
		entries[idx[cj.Namespace]].Count++
	}
	var sb strings.Builder
	sb.WriteString(`<div class="sidebar-title">Namespaces</div>`)
	sb.WriteString(`<ul class="ns-list" id="ns-nav">`)
	for _, e := range entries {
		activeClass := ""
		if e.Namespace == activeNS {
			activeClass = " active"
		}
		fmt.Fprintf(&sb, `<li><a class="ns-link%s" href="/clusters/%s/cronjobs/%s" data-ns="ns-%s"><span class="ns-link-name">%s</span><span class="ns-link-count">%d</span></a></li>`,
			activeClass, esc(clusterID), esc(e.Namespace), esc(e.Namespace), esc(e.Namespace), e.Count)
	}
	sb.WriteString(`</ul>`)
	if len(cronjobs) > 0 {
		fmt.Fprintf(&sb, `<div class="sidebar-stats"><span>%d cron(s)</span><span>%d ns</span></div>`, len(cronjobs), len(entries))
	}
	return sb.String()
}

// NamespaceRows returns only the <tr> rows for a namespace.
// Called by HTMX polling on the ClusterDetail and NamespaceDetail pages.
func (h *Handler) NamespaceRows(w http.ResponseWriter, r *http.Request) {
	clusterID := r.PathValue("clusterID")
	ns := r.PathValue("ns")
	ctx := r.Context()

	// This endpoint serves a bare-<tr> fragment for HTMX polling only. If a real
	// browser navigates here directly (bookmark, refresh, history restore, or a
	// polling request that the browser promoted to a top-level navigation after
	// the tab was suspended), there is no HX-Request header — serve the full,
	// styled namespace page instead of unstyled naked rows.
	if r.Header.Get("HX-Request") != "true" {
		http.Redirect(w, r, fmt.Sprintf("/clusters/%s/cronjobs/%s",
			url.PathEscape(clusterID), url.PathEscape(ns)), http.StatusSeeOther)
		return
	}

	allCronJobs, summaries, runningCount, err := h.cronJobRowInputs(ctx, clusterID)
	if err != nil {
		slog.Error("namespace view: failed to load cronjobs",
			"cluster", clusterID, "namespace", ns, "route", r.Pattern, "err", err)
		http.Error(w, "failed to load cronjobs", http.StatusInternalServerError)
		return
	}

	now := time.Now()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	for _, cj := range allCronJobs {
		if cj.Namespace != ns {
			continue
		}
		fmt.Fprint(w, renderCronJobRow(buildCronJobRow(clusterID, cj, summaries[cj.ID], runningCount, now)))
	}
}

// runningCountByCluster returns the number of currently-running runs per
// cluster ID, using a single aggregate query. CronJob IDs have the form
// "clusterID/namespace/name", so the cluster is the first path segment.
func (h *Handler) runningCountByCluster(ctx context.Context) map[string]int {
	out := map[string]int{}
	byCronJob, err := h.store.CountRunningRuns(ctx)
	if err != nil {
		return out
	}
	for cronJobID, n := range byCronJob {
		if i := strings.IndexByte(cronJobID, '/'); i > 0 {
			out[cronJobID[:i]] += n
		}
	}
	return out
}

func derefStr(s *string) string {
	if s == nil {
		return "—"
	}
	return *s
}

// sparklineRuns is how many recent run durations the row sparkline plots.
const sparklineRuns = 20

// buildCronJobRow assembles the display data for one CronJob table row from
// data already in hand: sum may be nil when the CronJob has never run, and
// runningCount is keyed by CronJob ID. It performs no I/O — the cluster-wide
// reads happen once per render in cronJobRowInputs (PERF-2).
//
// Both the next-run countdown and missed detection resolve the schedule in the
// CronJob's own time zone; evaluating them server-local is what made the
// dashboard disagree with Kubernetes (DOM-1). When the zone cannot be resolved
// the row shows no countdown and claims no missed run, rather than showing a
// confidently wrong one.
func buildCronJobRow(clusterID string, cj storage.CronJob, sum *storage.CronJobSummary, runningCount map[string]int, now time.Time) cronJobRowData {
	row := cronJobRowData{ClusterID: clusterID, CronJob: cj}
	if sum != nil {
		row.LastRun = sum.LastRun
		row.Stats7d = sum.Stats7d
		row.Durations = sum.Durations
	}
	row.IsConcurrent = runningCount[cj.ID] > 1

	tz := cj.TZ()
	if next, err := schedule.NextRun(cj.Schedule, tz, now); err == nil {
		row.NextRun = next
	} else {
		row.ScheduleError = true
	}

	if !cj.Suspended {
		if prev, err := schedule.PrevRun(cj.Schedule, tz, now); err == nil {
			if now.Sub(prev) > 5*time.Minute {
				if row.LastRun == nil || (row.LastRun.Status != "running" && row.LastRun.StartedAt.Before(prev)) {
					row.IsMissed = true
				}
			}
		}
	}
	return row
}

// cronJobRowInputs fetches everything the CronJob rows of one cluster need:
// the CronJobs themselves plus, in a fixed number of queries, their summaries
// and running-run counts. This replaces the previous three-queries-per-CronJob
// fan-out, which the 10 s HTMX poll of every open tab re-ran in full (PERF-2).
func (h *Handler) cronJobRowInputs(ctx context.Context, clusterID string) (
	cronjobs []storage.CronJob,
	summaries map[string]*storage.CronJobSummary,
	runningCount map[string]int,
	err error,
) {
	cronjobs, err = h.store.ListCronJobs(ctx, clusterID)
	if err != nil {
		return nil, nil, nil, err
	}
	summaries, err = h.store.GetCronJobSummaries(ctx, clusterID, sparklineRuns)
	if err != nil {
		return nil, nil, nil, err
	}
	runningCount, err = h.store.CountRunningRuns(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	return cronjobs, summaries, runningCount, nil
}
