package api

import (
	"fmt"
	"strings"
	"time"

	"github.com/kubecron/kubecron/internal/storage"
)

// sparklineSVG renders a 60×16 SVG polyline for the given durations.
// durations are newest-first (DESC from DB), so we reverse them first.
// Returns "" when the slice is empty.
func sparklineSVG(durations []int64) string {
	if len(durations) == 0 {
		return ""
	}
	n := len(durations)
	rev := make([]int64, n)
	for i, v := range durations {
		rev[n-1-i] = v
	}
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
	const monthH = 14
	svgW := labelW + numWeeks*step
	svgH := monthH + 7*step + 2

	var sb strings.Builder
	sb.WriteString(`<div style="overflow-x:auto;-webkit-overflow-scrolling:touch;">`)
	fmt.Fprintf(&sb, `<svg width="%d" height="%d" viewBox="0 0 %d %d" style="display:block;min-width:%dpx;">`,
		svgW, svgH, svgW, svgH, svgW)

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

	for row, label := range map[int]string{0: "Mo", 2: "We", 4: "Fr", 6: "Su"} {
		y := monthH + row*step + cellSize - 1
		fmt.Fprintf(&sb, `<text x="0" y="%d" font-size="9" fill="#718096" font-family="monospace">%s</text>`, y, label)
	}

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
		var clickable bool
		if day.Before(start) {
			color = "#1a1d27"
			tooltip = dayStr
		} else if stat, ok := byDay[dayStr]; ok {
			clickable = true
			if stat.Running > 0 {
				color = "#4299e1"
				tooltip = fmt.Sprintf("%s: %d running, %d/%d ok", dayStr, stat.Running, stat.Succeeded, stat.Total-stat.Running)
			} else {
				switch {
				case stat.Succeeded == stat.Total:
					color = "var(--green)"
				case stat.Succeeded == 0:
					color = "var(--red)"
				default:
					color = "var(--yellow)"
				}
				tooltip = fmt.Sprintf("%s: %d/%d ok", dayStr, stat.Succeeded, stat.Total)
			}
		} else {
			color = "var(--border)"
			tooltip = dayStr + ": no runs"
		}

		onclick := ""
		cursor := ""
		if clickable {
			onclick = fmt.Sprintf(` onclick="location.href=location.pathname+'?day=%s'"`, dayStr)
			cursor = ` style="cursor:pointer;"`
		}
		fmt.Fprintf(&sb,
			`<rect x="%d" y="%d" width="%d" height="%d" rx="2" fill="%s" opacity="0.85"%s%s><title>%s</title></rect>`,
			x, y, cellSize, cellSize, color, cursor, onclick, esc(tooltip))
	}

	sb.WriteString(`</svg>`)
	sb.WriteString(`<div style="display:flex;gap:12px;margin-top:8px;font-family:monospace;font-size:11px;color:#718096;flex-wrap:wrap;">`)
	for _, item := range []struct{ color, label string }{
		{"var(--green)", "all ok"},
		{"var(--yellow)", "partial"},
		{"var(--red)", "all failed"},
		{"#4299e1", "running"},
		{"var(--border)", "no runs"},
	} {
		fmt.Fprintf(&sb,
			`<span style="display:flex;align-items:center;gap:4px;"><svg width="10" height="10"><rect width="10" height="10" rx="2" fill="%s" opacity="0.85"/></svg>%s</span>`,
			item.color, item.label)
	}
	sb.WriteString(`</div></div>`)
	return sb.String()
}
