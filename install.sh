#!/bin/sh
# honjoji installer for macOS / Linux (and Git Bash on Windows). No sudo needed:
# installs to ~/.local/bin and adds it to PATH in your shell profile.
#   sh install.sh               install (or update) and run
#   sh install.sh --no-run      install only
#   sh install.sh --uninstall   remove
set -e
cd "$(dirname "$0")"

case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*)
    exec powershell.exe -NoProfile -ExecutionPolicy Bypass -File install.ps1 \
      $( [ "$1" = "--no-run" ] && echo -NoRun ) $( [ "$1" = "--uninstall" ] && echo -Uninstall ) ;;
  Darwin) os=darwin ;;
  Linux)  os=linux ;;
  *) echo "honjoji: unsupported OS $(uname -s)"; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64)  arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "honjoji: unsupported CPU $(uname -m)"; exit 1 ;;
esac

dest="$HOME/.local/bin"
if [ "$1" = "--uninstall" ]; then
  rm -f "$dest/honjoji"
  echo "honjoji removed. (Your cache at ~/.honjoji can be deleted too.)"
  exit 0
fi

src="dist/honjoji-$os-$arch"
if [ ! -f "$src" ]; then
  if command -v go >/dev/null 2>&1; then
    echo "No prebuilt binary for $os/$arch, building with Go..."
    src="${TMPDIR:-/tmp}/honjoji-build"
    go build -trimpath -ldflags "-s -w" -o "$src" .
  else
    echo "honjoji: missing $src. Try 'git pull' to fetch the prebuilt binaries."; exit 1
  fi
fi

mkdir -p "$dest"
cp "$src" "$dest/honjoji.tmp"
chmod +x "$dest/honjoji.tmp"
mv -f "$dest/honjoji.tmp" "$dest/honjoji"   # atomic, so updating works even while it's open
[ "$os" = darwin ] && xattr -d com.apple.quarantine "$dest/honjoji" 2>/dev/null || true

case ":$PATH:" in
  *":$dest:"*) ;;
  *)
    case "$(basename "${SHELL:-sh}")" in
      zsh)  rc="$HOME/.zshrc" ;;
      bash) if [ "$os" = darwin ]; then rc="$HOME/.bash_profile"; else rc="$HOME/.bashrc"; fi ;;
      *)    rc="$HOME/.profile" ;;
    esac
    line='export PATH="$HOME/.local/bin:$PATH"'
    grep -qsF "$line" "$rc" || printf '\n# added by honjoji\n%s\n' "$line" >> "$rc"
    echo "Added ~/.local/bin to PATH in $rc"
    ;;
esac

echo ""
echo "honjoji installed to $dest/honjoji"
claude_dir="${CLAUDE_CONFIG_DIR:-$HOME/.claude}"
if [ "$os" = linux ] && [ ! -f "$claude_dir/.credentials.json" ]; then
  echo "Note: no Claude Code login found yet. Run 'claude' and log in; honjoji picks it up automatically."
fi
[ "$os" = darwin ] && echo "Note: macOS may ask once to let honjoji read your Claude login from the Keychain. Choose 'Always Allow'."
echo "Type 'honjoji' in any NEW terminal to open it. Press Esc to quit."
echo ""

[ "$1" = "--no-run" ] || exec "$dest/honjoji"
