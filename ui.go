package main

import (
	"bufio"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type rgb struct{ r, g, b uint8 }

func hexRGB(h string) rgb {
	v, err := strconv.ParseUint(strings.TrimPrefix(h, "#"), 16, 32)
	if err != nil {
		return rgb{155, 127, 209}
	}
	return rgb{uint8(v >> 16), uint8(v >> 8), uint8(v)}
}

var (
	cDim   = rgb{125, 125, 140}
	cTrack = rgb{68, 68, 80}
	cAmber = rgb{230, 170, 60}
	cRed   = rgb{235, 85, 85}
)

const reset = "\x1b[0m"

func fg(c rgb, s string) string   { return fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s%s", c.r, c.g, c.b, s, reset) }
func bold(s string) string        { return "\x1b[1m" + s + reset }
func boldFg(c rgb, s string) string {
	return fmt.Sprintf("\x1b[1;38;2;%d;%d;%dm%s%s", c.r, c.g, c.b, s, reset)
}
func dim(s string) string { return fg(cDim, s) }

// visLen counts printed columns, skipping ANSI escape sequences.
func visLen(s string) int {
	n := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			for i++; i < len(s) && !(s[i] >= '@' && s[i] <= '~' && s[i] != '['); i++ {
			}
			i++
			continue
		}
		_, sz := utf8.DecodeRuneInString(s[i:])
		i += sz
		n++
	}
	return n
}

