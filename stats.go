package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The Stats tab mirrors Claude Code's /stats Overview: activity heatmap,
// two columns of facts, a token breakdown and a rotating fun fact.

var statRanges = []struct {
	Label string
	Days  int // 0 = all time
}{{"All time", 0}, {"Last 7 days", 7}, {"Last 30 days", 30}}

var cClaude = hexRGB("#da7756") // Claude Code's heatmap / value colour

type Overview struct {
	Daily                  map[int]int // local day number -> messages, all time
	Today                  int
	Fav                    string
	Total, In, Out, CR, CW int64
	Sessions               int
	Longest                time.Duration
	ActiveDays, TotalDays  int
	CurStreak, LongStreak  int
	PeakDay                int // -1 when none
	HasData                bool
}

func dayOffset(now time.Time) int64 {
	_, off := now.Zone()
	return int64(off)
}

func dayNum(ts, off int64) int { return int((ts + off) / 86400) }

func dayTime(d int, off int64) time.Time { return time.Unix(int64(d)*86400-off, 0) }

func (s *Store) overview(now time.Time, rng int) Overview {
	off := dayOffset(now)
	ov := Overview{Daily: map[int]int{}, Today: dayNum(now.Unix(), off), PeakDay: -1}
	if len(s.Recs) == 0 {
		return ov
	}
	ov.HasData = true
	first := ov.Today
	for _, r := range s.Recs {
		d := dayNum(r.TS, off)
		ov.Daily[d]++
		first = min(first, d)
	}
	// The current streak is always all-time, as in Claude Code.
	for d := ov.Today; ov.Daily[d] > 0; d-- {
		ov.CurStreak++
	}

	from := first
	ov.TotalDays = ov.Today - first + 1
	if n := statRanges[rng].Days; n > 0 {
		from, ov.TotalDays = ov.Today-n+1, n
	}

	perModel := map[uint16]int64{}
	type span struct{ a, b int64 }
	sess := map[uint32]*span{}
	for _, r := range s.Recs {
		if dayNum(r.TS, off) < from {
			continue
		}
		t := r.In + r.Out + r.CR + r.CW
		perModel[r.Model] += t
		ov.Total += t
		ov.In += r.In
		ov.Out += r.Out
		ov.CR += r.CR
		ov.CW += r.CW
		if sp := sess[r.Sess]; sp == nil {
			sess[r.Sess] = &span{r.TS, r.TS}
		} else {
			sp.a, sp.b = min(sp.a, r.TS), max(sp.b, r.TS)
		}
	}
	var best int64 = -1
	for m, t := range perModel {
		if t > best {
			best, ov.Fav = t, s.Models[m]
		}
	}
	ov.Sessions = len(sess)
	for _, sp := range sess {
		ov.Longest = max(ov.Longest, time.Duration(sp.b-sp.a)*time.Second)
	}
	run, peak := 0, 0
	for d := from; d <= ov.Today; d++ {
		c := ov.Daily[d]
		if c == 0 {
			run = 0
			continue
		}
		ov.ActiveDays++
		run++
		ov.LongStreak = max(ov.LongStreak, run)
		if c > peak {
			peak, ov.PeakDay = c, d
		}
	}
	return ov
}

// ---- rendering ------------------------------------------------------------

var monthNames = []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}

// heatmap draws the GitHub-style grid exactly like Claude Code: weeks as
// columns, Mon/Wed/Fri labels, quartile shading of daily message counts.
func heatmap(ov Overview, now time.Time, width int, months, legend bool) []string {
	off := dayOffset(now)
	weeks := min(52, max(10, width-4))
	var counts []int
	for _, c := range ov.Daily {
		if c > 0 {
			counts = append(counts, c)
		}
	}
	slices.Sort(counts)
	level := func(c int) int {
		n := len(counts)
		switch {
		case c == 0 || n == 0:
			return 0
		case c >= counts[n*3/4]:
			return 4
		case c >= counts[n/2]:
			return 3
		case c >= counts[n/4]:
			return 2
		}
		return 1
	}
	cells := []string{fg(cDim, "·"), fg(cClaude, "░"), fg(cClaude, "▒"), fg(cClaude, "▓"), fg(cClaude, "█")}

	start := ov.Today - int(now.Weekday()) - (weeks-1)*7
	var rows [7]strings.Builder
	var monthSeq []int
	last := -1
	for w := 0; w < weeks; w++ {
		for r := 0; r < 7; r++ {
			d := start + w*7 + r
			if r == 0 {
				if m := int(dayTime(d, off).Month()) - 1; m != last {
					monthSeq, last = append(monthSeq, m), m
				}
			}
			if d > ov.Today {
				rows[r].WriteString(" ")
				continue
			}
			rows[r].WriteString(cells[level(ov.Daily[d])])
		}
	}

	var out []string
	if months {
		q := weeks / max(len(monthSeq), 1)
		var b strings.Builder
		for _, m := range monthSeq {
			b.WriteString(padR(monthNames[m], q))
		}
		out = append(out, "    "+b.String())
	}
	days := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	for r := 0; r < 7; r++ {
		label := "   "
		if r == 1 || r == 3 || r == 5 {
			label = days[r]
		}
		out = append(out, label+" "+rows[r].String())
	}
	if legend {
		out = append(out, "", "    Less "+cells[1]+" "+cells[2]+" "+cells[3]+" "+cells[4]+" More")
	}
	return out
}

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

