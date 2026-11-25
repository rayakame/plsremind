#!/usr/bin/env bash
# Marks the newest server release that has its deploy files as the latest; the guides download from it.
set -euo pipefail

# GitHub makes every new release the latest, an SDK's too, and a server release before its files are attached.
# Assigned on its own, so that a failed lookup fails instead of meaning "no release".
finished="$(gh api --paginate "repos/$GITHUB_REPOSITORY/releases?per_page=100" --jq '.[]
  | select((.draft or .prerelease) | not) | select(.tag_name | test("^v[0-9]"))
  | select(["compose.yaml", "stack.yaml", "env.example", "plsremind-monitoring.tar.gz"] - [.assets[] | select(.state == "uploaded") | .name] == [])
  | "\(.created_at) \(.tag_name)"')"
# The list's order is not documented; created_at is what GitHub itself orders "latest" by.
newest="$(sort -r <<< "$finished" | awk 'NR == 1 { print $2 }')"
if [ -z "$newest" ]; then
  echo "no server release has its deploy files yet"
  exit 0
fi
gh release edit "$newest" --repo "$GITHUB_REPOSITORY" --latest >/dev/null
echo "$newest is the latest release"
