package main

import (
	"embed"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

//go:embed mascot/manifest.json mascot/ansi/truecolor
var mascotFS embed.FS

type step struct {
	Frame int `json:"frame"`
	MS    int `json:"ms"`
}

type manifest struct {
	Models map[string]struct {
		Outfit string `json:"outfit"`
		Accent string `json:"accent"`
		Label  string `json:"label"`
	} `json:"models"`
	Expressions map[string]struct {
		Timeline []step `json:"timeline"`
		Kaomoji  string `json:"kaomoji"`
	} `json:"expressions"`
}

var mf manifest

func loadManifest() {
	b, _ := mascotFS.ReadFile("mascot/manifest.json")
	json.Unmarshal(b, &mf)
}

var frameCache = map[string][]string{}

// frame returns the 10 pre-rendered rows of one sprite frame.
func frame(outfit, expr string, i int) []string {
	key := outfit + "/" + expr + "." + strconv.Itoa(i)
	if f, ok := frameCache[key]; ok {
		return f
	}
	b, err := mascotFS.ReadFile("mascot/ansi/truecolor/" + key + ".ans")
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimRight(strings.ReplaceAll(string(b), "\r", ""), "\n"), "\n")
	frameCache[key] = lines
	return lines
}

// family maps a model id to its manifest key: opus, sonnet, haiku, fable or default.
func family(model string) string {
	for _, f := range []string{"opus", "sonnet", "haiku", "fable"} {
		if strings.Contains(model, f) {
			return f
		}
	}
	return "default"
}

func accentOf(model string) rgb {
	return hexRGB(mf.Models[family(model)].Accent)
}

func outfitOf(model string) string {
	if o := mf.Models[family(model)].Outfit; o != "" {
		return o
	}
	return "shirt"
}

// prettyModel turns "claude-opus-5-5" into "Opus 5.5" and
// "claude-3-5-sonnet-20241022" into "Sonnet 3.5".
func prettyModel(id string) string {
	id = strings.TrimSuffix(strings.TrimPrefix(id, "claude-"), "[1m]")
	var fam string
	var nums []string
	for _, p := range strings.Split(id, "-") {
		if _, err := strconv.Atoi(p); err == nil {
			if len(p) < 8 { // skip date stamps
				nums = append(nums, p)
			}
		} else if fam == "" {
			fam = p
		}
	}
	if fam == "" {
		return id
	}
	s := strings.ToUpper(fam[:1]) + fam[1:]
	if len(nums) > 0 {
		s += " " + strings.Join(nums, ".")
	}
	return s
}

// ---- state → expression ---------------------------------------------------

type moodInput struct {
	now        time.Time
	started    time.Time
	sessionPct float64
	weeklyPct  float64
	known      bool // do we have a usage percentage at all
	generating bool
	opus       bool
	idle       time.Duration
	errAt      time.Time
	spikeAt    time.Time
	resetAt    time.Time // moment we saw the 5h window roll over
}

// pickExpression follows manifest.json › state_rules, first match wins.
func pickExpression(in moodInput) string {
	within := func(t time.Time, d time.Duration) bool { return !t.IsZero() && in.now.Sub(t) < d }
	u := in.sessionPct
	switch {
	case within(in.errAt, 4*time.Second):
		return "sassy"
	case in.known && u >= 100:
		return "panic"
	case in.known && u >= 95:
		return "despair"
	case within(in.spikeAt, 3*time.Second):
		return "shocked"
	case in.known && u >= 85:
		return "nervous"
	case in.known && in.weeklyPct >= 80:
		return "flustered"
	case within(in.started, 4*time.Second), within(in.resetAt, 4*time.Second):
		return "cheerful"
	case in.idle >= 10*time.Minute:
		return "sleepy"
	case in.known && u >= 75:
		return "glare"
	case in.known && u >= 60:
		return "annoyed"
	case in.generating && in.opus:
		return "determined"
	case in.generating:
		return "focused"
	case in.known && u < 10:
		return "bliss"
	case in.idle >= time.Minute && (!in.known || u < 40):
		return "relaxed"
	}
	return "neutral"
}

// ---- animation ------------------------------------------------------------

type animator struct {
	expr  string
	since time.Time
}

const (
	bobEvery = 4200 * time.Millisecond
	bobDip   = 650 * time.Millisecond
	quipTick = 9 * time.Second
)

// set switches expression, restarting its timeline only when it changes.
func (a *animator) set(expr string, now time.Time) {
	if expr != a.expr {
		a.expr, a.since = expr, now
	}
}

// at returns the frame index, whether the sprite is dipped (a gentle breathing
// bob for calm or static faces), and when the picture next changes.
func (a *animator) at(now time.Time) (idx int, dip bool, next time.Time) {
	tl := mf.Expressions[a.expr].Timeline
	el := now.Sub(a.since)
	next = now.Add(time.Hour)
	calm := len(tl) <= 1 || tl[0].MS >= 2000
	if len(tl) > 1 && tl[0].MS > 0 {
		var total time.Duration
		for _, s := range tl {
			total += time.Duration(s.MS) * time.Millisecond
		}
		pos := el % total
		for _, s := range tl {
			d := time.Duration(s.MS) * time.Millisecond
			if pos < d {
				idx = s.Frame
				next = now.Add(d - pos)
				break
			}
			pos -= d
		}
	}
	if calm {
		p := el % bobEvery
		dip = p >= bobEvery-bobDip
		var nb time.Time
		if dip {
			nb = now.Add(bobEvery - p)
		} else {
			nb = now.Add(bobEvery - bobDip - p)
		}
		if nb.Before(next) {
			next = nb
		}
	}
	return
}

var quips = map[string][]string{
	"neutral":    {"all good here.", "watching the meter.", "steady as she goes.", "nothing to report~"},
	"bliss":      {"so much room~", "fresh window, go wild!", "tokens for days.", "wheee~"},
	"cheerful":   {"hi hi! let's go!", "new window, new me!"},
	"relaxed":    {"taking a breather.", "tea break~", "quiet in here."},
	"focused":    {"claude is thinking...", "tools go brrr.", "counting tokens..."},
	"determined": {"opus is cooking!", "big brain time.", "full power!!"},
	"annoyed":    {"past the halfway mark.", "pace yourself, hm?", "getting pricey."},
	"glare":      {"...seriously?", "three quarters gone.", "i'm watching you."},
	"sleepy":     {"zzz...", "wake me when you're back.", "five more minutes..."},
	"shocked":    {"WHOA that spike!", "where'd it all go?!"},
	"nervous":    {"running low...", "careful now...", "maybe wrap it up?"},
	"despair":    {"almost empty...", "it was nice while it lasted."},
	"panic":      {"LIMIT HIT!!", "wait for the reset!!"},
	"flustered":  {"the weekly cap is close!", "save some for later!"},
	"sassy":      {"the api said no.", "rude. try again."},
}

func quipFor(expr string, now time.Time) string {
	q := quips[expr]
	if len(q) == 0 {
		return ""
	}
	return q[int(now.Unix()/int64(quipTick/time.Second))%len(q)]
}
