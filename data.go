package main

import (
	"bufio"
	"bytes"
	"encoding/gob"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Rec is one de-duplicated assistant response.
type Rec struct {
	TS             int64 // unix seconds
	Model          uint16
	In, Out, CR, CW int64
}

// Fresh is the token count we treat as "usage": everything except cheap cache reads.
func (r Rec) Fresh() int64 { return r.In + r.Out + r.CW }

type fileState struct {
	Off, Size, Mod int64
}

// Store holds every usage record plus per-file read offsets, so each scan only
// reads bytes appended since the last one. It is persisted to disk between runs.
type Store struct {
	Recs      []Rec
	Models    []string
	Seen      map[string]int // message.id+requestId -> index in Recs
	Files     map[string]*fileState
	PctPerTok float64 // learned from the usage API, used for offline estimates

	root      string
	modelIdx  map[string]uint16
	lastMod   time.Time // newest transcript write we've seen
	lastErr   time.Time // newest API error line
	dirty     bool
	cachePath string
}

func newStore(root, cachePath string) *Store {
	s := &Store{Seen: map[string]int{}, Files: map[string]*fileState{}, root: root, cachePath: cachePath}
	if f, err := os.Open(cachePath); err == nil {
		var c Store
		if gob.NewDecoder(f).Decode(&c) == nil && c.Seen != nil && c.Files != nil {
			s.Recs, s.Models, s.Seen, s.Files, s.PctPerTok = c.Recs, c.Models, c.Seen, c.Files, c.PctPerTok
		}
		f.Close()
	}
	s.modelIdx = map[string]uint16{}
	for i, m := range s.Models {
		s.modelIdx[m] = uint16(i)
	}
	for _, st := range s.Files {
		if t := time.Unix(0, st.Mod); t.After(s.lastMod) {
			s.lastMod = t
		}
	}
	return s
}

func (s *Store) save() {
	if !s.dirty {
		return
	}
	os.MkdirAll(filepath.Dir(s.cachePath), 0o755)
	tmp := s.cachePath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return
	}
	err = gob.NewEncoder(f).Encode(s)
	f.Close()
	if err == nil && os.Rename(tmp, s.cachePath) == nil {
		s.dirty = false
	}
}

// hotFor is how recently a transcript must have been written to be re-checked
// every second; everything else is only picked up by the periodic full walk.
const hotFor = 30 * time.Minute

// scan reads only what changed. A full walk finds new and removed transcripts;
// otherwise just the recently active ones are stat'ed, which is far cheaper.
// Returns true if new records arrived.
func (s *Store) scan(full bool) bool {
	changed := false
	if !full {
		cutoff := time.Now().Add(-hotFor).UnixNano()
		for path, st := range s.Files {
			if st.Mod < cutoff {
				continue
			}
			if info, err := os.Stat(path); err == nil && s.check(path, st, info) {
				changed = true
			}
		}
		return changed
	}
	alive := map[string]bool{}
	filepath.WalkDir(s.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		alive[path] = true
		st := s.Files[path]
		if st == nil {
			st = &fileState{}
			s.Files[path] = st
		}
		if s.check(path, st, info) {
			changed = true
		}
		return nil
	})
	for p := range s.Files {
		if !alive[p] {
			delete(s.Files, p) // transcript cleaned up; its records stay in Recs
			s.dirty = true
		}
	}
	return changed
}

// check reads a transcript's new bytes if its size or mtime moved.
func (s *Store) check(path string, st *fileState, info fs.FileInfo) bool {
	size, mod := info.Size(), info.ModTime().UnixNano()
	if t := info.ModTime(); t.After(s.lastMod) {
		s.lastMod = t
	}
	if st.Size == size && st.Mod == mod {
		return false
	}
	if size < st.Off { // rewritten; the Seen set keeps us from double counting
		st.Off = 0
	}
	changed := s.readFrom(path, st)
	st.Size, st.Mod = size, mod
	s.dirty = true
	return changed
}

var (
	kUsage     = []byte(`"usage"`)
	kAssistant = []byte(`"assistant"`)
	kAPIErr    = []byte(`"isApiErrorMessage":true`)
)

