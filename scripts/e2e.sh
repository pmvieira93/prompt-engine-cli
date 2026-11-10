#!/usr/bin/env bash
#
# End-to-end tests: the whole wrapper, run in a test terminal
# (tools/vtharness) around a stand-in for claude (tools/fakeclaude), with
# the fake suggester and fake rewrites. No account, network or model is
# needed, so CI runs it on every push. scripts/smoke.sh checks the same
# flows against the real claude and models.
#
#   scripts/e2e.sh [case]...   cases: ghost rules rules_all rules_sections rules_hook panel preview auto accept style history usage guards guards_off rewrite nested
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
# Named claude, as prompt-engine only sets up the rules hook for claude.
mkdir -p "$bin/fake"
go build -o "$bin/fake/claude" ./tools/fakeclaude
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
command: $bin/fake/claude
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
			CLAUDE_CONFIG_DIR="$dir/claude" \
			${NESTED:+PROMPT_ENGINE_ACTIVE=1} \
			${PE_HISTORY:+PROMPT_ENGINE_HISTORY="$PE_HISTORY"} \
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
sent_any() { grep -qF -- "$1" "$dir/sent.jsonl"; }
first_sent() { [ "$(head -n 1 "$dir/sent.jsonl")" = "$1" ]; }
shown() { grep -qF -- "$1" "$dir/screen.txt"; }
fails_shown() { ! shown "$1"; }
same() { [ -n "$1" ] && [ "$1" = "$2" ]; }
printed() { grep -qF -- "$1" "$dir/out.json"; }

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
send "say hello there"
sleep 300ms
send "\r"
sleep 500ms
send "ok thanks"
sleep 300ms
send "\r"
sleep 500ms
send "\x04"
exit'
	check "rules appended" logged 'rules: appended append.txt (1 line)'
	check "claude received the prompt and the rules" first_sent '"say hello there\n\n# Guidance\n- Run the tests before saying it works."'
	check "a short prompt gets no rules" logged 'rules: not added (short prompt)'
	check "claude received the short prompt as typed" sent '"ok thanks"'
}

case_rules_all() {
	echo "rules_all: templates.min_words: 1 adds the rules to short prompts, whatever expand.min_words is"
	mkdir -p "$work/rules_all/templates"
	printf -- '- Run the tests before saying it works.\n' >"$work/rules_all/templates/append.txt"
	run_case rules_all 'reply on
wait "❯" 5s
send "ok thanks"
sleep 300ms
send "\r"
sleep 500ms
send "\x04"
exit' 'expand:
  min_words: 10
templates:
  min_words: 1'
	check "claude received the short prompt and the rules" sent '"ok thanks\n\n# Guidance\n- Run the tests before saying it works."'
}

case_rules_sections() {
	echo "rules_sections: a section of append.txt is added only to prompts with its words"
	mkdir -p "$work/rules_sections/templates"
	printf -- 'Keep changes small.\n\n[test|tests]\nRun them first.\n' >"$work/rules_sections/templates/append.txt"
	run_case rules_sections 'reply on
wait "❯" 5s
send "explain the loader"
sleep 300ms
send "\r"
sleep 500ms
send "fix the failing tests"
sleep 300ms
send "\r"
sleep 500ms
send "\x04"
exit'
	check "no match: only the always-added text" first_sent '"explain the loader\n\n# Guidance\nKeep changes small."'
	check "a match adds the section" sent '"fix the failing tests\n\n# Guidance\nKeep changes small.\n\nRun them first."'
	check "the log names the section" logged 'rules: appended append.txt (3 lines; sections: test)'
}

case_rules_hook() {
	echo "rules_hook: rules of any length reach claude as hook context, not as a paste"
	mkdir -p "$work/rules_hook/templates"
	printf -- 'First line.\nSecond line.\nThird line.\nFourth line.\n' >"$work/rules_hook/templates/append.txt"
	run_case rules_hook 'reply on
wait "❯" 5s
send "explain the loader"
sleep 300ms
send "\r"
sleep 500ms
send "explain this: "
sleep 200ms
send "\x1b[200~a\nb\nc\nd\x1b[201~"
sleep 300ms
send "\r"
sleep 500ms
send "fix the parser now"
sleep 300ms
send "\x11"
sleep 200ms
send "\r"
sleep 500ms
send "\x04"
exit'
	check "long rules arrive untagged" first_sent '"explain the loader\n\n# Guidance\nFirst line.\nSecond line.\nThird line.\nFourth line."'
	check "a large paste gets the rules too" sent_any 'a\nb\nc\nd'
	check "the rules stay outside the tags" sent_any '\n\n\n# Guidance\nFirst line.\nSecond line.'
	check "leader, then Enter sends no rules" sent '"fix the parser now"'
	check "the hook logged it" logged 'rules: hook gave claude'
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
	check "claude received its suggestion with the rules" sent '"show me main.go\n\n# Guidance\n- Run the tests before saying it works."'
}

