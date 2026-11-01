#!/usr/bin/env bash
#
# prompt-engine installer
#
# Checks the system, installs what is missing (with your consent), builds
# prompt-engine, downloads the local models it uses, and runs a health check.
#
#   ./install.sh                 install, asking before each change
#   ./install.sh --expand-model  add the prompt-rewrite model later
#   ./install.sh --check         only run the health check
#   ./install.sh --help          every option
#
# Supports macOS and Linux. Works with bash 3.2 (the macOS default).

set -euo pipefail

# ─── Constants ────────────────────────────────────────────────────────────────

readonly REPO_URL="https://github.com/pmvieira93/prompt-engine-cli.git"
readonly GO_MIN="1.26"
readonly GHOST_MODEL_DEFAULT="qwen2.5-coder:1.5b"
readonly GHOST_MODEL_SIZE="1.0 GB"
readonly EXPAND_MODEL_DEFAULT="qwen2.5:7b"
readonly EXPAND_MODEL_SIZE="4.7 GB"
readonly RAM_MIN_GB=8       # ghost text plus Claude Code
readonly RAM_EXPAND_GB=16   # comfortable with the 7B rewrite model loaded too
readonly DISK_MIN_GB=3      # Go build cache, the binary and the ghost model

# AI coding CLIs we look for: "command|name". Only those in SUPPORTED_CLIS
# can be chosen; the others are listed as found, not supported yet, until
# they're tested (PRD Phase 5).
readonly SUPPORTED_CLIS="claude"
readonly AI_CLIS="claude|Claude Code
gemini|Gemini CLI
copilot|GitHub Copilot CLI
codex|OpenAI Codex CLI
cursor-agent|Cursor CLI
aider|Aider
opencode|OpenCode"

# ─── Options ──────────────────────────────────────────────────────────────────

ASSUME_YES=0
DRY_RUN=0
MODE=install           # install | check | expand-model
OFFER_EXPAND=1
PREFIX="${PREFIX:-$HOME/.local/bin}"
CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/prompt-engine"
CONFIG_FILE="${PROMPT_ENGINE_CONFIG:-$CONFIG_DIR/config.yaml}"
LOG_FILE="${TMPDIR:-/tmp}/prompt-engine-install.$(date +%Y%m%d-%H%M%S).log"

usage() {
	cat <<EOF
prompt-engine installer

Usage: ./install.sh [options]

Options:
  -y, --yes          Answer yes to every question (unattended install)
      --prefix DIR   Where to install the binary (default: ~/.local/bin)
      --check        Only run the health check; change nothing
      --expand-model Only download the prompt-rewrite model ($EXPAND_MODEL_DEFAULT)
      --skip-expand  Don't offer the prompt-rewrite model
      --dry-run      Show what would be done; change nothing
      --no-color     Plain output
  -h, --help         Show this help

Environment:
  PREFIX, XDG_CONFIG_HOME and PROMPT_ENGINE_CONFIG are honoured. NO_COLOR
  turns colours off.

The installer asks before installing anything. Command output goes to a
log file, shown at the end.
EOF
}

NO_COLOR="${NO_COLOR:-}"
while [ $# -gt 0 ]; do
	case "$1" in
	-y | --yes) ASSUME_YES=1 ;;
	--prefix)
		[ $# -ge 2 ] || { echo "--prefix needs a directory" >&2; exit 2; }
		PREFIX="$2"
		shift
		;;
	--prefix=*) PREFIX="${1#--prefix=}" ;;
	--check) MODE=check ;;
	--expand-model) MODE=expand-model ;;
	--skip-expand) OFFER_EXPAND=0 ;;
	--dry-run) DRY_RUN=1 ;;
	--no-color) NO_COLOR=1 ;;
	-h | --help) usage; exit 0 ;;
	*) echo "Unknown option: $1 (see --help)" >&2; exit 2 ;;
	esac
	shift
done

# ─── Output ───────────────────────────────────────────────────────────────────

if [ -t 1 ] && [ -z "$NO_COLOR" ]; then
	B=$'\033[1m' D=$'\033[2m' R=$'\033[31m' G=$'\033[32m' Y=$'\033[33m' C=$'\033[36m' N=$'\033[0m'
else
	B="" D="" R="" G="" Y="" C="" N=""
fi

