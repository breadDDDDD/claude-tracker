// honjoji — a tiny terminal tracker for Claude usage. Runs only while open;
// Esc quits.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"golang.org/x/term"
)

const (
	scanEvery    = time.Second      // re-check recently active transcripts
	walkEvery    = 10 * time.Second // full walk to find brand-new sessions
	apiRetry     = 2 * time.Minute
	activeWindow = 6 * time.Second // transcript written this recently => claude is working
	saveEvery    = 5 * time.Minute
)

type sample struct {
	t time.Time
	u float64
}

type app struct {
	store   *Store
	stats   Stats
	statsAt time.Time
	usage   Usage // last successful fetch, or the latest error
	usageAt time.Time
	anim    animator

	tab, period int
	statsRange  int      // Stats tab: index into statRanges
	ov          Overview // Stats tab data, computed only while that tab is open
	started     time.Time
	resetAt     time.Time
	spikeAt     time.Time
	samples     []sample
	claudeDir   string
}

func (a *app) windowStart() time.Time {
	if s := a.usage.Session; s.OK {
		return s.Reset.Add(-5 * time.Hour)
	}
	return time.Now().Add(-5 * time.Hour)
}

func (a *app) refreshStats(now time.Time) {
	a.stats = a.store.stats(now, a.period, a.windowStart())
	if a.tab == 2 {
		a.ov = a.store.overview(now, a.statsRange)
	}
	a.statsAt = now
}

// session returns the 5h usage: live from the API when we have it, otherwise an
// estimate from local tokens scaled by the ratio learned from the API.
func (a *app) session(now time.Time) (pct float64, rst time.Time, est, ok bool) {
	if s := a.usage.Session; s.OK && (s.Reset.IsZero() || now.Before(s.Reset)) {
		return s.Util, s.Reset, false, true
	}
	if a.store.PctPerTok > 0 {
		return float64(a.stats.WindowTok) * a.store.PctPerTok, time.Time{}, true, true
	}
	return 0, time.Time{}, false, false
}

// applyUsage merges an API result, detecting window resets and burn spikes and
// recalibrating the offline estimate.
func (a *app) applyUsage(u Usage, now time.Time) {
	if u.Err != nil {
		a.usage.Err, a.usage.Fetched = u.Err, u.Fetched
		return
	}
	old := a.usage.Session
	if old.OK && u.Session.Reset.After(old.Reset.Add(time.Minute)) && u.Session.Util < old.Util {
		a.resetAt, a.samples = now, nil
	}
	for _, s := range a.samples {
		if now.Sub(s.t) <= 2*time.Minute+10*time.Second && u.Session.Util-s.u >= 10 {
			a.spikeAt = now
		}
	}
	a.samples = append(a.samples, sample{now, u.Session.Util})
	for len(a.samples) > 0 && now.Sub(a.samples[0].t) > 3*time.Minute {
		a.samples = a.samples[1:]
	}
	a.usage, a.usageAt = u, now
	a.refreshStats(now)
	if u.Session.Util >= 3 && a.stats.WindowTok > 0 {
		a.store.PctPerTok = u.Session.Util / float64(a.stats.WindowTok)
		a.store.dirty = true
	}
}

func (a *app) mood(now time.Time) {
	pct, _, _, ok := a.session(now)
	wk := a.usage.Weekly
	a.anim.set(pickExpression(moodInput{
		now: now, started: a.started, sessionPct: pct, known: ok,
		weeklyPct:  map[bool]float64{true: wk.Util}[wk.OK && now.Before(wk.Reset)],
		generating: now.Sub(a.store.lastMod) < activeWindow,
		opus:       family(a.stats.Current) == "opus",
		idle:       now.Sub(a.store.lastMod),
		errAt:      a.store.lastErr, spikeAt: a.spikeAt, resetAt: a.resetAt,
	}), now)
}

