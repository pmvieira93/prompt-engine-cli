#!/usr/bin/env bash
#
# Builds the release archives for a version: one per platform, each with
# the prompt-engine binary, README.md, LICENSE, the config and guard rail
# templates, and the docs the README links to, plus checksums.txt.
#
#   scripts/build-release.sh v1.2.3 [dist]
#
# TARGETS="darwin/arm64" builds only the platforms listed (for tests).

set -euo pipefail

version="${1:?usage: scripts/build-release.sh <version> [output dir]}"
out="${2:-dist}"
targets="${TARGETS:-darwin/arm64 darwin/amd64 linux/amd64 linux/arm64}"

cd "$(dirname "$0")/.."
rm -rf "$out"
mkdir -p "$out"

for target in $targets; do
	os="${target%/*}"
	arch="${target#*/}"
	name="prompt-engine_${version}_${os}_${arch}"
	stage="$out/$name"
	mkdir -p "$stage"
	CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
		-ldflags "-s -w -X main.version=$version" \
		-o "$stage/prompt-engine" ./cmd/prompt-engine
	# The templates also stay at the top, where install.sh looks for them.
	cp README.md LICENSE PRD.md docs/config.example.yaml docs/guards.example.txt "$stage/"
	mkdir -p "$stage/docs/guide" "$stage/docs/media" "$stage/docs/spikes"
	cp docs/*.md docs/config.example.yaml docs/guards.example.txt "$stage/docs/"
	cp docs/guide/*.md "$stage/docs/guide/"
	cp docs/media/*.gif "$stage/docs/media/"
	cp docs/spikes/*.md "$stage/docs/spikes/"
	tar -czf "$out/$name.tar.gz" -C "$out" "$name"
	rm -rf "$stage"
	echo "built $out/$name.tar.gz"
done

# sha256sum on Linux, shasum on macOS.
(
	cd "$out"
	if command -v sha256sum >/dev/null; then
		sha256sum ./*.tar.gz
	else
		shasum -a 256 ./*.tar.gz
	fi
) | sed 's# \./# #' >"$out/checksums.txt"
echo "wrote $out/checksums.txt"
