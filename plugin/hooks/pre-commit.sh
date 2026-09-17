#!/bin/sh
set -u

[ -n "${TENETLINT_SKIP:-}" ] && exit 0

# The hook's "if" filter is best effort: Claude Code runs the hook anyway when
# it cannot tell what a Bash command expands to. So read the tool call and look
# at the command itself before spending a call on the model.
if ! grep -q '"command"[[:space:]]*:[[:space:]]*"[^"]*git commit'; then
	exit 0
fi

# A repository that does not use tenetlint, or a machine that has not installed
# it, must still be able to commit, so a missing binary is not a refusal.
if ! command -v tenet >/dev/null 2>&1; then
	echo "tenet is not on PATH; the staged changes were not linted" >&2
	exit 0
fi

# A broken run says why on stderr, and that reason is as worth showing as a
# finding is.
output=$(tenet --format json 2>&1)
status=$?
[ "$status" -eq 0 ] && exit 0

printf '%s\n' "$output" >&2
exit 2
