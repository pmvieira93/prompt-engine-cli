# Spike: a session "over another session" (Claude Code's daemon)

**Date:** 2026-11-25 · **Claude Code:** 2.1.311 · **Script:** [`daemon-env/run.sh`](daemon-env/run.sh)

## The report

Starting `pe` (prompt-engine) seemed to start a session over another one: `/exit` led back to a second session, and a second `/exit` was needed to quit.

## What was found

1. **Claude Code 2.1.311 runs a daemon.** The first `claude` that needs it starts `claude daemon run` (`origin: transient`), which keeps `claude bg-spare` processes warm and hosts background sessions (`claude daemon status`, `~/.claude/daemon.log`).
2. **The daemon keeps the environment of the `claude` that started it, and so does every session it hosts.** Here it was started on 2026-11-23 by a `claude` running under prompt-engine. Two days later the daemon and its spares still had that session's `PROMPT_ENGINE_ACTIVE`, `PROMPT_ENGINE_DEBUG_LOG` and `PROMPT_ENGINE_RULES_QUEUE`, a folder deleted when that session ended (`ps eww`).
3. **`/resume` attached to a background session.** The conversation had been resumed with `/resume` on 2026-11-23, and the daemon has hosted it since. Each later `pe` started a fresh `claude` (session 1, set up correctly); typing `/resume` and picking that conversation attached to the daemon's session (session 2): the daemon log has `bg claimed-spare … (slash)` at those moments (17:34 and 17:53 in the debug logs). `/exit` left session 2 running in the background and returned to session 1; a second `/exit` ended session 1.
4. **In session 2, prompt-engine's hook ran with the old environment:** it looked in the deleted rules folder (`rules: hook: no rules waiting for this prompt`) and logged to the 2026-11-23 file. Guidance from `append.txt` would not have reached Claude. (The developer has none, so nothing was lost.)

## The experiment

`run.sh` starts one real `claude` from a clean environment (`scripts/test-env.sh`) with a marker variable, `PE_EXP_ENV=client-<id>`, and a `--settings` hook whose command line holds a second marker; it sends `say ok`, then `/exit`, through `tools/vtharness`.

Result (one run):

```
arg=settings-3489 env=client-3489 pe_active=unset pe_queue=unset
EXIT <nil> (code 0)
```

* The hook ran with the new `claude`'s settings **and** its environment.
* The daemon claimed no spare for it (no new line in `daemon.log`).
* One `/exit` ended it.

So a session started normally is set up correctly, and prompt-engine passing the rules folder and log path through the environment is fine. The proposed fix, moving them into the hook command in `--settings`, was not needed, and would not have helped: a background session keeps the settings and environment it was started with.

## What to do

* To get out of it: from a plain terminal, once the sessions are closed, `claude daemon stop --any` ends the daemon and its background sessions (transcripts stay on disk).
* Resume at start, not with `/resume` inside a session: `pe --resume <session id>` or `pe --continue`. The conversation then runs in prompt-engine's own `claude`.
* If `/exit` keeps sending sessions to the background: `CLAUDE_CODE_DISABLE_BG_EXIT_HANDOFF=1` (under `env:` in prompt-engine's config) appears to turn that off. It is undocumented, found in the 2.1.311 binary, and not tested.

## Not done

* What Claude's "exit handoff" does exactly was not confirmed; only the logs and the processes were read.
* Proposed, not built: prompt-engine could notice that its prompts never reach its own hook, as in an attached background session, and say so ("this session doesn't run prompt-engine's hook: guidance isn't added"), instead of failing silently.
