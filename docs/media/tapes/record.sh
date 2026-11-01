#!/usr/bin/env bash
#
# Records the README's GIFs (docs/media/*.gif) with VHS
# (https://github.com/charmbracelet/vhs): a real prompt-engine and Claude
# Code session in a small demo project, ~/pe-demo/weather-app. The prompts
# sent are harmless, but they use a little of your Claude account.
#
#   docs/media/tapes/record.sh [ghost|rewrite|context|preview]...
#
# Needs vhs, Ollama with the two models, and Claude Code signed in. The
# first time, run claude once in ~/pe-demo/weather-app and trust the folder.
# Claude's answers differ between takes: check each GIF before committing
# it (one take named the full home path of the demo folder).

set -euo pipefail
cd "$(dirname "$0")/../../.."
tapes=docs/media/tapes
demo="$HOME/pe-demo"

go build -o bin/prompt-engine ./cmd/prompt-engine

if [ ! -d "$demo/weather-app/.git" ]; then
	mkdir -p "$demo/weather-app/src"
	printf '# weather-app\n\nA small CLI that prints the forecast.\n' >"$demo/weather-app/README.md"
	printf 'package main\n\nfunc main() {}\n' >"$demo/weather-app/src/main.go"
	git -C "$demo/weather-app" init -q -b main
	git -C "$demo/weather-app" add .
	git -C "$demo/weather-app" -c user.name=dev -c user.email=dev@example.com commit -qm "Start weather-app"
fi
cp "$tapes"/*.yaml "$demo/"

# A clean environment (scripts/test-env.sh): Claude Code's variables would
# stop claude from saving the transcript the context demo reads, and
# prompt-engine's would make the one recorded hand over to claude.
names=("$@")
[ ${#names[@]} -gt 0 ] || names=(ghost rewrite context preview)
for name in "${names[@]}"; do
	echo "recording $name"
	scripts/test-env.sh vhs "$tapes/$name.tape"
done