STEP=0
STEPS=6
step()  { STEP=$((STEP + 1)); printf '\n%s[%d/%d] %s%s\n' "$B" "$STEP" "$STEPS" "$1" "$N"; }
ok()    { printf '  %s✓%s %s\n' "$G" "$N" "$*"; }
bad()   { printf '  %s✗%s %s\n' "$R" "$N" "$*"; }
warn()  { printf '  %s!%s %s\n' "$Y" "$N" "$*"; }
info()  { printf '  %s•%s %s\n' "$C" "$N" "$*"; }
note()  { printf '    %s%s%s\n' "$D" "$*" "$N"; }
die()   { printf '\n%s✗ %s%s\n' "$R" "$*" "$N" >&2; [ -s "$LOG_FILE" ] && printf '  Log: %s\n' "$LOG_FILE" >&2; exit 1; }
row()   { printf '  %-22s %-26s %s\n' "$1" "$2" "$3"; }

banner() {
	printf '%s' "$B"
	cat <<'EOF'
  ┌─────────────────────────────────────────────┐
  │  prompt-engine · installer                  │
  │  Ghost text and prompt rewrites, locally    │
  └─────────────────────────────────────────────┘
EOF
	printf '%s' "$N"
	[ "$DRY_RUN" = 1 ] && printf '  %sDry run: nothing will be changed.%s\n' "$Y" "$N"
	return 0
}

# ─── Prompts ──────────────────────────────────────────────────────────────────

# A terminal to ask on, even when the script itself is piped in.
TTY=""
if [ -r /dev/tty ] && [ -w /dev/tty ] && { : </dev/tty; } 2>/dev/null; then TTY=/dev/tty; fi

# ask "question" [Y|N]: returns 0 for yes; the default is what Enter picks.
# Without a terminal the answer is no, so nothing is installed unasked;
# --yes says yes to everything.
ask() {
	local q="$1" def="${2:-Y}" hint ans
	[ "$ASSUME_YES" = 1 ] && { printf '  %s?%s %s %s(yes: --yes)%s\n' "$C" "$N" "$q" "$D" "$N"; return 0; }
	if [ -z "$TTY" ]; then
		printf '  %s?%s %s %s(no terminal: no; --yes answers yes)%s\n' "$C" "$N" "$q" "$D" "$N"
		return 1
	fi
	if [ "$def" = Y ]; then hint="[Y/n]"; else hint="[y/N]"; fi
	printf '  %s?%s %s %s ' "$C" "$N" "$q" "$hint" >"$TTY"
	read -r ans <"$TTY" || ans=""
	case "$ans" in
	[Yy] | [Yy][Ee][Ss]) return 0 ;;
	[Nn] | [Nn][Oo]) return 1 ;;
	*) [ "$def" = Y ] ;;
	esac
}

# choose "question" option…: prints the 1-based number picked (default 1).
choose() {
	local q="$1" i=1 ans
	shift
	if [ "$ASSUME_YES" = 1 ] || [ -z "$TTY" ]; then
		printf '  %s?%s %s %s(%s: 1, %s)%s\n' "$C" "$N" "$q" "$D" "$([ "$ASSUME_YES" = 1 ] && echo --yes || echo 'no terminal')" "$1" "$N" >&2
		echo 1
		return
	fi
	{
		printf '  %s?%s %s\n' "$C" "$N" "$q"
		for opt in "$@"; do printf '      %d) %s\n' "$i" "$opt"; i=$((i + 1)); done
		printf '    Choice [1]: '
	} >"$TTY"
	read -r ans <"$TTY" || ans=""
	case "$ans" in
	'' ) echo 1 ;;
	*[!0-9]*) echo 1 ;;
	*) if [ "$ans" -ge 1 ] && [ "$ans" -le $# ]; then echo "$ans"; else echo 1; fi ;;
	esac
}

# ─── Running commands ─────────────────────────────────────────────────────────

# quietly "label" cmd…: runs cmd with its output in the log, and reports.
quietly() {
	local label="$1"
	shift
	if [ "$DRY_RUN" = 1 ]; then info "$label ${D}(would run: $*)${N}"; return 0; fi
	[ -t 1 ] && printf '  %s…%s %s' "$C" "$N" "$label"
	{ printf '\n$ %s\n' "$*"; } >>"$LOG_FILE"
	if "$@" >>"$LOG_FILE" 2>&1; then
		[ -t 1 ] && printf '\r'; ok "$label"
	else
		[ -t 1 ] && printf '\r'; bad "$label"
		note "Last lines of $LOG_FILE:"
		tail -n 15 "$LOG_FILE" | sed 's/^/      /'
		return 1
	fi
}

