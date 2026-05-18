package api

import (
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/kubecron/kubecron/internal/storage"
)

// fmtDuration formats a millisecond duration in a human-readable way.
func fmtDuration(ms int64) string {
	switch {
	case ms < 1000:
		return fmt.Sprintf("%dms", ms)
	case ms < 60_000:
		return fmt.Sprintf("%.1fs", float64(ms)/1000)
	case ms < 3_600_000:
		return fmt.Sprintf("%dm %ds", ms/60_000, (ms%60_000)/1000)
	default:
		return fmt.Sprintf("%dh %dm", ms/3_600_000, (ms%3_600_000)/60_000)
	}
}

// esc safely escapes a string for HTML output.
func esc(s string) string { return html.EscapeString(s) }

// htmlHead returns the HTML <head> block + open <body> with nav and toast.
func htmlHead(title string) string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>` + esc(title) + ` — KubeCron</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link href="https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;600&family=Syne:wght@400;600;800&display=swap" rel="stylesheet">
<link rel="icon" type="image/svg+xml" href="/static/favicon.svg">
<link rel="icon" type="image/png" sizes="32x32" href="/static/favicon.png">
<link rel="stylesheet" href="/static/app.css">
<script src="https://unpkg.com/htmx.org@2.0.4" defer></script>
</head>
<body>
<nav>
  <a class="logo" href="/"><span style="font-family:var(--font-mono);">[KubeCron]</span></a>
  <a href="/">Clusters</a>
</nav>
<div id="toast"></div>
<script>
function showToast(msg, ok) {
  const t = document.getElementById('toast');
  t.textContent = msg;
  t.style.borderColor = ok ? 'var(--accent)' : 'var(--red)';
  t.style.color       = ok ? 'var(--accent)' : 'var(--red)';
  t.classList.add('show');
  setTimeout(() => t.classList.remove('show'), 3000);
}
document.body.addEventListener('htmx:afterRequest', function(e) {
  if (!e.detail.successful) showToast('Error ' + e.detail.xhr.status, false);
});
</script>
`
}

// htmlHeadSidebar starts a page with a 2-column layout (sidebar + main).
// sidebarHTML is injected raw into the aside; call htmlFoot to close.
func htmlHeadSidebar(title, sidebarHTML string) string {
	return htmlHead(title) + `<div class="page-layout"><aside class="sidebar">` + sidebarHTML + `</aside><div class="page-main">`
}

// htmlFootSidebar closes a sidebar-layout page.
const htmlFootSidebar = `</div></div>` + htmlFoot

const htmlFoot = `</body></html>`

// statusBadge returns a coloured HTML badge for a run status.
func statusBadge(status string) string {
	cls := map[string]string{
		"succeeded": "badge-success",
		"failed":    "badge-failed",
		"running":   "badge-running",
	}
	c, ok := cls[status]
	if !ok {
		c = "badge-muted"
	}
	return fmt.Sprintf(`<span class="badge %s">%s</span>`, c, esc(status))
}

// breadcrumb builds a "/" separated breadcrumb; each element is raw HTML.
func breadcrumb(parts ...string) string {
	var b strings.Builder
	b.WriteString(`<div class="breadcrumb">`)
	for i, p := range parts {
		if i > 0 {
			b.WriteString(`<span class="breadcrumb-sep">/</span>`)
		}
		b.WriteString(p)
	}
	b.WriteString(`</div>`)
	return b.String()
}

// countdownSpan renders a <span> with data-ts for client-side countdown JS.
func countdownSpan(t time.Time) string {
	return fmt.Sprintf(
		`<span class="countdown" data-ts="%d">%s</span>`,
		t.Unix(), t.Format("02/01 15:04"),
	)
}

// countdownJS is the inline script that powers all countdown timers on a page.
const countdownJS = `<script>
(function() {
  function update() {
    document.querySelectorAll('.countdown[data-ts]').forEach(function(el) {
      var diff = parseInt(el.dataset.ts) - Math.floor(Date.now() / 1000);
      if (diff <= 0) { el.textContent = 'now'; return; }
      var w = Math.floor(diff/604800), d = Math.floor((diff%604800)/86400);
      var h = Math.floor((diff%86400)/3600), m = Math.floor((diff%3600)/60), s = diff%60;
      if (w > 0)      el.textContent = d > 0 ? w+'w '+d+'j' : w+'w';
      else if (d > 0) el.textContent = d+'j '+h+'h';
      else if (h > 0) el.textContent = h+'h '+m+'m';
      else if (m > 0) el.textContent = m+'m '+s+'s';
      else            el.textContent = s+'s';
    });
  }
  update();
  setInterval(update, 1000);
})();
</script>`

// logSearchBar returns the search toolbar HTML that sits above the log terminal.
// runID is used to build the download link href.
func logSearchBar(runID string) string {
	return fmt.Sprintf(`<div style="display:flex;align-items:center;gap:10px;margin-bottom:8px;">
  <input id="log-search" type="text" placeholder="Filter by regex…"
    style="flex:1;background:var(--surface2);border:1px solid var(--border);border-radius:6px;
           padding:5px 10px;color:var(--text);font-family:var(--font-mono);font-size:12px;outline:none;">
  <span id="log-count" style="font-family:var(--font-mono);font-size:11px;color:var(--muted);white-space:nowrap;"></span>
  <a id="log-dl" href="/api/runs/%s/logs.txt" download
    style="font-family:var(--font-mono);font-size:11px;color:var(--accent);white-space:nowrap;">&#11015; .log</a>
