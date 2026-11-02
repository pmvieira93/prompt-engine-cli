#!/usr/bin/env bash
#
# Tests install.sh's release install (--from release --binary-only)
# against releases built here and served from a folder, so no network is
# needed: a pinned version, a rerun of the same version, an upgrade that
# keeps the config, a bad checksum that installs nothing, a missing config
# file, an old binary without --pe-version, the script piped in outside a
# clone, and a missing version. Everything
# runs through scripts/test-env.sh, in a throwaway folder.
#
#   scripts/install-test.sh            the local cases
#   scripts/install-test.sh --latest   also install the latest release
#                                      from GitHub (needs the network)
#
# On a failure, the folder is kept and named.

set -euo pipefail
cd "$(dirname "$0")/.."
root="$PWD"

latest=0
[ "${1:-}" = --latest ] && latest=1

work="$(mktemp -d "${TMPDIR:-/tmp}/pe-install-test.XXXXXX")"
case "$(uname -s)" in Darwin) os=darwin ;; *) os=linux ;; esac
case "$(uname -m)" in arm64 | aarch64) arch=arm64 ;; *) arch=amd64 ;; esac

failed=0
passed=0
pass() { passed=$((passed + 1)); echo "  ok   $1"; }
fail() { failed=$((failed + 1)); echo "  FAIL $1"; }
# check label command...: passes if the command succeeds.
check() {
	local label="$1"
	shift
	if "$@"; then pass "$label"; else fail "$label"; fi
}
fails() { ! "$@"; }
says() { grep -q "$2" "$work/$1.out"; }

# release tag: builds the archive for this platform, laid out like
# GitHub's <releases>/download/<tag>/.
release() {
	TARGETS="$os/$arch" scripts/build-release.sh "$1" "$work/releases/download/$1" >/dev/null
}

# run_install name [options]: runs install.sh with the releases folder, its
# output in $work/name.out.
run_install() {
	local name="$1"
	shift
	PE_TEST_HOME="$work/home" scripts/test-env.sh \
		PROMPT_ENGINE_RELEASES_URL="file://$work/releases" NO_COLOR=1 \
		bash "$root/install.sh" --from release --binary-only --prefix "$work/bin" "$@" \
		</dev/null >"$work/$name.out" 2>&1
}

bin="$work/bin/prompt-engine"
config="$work/home/config.yaml"
version() { "$bin" --pe-version 2>/dev/null; }
is_version() { [ "$(version)" = "prompt-engine $1" ]; }
config_kept() { tail -n 1 "$config" | grep -qx "# mine"; }
release_version() { version | grep -qE "^prompt-engine v[0-9]+\.[0-9]+\.[0-9]+"; }

echo "Building test releases in $work"
release v9.9.1
release v9.9.2

echo "pinned version"
check "installs" run_install pinned --version v9.9.1
check "the binary reports v9.9.1" is_version v9.9.1
check "writes the config template" cmp -s "$config" docs/config.example.yaml
check "says the checksum was verified" says pinned "Checksum verified"

echo "same version again"
echo "# mine" >>"$config"
check "succeeds" run_install again --version 9.9.1
check "says it is already installed" says again "v9.9.1 is already installed"

echo "upgrade"
check "succeeds" run_install upgrade --version v9.9.2
check "the binary reports v9.9.2" is_version v9.9.2
check "shows both versions" says upgrade "Installed v9.9.2 (was v9.9.1)"
check "keeps the config" config_kept

echo "bad checksum"
release v9.9.3
sums="$work/releases/download/v9.9.3/checksums.txt"
sed -E 's/^[0-9a-f]+/0000000000000000000000000000000000000000000000000000000000000000/' "$sums" >"$sums.bad"
mv "$sums.bad" "$sums"
check "fails" fails run_install badsum --version v9.9.3
check "says the checksum doesn't match" says badsum "Checksum mismatch"
check "leaves v9.9.2 installed" is_version v9.9.2

echo "no config file, same version"
rm "$config"
check "succeeds" run_install noconfig --version v9.9.2
check "says it reinstalled for the template" says noconfig "Reinstalled v9.9.2, for the config template"
check "writes the config template" cmp -s "$config" docs/config.example.yaml

echo "an installed binary without --pe-version"
printf '#!/bin/sh\nexit 1\n' >"$bin"
check "succeeds" run_install oldbin --version v9.9.2
check "the binary reports v9.9.2" is_version v9.9.2

echo "--version with --from source"
source_with_version() {
	local out status=0
	out="$(bash install.sh --from source --version v9.9.2 2>&1)" || status=$?
	[ "$status" -eq 2 ] && grep -q "can.t be used with --from source" <<<"$out"
}
check "fails, saying why" source_with_version

echo "piped in, outside a clone, without --from"
mkdir -p "$work/cwd"
rm -f "$bin"
if (cd "$work/cwd" && PE_TEST_HOME="$work/home" "$root/scripts/test-env.sh" \
	PROMPT_ENGINE_RELEASES_URL="file://$work/releases" NO_COLOR=1 \
	bash -s -- --binary-only --version v9.9.1 --prefix "$work/bin" \
	<"$root/install.sh" >"$work/piped.out" 2>&1); then
	pass "installs"
else
	fail "installs"
fi
check "installs from the release" says piped "Checksum verified"
check "the binary reports v9.9.1" is_version v9.9.1
check "its hints show the one-line command" says piped "install.sh | bash -s -- --check"

echo "missing version"
check "fails" fails run_install missing --version v9.9.9
check "says it couldn't download" says missing "Couldn.t download v9.9.9"

if [ "$latest" = 1 ]; then
	echo "latest release from GitHub"
	rm -rf "${work:?}/bin"
	if PE_TEST_HOME="$work/home" scripts/test-env.sh NO_COLOR=1 \
		bash "$root/install.sh" --from release --binary-only --prefix "$work/bin" \
		</dev/null >"$work/latest.out" 2>&1; then
		pass "installs"
	else
		fail "installs"
	fi
	check "the binary reports a release version" release_version
	check "says it is the latest" says latest "(the latest)"
fi

echo
echo "$passed passed, $failed failed"
if [ "$failed" -gt 0 ]; then
	echo "Kept for a look: $work"
	exit 1
fi
rm -rf "$work"
