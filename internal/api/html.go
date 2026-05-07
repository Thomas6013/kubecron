package api

import (
	"fmt"
	"html"
	"strings"
	"time"
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
<link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600&family=JetBrains+Mono:wght@400;600&display=swap" rel="stylesheet">
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
      var h = Math.floor(diff / 3600), m = Math.floor((diff % 3600) / 60), s = diff % 60;
      el.textContent = h > 0 ? h+'h '+m+'m' : m > 0 ? m+'m '+s+'s' : s+'s';
    });
  }
  update();
  setInterval(update, 1000);
})();
</script>`