case_style() {
	echo "style: Ctrl+Q, s picks the style for one prompt, and rewrites a rewrite again"
	run_case style 'reply on
wait "❯" 5s
send "fix the parser now"
sleep 300ms
send "\x11s"
wait "style: llm" 2s
dump
send "\x11e"
wait "(llm)" 3s
send "\x11s"
wait "(human)" 3s
send "\r"
sleep 500ms
send "fix the lexer too"
sleep 300ms
send "\x11e"
wait "Please fix the lexer too." 3s
sleep 200ms
send "\r"
sleep 500ms
send "\x04"
exit' 'expand:
  context: none'
	check "the style is chosen for this prompt" logged 'style: llm for this prompt (was default)'
	check "a faint note shows it" shown 'style: llm'
	check "the rewrite uses it" logged 'expand: replaced "fix the parser now" with "Please fix the parser now. (llm)"'
	check "a second press rewrites the original text again" logged 'expand: rewrote "fix the parser now" again: replaced "Please fix the parser now. (llm)" with "Please fix the parser now. (human)"'
	check "claude received the last style" first_sent '"Please fix the parser now. (human)"'
	check "the next prompt is back to expand.style" sent '"Please fix the lexer too."'

	echo "style in preview mode: the suggestion is prepared again in the new style"
	run_case style-preview 'reply on
wait "❯" 5s
send "fix the parser now"
wait "Suggested rewrite (not used yet)" 5s
send "\x11s"
wait "Suggested rewrite (llm, for this prompt)" 5s
dump
send "\x11e"
sleep 500ms
send "\r"
sleep 500ms
send "\x04"
exit' 'expand:
  on_enter: preview
  preview_pause: 300ms
  context: none
panel:
  visible: true'
	check "the panel names the style" shown 'Suggested rewrite (llm, for this prompt)'
	check "no note over the open panel" fails_shown 'style: llm'
	check "claude received the new style" sent '"Please fix the parser now. (llm)"'

	echo "style in auto mode: Enter rewrites in the chosen style"
	run_case style-auto 'reply on
wait "❯" 5s
send "fix the parser now"
sleep 300ms
send "\x11s"
sleep 200ms
send "\r"
sleep 800ms
send "\x04"
exit' 'expand:
  on_enter: auto
  context: none'
	check "claude received the chosen style" sent '"Please fix the parser now. (llm)"'
	check "the history records it" grep -q '"style":"llm"' "$dir/state/prompt-engine/history.jsonl"

	echo "style, then Ctrl+C: clearing the box resets the style"
	run_case style-clear 'reply on
wait "❯" 5s
send "fix the parser now"
sleep 300ms
send "\x11s"
sleep 200ms
send "\x03"
sleep 300ms
send "fix the lexer too"
sleep 300ms
send "\x11e"
wait "Please fix the lexer too." 3s
sleep 200ms
send "\r"
sleep 500ms
send "\x04"
exit' 'expand:
  context: none'
	check "the log says the style is back" logged 'style: back to default (the prompt was sent or cleared)'
	check "claude received the default style" sent '"Please fix the lexer too."'
}

case_history() {
	echo "history: PROMPT_ENGINE_HISTORY picks the file, and each line names the session"
	PE_HISTORY="$work/history/project.jsonl" run_case history 'reply on
wait "❯" 5s
send "say hello"
sleep 300ms
send "\r"
sleep 500ms
send "\x04"
exit'
	local id
	id="$(sed -n 's/.*session start: id \([0-9a-f]*\);.*/\1/p' "$dir/pe.log")"
	check "the log names the session" test -n "$id"
	check "the history goes to PROMPT_ENGINE_HISTORY" test -s "$work/history/project.jsonl"
	check "its line names the session" grep -q "\"session\":\"$id\"" "$work/history/project.jsonl"
	check "not to the default file" test ! -e "$dir/state/prompt-engine/history.jsonl"
}