</div>`, esc(runID))
}

// logSearchJS is the inline script that powers log search, level colorization and MutationObserver.
const logSearchJS = `<script>
(function(){
  var LEVELS = [
    {re:/FATAL|CRITICAL/i, cls:'log-fatal'},
    {re:/\bERROR\b|\bERR\b/i, cls:'log-error'},
    {re:/WARN(?:ING)?/i, cls:'log-warn'},
    {re:/\bINFO\b/i, cls:'log-info'},
    {re:/\bDEBUG\b/i, cls:'log-debug'}
  ];
  function levelClass(raw) {
    for (var i=0; i<LEVELS.length; i++) {
      if (LEVELS[i].re.test(raw)) return LEVELS[i].cls;
    }
    return '';
  }
  function renderLine(el, re) {
    var raw = el.dataset.raw || el.textContent;
    var lvl = levelClass(raw);
    if (re) {
      try {
        if (!re.test(raw)) { el.style.display='none'; return; }
        el.style.display='';
        el.className = 'll' + (lvl ? ' '+lvl : '');
        el.innerHTML = raw.replace(re, function(m){ return '<mark class="log-hl">'+m+'</mark>'; });
      } catch(e) { el.style.display=''; el.className='ll'+(lvl?' '+lvl:''); el.textContent=raw; }
    } else {
      el.style.display='';
      el.className = 'll' + (lvl ? ' '+lvl : '');
      el.textContent = raw;
    }
  }
  function applyFilter() {
    var pat = document.getElementById('log-search') ? document.getElementById('log-search').value : '';
    var re = null;
    if (pat) { try { re = new RegExp(pat,'gi'); } catch(e){} }
    var els = document.querySelectorAll('#log-term .ll');
    var shown=0;
    els.forEach(function(el){ renderLine(el,re); if(el.style.display!=='none') shown++; });
    var cnt = document.getElementById('log-count');
    if (cnt) cnt.textContent = pat ? shown+' / '+els.length+' lines' : els.length+' lines';
  }
  var srch = document.getElementById('log-search');
  if (srch) srch.addEventListener('input', applyFilter);
  // Apply level colors to already-existing lines on page load.
  document.querySelectorAll('#log-term .ll').forEach(function(el){ renderLine(el,null); });
  applyFilter();
  // Watch for new lines added by SSE.
  var term = document.getElementById('log-term');
  if (term) {
    var obs = new MutationObserver(function(mutations){
      var pat = srch ? srch.value : '';
      var re = null;
      if (pat) { try { re = new RegExp(pat,'gi'); } catch(e){} }
      mutations.forEach(function(m){
        m.addedNodes.forEach(function(node){
          if (node.classList && node.classList.contains('ll')) {
            renderLine(node, re);
          }
        });
      });
      // Auto-scroll only when not filtering.
      if (!pat) term.scrollTop = term.scrollHeight;
      var els = document.querySelectorAll('#log-term .ll');
      var shown=0;
      els.forEach(function(el){ if(el.style.display!=='none') shown++; });
      var cnt = document.getElementById('log-count');
      if (cnt) cnt.textContent = pat ? shown+' / '+els.length+' lines' : els.length+' lines';
    });
    obs.observe(term, {childList:true});
  }
})();
</script>`

// sparklineSVG renders a 60×16 SVG polyline for the given durations.
// durations are newest-first (DESC from DB), so we reverse them first.
// Returns "" when the slice is empty.
func sparklineSVG(durations []int64) string {
	if len(durations) == 0 {
		return ""
	}
	// Reverse to get oldest→newest (left→right).
	n := len(durations)
	rev := make([]int64, n)
	for i, v := range durations {
		rev[n-1-i] = v
	}
	// Find max for normalisation.
	var maxVal int64
	for _, v := range rev {
		if v > maxVal {
			maxVal = v
		}
	}
	if maxVal == 0 {
		maxVal = 1
	}
	const w, h = 60, 16
	var pts strings.Builder
	for i, v := range rev {
		x := float64(i) / float64(max(n-1, 1)) * w
		y := float64(h) - float64(v)/float64(maxVal)*float64(h-2) - 1
		if i == 0 {
			fmt.Fprintf(&pts, "%.1f,%.1f", x, y)
		} else {
			fmt.Fprintf(&pts, " %.1f,%.1f", x, y)
		}
	}
	return fmt.Sprintf(
		`<svg width="%d" height="%d" viewBox="0 0 %d %d" style="vertical-align:middle;margin-left:6px;">`,
		w, h, w, h,
	) + fmt.Sprintf(
		`<polyline points="%s" fill="none" stroke="var(--accent)" stroke-width="1.5" stroke-linejoin="round"/>`,
		pts.String(),
	) + `</svg>`
}

// heatmapHTML renders a calendar heatmap SVG for the given daily run stats.
// days is the number of calendar days to display (counting back from today).
func heatmapHTML(stats []storage.DailyRunStat, days int) string {
	byDay := make(map[string]storage.DailyRunStat, len(stats))
	for _, s := range stats {
		byDay[s.Day] = s
	}

	dayRow := func(w time.Weekday) int { return (int(w) + 6) % 7 }

	now := time.Now().UTC()
	end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	start := end.AddDate(0, 0, -(days - 1))

	gridStart := start
	for gridStart.Weekday() != time.Monday {
		gridStart = gridStart.AddDate(0, 0, -1)
	}

	totalGridDays := int(end.Sub(gridStart).Hours()/24) + 1
	numWeeks := (totalGridDays + 6) / 7

	const cellSize = 12
	const cellGap = 3
	const step = cellSize + cellGap
	const labelW = 26
	const monthH = 14 // space for month labels at top
	svgW := labelW + numWeeks*step
	svgH := monthH + 7*step + 2

	var sb strings.Builder
	// Outer div handles horizontal scroll on narrow screens.
	sb.WriteString(`<div style="overflow-x:auto;-webkit-overflow-scrolling:touch;">`)
	fmt.Fprintf(&sb, `<svg width="%d" height="%d" viewBox="0 0 %d %d" style="display:block;min-width:%dpx;">`,
		svgW, svgH, svgW, svgH, svgW)

	// Month labels above the grid.
	seenMonth := ""
	for col := 0; col < numWeeks; col++ {
		day := gridStart.AddDate(0, 0, col*7)
		m := day.Format("Jan")
		if m != seenMonth {
			seenMonth = m
			x := labelW + col*step
			fmt.Fprintf(&sb, `<text x="%d" y="%d" font-size="9" fill="#718096" font-family="monospace">%s</text>`,
				x, monthH-2, m)
		}
	}

	// Day-of-week labels (Mon, Wed, Fri, Sun only).
	for row, label := range map[int]string{0: "Mo", 2: "We", 4: "Fr", 6: "Su"} {
		y := monthH + row*step + cellSize - 1
		fmt.Fprintf(&sb, `<text x="0" y="%d" font-size="9" fill="#718096" font-family="monospace">%s</text>`, y, label)
	}

	// Cells.
	for d := 0; ; d++ {
		day := gridStart.AddDate(0, 0, d)
		if day.After(end) {
			break
		}
		col := d / 7
		row := dayRow(day.Weekday())
		x := labelW + col*step
		y := monthH + row*step

		dayStr := day.Format("2006-01-02")

		var color, tooltip string
		if day.Before(start) {
			// Days padding before the range: very dark, no tooltip detail.
			color = "#1a1d27"
			tooltip = dayStr
		} else if stat, ok := byDay[dayStr]; ok {
			tooltip = fmt.Sprintf("%s: %d/%d ok", dayStr, stat.Succeeded, stat.Total)
			switch {
			case stat.Succeeded == stat.Total:
				color = "var(--green)"
			case stat.Succeeded == 0:
				color = "var(--red)"
			default:
				color = "var(--yellow)"
			}
		} else {
			color = "var(--border)"
			tooltip = dayStr + ": no runs"
		}

		fmt.Fprintf(&sb,
			`<rect x="%d" y="%d" width="%d" height="%d" rx="2" fill="%s" opacity="0.85"><title>%s</title></rect>`,
			x, y, cellSize, cellSize, color, esc(tooltip))
	}

	sb.WriteString(`</svg>`)

	// Legend.
	sb.WriteString(`<div style="display:flex;gap:12px;margin-top:8px;font-family:monospace;font-size:11px;color:#718096;flex-wrap:wrap;">`)
	for _, item := range []struct{ color, label string }{
		{"var(--green)", "all ok"},
		{"var(--yellow)", "partial"},
		{"var(--red)", "all failed"},
		{"var(--border)", "no runs"},
	} {
		fmt.Fprintf(&sb,
			`<span style="display:flex;align-items:center;gap:4px;"><svg width="10" height="10"><rect width="10" height="10" rx="2" fill="%s" opacity="0.85"/></svg>%s</span>`,
			item.color, item.label)
	}
	sb.WriteString(`</div></div>`)
	return sb.String()
}
