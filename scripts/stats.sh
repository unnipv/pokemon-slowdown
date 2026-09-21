#!/bin/sh
# Aggregate counts for pokemon slowdown: release downloads, repo traffic and
# stars. Aggregates only — the binary has no telemetry and nothing here is
# per-user.
#
# Requires `gh`, authenticated. The traffic endpoints need push access to the
# repository; the rest works with read access.

set -eu

repo=${1:-unnipv/pokemon-slowdown}

echo "pokemon slowdown — $repo"
echo

echo "Release downloads (asset by asset; Homebrew installs are inside these"
echo "counts because the formula downloads the same tarballs — do not add them):"
gh api "repos/$repo/releases" --paginate --jq '
  [.[].assets[] | select(.name != "checksums.txt")] as $assets
  | "  \($assets | map(.download_count) | add // 0) total",
    ($assets[] | "  \(.download_count)  \(.name)")'
echo

echo "Traffic, last 14 days (the API keeps 14; the web UI shows the same):"
gh api "repos/$repo/traffic/clones" --jq '"  clones: \(.count) (\(.uniques) unique)"'
gh api "repos/$repo/traffic/views"  --jq '"  views:  \(.count) (\(.uniques) unique)"'
echo

gh api "repos/$repo" --jq '"\(.stargazers_count) stars · \(.open_issues_count) open issues/PRs"'
