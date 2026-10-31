#!/usr/bin/env bash
#
# Live smoke test: the main flows of prompt-engine against the real Claude
# Code and the local models, driven by VHS (https://github.com/charmbracelet/vhs)
# in a throwaway project, ~/pe-demo/smoke-app. scripts/e2e.sh checks the
# same flows without claude, in CI.
#
#   scripts/smoke.sh [--keep]
#
# It sends three harmless prompts (list the files, claude's own suggested
# next prompt, name two colours), so it uses a little of your Claude
# account. It then checks the debug log for each flow and prints pass,
# fail or skipped. The log, screenshots and a GIF of the run are kept in
# the folder it names; --keep also keeps the demo project.
#
# Needs: vhs, Claude Code signed in, Ollama running with llm.model and
# llm.expand_model.

set -euo pipefail
cd "$(dirname "$0")/.."
root="$PWD"
keep=0
[ "${1:-}" = "--keep" ] && keep=1

for tool in vhs claude go curl; do
	command -v "$tool" >/dev/null || { echo "smoke: $tool is not installed" >&2; exit 1; }
done
curl -fsS --max-time 2 http://localhost:11434/api/version >/dev/null ||
	{ echo "smoke: Ollama isn't running at localhost:11434" >&2; exit 1; }

out="$(mktemp -d "${TMPDIR:-/tmp}/pe-smoke.XXXXXX")"
demo="$HOME/pe-demo/smoke-app"
go build -o "$out/bin/prompt-engine" ./cmd/prompt-engine

if [ ! -d "$demo/.git" ]; then
	mkdir -p "$demo/src"
	printf '# weather-app\n\nA small CLI that prints the forecast.\n' >"$demo/README.md"
	printf 'package main\n\nfunc main() {}\n' >"$demo/src/main.go"
	git -C "$demo" init -q -b main
	git -C "$demo" add .
	git -C "$demo" -c user.name=dev -c user.email=dev@example.com commit -qm "Start weather-app"
fi

mkdir -p "$out/templates"
printf -- '- Keep the answer under 50 words.\n' >"$out/templates/append.txt"
cat >"$out/config.yaml" <<EOF
expand:
  on_enter: preview
  context: reply
templates:
  dir: $out/templates
EOF

# Waiting for claude's replies is hidden from the GIF (Hide/Show).
cat >"$out/smoke.tape" <<EOF
Output smoke.gif
Set Shell "bash"
Set FontSize 14
Set Width 1400
Set Height 700
Set TypingSpeed 50ms
Env PS1 "$ "
Env BASH_SILENCE_DEPRECATION_WARNING "1"
Hide
Type "export PATH=$out/bin:\$PATH && cd $demo && clear" Enter
Sleep 500ms
Show
Type "prompt-engine" Enter
Sleep 6s
Down
Sleep 300ms
Enter
Sleep 3s
Ctrl+Q
Type "p"
Sleep 1s
Screenshot panel.png
Type "list the files in this dire"
Sleep 2s
Screenshot ghost.png
Tab
Sleep 500ms
Enter
Hide
Sleep 18s
Show
Screenshot suggestion.png
Right
Enter
Hide
Sleep 18s
Show
Type "explain waht the main.go file dose"
Wait+Screen@20s /Suggested rewrite/
Screenshot preview.png
Ctrl+Q
Type "e"
Sleep 2s
Ctrl+U
Sleep 500ms
Type "name two colours, as a numbered list"
Enter
Hide
Sleep 15s
Show
Type "tell me a fun fact about the second one"
Sleep 500ms
Ctrl+Q
Type "e"
Sleep 6s
Screenshot context.png
Ctrl+U
Sleep 500ms
EOF

echo "smoke: running a live session in $demo (about 2 minutes)…"
(
	cd "$out"
	PE_TEST_HOME="$out/home" "$root/scripts/test-env.sh" \
		PROMPT_ENGINE_CONFIG="$out/config.yaml" \
		PROMPT_ENGINE_DEBUG_LOG="$out/pe.log" \
		vhs smoke.tape >"$out/vhs.txt" 2>&1
) || { echo "smoke: vhs failed; see $out/vhs.txt" >&2; exit 1; }

log="$out/pe.log"
failed=0
result() { printf '  %s %s\n' "$1" "$2"; [ "$1" != "✗" ] || failed=$((failed + 1)); }
has() { grep -qF -- "$1" "$log"; }

echo
echo "smoke: results (log: $log)"
if has "session start: config $out/config.yaml" && ! has 'already inside prompt-engine'; then
	result ✓ "started with the test config, not nested"
else
	result ✗ "started with the test config, not nested"
fi
if has 'leader: toggle panel'; then result ✓ "panel opened (Ctrl+Q, p)"; else result ✗ "panel opened (Ctrl+Q, p)"; fi
if has 'suggestion shown' && has 'suggestion accepted'; then
	result ✓ "ghost text shown and accepted with Tab"
else
	result ✗ "ghost text shown and accepted with Tab"
fi
if has 'rules: appended append.txt'; then result ✓ "rules appended to a prompt sent"; else result ✗ "rules appended to a prompt sent"; fi
# Read at Enter with text: claude offered a suggestion and → took it. With
# none offered, the box was empty and there's nothing to check.
accepted="$(grep -F 'resynced from screen at Enter' "$log" | head -n 1 || true)"
if [ -n "$accepted" ] && ! [[ "$accepted" =~ rev=[0-9]+\ \"\" ]]; then
	result ✓ "claude's suggestion taken with → and sent at once was read first"
else
	result – "claude's suggestion with → then Enter: skipped (claude offered no suggestion)"
fi
if has 'expand: preview ready for "explain waht the main.go file dose"' && has 'expand: used the preview'; then
	result ✓ "preview prepared on a pause and used with Ctrl+Q, e"
else
	result ✗ "preview prepared on a pause and used with Ctrl+Q, e"
fi
rewrite="$(grep -F 'expand: replaced "tell me a fun fact about the second one"' "$log" | tail -n 1 || true)"
if has 'context: reading the transcript' && [ -n "$rewrite" ] && [[ "${rewrite#*with}" != *"second one"* ]]; then
	result ✓ "reply context: \"the second one\" named (${rewrite#*with })"
else
	result ✗ "reply context: \"the second one\" named"
fi

echo
echo "smoke: screenshots and smoke.gif in $out"
[ "$keep" = 1 ] || { rm -rf "$demo"; rmdir "$HOME/pe-demo" 2>/dev/null || true; }
if [ "$failed" -gt 0 ]; then
	echo "smoke: $failed failed"
	exit 1
fi
echo "smoke: passed"