func padR(s string, w int) string {
	if n := visLen(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

func sevColor(pct float64, accent rgb) rgb {
	switch {
	case pct >= 85:
		return cRed
	case pct >= 60:
		return cAmber
	}
	return accent
}

func bar(pct float64, w int, c rgb) string {
	f := int(math.Round(math.Max(0, math.Min(100, pct)) / 100 * float64(w)))
	if pct > 0 && f == 0 {
		f = 1
	}
	return fg(c, strings.Repeat("━", f)) + fg(cTrack, strings.Repeat("━", w-f))
}

var sparkRunes = []rune("▁▂▃▄▅▆▇█")

func spark(vals []int64, c rgb) string {
	var mx int64
	for _, v := range vals {
		mx = max(mx, v)
	}
	var b strings.Builder
	for _, v := range vals {
		if v == 0 || mx == 0 {
			b.WriteString(fg(cTrack, "▁"))
			continue
		}
		i := int(float64(v) / float64(mx) * float64(len(sparkRunes)-1))
		b.WriteString(fg(c, string(sparkRunes[max(i, 1)])))
	}
	return b.String()
}

func human(n float64) string {
	switch {
	case n >= 1e9:
		return strconv.FormatFloat(n/1e9, 'f', 1, 64) + "B"
	case n >= 1e6:
		return strconv.FormatFloat(n/1e6, 'f', 1, 64) + "M"
	case n >= 1e3:
		return strconv.FormatFloat(n/1e3, 'f', 1, 64) + "k"
	}
	return strconv.Itoa(int(n))
}

func dur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	m := int(d.Minutes())
	switch {
	case m >= 48*60:
		return fmt.Sprintf("%dd %dh", m/1440, m%1440/60)
	case m >= 60:
		return fmt.Sprintf("%dh%02dm", m/60, m%60)
	}
	return fmt.Sprintf("%dm", m)
}

// ---- screen composition ---------------------------------------------------

var tabNames = []string{"Now", "Models", "Stats"}

// Tab/↑↓ always switch tabs; ←→ act inside the current tab.
var tabHints = []string{
	"tab/↑↓ switch · r refresh · esc quit",
	"tab/↑↓ switch · ←→ period · r refresh · esc quit",
	"tab/↑↓ switch · ←→ date range · r refresh · esc quit",
}

const (
	maxW     = 78
	spriteGap = "   " // breathing room on each side of the divider
	bodyRows  = 11 // sprite (10) + 1 for the bob
)

func (a *app) compose(now time.Time, w, h int) []string {
	st := a.stats
	accent := accentOf(st.Current)
	W := min(w, maxW)
	wide := W >= 72

	var out []string
	// header: title, tabs, current model
	hd := " " + boldFg(accent, "honjoji") + "  "
	for i, t := range tabNames {
		if i == a.tab {
			hd += fmt.Sprintf("\x1b[1;38;2;24;22;28;48;2;%d;%d;%dm %s %s", accent.r, accent.g, accent.b, t, reset)
		} else {
			hd += dim(" " + t + " ")
		}
		hd += " "
	}
	right := ""
	if st.Current != "" {
		right = fg(accent, "● ") + prettyModel(st.Current) + " "
	}
	if gap := W - visLen(hd) - visLen(right); gap > 0 {
		hd += strings.Repeat(" ", gap) + right
	}
	out = append(out, hd, fg(cTrack, strings.Repeat("─", W)), "")
	e := mf.Expressions[a.anim.expr]
	speech := "  " + boldFg(accent, e.Kaomoji) + "  " + dim("\x1b[3m"+quipFor(a.anim.expr, now))

	var panel []string
	switch a.tab {
	case 0:
		panel = a.nowPanel(now, accent)
	case 1:
		panel = a.modelsPanel(accent)
	default:
		panel = a.statsPanel(now)
	}

	idx, dip, _ := a.anim.at(now)
	sprite := frame(outfitOf(st.Current), a.anim.expr, idx)
	for i := 0; i < bodyRows; i++ {
		line := ""
		if wide {
			row := i
			if dip {
				row--
			}
			s := strings.Repeat(" ", 20)
			if row >= 0 && row < len(sprite) {
				s = sprite[row] + reset
			}
			line = "  " + s + spriteGap + fg(cTrack, "│") + spriteGap
		}
		if i < len(panel) {
			line += panel[i]
		}
		out = append(out, line)
	}

	foot := []string{"", dim(" " + tabHints[a.tab])}
	if a.tab == 2 {
		// The hour chart outranks Tally's speech line (she's already on screen),
		// so give up the speech rows when that's what lets the chart fit.
		if avail := h - len(out) - len(foot); avail-2 < statsBelowMin && avail >= statsBelowMin {
			return append(append(out, a.statsBelow(now, W, avail)...), foot...)
		}
	}
	// blank row keeps the bobbing sprite clear of the speech line
	out = append(out, "", speech)
	if a.tab == 2 {
		out = append(out, a.statsBelow(now, W, h-len(out)-len(foot))...)
	}
	return append(out, foot...)
}

func row(label, val string) string { return dim(padR(label, 13)) + val }

func (a *app) nowPanel(now time.Time, accent rgb) []string {
	st := a.stats
	p := make([]string, 0, bodyRows)

	pct, rst, est, ok := a.session(now)
	if ok {
		lbl := fmt.Sprintf("%3.0f%%", pct)
		if est {
			lbl = fmt.Sprintf("~%.0f%% est", pct)
		}
		p = append(p, row("5h session", bar(pct, 22, sevColor(pct, accent))+"  "+bold(lbl)))
		if !rst.IsZero() {
			p = append(p, row("", dim("resets in "+dur(rst.Sub(now))+" · "+rst.Local().Format("15:04"))))
		} else {
			p = append(p, row("", dim("reset time unknown (offline)")))
		}
	} else {
		p = append(p, row("5h session", bar(0, 22, accent)+"  "+dim("—")), row("", dim("waiting for usage data…")))
	}
	p = append(p, "")

	if wk := a.usage.Weekly; wk.OK && now.Before(wk.Reset) {
		p = append(p, row("weekly", bar(wk.Util, 22, sevColor(wk.Util, accent))+"  "+bold(fmt.Sprintf("%3.0f%%", wk.Util))),
			row("", dim("resets in "+dur(wk.Reset.Sub(now))+" · "+wk.Reset.Local().Format("Mon 15:04"))))
	} else {
		p = append(p, row("weekly", bar(0, 22, accent)+"  "+dim("—")), "")
	}
	p = append(p, "")

	if ok {
		left := bold(fmt.Sprintf("%.0f%%", math.Max(0, 100-pct)))
		rate := st.TokPerMin * a.store.PctPerTok // % per minute at the current burn
		switch {
		case pct >= 100:
			left += dim(" · wait for reset")
		case rate < 0.005:
			left += dim(" · idle pace")
		default:
			eta := time.Duration((100 - pct) / rate * float64(time.Minute))
			if !rst.IsZero() && now.Add(eta).After(rst) {
				left += dim(" · lasts until reset")
			} else {
				left += dim(" · ~" + dur(eta) + " at this pace")
			}
		}
		p = append(p, row("left", left))
	} else {
		p = append(p, row("left", dim("—")))
	}
	p = append(p,
		row("burn", padR(human(st.TokPerMin)+" tok/min", 15)+spark(st.Burn[:], accent)),
		row("today", human(float64(st.TodayTok))+" tok"+dim(" · ")+strconv.FormatInt(st.TodayMsg, 10)+" msgs"))

	idle := now.Sub(a.store.lastMod)
	switch {
	case idle < activeWindow:
		p = append(p, row("status", fg(accent, "● ")+"claude is working"))
	case a.store.lastMod.IsZero():
		p = append(p, row("status", dim("no sessions yet")))
	default:
		p = append(p, row("status", dim("idle "+dur(idle))))
	}

	// where the limit numbers come from: live API, stale, or failing
	switch {
	case a.usage.Err != nil && !a.usage.Fetched.IsZero():
		p = append(p, row("data", fg(cAmber, "● "+a.usage.Err.Error())))
	case a.usage.Session.OK && now.Sub(a.usageAt) < 2*time.Minute:
		p = append(p, row("data", fg(accent, "● ")+dim("live")))
	case a.usage.Session.OK:
		p = append(p, row("data", fg(accent, "● ")+dim("live · "+dur(now.Sub(a.usageAt))+" ago")))
	default:
		p = append(p, row("data", dim("● connecting…")))
	}
	return p
}

func (a *app) modelsPanel(accent rgb) []string {
	st := a.stats
	p := make([]string, 0, bodyRows)
	sel := ""
	for i, pr := range periods {
		if i == a.period {
			sel += boldFg(accent, "["+pr.Name+"]") + " "
		} else {
			sel += dim(" "+pr.Name+" ") + " "
		}
	}
	p = append(p, row("period", sel), "")
	for i := 0; i < 5; i++ {
		if i >= len(st.Top) {
			if i == 0 {
				p = append(p, dim("no activity in this period"))
			} else {
				p = append(p, "")
			}
			continue
		}
		m := st.Top[i]
		share := float64(m.Tok) / math.Max(1, float64(st.PeriodTok)) * 100
		c := accentOf(m.Name)
		p = append(p, dim(strconv.Itoa(i+1)+" ")+fg(c, padR(prettyModel(m.Name), 11))+bar(share, 14, c)+
			fmt.Sprintf(" %3.0f%% ", share)+dim(padR(human(float64(m.Tok)), 6)))
	}
	p = append(p, "",
		row("total", human(float64(st.PeriodTok))+" tok"+dim(" · ")+human(float64(st.PeriodMsg))+" msgs"))
	hit := float64(st.PeriodCR) / math.Max(1, float64(st.PeriodCR+st.PeriodIn)) * 100
	p = append(p, row("cache", human(float64(st.PeriodCR))+" read"+dim(" · ")+fmt.Sprintf("%.0f%% hit", hit)),
		row("14 days", spark(st.Daily[:], accent)))
	return p
}

// ---- diffing screen writer ------------------------------------------------

type screen struct {
	prev []string
	w, h int
}

// draw rewrites only the rows that changed since the last frame.
func (s *screen) draw(out *bufio.Writer, lines []string, w, h int) {
	full := w != s.w || h != s.h
	if full {
		out.WriteString("\x1b[2J")
		s.prev, s.w, s.h = nil, w, h
	}
	for i := 0; i < h; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		if !full && i < len(s.prev) && s.prev[i] == line {
			continue
		}
		fmt.Fprintf(out, "\x1b[%d;1H%s\x1b[0m\x1b[K", i+1, line)
	}
	s.prev = append(s.prev[:0], lines...)
	if len(s.prev) > h {
		s.prev = s.prev[:h]
	}
	out.Flush()
}
