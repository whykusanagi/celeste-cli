#!/usr/bin/env bash
# tui-snapshots.sh — render every sprint TUI component to a PNG for visual
# release verification. Renders each Bubble Tea View() to colored ANSI (no TTY
# needed) via the in-package TestRenderSmoke harness, then freezes each to a PNG.
#
#   ./scripts/tui-snapshots.sh [OUT_DIR]     # default: test-output/tui
#
# Output is gitignored (test-output/). Requires charmbracelet/freeze:
#   go install github.com/charmbracelet/freeze@latest
set -euo pipefail

cd "$(dirname "$0")/.."
shopt -s nullglob
OUT="${1:-test-output/tui}"

if ! command -v freeze >/dev/null 2>&1; then
	echo "error: 'freeze' not found. Install it:" >&2
	echo "  go install github.com/charmbracelet/freeze@latest" >&2
	exit 1
fi

mkdir -p "$OUT"
# Absolutize: `go test` runs with cwd = the package dir, so a relative
# TUI_SNAPSHOT_DIR would resolve under cmd/celeste/tui/, not the repo root.
OUT="$(cd "$OUT" && pwd)"
rm -f "$OUT"/*.ansi "$OUT"/*.png 2>/dev/null || true

# 1. Render components -> colored .ansi files (forces truecolor in the harness).
TUI_SNAPSHOT_DIR="$OUT" go test ./cmd/celeste/tui/ -run TestRenderSmoke -count=1 >/dev/null

# 2. Freeze each .ansi -> .png.
count=0
for f in "$OUT"/*.ansi; do
	freeze --language ansi "$f" -o "${f%.ansi}.png" >/dev/null
	count=$((count + 1))
done
rm -f "$OUT"/*.ansi

echo "✅ $count TUI snapshots → $OUT/"
for p in "$OUT"/*.png; do echo "   $p"; done
