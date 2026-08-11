package api

import (
	"fmt"
	"strings"

	"github.com/kubecron/kubecron/internal/storage"
)

// ── Overview page components ─────────────────────────────────────────────────
//
// The overview answers one question: where should attention go first. It leads
// with fleet-wide counters, then ranks CronJobs on the four axes that actually
// drive action — failures, wall-clock time, CPU and memory.

// overviewWindows are the ranking windows offered by the range switch, in the
// order they are rendered.
var overviewWindows = []int{1, 7, 30}

// overviewWindowLabel names a window in the range switch.
func overviewWindowLabel(days int) string {
	if days == 1 {
		return "24h"
	}
	return fmt.Sprintf("%dd", days)
}

// normalizeWindow clamps a requested window to one the overview offers,
// defaulting to 7 days. Anything else would silently produce a page whose
// heading disagrees with the data underneath it.
func normalizeWindow(days int) int {
	for _, w := range overviewWindows {
		if days == w {
			return w
		}
	}
	return 7
}

// sectionHeading renders the small uppercase label that separates the blocks
// of a page.
func sectionHeading(text string) string {
	return fmt.Sprintf(`<h2 class="section-heading">%s</h2>`, esc(text))
}

// statTile renders one KPI in the overview's top row. tone selects the accent
// colour and is one of "", "green", "red", "yellow", "running".
func statTile(label, value, sub, tone string) string {
	color := "var(--text)"
	switch tone {
	case "green":
		color = "var(--green)"
	case "red":
		color = "var(--red)"
	case "yellow":
		color = "var(--yellow)"
	case "running":
		color = "var(--running)"
	}
	subHTML := ""
	if sub != "" {
		subHTML = fmt.Sprintf(`<div class="stat-tile-sub">%s</div>`, esc(sub))
	}
	return fmt.Sprintf(`<div class="stat-tile">
  <div class="stat-tile-label">%s</div>
  <div class="stat-tile-value" style="color:%s;">%s</div>
  %s
</div>`, esc(label), color, esc(value), subHTML)
}

// rangeSwitchFor renders the 24h / 7d / 30d selector, marking active as
// current and keeping the reader on basePath. basePath must already be
// URL-escaped by the caller.
func rangeSwitchFor(basePath string, active int) string {
	var b strings.Builder
	b.WriteString(`<div class="range-switch">`)
	for _, w := range overviewWindows {
		cls := "range-opt"
		if w == active {
			cls += " active"
		}
		fmt.Fprintf(&b, `<a class="%s" href="%s?days=%d">%s</a>`, cls, basePath, w, overviewWindowLabel(w))
	}
	b.WriteString(`</div>`)
	return b.String()
}

// topList renders one ranked table. fmtValue turns the rank's Value into its
// display string in the unit the metric implies. An empty ranking renders an
// explicit empty state rather than a bare heading, so that "nothing to see" is
// distinguishable from "this panel is broken".
func topList(title, hint string, ranks []storage.CronJobRank, fmtValue func(int64) string) string {
	var b strings.Builder
	b.WriteString(`<div class="card top-list">`)
	fmt.Fprintf(&b, `<div class="top-list-head"><span class="top-list-title">%s</span><span class="top-list-hint">%s</span></div>`,
		esc(title), esc(hint))

	if len(ranks) == 0 {
		b.WriteString(`<div class="top-list-empty">no data in this window</div></div>`)
		return b.String()
	}

	// The bars are scaled against the leader, so the list reads as a
	// distribution rather than a column of unrelated numbers.
	top := ranks[0].Value
	if top <= 0 {
		top = 1
	}

	b.WriteString(`<div class="top-list-body">`)
	for _, r := range ranks {
		pct := float64(r.Value) / float64(top) * 100
		if pct < 2 {
			pct = 2 // keep the smallest bar visible
		}
		failedBadge := ""
		if r.Failed > 0 {
			failedBadge = fmt.Sprintf(`<span class="badge badge-failed" style="margin-left:6px;">%d fail</span>`, r.Failed)
		}
		fmt.Fprintf(&b, `<div class="top-row">
  <div class="top-row-head">
    <a class="top-row-name" href="/clusters/%s/cronjobs/%s/%s/runs" title="%s">%s</a>
    <span class="top-row-value">%s</span>
  </div>
  <div class="top-row-meta">
    <span class="top-row-loc">%s / %s</span>
    <span class="top-row-runs">%d run(s)%s</span>
  </div>
  <div class="top-bar"><div class="top-bar-fill" style="width:%.1f%%;"></div></div>
</div>`,
			esc(r.ClusterID), esc(r.Namespace), esc(r.Name),
			esc(r.CronJobID), esc(r.Name),
			esc(fmtValue(r.Value)),
			esc(r.ClusterID), esc(r.Namespace),
			r.Runs, failedBadge,
			pct,
		)
	}
	b.WriteString(`</div></div>`)
	return b.String()
}

// fmtMillicores renders a CPU quantity the way Kubernetes writes it: millicores
// below one core, then cores with two decimals.
func fmtMillicores(m int64) string {
	if m < 1000 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%.2f cores", float64(m)/1000)
}

// fmtBytes renders a byte count in binary units, matching the MiB convention
// already used on the run detail page.
func fmtBytes(b int64) string {
	const (
		kib = 1 << 10
		mib = 1 << 20
		gib = 1 << 30
	)
	switch {
	case b >= gib:
		return fmt.Sprintf("%.2f GiB", float64(b)/gib)
	case b >= mib:
		return fmt.Sprintf("%.0f MiB", float64(b)/mib)
	case b >= kib:
		return fmt.Sprintf("%.0f KiB", float64(b)/kib)
	default:
		return fmt.Sprintf("%d B", b)
	}
}
