# honjoji

A tiny terminal tracker for your Claude usage, with Tally the mascot (see `mascot/`).
It only runs while it's open: type `honjoji` in any terminal and press **Esc** to quit.

## Install

You need Claude Code installed and logged in on the machine. You don't need Go or admin rights.

```
git clone https://github.com/breadDDDDD/claude-tracker.git
cd claude-tracker
```

Then run one command:

| OS | Command | Installs to |
|---|---|---|
| Windows (cmd / PowerShell) | `.\install.cmd` | `%LOCALAPPDATA%\Programs\honjoji` + user PATH, plus a copy in `WindowsApps` so it works right away, even in terminals that were already open (e.g. VS Code) |
| macOS / Linux / Git Bash | `sh install.sh` | `~/.local/bin` + your shell profile |

The command installs honjoji and starts it right away. After that, type `honjoji` in any new terminal.
To update, run `git pull` and then the same command again. To remove it, use `.\install.cmd -Uninstall` or `sh install.sh --uninstall`.

honjoji uses the login Claude Code already saved on the machine: `~/.claude/.credentials.json` on Windows and Linux, and the Keychain on macOS, where it may ask once for access. There's nothing to configure.

Prebuilt binaries for Windows, macOS and Linux (x64 and ARM) are in `dist/`. Maintainers can rebuild them with `sh build.sh`, which needs Go.

## Keys

| Key | Action |
|---|---|
| `Tab` / `↓` | next tab (Now → Models → Stats) |
| `Shift+Tab` / `↑` | previous tab |
| `←` `→` | change the view inside the tab: the period on **Models** (today / 7d / 30d / all), the date range on **Stats** (Today / Last 7 days / Last 30 days) |
| `r` | refresh usage now |
| `Esc` `q` `Ctrl+C` | quit |

## Flags

- `-once` prints one snapshot and exits.
- `-tab models` / `-tab stats` opens on that tab.
- `-no-api` works fully offline, using local estimates only.
- `-api-every 60s` sets the usage API poll interval (minimum 30s).

## Stats tab

Shows what Claude Code's `/stats` doesn't: favorite model, top project, responses, tool calls (and per reply), average active session length, words written (about 0.75 × output tokens), cache hit rate, most active day and current streak. Below that are an **activity-by-hour** chart and a rotating fun fact. The chart grows or shrinks with the terminal, and Tally's speech line gives way first on short screens. Ranges are Today / Last 7 days / Last 30 days, which is everything Claude Code keeps on disk.

Token totals, session counts and all-time history are left out on purpose, because they can't match `/stats`:
- `/stats` adds up every streamed log line, and each response is written 2–3 times. Its token numbers come out about 2.5× the real count; honjoji counts each response once.
- `/stats` keeps its own history past the 30 days of transcripts that honjoji can read.

"Avg session" counts only active time. Pauses longer than 15 minutes between responses are left out, so resumed sessions don't count days of idling.

## Where the numbers come from

- **5h session / weekly %, reset times:** Anthropic's usage endpoint, the same one behind Claude Code's `/usage`. It's polled every 60s with the login Claude Code saved in `~/.claude/.credentials.json`. honjoji only reads that token and never refreshes it. The endpoint is undocumented. If it fails, the 5h figure falls back to an estimate (`~NN% est`) based on local tokens and a ratio learned from the last successful call.
- **Tokens, models, burn and history:** your local transcripts in `~/.claude/projects/**/*.jsonl`. Entries are de-duplicated by message and request id. "tok" means input + output + cache-write tokens. Cache reads are shown separately.
- **Cache:** `~/.honjoji/cache.gob` stores parsed records and read offsets, so each launch only reads new bytes.

## Footprint

Each second it checks only the transcripts written in the last 30 minutes, reading just the bytes appended since the last check. A full folder scan for new sessions runs every 10s. It redraws only the rows that changed, and only when data changes or the next animation frame is due. Measured on a 16-thread laptop while Claude was active: 0–1.8% of one core (about 0.1% of total CPU), a working set of about 19 MB, 9 threads, no disk activity between reads, and a cold index of 140 MB of logs in under a second.
