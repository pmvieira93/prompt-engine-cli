#!/usr/bin/env bash
# Does a claude session started from one of the daemon's spares (Claude Code
# 2.1.311) get its hook from the new claude's --settings, and its environment
# from the new claude or from the daemon? It starts one real claude in a clean
# environment with a marker variable, and a hook whose command line holds a
# second marker; sends "say ok" (one short prompt), then /exit; and prints
# what the hook saw, the daemon's last log lines, and whether /exit ended it.
#
#   docs/spikes/daemon-env/run.sh
set -euo pipefail
cd "$(dirname "$0")/../../.."
work=$(mktemp -d)
id=$RANDOM
go build -o "$work/vth" ./tools/vtharness
out="$work/hook.txt"
# shellcheck disable=SC2016 # the ${…} are for the hook's shell, not this one
settings=$(python3 -c '
import json, sys
cmd = ("echo \"arg=" + sys.argv[1] + " env=${PE_EXP_ENV:-unset} pe_active=${PROMPT_ENGINE_ACTIVE:-unset} "
       "pe_queue=${PROMPT_ENGINE_RULES_QUEUE:-unset}\" >> " + sys.argv[2])
print(json.dumps({"hooks": {"UserPromptSubmit": [{"hooks": [{"type": "command", "command": cmd}]}]}}))
' "settings-$id" "$out")
script='reply on
wait "❯" 30s
send "say ok"
sleep 500ms
send "\r"
wait "● " 90s
sleep 3s
send "/exit"
sleep 300ms
send "\r"
sleep 5s
dump
exit'
# A watchdog: if /exit doesn't end claude, the harness would wait for ever.
( sleep 180; pkill -f "$work/vth" 2>/dev/null || true ) &
watchdog=$!
printf '%s\n' "$script" | scripts/test-env.sh "PE_EXP_ENV=client-$id" "$work/vth" -cols 100 -rows 30 -- claude --settings "$settings" >"$work/screen.txt" 2>&1 || true
kill "$watchdog" 2>/dev/null || true
echo "== the new claude: env PE_EXP_ENV=client-$id, hook arg=settings-$id"
echo "== what the hook saw:"
cat "$out" 2>/dev/null || echo "(the hook didn't run)"
echo "== the daemon's last lines:"
tail -n 6 ~/.claude/daemon.log | cut -c1-160
echo "== the harness's end (exit code, if claude exited):"
tail -n 4 "$work/screen.txt"
echo "(files in $work)"