# loudly cmd…: runs cmd with its output on screen (downloads with progress).
loudly() {
	if [ "$DRY_RUN" = 1 ]; then info "would run: $*"; return 0; fi
	"$@"
}

have() { command -v "$1" >/dev/null 2>&1; }

# version_ge A B: whether version A >= B, comparing up to three parts.
version_ge() {
	local IFS=. i a b
	# shellcheck disable=SC2206
	a=($1) b=($2)
	for i in 0 1 2; do
		if [ "${a[i]:-0}" -gt "${b[i]:-0}" ]; then return 0; fi
		if [ "${a[i]:-0}" -lt "${b[i]:-0}" ]; then return 1; fi
	done
	return 0
}

add_path() { case ":$PATH:" in *":$1:"*) ;; *) PATH="$1:$PATH"; export PATH ;; esac; }

# ─── System facts ─────────────────────────────────────────────────────────────

OS="" ARCH="" OS_NAME="" RAM_GB=0 DISK_GB=0 PKG=""

detect_system() {
	case "$(uname -s)" in
	Darwin) OS=macos; OS_NAME="macOS $(sw_vers -productVersion 2>/dev/null || echo '?')" ;;
	Linux)
		OS=linux
		OS_NAME="Linux"
		# shellcheck disable=SC1091
		if [ -r /etc/os-release ]; then OS_NAME="$(. /etc/os-release && echo "${PRETTY_NAME:-Linux}")"; fi
		;;
	*) die "Unsupported system: $(uname -s). prompt-engine runs on macOS and Linux." ;;
	esac
	case "$(uname -m)" in
	arm64 | aarch64) ARCH=arm64 ;;
	x86_64 | amd64) ARCH=amd64 ;;
	*) ARCH="$(uname -m)" ;;
	esac
	if [ "$OS" = macos ]; then
		RAM_GB=$(($(/usr/sbin/sysctl -n hw.memsize 2>/dev/null || echo 0) / 1024 / 1024 / 1024))
	else
		RAM_GB=$(($(awk '/^MemTotal:/ {print $2}' /proc/meminfo) / 1024 / 1024))
	fi
	DISK_GB=$(($(df -Pk "$HOME" | awk 'NR==2 {print $4}') / 1024 / 1024))
	if have brew; then PKG=brew
	elif have apt-get; then PKG=apt
	elif have dnf; then PKG=dnf
	elif have pacman; then PKG=pacman
	elif have zypper; then PKG=zypper
	fi
	return 0
}

# A value from the user's config file ("" if unset): the first line "key:".
config_value() {
	[ -r "$CONFIG_FILE" ] || return 0
	awk -v k="$1" '
		$0 ~ "^[[:space:]]*" k ":" {
			sub("^[[:space:]]*" k ":[[:space:]]*", ""); sub("[[:space:]]+#.*$", "")
			gsub(/^["'\'']|["'\'']$/, ""); print; exit
		}' "$CONFIG_FILE"
}

GHOST_MODEL="" EXPAND_MODEL="" OLLAMA_URL=""

resolve_settings() {
	GHOST_MODEL="$(config_value model)"
	EXPAND_MODEL="$(config_value expand_model)"
	OLLAMA_URL="$(config_value endpoint)"
	GHOST_MODEL="${GHOST_MODEL:-$GHOST_MODEL_DEFAULT}"
	EXPAND_MODEL="${EXPAND_MODEL:-$EXPAND_MODEL_DEFAULT}"
	OLLAMA_URL="${OLLAMA_URL:-http://localhost:11434}"
	OLLAMA_URL="${OLLAMA_URL%/}"
}

# ─── Ollama ───────────────────────────────────────────────────────────────────

ollama_up() { curl -fsS --max-time 2 "$OLLAMA_URL/api/version" >/dev/null 2>&1; }

ollama_version() {
	curl -fsS --max-time 2 "$OLLAMA_URL/api/version" 2>/dev/null | sed -n 's/.*"version":"\([^"]*\)".*/\1/p'
}

# The installed models, one per line, with ":latest" for an untagged name.
ollama_models() {
	curl -fsS --max-time 5 "$OLLAMA_URL/api/tags" 2>/dev/null |
		grep -o '"name":"[^"]*"' | sed 's/"name":"//; s/"$//'
}

