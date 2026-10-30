#!/usr/bin/env bash
#
# Builds the release archives for a version: one per platform, each with
# the prompt-engine binary, README.md, LICENSE and the config template,
# plus checksums.txt.
#
#   scripts/build-release.sh v1.2.3 [dist]

set -euo pipefail

version="${1:?usage: scripts/build-release.sh <version> [output dir]}"
out="${2:-dist}"
targets="darwin/arm64 darwin/amd64 linux/amd64 linux/arm64"

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
	cp README.md LICENSE docs/config.example.yaml "$stage/"
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
