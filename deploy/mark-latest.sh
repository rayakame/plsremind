#!/usr/bin/env bash
# Marks the newest server release that has its deploy files as the latest; the guides download from it.
set -euo pipefail

# GitHub makes every new release the latest, an SDK's too, and a server release before its files are attached.
# Assigned on its own, so that a failed lookup fails instead of meaning "no release".
newest="$(python3 "$(dirname "$0")/releases.py" compose.yaml stack.yaml env.example plsremind-monitoring.tar.gz)"
if [ -z "$newest" ]; then
  echo "no server release has its deploy files yet"
  exit 0
fi
gh release edit "$newest" --repo "$GITHUB_REPOSITORY" --latest >/dev/null
echo "$newest is the latest release"
