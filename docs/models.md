# Local models

[README](../README.md) · [Install](install.md) · [Configuration reference](reference.md)

`prompt-engine` uses two models, each chosen for its job:

| Capability | Default | Size | Required | Why this model |
|---|---|---|---|---|
| Ghost text (`llm.model`) | `qwen2.5-coder:1.5b` | ~1 GB | Yes | It supports **fill-in-the-middle**, which finishing a half-typed word needs. It answers in 50–300 ms, fast enough to feel instant. |
| Prompt rewrites (`llm.expand_model`) | `qwen2.5:7b` | ~4.7 GB | No | In testing, 7B was the smallest size that rewrote **faithfully**. The 1.5B and 3B models invented requirements and misread shorthand. It takes about 1–3 s, which is fine because it only runs on a pause, on request, or on Enter. |

**Other options.** Both settings take any Ollama model name, so you can trade speed for quality.

| Capability | Option | Trade-off |
|---|---|---|
| Ghost text | `qwen2.5-coder:0.5b` | Faster, on smaller machines, with weaker suggestions |
| Ghost text | `qwen2.5-coder:3b` | Better suggestions, at a higher latency |
| Ghost text | Other fill-in-the-middle coders (`deepseek-coder`, `starcoder2`, `codegemma`) | Not tested with `prompt-engine`. The model **must** support fill-in-the-middle; `./install.sh --check` verifies it. |
| Rewrites | `qwen2.5:14b` | Closer rewrites if you have the memory (~9 GB). Not yet tested. |
| Rewrites | Other 7–9B instruction models (`llama3.1:8b`, `gemma2:9b`) | Not tested. The guard rails catch rewrites that drift. |

**Ollama on another machine.** Point `llm.endpoint` at it, for example `http://gpu-box:11434`.

**No models at all.** With `llm.enabled: false`, ghost text is off, and `prompt-engine` still adds your rules and shows the panel.
