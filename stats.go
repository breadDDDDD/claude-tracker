package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The Stats tab shows what Claude Code's /stats doesn't: projects, tool calls,
// session length, cache efficiency and when in the day you work. It avoids
// token totals, session counts and all-time history on purpose: /stats sums
// every streamed log line (about 2.5x the real token count) and keeps history
// past the 30 days of transcripts we can read, so those numbers never agree.

// Ranges stay within the transcripts Claude Code keeps, so they're complete.
var statRanges = []struct {
	Label string
	Days  int // 1 = today only
}{{"Today", 1}, {"Last 7 days", 7}, {"Last 30 days", 30}}

const defaultStatsRange = 2

// idleGap: a pause longer than this between responses isn't session time.
const idleGap = 15 * time.Minute

var cClaude = hexRGB("#da7756") // Claude Code's orange

type Overview struct {
	Fav          string
	TopProj      string
	TopProjShare float64 // of responses in range
	Projects     int
	Responses    int64
	Tools        int64
	Sessions     int
	AvgSession   time.Duration
	CacheHit     float64 // share of input served from cache, 0..1
	Out          int64
	PeakDay      int // local day number, -1 if none
	CurStreak    int
	Hours        [24]int64 // responses per local hour of day
	HasData      bool
}

func dayOffset(now time.Time) int64 {
	_, off := now.Zone()
	return int64(off)
}

func dayNum(ts, off int64) int { return int((ts + off) / 86400) }

func dayTime(d int, off int64) time.Time { return time.Unix(int64(d)*86400-off, 0) }

func (s *Store) overview(now time.Time, rng int) Overview {
	off := dayOffset(now)
	today := dayNum(now.Unix(), off)
	from := today - statRanges[rng].Days + 1
	ov := Overview{PeakDay: -1}

	active := map[int]bool{}
	daily := map[int]int64{}
	perModel := map[uint16]int64{}
	perProj := map[uint16]int64{}
	sess := map[uint32][]int64{}
	var in, cr int64
	for _, r := range s.Recs {
		d := dayNum(r.TS, off)
		active[d] = true
		if d < from {
			continue
		}
		ov.Responses++
		ov.Tools += int64(r.Tools)
		ov.Out += r.Out
		in += r.In + r.CW
		cr += r.CR
		daily[d]++
		perModel[r.Model] += r.In + r.Out + r.CR + r.CW
		if int(r.Sess) < len(s.SessProj) {
			perProj[s.SessProj[r.Sess]]++
		}
		ov.Hours[(r.TS+off)%86400/3600]++
		sess[r.Sess] = append(sess[r.Sess], r.TS)
	}
	for d := today; active[d]; d-- {
		ov.CurStreak++
	}
	if ov.Responses == 0 {
		return ov
	}
	ov.HasData = true

	var best int64 = -1
	for m, t := range perModel {
		if t > best {
			best, ov.Fav = t, s.Models[m]
		}
	}
	best = -1
	for p, n := range perProj {
		if n > best {
			best, ov.TopProj = n, s.Projects[p]
		}
	}
	ov.Projects = len(perProj)
	ov.TopProjShare = float64(best) / float64(ov.Responses)

	// Active time only: resumed sessions would otherwise count days of idling.
	var total time.Duration
	for _, ts := range sess {
		slices.Sort(ts)
		for i := 1; i < len(ts); i++ {
			if gap := ts[i] - ts[i-1]; gap <= int64(idleGap/time.Second) {
				total += time.Duration(gap) * time.Second
			}
		}
	}
	ov.Sessions = len(sess)
	ov.AvgSession = total / time.Duration(len(sess))
	if in+cr > 0 {
		ov.CacheHit = float64(cr) / float64(in+cr)
	}
	best = -1
	for d, n := range daily {
		if n > best || (n == best && d > ov.PeakDay) {
			best, ov.PeakDay = n, d
		}
	}
	return ov
}

// ---- helpers --------------------------------------------------------------

