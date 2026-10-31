#!/usr/bin/env bash
#
# End-to-end tests: the whole wrapper, run in a test terminal
# (tools/vtharness) around a stand-in for claude (tools/fakeclaude), with
# the fake suggester and fake rewrites. No account, network or model is
# needed, so CI runs it on every push. scripts/smoke.sh checks the same
# flows against the real claude and models.
#
#   scripts/e2e.sh [case]...   cases: ghost rules panel preview auto accept nested
#
# Each case checks the debug log, what claude received (the fake records
# each submitted prompt), and the screen. On a failure, the case's files
# are kept and named.

set -euo pipefail
cd "$(dirname "$0")/.."
root="$PWD"

work="$(mktemp -d "${TMPDIR:-/tmp}/pe-e2e.XXXXXX")"
bin="$work/bin"
mkdir -p "$bin"
go build -o "$bin/prompt-engine" ./cmd/prompt-engine
go build -o "$bin/fakeclaude" ./tools/fakeclaude
go build -o "$bin/vtharness" ./tools/vtharness

failed=0
passed=0

# run_case name script [extra config lines]: runs prompt-engine around the
# fake claude, from a folder outside any git repository (so rewrites get
# no "Context:" lines), with the case's config and an optional rules file.
run_case() {
	local name="$1" script="$2" extra="${3:-}"
	dir="$work/$name"
	mkdir -p "$dir/templates"
	cat >"$dir/config.yaml" <<EOF
command: $bin/fakeclaude
args: ["-log", "$dir/sent.jsonl"]
$extra
EOF
	: >"$dir/sent.jsonl"
	(
		cd "$dir"
		PE_TEST_HOME="$dir" "$root/scripts/test-env.sh" \
			PROMPT_ENGINE_CONFIG="$dir/config.yaml" \
			PROMPT_ENGINE_DEBUG_LOG="$dir/pe.log" \
			PROMPT_ENGINE_SUGGESTER=fake \
			${NESTED:+PROMPT_ENGINE_ACTIVE=1} \
			"$bin/vtharness" -cols 120 -rows 30 -- "$bin/prompt-engine" \
			<<<"$script" >"$dir/screen.txt" 2>&1
	) 2>/dev/null || echo "harness: exited with an error" >>"$dir/screen.txt"
}

# check description condition...: records a pass or a failure for the
# current case.
check() {
	local what="$1"
	shift
	if "$@"; then
		passed=$((passed + 1))
		printf '  ✓ %s\n' "$what"
	else
		failed=$((failed + 1))
		printf '  ✗ %s (files: %s)\n' "$what" "$dir"
	fi
}
logged() { grep -qF -- "$1" "$dir/pe.log"; }
sent() { [ "$(tail -n 1 "$dir/sent.jsonl")" = "$1" ]; }
shown() { grep -qF -- "$1" "$dir/screen.txt"; }

case_ghost() {
	echo "ghost: a suggestion, accepted with Tab, is sent"
	run_case ghost 'reply on
wait "❯" 5s
send "please impl"
wait "please implement" 3s
send "\t"
sleep 300ms
send "\r"
sleep 500ms
send "\x04"
exit'
	check "suggestion shown and accepted" logged 'suggestion accepted "ement"'
	check "claude received the accepted text" sent '"please implement"'
}

case_rules() {
	echo "rules: templates/append.txt is added to the prompt sent"
	mkdir -p "$work/rules/templates"
	printf -- '- Run the tests before saying it works.\n' >"$work/rules/templates/append.txt"
	run_case rules 'reply on
wait "❯" 5s
send "say hello"
sleep 300ms
send "\r"
sleep 500ms
send "\x04"
exit'
	check "rules appended" logged 'rules: appended append.txt (1 line)'
	check "claude received the prompt and the rules" sent '"say hello\n\n- Run the tests before saying it works."'
}

case_panel() {
	echo "panel: Ctrl+Q, then p opens the side panel"
	run_case panel 'reply on
wait "❯" 5s
send "\x11p"
wait "Prompt diff" 3s
dump
send "\x04"
exit'
	check "the panel shows" shown 'Prompt diff'
	check "it says when rewrites happen" shown 'on request (ctrl+q then e)'
}

case_preview() {
	echo "preview: a rewrite prepared on a pause, applied with Ctrl+Q, e"
	run_case preview 'reply on
wait "❯" 5s
send "fix the parser now"
wait "Suggested rewrite" 5s
send "\x11e"
sleep 500ms
send "\r"
sleep 500ms
send "\x04"
exit' 'expand:
  on_enter: preview
  preview_pause: 300ms
panel:
  visible: true'
	check "preview prepared" logged 'expand: preview ready for "fix the parser now"'
	check "claude received the rewrite" sent '"Please fix the parser now."'
}

case_auto() {
	echo "auto: Enter rewrites the prompt, then sends it"
	run_case auto 'reply on
wait "❯" 5s
send "fix the parser now"
sleep 300ms
send "\r"
sleep 1s
send "\x04"
exit' 'expand:
  on_enter: auto'
	check "claude received the rewrite" sent '"Please fix the parser now."'
}

case_accept() {
	echo "accept: claude's suggestion taken with → and sent at once gets the rules"
	mkdir -p "$work/accept/templates"
	printf -- '- Run the tests before saying it works.\n' >"$work/accept/templates/append.txt"
	run_case accept 'reply on
wait "❯" 5s
send "say hello"
sleep 300ms
send "\r"
wait "Reply to: say hello" 3s
send "\x1b[C\r"
sleep 500ms
send "\x04"
exit'
	check "the prompt was read from the screen at Enter" logged 'buffer resynced from screen at Enter'
	check "claude received its suggestion with the rules" sent '"show me main.go\n\n- Run the tests before saying it works."'
}

case_nested() {
	echo "nested: inside prompt-engine already, it hands over and says so"
	NESTED=1 run_case nested 'reply on
wait "❯" 5s
send "\x04"
exit'
	check "the log says it ran without prompt-engine" logged 'already inside prompt-engine'
}

cases=("$@")
[ ${#cases[@]} -gt 0 ] || cases=(ghost rules panel preview auto accept nested)
for c in "${cases[@]}"; do
	"case_$c"
done

echo
if [ "$failed" -gt 0 ]; then
	echo "e2e: $failed failed, $passed passed (files in $work)"
	exit 1
fi
echo "e2e: all $passed passed"
rm -rf "$work"