model_installed() {
	local m="$1"
	case "$m" in *:*) ;; *) m="$m:latest" ;; esac
	ollama_models | grep -qxF "$m"
}

start_ollama() {
	ollama_up && return 0
	if [ "$DRY_RUN" = 1 ]; then info "would start the Ollama server"; return 0; fi
	if [ "$OS" = macos ] && [ -d /Applications/Ollama.app ]; then
		open -a Ollama >>"$LOG_FILE" 2>&1 || true
	elif [ "$OS" = macos ] && have brew && brew list ollama >/dev/null 2>&1; then
		brew services start ollama >>"$LOG_FILE" 2>&1 || true
	elif have systemctl && systemctl list-unit-files ollama.service >/dev/null 2>&1; then
		# shellcheck disable=SC2024 # the log is ours, not root's
		sudo systemctl enable --now ollama >>"$LOG_FILE" 2>&1 || true
	else
		nohup ollama serve >>"$LOG_FILE" 2>&1 &
	fi
	local i
	for i in $(seq 1 30); do ollama_up && return 0; sleep 1; done
	return 1
}

# ─── Step 1: system ───────────────────────────────────────────────────────────

check_system() {
	step "System check"
	detect_system
	ok "$OS_NAME ($ARCH)"
	if [ "$RAM_GB" -ge "$RAM_EXPAND_GB" ]; then
		ok "Memory: ${RAM_GB} GB"
	elif [ "$RAM_GB" -ge "$RAM_MIN_GB" ]; then
		warn "Memory: ${RAM_GB} GB: enough for ghost text; the rewrite model wants ${RAM_EXPAND_GB} GB"
	else
		warn "Memory: ${RAM_GB} GB: below the ${RAM_MIN_GB} GB minimum; suggestions may be slow"
	fi
	if [ "$DISK_GB" -ge "$DISK_MIN_GB" ]; then
		ok "Free disk: ${DISK_GB} GB"
	else
		die "Free disk: ${DISK_GB} GB, below the ${DISK_MIN_GB} GB needed."
	fi
	if [ -n "$PKG" ]; then ok "Package manager: $PKG"; else warn "No package manager found (Homebrew, apt, dnf, pacman or zypper)"; fi
	have curl || die "curl is required to run this installer. Install it and run again."
	ok "curl"
	if [ -n "${TERM_PROGRAM:-}" ]; then ok "Terminal: $TERM_PROGRAM"; fi
}

# ─── Step 2: AI coding CLI ────────────────────────────────────────────────────

AI_DEFAULT=claude

check_ai_cli() {
	step "AI coding CLI"
	local supported=() labels=() others=0 cmd name v
	while IFS='|' read -r cmd name; do
		have "$cmd" || continue
		case " $SUPPORTED_CLIS " in
		*" $cmd "*)
			v="$("$cmd" --version 2>/dev/null | head -n 1 || true)"
			supported+=("$cmd")
			labels+=("$name ($cmd${v:+, $v})")
			ok "$name ${D}$(command -v "$cmd")${N}"
			;;
		*)
			info "$name ($cmd): found, not supported yet"
			others=$((others + 1))
			;;
		esac
	done <<EOF
