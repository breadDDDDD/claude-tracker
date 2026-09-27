package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

// Limit is one rate-limit bucket as reported by Anthropic.
type Limit struct {
	Util  float64
	Reset time.Time
	OK    bool
}

type Usage struct {
	Session Limit // rolling 5h window
	Weekly  Limit
	Fetched time.Time // when this data (or error) was obtained
	Err     error
	RetryAt time.Time // set with errRateLimited: don't ask again before this
}

var errRateLimited = errors.New("rate limited")

// The last good reading and any rate-limit backoff are shared through a small
// file, so every open honjoji (and every relaunch) reuses one request a minute
// and shows real numbers immediately instead of waiting on the network.
type usageShared struct {
	Session, Weekly Limit
	Fetched         time.Time // last successful fetch
	RetryAt         time.Time // nobody asks before this after a 429
}

func sharedPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".honjoji", "usage.json")
}

func loadShared() usageShared {
	var c usageShared
	if b, err := os.ReadFile(sharedPath()); err == nil {
		json.Unmarshal(b, &c)
	}
	return c
}

func saveShared(c usageShared) {
	os.MkdirAll(filepath.Dir(sharedPath()), 0o755)
	if b, err := json.Marshal(c); err == nil {
		tmp := sharedPath() + ".tmp"
		if os.WriteFile(tmp, b, 0o644) == nil {
			os.Rename(tmp, sharedPath())
		}
	}
}

// getUsage returns fresh-enough usage, hitting the network only when no
// honjoji fetched within maxAge and no rate-limit backoff is pending.
// backoff is the wait to use after a 429 that didn't say how long.
func getUsage(claudeDir string, maxAge, backoff time.Duration) Usage {
	c := loadShared()
	now := time.Now()
	cached := Usage{Session: c.Session, Weekly: c.Weekly, Fetched: c.Fetched}
	if c.Session.OK && now.Sub(c.Fetched) < maxAge {
		return cached
	}
	if now.Before(c.RetryAt) {
		cached.Err, cached.RetryAt = errRateLimited, c.RetryAt
		return cached
	}
	u := fetchUsage(claudeDir)
	switch {
	case u.Err == nil:
		saveShared(usageShared{Session: u.Session, Weekly: u.Weekly, Fetched: u.Fetched})
		return u
	case u.Err == errRateLimited:
		if u.RetryAt.IsZero() {
			u.RetryAt = now.Add(backoff)
		}
		c.RetryAt = u.RetryAt
		saveShared(c)
	}
	// Keep showing the last good numbers alongside the error.
	cached.Err, cached.RetryAt = u.Err, u.RetryAt
	return cached
}

var httpClient = &http.Client{Timeout: 10 * time.Second}

// readCredentials finds the login Claude Code already saved on this machine:
// ~/.claude/.credentials.json on Windows and Linux, the login Keychain on macOS.
func readCredentials(claudeDir string) ([]byte, error) {
	b, err := os.ReadFile(filepath.Join(claudeDir, ".credentials.json"))
	if err == nil || runtime.GOOS != "darwin" {
		return b, err
	}
	return exec.Command("security", "find-generic-password", "-s", "Claude Code-credentials", "-w").Output()
}

// fetchUsage asks the same endpoint Claude Code's /usage screen uses. The OAuth
// token is re-read from Claude Code's credentials file every call and never
// refreshed here, so we never interfere with Claude Code's own login.
func fetchUsage(claudeDir string) Usage {
	u := Usage{Fetched: time.Now()}
	b, err := readCredentials(claudeDir)
	if err != nil {
		u.Err = errors.New("no Claude login found, run claude once")
		return u
	}
	var cred struct {
		OAuth struct {
			Token     string `json:"accessToken"`
			ExpiresAt int64  `json:"expiresAt"`
		} `json:"claudeAiOauth"`
	}
	if json.Unmarshal(b, &cred) != nil || cred.OAuth.Token == "" {
		u.Err = errors.New("no Claude login found")
		return u
	}
	if cred.OAuth.ExpiresAt > 0 && time.UnixMilli(cred.OAuth.ExpiresAt).Before(time.Now()) {
		u.Err = errors.New("login expired, open claude once")
		return u
	}
	req, _ := http.NewRequest("GET", "https://api.anthropic.com/api/oauth/usage", nil)
	req.Header.Set("Authorization", "Bearer "+cred.OAuth.Token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("User-Agent", "honjoji/1.0")
	resp, err := httpClient.Do(req)
	if err != nil {
		u.Err = errors.New("offline")
		return u
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		u.Err = errRateLimited
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 {
			u.RetryAt = time.Now().Add(time.Duration(s) * time.Second)
		}
		return u
	}
	if resp.StatusCode != 200 {
		u.Err = fmt.Errorf("usage api %d", resp.StatusCode)
		return u
	}
	type bucket struct {
		Util    *float64 `json:"utilization"`
		ResetAt string   `json:"resets_at"`
	}
	var body struct {
		FiveHour *bucket `json:"five_hour"`
		SevenDay *bucket `json:"seven_day"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		u.Err = errors.New("unexpected usage format")
		return u
	}
	conv := func(b *bucket) Limit {
		if b == nil || b.Util == nil {
			return Limit{}
		}
		t, _ := time.Parse(time.RFC3339Nano, b.ResetAt)
		return Limit{Util: *b.Util, Reset: t, OK: true}
	}
	u.Session, u.Weekly = conv(body.FiveHour), conv(body.SevenDay)
	if !u.Session.OK {
		u.Err = errors.New("unexpected usage format")
	}
	return u
}