case_usage() {
	echo "usage: the panel shows the tokens from the transcript, and the history records each prompt's"
	PE_HISTORY="$work/history/usage.jsonl" run_case usage 'reply on
wait "❯" 5s
send "say hello"
sleep 300ms
send "\r"
wait "Reply to: say hello" 3s
send "\x11p"
wait "tokens:" 5s
dump
send "say bye"
sleep 300ms
send "\r"
wait "Reply to: say bye" 3s
sleep 2500ms
send "\x04"
exit'
	local h="$work/history/usage.jsonl"
	check "the panel shows the context" shown 'tokens:      context 41k · claude-fake-1'
	check "and the reply's tokens" shown 'reply:     41k in (99% cached), 250 out'
	check "the log has the first reply's usage" logged 'usage: reply 1 call, 41k in (99% cached), 250 out; context 41k, claude-fake-1'
	check "the history has a usage line for each prompt" test "$(grep -c '"kind":"usage"' "$h")" -eq 2
	check "the first names the first prompt" same "$(sed -n 's/.*"kind":"usage","prompt_time":"\([^"]*\)".*"cache_read_tokens":41000.*/\1/p' "$h")" "$(sed -n '1s/^{"time":"\([^"]*\)".*/\1/p' "$h")"
	check "the second, recorded at exit, names the second" same "$(sed -n 's/.*"kind":"usage","prompt_time":"\([^"]*\)".*"cache_read_tokens":42000.*/\1/p' "$h")" "$(grep -v '"kind"' "$h" | sed -n '2s/^{"time":"\([^"]*\)".*/\1/p')"
}

case_guards() {
	echo "guards: templates/guards.txt adds a rule, an edit applies at once, and deleting it leaves the built-in guards"
	local g="$work/guards/templates/guards.txt" sent="$work/guards/sent.jsonl"
	mkdir -p "$work/guards/templates"
	printf -- '# no please\nnever add: please\n' >"$g"
	# Each edit waits for claude to receive a prompt, not for a time, so a
	# slow start can't move it before the prompt it should follow.
	(
		lines() { [ -f "$sent" ] && wc -l <"$sent" || echo 0; }
		for _ in $(seq 150); do [ "$(lines)" -ge 1 ] && break; sleep 0.1; done
		printf -- 'turn off: question\n' >"$g"
		for _ in $(seq 150); do [ "$(lines)" -ge 2 ] && break; sleep 0.1; done
		rm -f "$g"
	) &
	run_case guards 'reply on
wait "❯" 5s
send "fix the parser now"
sleep 300ms
send "\r"
sleep 3s
send "fix the lexer now"
sleep 300ms
send "\r"
sleep 3s
send "\x04"
exit' 'expand:
  on_enter: auto'
	wait
	check "the rule was read" logged 'guards.txt: never add 1'
	check "it stopped the rewrite" logged 'not used (guard never-add: the rewrite added "please" (guards.txt line 2: never add)); sending as typed'
	check "claude received the prompt as typed" first_sent '"fix the parser now"'
	check "the edit was read" logged 'guards.txt: off: question'
	check "the next prompt was rewritten" grep -qF '"Please fix the lexer now.' "$dir/sent.jsonl"
	check "deleting the file leaves the built-in guards" logged 'guards: built-in only (no '
}

case_guards_off() {
	echo "guards_off: expand.guards: off ignores templates/guards.txt"
	mkdir -p "$work/guards_off/templates"
	printf -- 'never add: please\n' >"$work/guards_off/templates/guards.txt"
	run_case guards_off 'reply on
wait "❯" 5s
send "fix the parser now"
sleep 300ms
send "\r"
sleep 1s
send "\x04"
exit' 'expand:
  on_enter: auto
  guards: off'
	check "the rule didn't stop the rewrite" sent '"Please fix the parser now."'
	check "the log says guards.txt is ignored" logged 'guards: expand.guards is off: '
}

case_rewrite() {
	echo "rewrite: --pe-rewrite prints, without a terminal, what auto mode would send and the guidance"
	dir="$work/rewrite"
	mkdir -p "$dir/templates"
	# The fake claude logs what it receives, so the last check can fail.
	printf 'command: %s\nargs: ["-log", "%s"]\n' "$bin/fake/claude" "$dir/sent.jsonl" >"$dir/config.yaml"
	printf 'Keep changes small.\n\n[test|tests]\nRun the tests.\n' >"$dir/templates/append.txt"
	(
		cd "$dir"
		PE_TEST_HOME="$dir" "$root/scripts/test-env.sh" \
			PROMPT_ENGINE_CONFIG="$dir/config.yaml" \
			PROMPT_ENGINE_SUGGESTER=fake \
			"$bin/prompt-engine" --pe-rewrite "fix the parser tests" </dev/null >"$dir/out.json" 2>"$dir/err.txt"
	) && echo 0 >"$dir/code" || echo $? >"$dir/code"
	check "it exited with 0" [ "$(cat "$dir/code")" = 0 ]
	check "the prompt is the rewrite auto mode sends" printed '"prompt": "Please fix the parser tests.",'
	check "it was rewritten" printed '"rewritten": true,'
	check "the guidance is apart, with its section" printed '"guidance": "# Guidance\nKeep changes small.\n\nRun the tests.",'
	check "nothing on stderr but test-env's note" [ -z "$(grep -v '^test-env:' "$dir/err.txt")" ]
	check "claude wasn't started" [ ! -e "$dir/sent.jsonl" ]
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
[ ${#cases[@]} -gt 0 ] || cases=(ghost rules rules_all rules_sections rules_hook panel preview auto accept style history usage guards guards_off rewrite nested)
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