$AI_CLIS
EOF
	if have gh && gh extension list 2>/dev/null | grep -q 'gh-copilot'; then
		info "GitHub Copilot in the GitHub CLI (gh copilot): found, not supported yet"
		others=$((others + 1))
	fi
	if [ "$others" -gt 0 ]; then
		note "prompt-engine wraps Claude Code today; support for other CLIs is planned."
	fi

	if [ ${#supported[@]} -eq 0 ]; then
		warn "Claude Code isn't installed. prompt-engine wraps it; it is offered below."
	elif [ ${#supported[@]} -gt 1 ]; then
		local pick
		pick="$(choose "Several supported AI CLIs are installed. Which should prompt-engine wrap by default?" "${labels[@]}")"
		AI_DEFAULT="${supported[$((pick - 1))]}"
	else
		AI_DEFAULT="${supported[0]}"
	fi
	ok "Default: $(cli_name "$AI_DEFAULT") ($AI_DEFAULT)"
}

# cli_name returns the name AI_CLIS gives a command.
cli_name() { sed -n "s/^$1|//p" <<<"$AI_CLIS"; }

# ─── Step 3: dependencies ─────────────────────────────────────────────────────

MISSING=""   # one "id|what|how" line per missing dependency

need() { MISSING="${MISSING}$1|$2|$3
"; }

go_version() { go version 2>/dev/null | sed -n 's/.*go\([0-9][0-9.]*\).*/\1/p'; }

in_repo() { [ -f "$SRC_DIR/go.mod" ] && grep -q 'prompt-engine-cli' "$SRC_DIR/go.mod"; }

check_dependencies() {
	step "Dependencies"
	local v
	# Homebrew is how Go and Ollama are installed on macOS.
	if [ "$OS" = macos ] && ! have brew && { ! have go || ! have ollama; }; then
		need brew "Homebrew" "official installer from brew.sh"
	fi
	if ! in_repo; then
		if have git; then ok "git $(git --version | awk '{print $3}')"; else need git "git (to download the source)" "$(pkg_how git)"; fi
	fi
	if have go; then
		v="$(go_version)"
		if version_ge "$v" "$GO_MIN"; then ok "Go $v"; else need go "Go $GO_MIN+ (found $v)" "$(go_how)"; fi
	else
		need go "Go $GO_MIN+" "$(go_how)"
	fi
	if have ollama; then
		ok "Ollama $(ollama --version 2>/dev/null | awk '{print $NF}' | head -n 1)"
	else
		need ollama "Ollama (runs the local models)" "$(ollama_how)"
	fi
	if have claude; then
		ok "Claude Code $(claude --version 2>/dev/null | awk '{print $1}')"
	else
		need claude "Claude Code" "official installer from claude.ai"
	fi

	[ -z "$MISSING" ] && { ok "All dependencies are installed"; return 0; }

	printf '\n  %sMissing:%s\n' "$B" "$N"
	local id what how
	while IFS='|' read -r id what how; do
		[ -n "$id" ] && printf '    %-32s %s%s%s\n' "$what" "$D" "$how" "$N"
	done <<EOF
$MISSING
EOF
	echo
	if ! ask "Install all of them now?" Y; then
		printf '\n  Install them yourself, then run this script again:\n'
		while IFS='|' read -r id what how; do [ -n "$id" ] && note "$(manual_hint "$id")"; done <<EOF
$MISSING
EOF
		exit 1
	fi
	while IFS='|' read -r id what how; do
		[ -n "$id" ] && install_dep "$id"
	done <<EOF
$MISSING
EOF
}

pkg_how() {
	case "$PKG" in
	brew) echo "brew install $1" ;;
	apt) echo "sudo apt-get install $1" ;;
	dnf) echo "sudo dnf install $1" ;;
	pacman) echo "sudo pacman -S $1" ;;
	zypper) echo "sudo zypper install $1" ;;
	*) echo "your package manager" ;;
	esac
}
go_how() { if [ "$OS" = macos ]; then echo "brew install go"; else echo "official tarball into ~/.local/go"; fi; }
ollama_how() { if [ "$OS" = macos ]; then echo "brew install ollama"; else echo "official installer from ollama.com"; fi; }

manual_hint() {
	# shellcheck disable=SC2016 # printed for the user to run
	case "$1" in
	brew) echo 'Homebrew: /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"' ;;
	git) echo "git: $(pkg_how git)" ;;
	go) echo "Go: https://go.dev/dl/ (version $GO_MIN or newer)" ;;
	ollama) echo "Ollama: https://ollama.com/download" ;;
	claude) echo "Claude Code: curl -fsSL https://claude.ai/install.sh | bash" ;;
	esac
}

install_dep() {
	case "$1" in
	brew)
		loudly /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)" ||
			die "Homebrew didn't install."
		local b
		for b in /opt/homebrew/bin/brew /usr/local/bin/brew; do [ -x "$b" ] && eval "$("$b" shellenv)"; done
		PKG=brew
		;;
	git)
		case "$PKG" in
		brew) quietly "Installing git" brew install git ;;
		apt) quietly "Installing git" sudo apt-get install -y git ;;
		dnf) quietly "Installing git" sudo dnf install -y git ;;
		pacman) quietly "Installing git" sudo pacman -S --noconfirm git ;;
		zypper) quietly "Installing git" sudo zypper install -y git ;;
		*) die "Install git yourself, then run this script again." ;;
		esac || die "git didn't install."
		;;
	go)
		if [ "$OS" = macos ]; then
			quietly "Installing Go" brew install go || die "Go didn't install."
		else
			install_go_tarball
		fi
		;;
	ollama)
		if [ "$OS" = macos ]; then
			quietly "Installing Ollama" brew install ollama || die "Ollama didn't install."
		else
			loudly sh -c "curl -fsSL https://ollama.com/install.sh | sh" || die "Ollama didn't install."
		fi
		;;
	claude)
		loudly bash -c "curl -fsSL https://claude.ai/install.sh | bash" || die "Claude Code didn't install."
		add_path "$HOME/.local/bin"
		note "Run 'claude' once to sign in before using prompt-engine."
		;;
	esac
}

