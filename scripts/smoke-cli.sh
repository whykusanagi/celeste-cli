#!/usr/bin/env bash
# smoke-cli.sh — drive the real celeste binary through new-feature code paths to
# confirm they still work end-to-end, including one live call to the configured
# model (sakana/fugu by default) so we know model wiring is intact.
#
#   ./scripts/smoke-cli.sh            # uses `celeste` on PATH
#   CELESTE=./celeste ./scripts/smoke-cli.sh
#
# The live model call costs a few tokens. Skip it with SMOKE_NO_MODEL=1.
set -uo pipefail

CELESTE="${CELESTE:-celeste}"
pass=0
fail=0

# check NAME "command" [expected-substring]
check() {
	local name="$1" cmd="$2" want="${3:-}" out rc
	out="$(eval "$cmd" 2>&1)"
	rc=$?
	if [ "$rc" -ne 0 ]; then
		printf '✗ %-22s (exit %d)\n' "$name" "$rc"
		echo "$out" | head -3 | sed 's/^/    /'
		fail=$((fail + 1))
		return
	fi
	if [ -n "$want" ] && ! grep -qF "$want" <<<"$out"; then
		printf '✗ %-22s (missing %q)\n' "$name" "$want"
		echo "$out" | head -3 | sed 's/^/    /'
		fail=$((fail + 1))
		return
	fi
	printf '✓ %-22s\n' "$name"
	pass=$((pass + 1))
}

echo "celeste CLI smoke test ($("$CELESTE" version 2>/dev/null))"
echo "---"

check "version"            "$CELESTE version"                     "Celeste CLI"
check "mcp install dry-run" "$CELESTE mcp install --dry-run"      "would write"
check "mcp install codex"   "$CELESTE mcp install --client codex" "mcp_servers.celeste"
check "index status"        "$CELESTE index status"               "index"

if [ "${SMOKE_NO_MODEL:-}" = "1" ]; then
	printf '· %-22s (skipped: SMOKE_NO_MODEL=1)\n' "model responds"
else
	# Live one-shot against the configured model — proves model wiring works.
	check "model responds" \
		"$CELESTE message 'Reply with exactly this token and nothing else: SMOKE_OK_7F3'" \
		"SMOKE_OK_7F3"
fi

echo "---"
echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ]