func main() {
	once := flag.Bool("once", false, "print one snapshot and exit")
	noAPI := flag.Bool("no-api", false, "never contact the usage API (local estimates only)")
	apiEvery := flag.Duration("api-every", 60*time.Second, "how often to poll the usage API")
	startTab := flag.String("tab", "now", "tab to open on: now, models or stats")
	flag.Parse()
	if *apiEvery < 30*time.Second {
		*apiEvery = 30 * time.Second
	}

	// Stay small: two scheduler threads are plenty, a soft heap cap, a more eager GC.
	runtime.GOMAXPROCS(2)
	debug.SetMemoryLimit(16 << 20)
	debug.SetGCPercent(50)

	restoreConsole := setupConsole()
	defer restoreConsole()
	loadManifest()

	home, _ := os.UserHomeDir()
	claudeDir := filepath.Join(home, ".claude")
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		claudeDir = d
	}
	a := &app{claudeDir: claudeDir, started: time.Now()}
	switch strings.ToLower(*startTab) {
	case "models", "m", "2":
		a.tab = 1
	case "stats", "s", "3":
		a.tab = 2
	}
	a.store = newStore(filepath.Join(claudeDir, "projects"), filepath.Join(home, ".honjoji", "cache.gob"))
	if len(a.store.Files) == 0 {
		fmt.Fprint(os.Stderr, "honjoji: indexing your Claude logs (first run only)…\r")
	}
	a.store.scan(true)
	a.store.save()
	debug.FreeOSMemory() // drop the one-off parsing garbage

	if *once {
		if !*noAPI {
			a.applyUsage(fetchUsage(claudeDir), time.Now())
		}
		now := time.Now()
		a.refreshStats(now)
		a.started = time.Time{}
		a.mood(now)
		a.store.save()
		fmt.Println(strings.Join(a.compose(now, maxW, 40),"\x1b[0m\n") + "\x1b[0m")
		return
	}

	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "honjoji needs an interactive terminal:", err)
		os.Exit(1)
	}
	out := bufio.NewWriterSize(os.Stdout, 32<<10)
	out.WriteString("\x1b[?1049h\x1b[?25l")
	out.Flush()
	defer func() {
		out.WriteString("\x1b[0m\x1b[?25h\x1b[?1049l")
		out.Flush()
		term.Restore(fd, oldState)
		a.store.save()
	}()

	keys := make(chan string, 8)
	go func() {
		buf := make([]byte, 64)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				close(keys)
				return
			}
			keys <- string(buf[:n])
		}
	}()

	usageCh := make(chan Usage, 1)
	fetching := false
	var nextFetch time.Time
	fetch := func() {
		if *noAPI || fetching {
			return
		}
		fetching = true
		go func() { usageCh <- fetchUsage(claudeDir) }()
	}
	fetch()

	tick := time.NewTicker(scanEvery)
	defer tick.Stop()
	anim := time.NewTimer(0)
	lastSave, lastWalk := time.Now(), time.Now()
	scr := &screen{}

	for {
		now := time.Now()
		if now.Sub(a.statsAt) >= 10*time.Second {
			a.refreshStats(now) // time windows slide even without new data
		}
		a.mood(now)
		w, h, err := term.GetSize(int(os.Stdout.Fd()))
		if err != nil {
			w, h = 80, 24
		}
		scr.draw(out, a.compose(now, w, h), w, h)
		_, _, next := a.anim.at(now)
		anim.Reset(max(next.Sub(now), 20*time.Millisecond))

		select {
		case k, ok := <-keys:
			if !ok {
				return
			}
			switch k {
			case "\x1b", "q", "Q", "\x03":
				return
			case "\t", "\x1b[C", "\x1b[D", "1", "2", "3":
				switch k {
				case "1", "2", "3":
					a.tab = int(k[0] - '1')
				case "\x1b[D":
					a.tab = (a.tab + len(tabNames) - 1) % len(tabNames)
				default:
					a.tab = (a.tab + 1) % len(tabNames)
				}
				a.refreshStats(time.Now())
			case "p", "P", "\x1b[A", "\x1b[B":
				d := 1
				if k == "\x1b[A" {
					d = len(periods) - 1
				}
				a.period = (a.period + d) % len(periods)
				a.tab = 1
				a.refreshStats(time.Now())
			case "r", "R":
				if a.tab == 2 { // as in Claude Code's /stats, r cycles the date range
					a.statsRange = (a.statsRange + 1) % len(statRanges)
					a.refreshStats(time.Now())
				} else if time.Since(a.usage.Fetched) > 10*time.Second {
					fetch()
				}
			}
		case u := <-usageCh:
			fetching = false
			a.applyUsage(u, time.Now())
			if u.Err != nil {
				nextFetch = time.Now().Add(max(apiRetry, *apiEvery))
			} else {
				nextFetch = time.Now().Add(*apiEvery)
			}
		case <-tick.C:
			full := time.Since(lastWalk) >= walkEvery
			if full {
				lastWalk = time.Now()
			}
			if a.store.scan(full) {
				a.refreshStats(time.Now())
			}
			if !nextFetch.IsZero() && time.Now().After(nextFetch) {
				nextFetch = time.Time{}
				fetch()
			}
			if time.Since(lastSave) > saveEvery {
				a.store.save()
				lastSave = time.Now()
			}
		case <-anim.C:
		}
	}
}