install_go_tarball() {
	local ver url dest="$HOME/.local/go"
	ver="$(curl -fsSL 'https://go.dev/VERSION?m=text' | head -n 1)"
	[ -n "$ver" ] || die "Couldn't find the latest Go version."
	url="https://go.dev/dl/${ver}.linux-${ARCH}.tar.gz"
	if [ "$DRY_RUN" = 1 ]; then info "would install $ver from $url into $dest"; return 0; fi
	quietly "Downloading $ver" sh -c "rm -rf '$dest' && mkdir -p '$HOME/.local' && curl -fsSL '$url' | tar -xz -C '$HOME/.local'" ||
		die "Go didn't install."
	add_path "$dest/bin"
	PATH_HINTS="${PATH_HINTS}$dest/bin
"
}

# ─── Step 4: build and install ────────────────────────────────────────────────

SRC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd)" || SRC_DIR="$PWD"
PATH_HINTS=""

install_binary() {
	step "Build and install prompt-engine"
	if ! in_repo; then
		SRC_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/prompt-engine/src"
		if [ -d "$SRC_DIR/.git" ]; then
			quietly "Updating the source in $SRC_DIR" git -C "$SRC_DIR" pull --ff-only || die "Couldn't update the source."
		else
			quietly "Downloading the source to $SRC_DIR" git clone --depth 1 "$REPO_URL" "$SRC_DIR" || die "Couldn't download the source."
		fi
	fi
	info "Source: $SRC_DIR"
	if [ "$DRY_RUN" = 0 ]; then mkdir -p "$PREFIX"; fi
	quietly "Building into $PREFIX/prompt-engine" \
		sh -c "cd '$SRC_DIR' && go build -trimpath -o '$PREFIX/prompt-engine' ./cmd/prompt-engine" ||
		die "The build failed."

	case ":$PATH:" in
	*":$PREFIX:"*) ok "$PREFIX is on your PATH" ;;
	*) warn "$PREFIX is not on your PATH"; PATH_HINTS="${PATH_HINTS}$PREFIX
" ;;
	esac

	if [ -f "$CONFIG_FILE" ]; then
		ok "Config kept: $CONFIG_FILE"
	elif [ "$DRY_RUN" = 1 ]; then
		info "would write the config template to $CONFIG_FILE"
	else
		mkdir -p "$(dirname "$CONFIG_FILE")"
		cp "$SRC_DIR/docs/config.example.yaml" "$CONFIG_FILE"
		ok "Config written: $CONFIG_FILE ${D}(every setting, at its default)${N}"
	fi
}

# ─── Step 5: models ───────────────────────────────────────────────────────────

ensure_ollama_running() {
	if ollama_up; then ok "Ollama server: $OLLAMA_URL (version $(ollama_version))"; return 0; fi
	quietly "Starting the Ollama server" start_ollama || die "The Ollama server didn't start. Try: ollama serve"
}

pull_model() {
	info "Downloading $1 ($2)…"
	loudly ollama pull "$1" || die "Couldn't download $1."
	ok "$1 installed"
}