func longDur(d time.Duration) string {
	s := int(d.Seconds())
	switch {
	case s >= 86400:
		return fmt.Sprintf("%dd %dh %dm", s/86400, s%86400/3600, s%3600/60)
	case s >= 3600:
		return fmt.Sprintf("%dh %dm", s/3600, s%3600/60)
	case s >= 60:
		return fmt.Sprintf("%dm", s/60)
	}
	return fmt.Sprintf("%ds", s)
}

func plural(n int, one string) string {
	if n == 1 {
		return one
	}
	return one + "s"
}

// comma formats 12345 as "12,345".
func comma(n int64) string {
	s := strconv.FormatInt(n, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// ---- fun facts ------------------------------------------------------------

var funBooks = []struct {
	name   string
	tokens int64
}{
	{"The Little Prince", 22000}, {"The Old Man and the Sea", 35000}, {"A Christmas Carol", 37000},
	{"Animal Farm", 39000}, {"Fahrenheit 451", 60000}, {"The Great Gatsby", 62000},
	{"Slaughterhouse-Five", 64000}, {"Brave New World", 83000}, {"The Catcher in the Rye", 95000},
	{"Harry Potter and the Philosopher's Stone", 103000}, {"The Hobbit", 123000}, {"1984", 123000},
	{"To Kill a Mockingbird", 130000}, {"Pride and Prejudice", 156000}, {"Dune", 244000},
	{"Moby-Dick", 268000}, {"Crime and Punishment", 274000}, {"A Game of Thrones", 381000},
	{"Anna Karenina", 468000}, {"Don Quixote", 520000}, {"The Lord of the Rings", 576000},
	{"The Count of Monte Cristo", 603000}, {"Les Misérables", 689000}, {"War and Peace", 730000},
}

var funDurations = []struct {
	name    string
	minutes float64
}{
	{"a TED talk", 18}, {"an episode of The Office", 22}, {"listening to Abbey Road", 47},
	{"a yoga class", 60}, {"a World Cup soccer match", 90}, {"a half marathon (average time)", 120},
	{"the movie Inception", 148}, {"watching Titanic", 195}, {"a transatlantic flight", 420},
	{"a full night of sleep", 480},
}

// funFact compares what Claude wrote and how long you work to familiar
// things, rotating every 30s.
func funFact(ov Overview, now time.Time) string {
	var c []string
	for _, b := range funBooks {
		if ov.Out < b.tokens {
			continue
		}
		if r := float64(ov.Out) / float64(b.tokens); r >= 2 {
			c = append(c, fmt.Sprintf("Claude's replies add up to ~%dx the length of %s", int(r), b.name))
		} else {
			c = append(c, "Claude's replies add up to about the length of "+b.name)
		}
	}
	for _, d := range funDurations {
		switch r := ov.AvgSession.Minutes() / d.minutes; {
		case r >= 2:
			c = append(c, fmt.Sprintf("Your average session is ~%dx longer than %s", int(r), d.name))
		case r >= 1:
			c = append(c, "Your average session is about as long as "+d.name)
		}
	}
	if len(c) == 0 {
		return ""
	}
	return c[int(now.Unix()/30)%len(c)]
}

// ---- rendering ------------------------------------------------------------

// statsPanel is the column beside Tally: date range and one fact per row.
func (a *app) statsPanel(now time.Time) []string {
	ov := a.ov
	var sel []string
	for i, r := range statRanges {
		if i == a.statsRange {
			sel = append(sel, boldFg(cClaude, r.Label))
		} else {
			sel = append(sel, dim(r.Label))
		}
	}
	head := []string{strings.Join(sel, dim(" · ")), ""}
	if !ov.HasData {
		return append(head, dim("No activity in this range."))
	}
	val := func(s string) string { return fg(cClaude, s) }
	row := func(label, v string) string { return dim(padR(label, 17)) + v }

	perReply := float64(ov.Tools) / float64(ov.Responses)
	proj := val(ov.TopProj) + dim(fmt.Sprintf(" · %.0f%%", ov.TopProjShare*100))
	if ov.Projects > 1 {
		proj += dim(fmt.Sprintf(" of %d projects", ov.Projects))
	}
	return append(head,
		row("Favorite model", val(prettyModel(ov.Fav))),
		row("Top project", proj),
		row("Responses", val(comma(ov.Responses))),
		row("Tool calls", val(comma(ov.Tools))+dim(fmt.Sprintf(" · %.1f per reply", perReply))),
		row("Avg session", val(longDur(ov.AvgSession))+dim(fmt.Sprintf(" · %d %s", ov.Sessions, plural(ov.Sessions, "session")))),
		row("Words written", val("~"+human(float64(ov.Out)*0.75))),
		row("Cache hit rate", val(fmt.Sprintf("%.0f%%", ov.CacheHit*100))),
		row("Most active day", val(dayTime(ov.PeakDay, dayOffset(now)).Format("Mon, Jan 2"))),
		row("Current streak", boldFg(cClaude, strconv.Itoa(ov.CurStreak))+" "+plural(ov.CurStreak, "day")),
	)
}

// statsBelowMin is the fewest rows the hour chart needs (title, 2 bars, axis).
const statsBelowMin = 4

// statsBelow is the full-width part under Tally: an activity-by-hour chart
// that grows with the space available, then a fun fact.
func (a *app) statsBelow(now time.Time, width, rows int) []string {
	ov := a.ov
	if !ov.HasData || rows < statsBelowMin {
		return nil
	}
	budget := rows - statsBelowMin
	take := func(n int) bool {
		if budget >= n {
			budget -= n
			return true
		}
		return false
	}
	gap := take(1)
	fun := funFact(ov, now)
	if r := []rune(fun); len(r) > width-2 {
		fun = string(r[:width-3]) + "…"
	}
	withFun := fun != "" && take(2)
	height := 2
	for height < 4 && take(1) {
		height++
	}

	var out []string
	if gap {
		out = append(out, "")
	}
	out = append(out, hourChart(ov.Hours, width-2, height)...)
	if withFun {
		out = append(out, "", fg(cClaude, fun))
	}
	for i := range out {
		if out[i] != "" {
			out[i] = "  " + out[i]
		}
	}
	return out
}

// hourChart draws responses per hour of day as eighth-block columns.
func hourChart(hours [24]int64, width, height int) []string {
	colW := 3 // two-cell bar + gap
	if width < 24*colW {
		colW = 2
	}
	var peak int
	var mx int64
	for h, n := range hours {
		if n > mx {
			mx, peak = n, h
		}
	}
	title := dim("Activity by hour")
	if mx > 0 {
		pk := fmt.Sprintf("busiest %02d:00–%02d:00", peak, (peak+1)%24)
		if gap := 24*colW - 1 - visLen(title) - len([]rune(pk)); gap > 0 {
			title += strings.Repeat(" ", gap) + fg(cClaude, pk)
		}
	}
	out := []string{title}
	blocks := []rune(" ▁▂▃▄▅▆▇█")
	for r := height - 1; r >= 0; r-- {
		var b strings.Builder
		for h := 0; h < 24; h++ {
			e := 0
			if mx > 0 {
				e = int(float64(hours[h]) / float64(mx) * float64(height*8))
			}
			lvl := min(max(e-r*8, 0), 8)
			cell := string(blocks[lvl])
			switch {
			case lvl > 0:
				b.WriteString(fg(cClaude, strings.Repeat(cell, colW-1)))
			case r == 0:
				b.WriteString(fg(cTrack, strings.Repeat("▁", colW-1)))
			default:
				b.WriteString(strings.Repeat(" ", colW-1))
			}
			b.WriteString(" ")
		}
		out = append(out, b.String())
	}
	var axis strings.Builder
	for h := 0; h < 24; h += 3 {
		axis.WriteString(padR(strconv.Itoa(h), 3*colW))
	}
	return append(out, dim(strings.TrimRight(axis.String(), " ")))
}
