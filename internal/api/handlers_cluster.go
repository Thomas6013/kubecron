package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

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
	resp := make([]clusterResponse, 0, len(clusters))
	for _, c := range clusters {
		cronjobs, _ := h.store.ListCronJobs(ctx, c.ID)
		running := 0
		for _, cj := range cronjobs {
			runs, _ := h.store.ListJobRuns(ctx, cj.ID)
			for _, run := range runs {
				if run.Status == "running" {
					running++
				}
			}
		}
		resp = append(resp, clusterResponse{
			ID: c.ID, Name: c.Name,
			CronJobCount: len(cronjobs), RunningCount: running,
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
		http.Error(w, "failed to load clusters", http.StatusInternalServerError)
		return
	}

	cards := make([]card, 0, len(clusters))
	for _, c := range clusters {
		cjs, _ := h.store.ListCronJobs(ctx, c.ID)
		running := 0
		for _, cj := range cjs {
			runs, _ := h.store.ListJobRuns(ctx, cj.ID)
			for _, run := range runs {
				if run.Status == "running" {
					running++
				}
			}
		}
		cards = append(cards, card{c, len(cjs), running})
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, htmlHead("Dashboard"))
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

	cronjobs, err := h.store.ListCronJobs(ctx, clusterID)
	if err != nil {
		http.Error(w, "failed to load cronjobs", http.StatusInternalServerError)
		return
	}

	type row struct {
		storage.CronJob
		NextRun time.Time
		LastRun *storage.JobRun
		Stats7d *storage.RunStats
	}

	type nsGroup struct {
		Namespace string
		Rows      []row
	}

	// ListCronJobs returns rows ordered by namespace, name — preserve that order.
	var groups []nsGroup
	nsIdx := map[string]int{}
	for _, cj := range cronjobs {
		r := row{CronJob: cj}
		r.NextRun, _ = schedule.NextRun(cj.Schedule, time.Now())
		r.LastRun, _ = h.store.GetLastJobRun(ctx, cj.ID)
		r.Stats7d, _ = h.store.GetRunStats7d(ctx, cj.ID)
		if _, ok := nsIdx[cj.Namespace]; !ok {
			nsIdx[cj.Namespace] = len(groups)
			groups = append(groups, nsGroup{Namespace: cj.Namespace})
		}
		i := nsIdx[cj.Namespace]
		groups[i].Rows = append(groups[i].Rows, r)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, htmlHeadSidebar(clusterID, buildNsSidebar(clusterID, cronjobs, "")))

	// Breadcrumb + page header inside main area
	fmt.Fprint(w, `<div class="page-content">`)
	fmt.Fprint(w, breadcrumb(
		`<a href="/">clusters</a>`,
		`<span>`+esc(clusterID)+`</span>`,
	))

	if len(groups) == 0 {
		fmt.Fprint(w, `<div class="card" style="text-align:center;padding:3rem;color:var(--muted);font-family:var(--font-mono);">No CronJobs found in this cluster.</div>`)
	} else {
		for _, g := range groups {
			fmt.Fprintf(w, `<div class="ns-section" id="ns-%s">`, esc(g.Namespace))
			fmt.Fprintf(w, `<div class="ns-header"><span class="ns-tag">namespace</span><span class="ns-name">%s</span><span class="ns-count">%d cron(s)</span></div>`,
				esc(g.Namespace), len(g.Rows))

			fmt.Fprint(w, `<div class="card" style="padding:0;overflow:hidden;"><table>
<thead><tr>
  <th>Name</th><th>Schedule</th><th>Next run</th>
  <th>Last status</th><th>7d</th><th>Resources</th><th>Actions</th>
</tr></thead><tbody>`)

			for _, row := range g.Rows {
				lastStatus := `<span style="color:var(--muted);">—</span>`
				if row.LastRun != nil {
					lastStatus = statusBadge(row.LastRun.Status)
				}

				stats7d := `<span style="color:var(--muted);">—</span>`
				if row.Stats7d != nil && row.Stats7d.Total > 0 {
					color := "var(--green)"
					if row.Stats7d.Failed > 0 {
						color = "var(--yellow)"
					}
					if row.Stats7d.Succeeded == 0 {
						color = "var(--red)"
					}
					stats7d = fmt.Sprintf(`<span style="font-family:var(--font-mono);font-size:0.8rem;color:%s;">%d/%d</span>`,
						color, row.Stats7d.Succeeded, row.Stats7d.Total)
				}

				resources := `<span style="color:var(--muted);">—</span>`
				if row.CPURequest != nil || row.MemoryRequest != nil {
					resources = ""
					if row.CPURequest != nil {
						resources += fmt.Sprintf(`<span class="badge" style="margin-right:3px;">cpu:%s</span>`, esc(*row.CPURequest))
					}
					if row.MemoryRequest != nil {
						resources += fmt.Sprintf(`<span class="badge">mem:%s</span>`, esc(*row.MemoryRequest))
					}
				}

				nameColor := "var(--accent)"
				suspendedTag := ""
				if row.Suspended {
					nameColor = "var(--yellow)"
					suspendedTag = ` <span class="badge badge-suspended">paused</span>`
				}

				suspendBtn := fmt.Sprintf(
					`<button class="btn ghost" style="font-size:0.75rem;"
					  hx-post="/api/clusters/%s/cronjobs/%s/%s/suspend"
					  hx-confirm="Suspend %s?"
					  hx-swap="none"
					  hx-on::after-request="if(event.detail.successful){showToast('Suspended',true);setTimeout(()=>location.reload(),800);}">⏸</button>`,
					esc(clusterID), esc(row.Namespace), esc(row.Name), esc(row.Name))
				if row.Suspended {
					suspendBtn = fmt.Sprintf(
						`<button class="btn" style="font-size:0.75rem;"
						  hx-post="/api/clusters/%s/cronjobs/%s/%s/resume"
						  hx-confirm="Resume %s?"
						  hx-swap="none"
						  hx-on::after-request="if(event.detail.successful){showToast('Resumed',true);setTimeout(()=>location.reload(),800);}">▶ Resume</button>`,
						esc(clusterID), esc(row.Namespace), esc(row.Name), esc(row.Name))
				}

				triggerBtn := fmt.Sprintf(
					`<button class="btn" style="font-size:0.75rem;margin-left:4px;"
					  hx-post="/api/clusters/%s/cronjobs/%s/%s/trigger"
					  hx-confirm="Trigger a manual run of %s?"
					  hx-swap="none"
					  hx-on::after-request="if(event.detail.successful){var d=JSON.parse(event.detail.xhr.responseText);window.location='/clusters/%s/cronjobs/%s/%s/runs/'+d.run_id;}else{showToast('Trigger failed',false);}">▶ Run</button>`,
					esc(clusterID), esc(row.Namespace), esc(row.Name), esc(row.Name),
					esc(clusterID), esc(row.Namespace), esc(row.Name))

				fmt.Fprintf(w, `
<tr style="cursor:pointer;" onclick="window.location='/clusters/%s/cronjobs/%s/%s/runs'">
  <td><span style="font-family:var(--font-mono);color:%s;">%s</span>%s</td>
  <td><code style="font-size:0.8rem;color:var(--muted);">%s</code></td>
  <td>%s</td>
  <td>%s</td>
  <td>%s</td>
  <td>%s</td>
  <td style="white-space:nowrap;" onclick="event.stopPropagation()">%s%s</td>
</tr>`,
					esc(clusterID), esc(row.Namespace), esc(row.Name),
					nameColor, esc(row.Name), suspendedTag,
					esc(row.Schedule),
					countdownSpan(row.NextRun),
					lastStatus, stats7d, resources,
					suspendBtn, triggerBtn,
				)
			}

			fmt.Fprint(w, `</tbody></table></div></div>`) // card + ns-section
		}
	}

	fmt.Fprint(w, `</div>`) // page-content
	fmt.Fprint(w, countdownJS)
	fmt.Fprint(w, htmlFootSidebar)
}

// NamespaceDetail shows all CronJobs in a single namespace.
func (h *Handler) NamespaceDetail(w http.ResponseWriter, r *http.Request) {
	clusterID := r.PathValue("clusterID")
	ns := r.PathValue("ns")
	ctx := r.Context()

	allCronJobs, err := h.store.ListCronJobs(ctx, clusterID)
	if err != nil {
		http.Error(w, "failed to load cronjobs", http.StatusInternalServerError)
		return
	}

	type row struct {
		storage.CronJob
		NextRun time.Time
		LastRun *storage.JobRun
		Stats7d *storage.RunStats
	}
	var rows []row
	for _, cj := range allCronJobs {
		if cj.Namespace != ns {
			continue
		}
		r2 := row{CronJob: cj}
		r2.NextRun, _ = schedule.NextRun(cj.Schedule, time.Now())
		r2.LastRun, _ = h.store.GetLastJobRun(ctx, cj.ID)
		r2.Stats7d, _ = h.store.GetRunStats7d(ctx, cj.ID)
		rows = append(rows, r2)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, htmlHeadSidebar(clusterID, buildNsSidebar(clusterID, allCronJobs, ns)))
	fmt.Fprint(w, `<div class="page-content">`)
	fmt.Fprint(w, breadcrumb(
		`<a href="/">clusters</a>`,
		`<a href="/clusters/`+esc(clusterID)+`">`+esc(clusterID)+`</a>`,
		`<span>`+esc(ns)+`</span>`,
	))

	if len(rows) == 0 {
		fmt.Fprint(w, `<div class="card" style="text-align:center;padding:3rem;color:var(--muted);font-family:var(--font-mono);">No CronJobs in this namespace.</div>`)
	} else {
		fmt.Fprint(w, `<div class="card" style="padding:0;overflow:hidden;"><table>
<thead><tr>
  <th>Name</th><th>Schedule</th><th>Next run</th>
  <th>Last status</th><th>7d</th><th>Resources</th><th>Actions</th>
</tr></thead><tbody>`)

		for _, row := range rows {
			lastStatus := `<span style="color:var(--muted);">—</span>`
			if row.LastRun != nil {
				lastStatus = statusBadge(row.LastRun.Status)
			}
			stats7d := `<span style="color:var(--muted);">—</span>`
			if row.Stats7d != nil && row.Stats7d.Total > 0 {
				color := "var(--green)"
				if row.Stats7d.Failed > 0 {
					color = "var(--yellow)"
				}
				if row.Stats7d.Succeeded == 0 {
					color = "var(--red)"
				}
				stats7d = fmt.Sprintf(`<span style="font-family:var(--font-mono);font-size:0.8rem;color:%s;">%d/%d</span>`,
					color, row.Stats7d.Succeeded, row.Stats7d.Total)
			}
			resources := `<span style="color:var(--muted);">—</span>`
			if row.CPURequest != nil || row.MemoryRequest != nil {
				resources = ""
				if row.CPURequest != nil {
					resources += fmt.Sprintf(`<span class="badge" style="margin-right:3px;">cpu:%s</span>`, esc(*row.CPURequest))
				}
				if row.MemoryRequest != nil {
					resources += fmt.Sprintf(`<span class="badge">mem:%s</span>`, esc(*row.MemoryRequest))
				}
			}
			nameColor := "var(--accent)"
			suspendedTag := ""
			if row.Suspended {
				nameColor = "var(--yellow)"
				suspendedTag = ` <span class="badge badge-suspended">paused</span>`
			}
			suspendBtn := fmt.Sprintf(
				`<button class="btn ghost" style="font-size:0.75rem;"
				  hx-post="/api/clusters/%s/cronjobs/%s/%s/suspend"
				  hx-confirm="Suspend %s?"
				  hx-swap="none"
				  hx-on::after-request="if(event.detail.successful){showToast('Suspended',true);setTimeout(()=>location.reload(),800);}">⏸</button>`,
				esc(clusterID), esc(row.Namespace), esc(row.Name), esc(row.Name))
			if row.Suspended {
				suspendBtn = fmt.Sprintf(
					`<button class="btn" style="font-size:0.75rem;"
					  hx-post="/api/clusters/%s/cronjobs/%s/%s/resume"
					  hx-confirm="Resume %s?"
					  hx-swap="none"
					  hx-on::after-request="if(event.detail.successful){showToast('Resumed',true);setTimeout(()=>location.reload(),800);}">▶ Resume</button>`,
					esc(clusterID), esc(row.Namespace), esc(row.Name), esc(row.Name))
			}
			triggerBtn := fmt.Sprintf(
				`<button class="btn" style="font-size:0.75rem;margin-left:4px;"
				  hx-post="/api/clusters/%s/cronjobs/%s/%s/trigger"
				  hx-confirm="Trigger a manual run of %s?"
				  hx-swap="none"
				  hx-on::after-request="if(event.detail.successful){var d=JSON.parse(event.detail.xhr.responseText);window.location='/clusters/%s/cronjobs/%s/%s/runs/'+d.run_id;}else{showToast('Trigger failed',false);}">▶ Run</button>`,
				esc(clusterID), esc(row.Namespace), esc(row.Name), esc(row.Name),
				esc(clusterID), esc(row.Namespace), esc(row.Name))

			fmt.Fprintf(w, `
<tr style="cursor:pointer;" onclick="window.location='/clusters/%s/cronjobs/%s/%s/runs'">
  <td><span style="font-family:var(--font-mono);color:%s;">%s</span>%s</td>
  <td><code style="font-size:0.8rem;color:var(--muted);">%s</code></td>
  <td>%s</td>
  <td>%s</td>
  <td>%s</td>
  <td>%s</td>
  <td style="white-space:nowrap;" onclick="event.stopPropagation()">%s%s</td>
</tr>`,
				esc(clusterID), esc(row.Namespace), esc(row.Name),
				nameColor, esc(row.Name), suspendedTag,
				esc(row.Schedule),
				countdownSpan(row.NextRun),
				lastStatus, stats7d, resources,
				suspendBtn, triggerBtn,
			)
		}
		fmt.Fprint(w, `</tbody></table></div>`)
	}

	fmt.Fprint(w, `</div>`) // page-content
	fmt.Fprint(w, countdownJS)
	fmt.Fprint(w, htmlFootSidebar)
}

// buildNsSidebar returns the sidebar HTML for namespace navigation.
// activeNS is highlighted; pass "" for no active item (e.g. ClusterDetail).
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

func derefStr(s *string) string {
	if s == nil {
		return "—"
	}
	return *s
}
