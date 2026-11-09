# Install

[README](../README.md) · [Getting Started](GETTING_STARTED.md) · [Local models](models.md)

## Requirements

| | Minimum | Recommended |
|---|---|---|
| **System** | macOS or Linux (Windows isn't supported) | macOS on Apple Silicon |
| **Memory** | 8 GB, for ghost text | 16 GB, to keep the rewrite model loaded too |
| **Disk** | 2 GB, for the binary and the ghost text model (3 GB to build from source) | 8 GB, with the rewrite model |
| **Claude Code** | Installed and signed in | Latest version |
| **Ollama** | Running locally, or reachable over the network | Latest version |
| **Go** | 1.26, only to build from source | Latest version |
| **Terminal** | 256 colours and bracketed paste | iTerm2 (tested), Terminal.app, or any modern terminal |

## One-line install (recommended)

```sh
curl -fsSL https://raw.githubusercontent.com/pmvieira93/prompt-engine-cli/master/install.sh | bash
```

This installs the latest release. Go and a clone aren't needed. To pin the script and the release to one version, use the script from that tag:

```sh
curl -fsSL https://raw.githubusercontent.com/pmvieira93/prompt-engine-cli/v0.1.4/install.sh | bash -s -- --version v0.1.4
```

The script asks before it changes anything, on your terminal even when it's piped in. Read it first if you like: it's [`install.sh`](../install.sh). In order, it:

1. **Checks your system:** OS, memory, disk and package manager.
2. **Finds your AI coding CLIs.** Claude Code is the one supported today. Others it finds (Gemini, GitHub Copilot, Codex…) are listed as "found, not supported yet" and can't be chosen until they're tested.
3. **Lists missing dependencies** (Homebrew on macOS, Ollama, Claude Code; Go and git only to build from source) and offers to install them all.
4. **Installs `prompt-engine`** into `~/.local/bin`:
   - from a release: downloads the archive for your system, checks it against the release's `checksums.txt` (SHA-256), and stops if it doesn't match;
   - from source: builds it with Go.

   If you have no config file, it writes a commented one. If you have no `guards.txt`, it writes one that is all comments, for [your own guard rails](guide/guard-rails.md), in the folder `templates.dir` names.
5. **Downloads the models.** The ghost text model is required. The rewrite model is optional: download it now, or later with `--expand-model`.
6. **Runs a health check:**
   - the binary starts Claude Code;
   - your config is valid;
   - Ollama answers;
   - each model returns a real completion.

**Upgrading:** run the same line again. It shows the installed and the new version, replaces the binary and keeps your config. To upgrade only the binary, add `--binary-only`:

```sh
curl -fsSL https://raw.githubusercontent.com/pmvieira93/prompt-engine-cli/master/install.sh | bash -s -- --binary-only
```

Options go after `bash -s --` when the script is piped in, or after `./install.sh` in a clone:

| Option | Effect |
|---|---|
| `--from release` | Install a ready-built binary (the default outside a clone) |
| `--from source` | Build with Go (the default in a clone) |
| `--version TAG` | Install that release, e.g. `v0.1.4` (default: the latest); implies `--from release` |
| `--binary-only` | Only install or upgrade the binary: no dependencies, models or health check |
| `--check` | Only run the health check |
| `--expand-model` | Only download the rewrite model |
| `--yes` | Answer yes to every question (unattended) |
| `--prefix DIR` | Install somewhere other than `~/.local/bin` |
| `--skip-expand` | Don't offer the rewrite model |
| `--dry-run` | Show what would be done, change nothing |

The binary isn't signed yet. A binary downloaded with `curl` isn't quarantined by macOS, and the script clears the flag anyway, so it runs. Company-managed Macs may still block it: that's for your IT to allow.

## From a clone

```sh
git clone https://github.com/pmvieira93/prompt-engine-cli.git
cd prompt-engine-cli
./install.sh
```

The same script. In a clone it builds from that source (it needs Go); `./install.sh --from release` installs the latest release instead.

## From a release, by hand

Each [release](https://github.com/pmvieira93/prompt-engine-cli/releases) has a ready-built binary for macOS and Linux (Apple Silicon or Intel, arm64 or amd64), with `checksums.txt` to verify it:

```sh
# Pick your version and platform: darwin_arm64, darwin_amd64, linux_amd64 or linux_arm64.
curl -fsSLO https://github.com/pmvieira93/prompt-engine-cli/releases/download/v1.0.0/prompt-engine_v1.0.0_darwin_arm64.tar.gz
tar -xzf prompt-engine_v1.0.0_darwin_arm64.tar.gz
mv prompt-engine_v1.0.0_darwin_arm64/prompt-engine ~/.local/bin/
prompt-engine --pe-version
```

You still need Claude Code, Ollama and the models (steps 1 and 2 of [Building by hand](#building-by-hand)). The binary isn't signed: if macOS blocks it (a browser download is quarantined), run `xattr -d com.apple.quarantine ~/.local/bin/prompt-engine`.

## Building by hand

```sh
# 1. Dependencies (macOS; on Linux use your package manager and ollama.com/download)
brew install go ollama
brew services start ollama
curl -fsSL https://claude.ai/install.sh | bash   # Claude Code; run `claude` once to sign in

# 2. Models
ollama pull qwen2.5-coder:1.5b   # ghost text (required, ~1 GB)
ollama pull qwen2.5:7b           # prompt rewrites (optional, ~4.7 GB)

# 3. Build
go build -o ~/.local/bin/prompt-engine ./cmd/prompt-engine

# 4. Optional: start from the full config template
mkdir -p ~/.config/prompt-engine
cp docs/config.example.yaml ~/.config/prompt-engine/config.yaml
```

Then run `prompt-engine` where you would run `claude`. `prompt-engine` won't wrap itself twice, so an alias is safe ([recipe](GETTING_STARTED.md#4-common-recipes)).

## Uninstall

```sh
rm ~/.local/bin/prompt-engine
rm -r ~/.config/prompt-engine ~/.local/state/prompt-engine   # settings and history
ollama rm qwen2.5-coder:1.5b qwen2.5:7b                      # the models, if nothing else uses them
```