// funFact picks one of Claude Code's comparisons, rotating every 30s.
func funFact(ov Overview, now time.Time) string {
	var c []string
	if io := ov.In + ov.Out; io > 0 {
		for _, b := range funBooks {
			if io < b.tokens {
				continue
			}
			if r := float64(io) / float64(b.tokens); r >= 2 {
				c = append(c, fmt.Sprintf("Your input and output are ~%dx the tokens in %s", int(r), b.name))
			} else {
				c = append(c, "Your input and output are about as many tokens as "+b.name)
			}
		}
	}
	if ov.Longest > 0 {
		for _, d := range funDurations {
			if r := ov.Longest.Minutes() / d.minutes; r >= 2 {
				c = append(c, fmt.Sprintf("Your longest session is ~%dx longer than %s", int(r), d.name))
			}
		}
	}
	if len(c) == 0 {
		return ""
	}
	return c[int(now.Unix()/30)%len(c)]
}

// statsView lays the tab out full width; rows is the height available, used
// to drop the heatmap's month labels and legend on short terminals.
func (a *app) statsView(now time.Time, width, rows int) []string {
	ov := a.ov
	if !ov.HasData {
		return []string{"", "  " + fg(cAmber, "No stats available yet. Start using Claude Code!")}
	}
	val := func(s string) string { return fg(cClaude, s) }
	col := func(label, v string) string { return padR(label+": "+v, 32) }

	var sel []string
	for i, r := range statRanges {
		if i == a.statsRange {
			sel = append(sel, boldFg(cClaude, r.Label))
		} else {
			sel = append(sel, dim(r.Label))
		}
	}

	facts := []string{
		col("Favorite model", val(prettyModel(ov.Fav))) + "Total tokens: " + val(human(float64(ov.Total))),
		"",
		col("Sessions", val(human(float64(ov.Sessions)))) + "Longest session: " + val(longDur(ov.Longest)),
		col("Active days", val(strconv.Itoa(ov.ActiveDays))+dim("/"+strconv.Itoa(ov.TotalDays))) +
			"Longest streak: " + boldFg(cClaude, strconv.Itoa(ov.LongStreak)) + " " + plural(ov.LongStreak, "day"),
	}
	peak := ""
	if ov.PeakDay >= 0 {
		peak = "Most active day: " + val(dayTime(ov.PeakDay, dayOffset(now)).Format("Jan 2"))
	}
	facts = append(facts,
		padR(peak, 32)+"Current streak: "+boldFg(cClaude, strconv.Itoa(ov.CurStreak))+" "+plural(ov.CurStreak, "day"),
		dim(fmt.Sprintf("Input %s · Output %s · Cache read %s · Cache write %s",
			human(float64(ov.In)), human(float64(ov.Out)), human(float64(ov.CR)), human(float64(ov.CW)))))
	fun := funFact(ov, now)
	if r := []rune(fun); len(r) > width-2 {
		fun = string(r[:width-3]) + "…"
	}

	// Fit short terminals: the facts always show, then the heatmap grid, the
	// fun fact, month labels and finally the legend, as space allows.
	budget := rows - 2 - len(facts)
	take := func(n int) bool {
		if budget >= n {
			budget -= n
			return true
		}
		return false
	}
	grid := take(7)
	gap := grid && take(1)
	withFun := fun != "" && take(2)
	months := grid && take(1)
	legend := grid && take(2)

	out := []string{strings.Join(sel, dim(" · ")), ""}
	if grid {
		out = append(out, heatmap(ov, now, width-2, months, legend)...)
		if gap {
			out = append(out, "")
		}
	}
	out = append(out, facts...)
	if withFun {
		out = append(out, "", val(fun))
	}
	for i := range out {
		if out[i] != "" {
			out[i] = "  " + out[i]
		}
	}
	return out
}