install_models() {
	step "Local models"
	ensure_ollama_running
	local ghost_state expand_state
	if model_installed "$GHOST_MODEL"; then ghost_state="${G}installed${N}"; else ghost_state="${Y}missing${N}"; fi
	if model_installed "$EXPAND_MODEL"; then expand_state="${G}installed${N}"; else expand_state="${Y}missing${N}"; fi
	printf '\n'
	row "${B}Capability${N}" "${B}Model${N}" "${B}Status${N}"
	row "Ghost text" "$GHOST_MODEL" "$ghost_state  ${D}required · $GHOST_MODEL_SIZE${N}"
	row "Prompt rewrites" "$EXPAND_MODEL" "$expand_state  ${D}optional · $EXPAND_MODEL_SIZE${N}"
	printf '\n'

	if model_installed "$GHOST_MODEL"; then
		ok "Ghost text model ready"
	elif ask "Download the ghost text model $GHOST_MODEL ($GHOST_MODEL_SIZE)? It is required." Y; then
		pull_model "$GHOST_MODEL" "$GHOST_MODEL_SIZE"
	else
		warn "Skipped. Ghost text stays off until you run: ollama pull $GHOST_MODEL"
	fi

	[ "$OFFER_EXPAND" = 1 ] || { note "Rewrite model skipped (--skip-expand)."; return 0; }
	if model_installed "$EXPAND_MODEL"; then
		ok "Rewrite model ready"
		return 0
	fi
	[ "$RAM_GB" -lt "$RAM_EXPAND_GB" ] &&
		warn "With ${RAM_GB} GB of memory the rewrite model may be slow; ${RAM_EXPAND_GB} GB is comfortable."
	if ask "Download the rewrite model $EXPAND_MODEL ($EXPAND_MODEL_SIZE) now? You can do it later." N; then
		pull_model "$EXPAND_MODEL" "$EXPAND_MODEL_SIZE"
	else
		note "Later: ./install.sh --expand-model"
	fi
}

# ─── Step 6: health check ─────────────────────────────────────────────────────

PROBLEMS=0
WARNINGS=0
fail_check() { bad "$1"; [ -n "${2:-}" ] && note "Fix: $2"; PROBLEMS=$((PROBLEMS + 1)); }
warn_check() { warn "$1"; [ -n "${2:-}" ] && note "$2"; WARNINGS=$((WARNINGS + 1)); }

# The time, in ms, an Ollama request takes, or "" if it failed; the reply
# body goes to $REPLY_FILE.
REPLY_FILE="${TMPDIR:-/tmp}/prompt-engine-check.$$"
timed_post() {
	local start end
	start=$(perl -MTime::HiRes=time -e 'printf "%d", time*1000' 2>/dev/null || date +%s000)
	curl -sS --max-time "$2" -o "$REPLY_FILE" "$OLLAMA_URL$1" -d "$3" 2>>"$LOG_FILE" || return 1
	end=$(perl -MTime::HiRes=time -e 'printf "%d", time*1000' 2>/dev/null || date +%s000)
	grep -q '"error"' "$REPLY_FILE" && return 1
	echo $((end - start))
}
reply_error() { sed -n 's/.*"error":"\([^"]*\)".*/\1/p' "$REPLY_FILE" 2>/dev/null; }

health_check() {
	step "Health check"
	local bin="$PREFIX/prompt-engine" out ms

	if [ -x "$bin" ]; then ok "prompt-engine installed: $bin"
	elif have prompt-engine; then bin="$(command -v prompt-engine)"; ok "prompt-engine installed: $bin"
	else fail_check "prompt-engine is not installed" "./install.sh"; fi

	if have claude; then ok "Claude Code $(claude --version 2>/dev/null | awk '{print $1}')"
	else fail_check "Claude Code is not installed" "curl -fsSL https://claude.ai/install.sh | bash"; fi

	# Without a terminal, prompt-engine loads its config, then hands over to
	# claude: config problems show up as "prompt-engine: …" warnings.
	if [ -x "$bin" ] && have claude; then
		out="$("$bin" --version </dev/null 2>&1 | cat)"
		if printf '%s' "$out" | grep -q '^prompt-engine:'; then
			fail_check "Config: $(printf '%s' "$out" | tr '\n' ' ' | sed -e 's/.*prompt-engine: //' -e 's/; using defaults.*//' -e 's/  */ /g')" \
				"edit $CONFIG_FILE (template: docs/config.example.yaml)"
		elif [ -f "$CONFIG_FILE" ]; then
			ok "Config valid: $CONFIG_FILE"
		else
			ok "Config: built-in defaults (no file at $CONFIG_FILE)"
		fi
		if printf '%s' "$out" | grep -qi 'claude'; then ok "prompt-engine starts Claude Code"
		else fail_check "prompt-engine didn't start Claude Code: $out"; fi
	fi

	if ollama_up; then ok "Ollama server: $OLLAMA_URL (version $(ollama_version))"
	else fail_check "Ollama server isn't reachable at $OLLAMA_URL" "ollama serve (or brew services start ollama)"; fi

	if ollama_up; then
		if ! model_installed "$GHOST_MODEL"; then
			fail_check "Ghost text model $GHOST_MODEL is missing" "ollama pull $GHOST_MODEL"
		elif ms="$(timed_post /api/generate 60 "{\"model\":\"$GHOST_MODEL\",\"prompt\":\"refa\",\"suffix\":\"\\n\",\"stream\":false,\"keep_alive\":\"30m\",\"options\":{\"num_predict\":4}}")"; then
			ok "Ghost text model $GHOST_MODEL answers fill-in-the-middle requests (${ms} ms)"
		else
			fail_check "Ghost text model $GHOST_MODEL failed: $(reply_error)" \
				"use a model that supports fill-in-the-middle (llm.model), e.g. $GHOST_MODEL_DEFAULT"
		fi
		if ! model_installed "$EXPAND_MODEL"; then
			warn_check "Rewrite model $EXPAND_MODEL not installed (optional)" "Add it later: ./install.sh --expand-model"
		elif ms="$(timed_post /api/chat 120 "{\"model\":\"$EXPAND_MODEL\",\"messages\":[{\"role\":\"user\",\"content\":\"Say OK.\"}],\"stream\":false,\"options\":{\"num_predict\":4}}")"; then
			ok "Rewrite model $EXPAND_MODEL answers (${ms} ms, including loading)"
		else
			warn_check "Rewrite model $EXPAND_MODEL failed: $(reply_error)" "ollama pull $EXPAND_MODEL"
		fi
	fi
	rm -f "$REPLY_FILE"
}

