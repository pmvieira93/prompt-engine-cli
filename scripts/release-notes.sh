#!/usr/bin/env bash
#
# Writes release notes for a tag, in Markdown, from the history since the
# previous tag (or since the first commit): each merge into the main line
# with the commits it brought in, then any commits made on the main line
# directly. Work here is merged with --no-ff, not through pull requests,
# so GitHub's generated notes would be empty.
#
#   scripts/release-notes.sh v1.2.3 [owner/repo]

set -euo pipefail

tag="${1:?usage: scripts/release-notes.sh <tag> [owner/repo]}"
repo="${2:-${GITHUB_REPOSITORY:-}}"

git rev-parse -q --verify "refs/tags/$tag" >/dev/null || { echo "no tag $tag" >&2; exit 1; }
prev="$(git describe --tags --abbrev=0 "$tag^" 2>/dev/null || true)"
range="$tag"
[ -n "$prev" ] && range="$prev..$tag"

# "Merge FR-8: rewrite styles" reads as "FR-8: rewrite styles".
title() { sed -E 's/^Merge (branch |the )?//' <<<"$1" | awk '{ print toupper(substr($0, 1, 1)) substr($0, 2) }'; }

echo "## What's changed"
echo
merges="$(git log --first-parent --merges --format='%H' "$range")"
if [ -n "$merges" ]; then
	while read -r sha; do
		printf -- '- **%s** (%s)\n' "$(title "$(git log -1 --format=%s "$sha")")" "$(git rev-parse --short "$sha")"
		git log --reverse --format='  - %s (%h)' "$sha^1..$sha^2"
	done <<<"$merges"
	echo
fi

direct="$(git log --first-parent --no-merges --reverse --format='- %s (%h)' "$range")"
if [ -n "$direct" ]; then
	if [ -n "$merges" ]; then
		echo "### Other changes"
		echo
	fi
	echo "$direct"
	echo
fi

if [ -n "$prev" ] && [ -n "$repo" ]; then
	echo "**Full changelog:** https://github.com/$repo/compare/$prev...$tag"
elif [ -z "$prev" ]; then
	echo "First release."
fi