type rawLine struct {
	Timestamp string `json:"timestamp"`
	RequestID string `json:"requestId"`
	Message   struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			In int64 `json:"input_tokens"`
			Out int64 `json:"output_tokens"`
			CR  int64 `json:"cache_read_input_tokens"`
			CW  int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

func (s *Store) readFrom(path string, st *fileState) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	if _, err := f.Seek(st.Off, io.SeekStart); err != nil {
		return false
	}
	r := bufio.NewReaderSize(f, 64<<10)
	changed := false
	for {
		line, err := r.ReadBytes('\n')
		if err != nil { // partial trailing line: leave it for the next scan
			break
		}
		st.Off += int64(len(line))
		if bytes.Contains(line, kAPIErr) {
			var l rawLine
			if json.Unmarshal(line, &l) == nil {
				if t, e := time.Parse(time.RFC3339Nano, l.Timestamp); e == nil && t.After(s.lastErr) {
					s.lastErr = t
				}
			}
		}
		if !bytes.Contains(line, kUsage) || !bytes.Contains(line, kAssistant) {
			continue
		}
		var l rawLine
		if json.Unmarshal(line, &l) != nil || l.Message.Usage == nil {
			continue
		}
		m := l.Message.Model
		if m == "" || strings.HasPrefix(m, "<") {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, l.Timestamp)
		if err != nil {
			continue
		}
		u := l.Message.Usage
		rec := Rec{TS: t.Unix(), Model: s.model(m), In: u.In, Out: u.Out, CR: u.CR, CW: u.CW}
		key := l.Message.ID + ":" + l.RequestID
		if i, ok := s.Seen[key]; ok {
			// Streaming writes the same message several times; keep the most complete.
			if rec.Out > s.Recs[i].Out {
				s.Recs[i] = rec
				changed = true
			}
			continue
		}
		s.Seen[key] = len(s.Recs)
		s.Recs = append(s.Recs, rec)
		changed = true
	}
	return changed
}

func (s *Store) model(name string) uint16 {
	if i, ok := s.modelIdx[name]; ok {
		return i
	}
	i := uint16(len(s.Models))
	s.Models = append(s.Models, name)
	s.modelIdx[name] = i
	return i
}

// ---- aggregates -----------------------------------------------------------

type modelStat struct {
	Name     string
	Tok, Msg int64
}

type Stats struct {
	Current      string // model of the newest record
	TodayTok     int64
	TodayMsg     int64
	TokPerMin    float64 // last 10 min
	Burn         [15]int64 // last 30 min, 2-min buckets
	Daily        [14]int64 // last 14 days
	Top          []modelStat
	PeriodTok    int64
	PeriodMsg    int64
	PeriodCR     int64
	PeriodIn     int64
	WindowTok    int64 // fresh tokens since the current 5h window started
}

var periods = []struct {
	Name string
	Days int
}{{"today", 0}, {"7d", 7}, {"30d", 30}, {"all", -1}}

func (s *Store) stats(now time.Time, period int, windowStart time.Time) Stats {
	var st Stats
	y, mo, d := now.Date()
	midnight := time.Date(y, mo, d, 0, 0, 0, 0, now.Location())
	var pStart int64
	switch p := periods[period]; {
	case p.Days == 0:
		pStart = midnight.Unix()
	case p.Days > 0:
		pStart = now.Add(-time.Duration(p.Days) * 24 * time.Hour).Unix()
	default:
		pStart = 0
	}
	nowU, midU, winU := now.Unix(), midnight.Unix(), windowStart.Unix()
	per := map[uint16]*modelStat{}
	var newest int64 = -1
	for _, r := range s.Recs {
		f := r.Fresh()
		if r.TS > newest {
			newest, st.Current = r.TS, s.Models[r.Model]
		}
		if r.TS >= midU {
			st.TodayTok += f
			st.TodayMsg++
		}
		if age := nowU - r.TS; age >= 0 {
			if age < 600 {
				st.TokPerMin += float64(f) / 10
			}
			if age < 1800 {
				st.Burn[14-age/120] += f
			}
		}
		if day := (midU - r.TS + 86399) / 86400; r.TS >= midU {
			st.Daily[13] += f
		} else if day < 14 {
			st.Daily[13-day] += f
		}
		if !windowStart.IsZero() && r.TS >= winU {
			st.WindowTok += f
		}
		if r.TS >= pStart {
			m := per[r.Model]
			if m == nil {
				m = &modelStat{Name: s.Models[r.Model]}
				per[r.Model] = m
			}
			m.Tok += f
			m.Msg++
			st.PeriodTok += f
			st.PeriodMsg++
			st.PeriodCR += r.CR
			st.PeriodIn += r.In + r.CW
		}
	}
	for _, m := range per {
		st.Top = append(st.Top, *m)
	}
	// small n: insertion sort by tokens
	for i := 1; i < len(st.Top); i++ {
		for j := i; j > 0 && st.Top[j].Tok > st.Top[j-1].Tok; j-- {
			st.Top[j], st.Top[j-1] = st.Top[j-1], st.Top[j]
		}
	}
	return st
}