# ─── Summary ──────────────────────────────────────────────────────────────────

summary() {
	printf '\n'
	if [ -n "$PATH_HINTS" ]; then
		printf '  %sAdd to your shell profile (~/.zshrc or ~/.bashrc):%s\n' "$B" "$N"
		# shellcheck disable=SC2016 # $PATH is for the profile
		printf '%s' "$PATH_HINTS" | while read -r d; do [ -n "$d" ] && printf '    export PATH="%s:$PATH"\n' "$d"; done
		printf '\n'
	fi
	if [ "$DRY_RUN" = 1 ]; then
		printf '  %sDry run finished: nothing was changed.%s\n' "$B" "$N"
	elif [ "$PROBLEMS" -gt 0 ]; then
		printf '  %s✗ %d problem(s) to fix%s (see "Fix:" above).\n' "$R" "$PROBLEMS" "$N"
	else
		printf '  %s✓ prompt-engine is ready.%s' "$G" "$N"
		[ "$WARNINGS" -gt 0 ] && printf ' %s(%d optional item(s) above)%s' "$D" "$WARNINGS" "$N"
		printf '\n\n'
		cat <<EOF
  Start it in a project, instead of 'claude':
    prompt-engine

  Then: type and pause for ghost text (Tab or → accepts it, Esc dismisses it).
  Ctrl+Q, then e rewrites the prompt · Ctrl+Q, then p opens the side panel.
  Guide: docs/GETTING_STARTED.md · Settings: $CONFIG_FILE
EOF
	fi
	[ -s "$LOG_FILE" ] && printf '\n  %sLog: %s%s\n' "$D" "$LOG_FILE" "$N"
	[ "$DRY_RUN" = 1 ] || [ "$PROBLEMS" -eq 0 ]
}

# ─── Main ─────────────────────────────────────────────────────────────────────

main() {
	banner
	case "$MODE" in
	check)
		STEPS=1
		detect_system
		resolve_settings
		health_check
		;;
	expand-model)
		STEPS=2
		detect_system
		resolve_settings
		have ollama || die "Ollama isn't installed. Run ./install.sh first."
		step "Rewrite model"
		ensure_ollama_running
		if model_installed "$EXPAND_MODEL"; then
			ok "$EXPAND_MODEL is already installed"
		else
			[ "$RAM_GB" -lt "$RAM_EXPAND_GB" ] &&
				warn "With ${RAM_GB} GB of memory the rewrite model may be slow; ${RAM_EXPAND_GB} GB is comfortable."
			if ask "Download $EXPAND_MODEL ($EXPAND_MODEL_SIZE)?" Y; then pull_model "$EXPAND_MODEL" "$EXPAND_MODEL_SIZE"; fi
		fi
		health_check
		;;
	install)
		check_system
		check_ai_cli
		check_dependencies
		install_binary
		resolve_settings
		install_models
		health_check
		;;
	esac
	summary
}

main
