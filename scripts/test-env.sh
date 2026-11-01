#!/usr/bin/env bash
#
# Runs a command in a clean environment for testing prompt-engine, safe to
# use from a session that itself runs under prompt-engine or Claude Code:
#
#   - removes every PROMPT_ENGINE_* variable: PROMPT_ENGINE_ACTIVE would make
#     the prompt-engine under test hand over to claude, PROMPT_ENGINE_CONFIG
#     would load your config, PROMPT_ENGINE_DEBUG_LOG would mix test lines
#     into your log;
#   - removes every CLAUDE* variable: inherited from a Claude Code session,
#     they stop claude from saving its transcript;
#   - keeps test data apart: the history goes to a throwaway folder
#     (XDG_STATE_HOME), and the config is a throwaway file unless given.
#
#   scripts/test-env.sh [NAME=value]... command [args]...
#
# NAME=value pairs before the command are set after cleaning, e.g.
# PROMPT_ENGINE_DEBUG_LOG=/path/test.log. PE_TEST_HOME picks the throwaway
# folder (default: a new one under $TMPDIR); it is printed to stderr.

set -euo pipefail

home="${PE_TEST_HOME:-}"
if [ -z "$home" ]; then
	home="$(mktemp -d "${TMPDIR:-/tmp}/pe-test.XXXXXX")"
fi
mkdir -p "$home/state"
echo "test-env: test data in $home" >&2

unset_args=()
for v in $(env | sed -nE 's/^((CLAUDE|PROMPT_ENGINE_)[A-Za-z0-9_]*)=.*/\1/p'); do
	unset_args+=(-u "$v")
done

set_args=("PROMPT_ENGINE_CONFIG=$home/config.yaml" "XDG_STATE_HOME=$home/state")
while [ $# -gt 0 ] && [[ "$1" =~ ^[A-Za-z_][A-Za-z0-9_]*= ]]; do
	set_args+=("$1")
	shift
done
[ $# -gt 0 ] || { echo "usage: scripts/test-env.sh [NAME=value]... command [args]..." >&2; exit 2; }

exec env ${unset_args[@]+"${unset_args[@]}"} "${set_args[@]}" "$@"
