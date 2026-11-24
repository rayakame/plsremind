#!/usr/bin/env bash
# Updates the previous release, full of data, to a candidate hub image and checks that it all survived.
# PREVIOUS_BUNDLE and PREVIOUS_HUB_IMAGE stand in for a published release, to try this before one exists.
set -euo pipefail

cd "$(dirname "$0")"
previous="${1:?usage: ./upgrade-test.sh <previous version|none> <candidate hub image>}"
candidate="${2:?usage: ./upgrade-test.sh <previous version|none> <candidate hub image>}"
work="$(mktemp -d)"
server="$work/server"
port="${PLSREMIND_PORT:-18090}"
base="http://localhost:${port}"
export COMPOSE_PROJECT_NAME=plsremind-upgrade
unset COMPOSE_FILE COMPOSE_PROFILES

cleanup() {
  (cd "$server" 2>/dev/null && docker compose down -v --remove-orphans >/dev/null 2>&1) || true
  rm -rf "$work"
}
trap cleanup EXIT

fail() {
  echo "upgrade: $1" >&2
  (cd "$server" && docker compose logs hub >&2) || true
  exit 1
}

# The hub runs the given image; everything else comes from the bundle unpacked into the folder.
use_hub() {
  printf 'services:\n  hub:\n    image: %s\n    pull_policy: %s\n' "$1" "$2" > "$server/compose.override.yaml"
}

start() {
  (cd "$server" && docker compose up -d --wait db hub) || fail "the hub did not become ready"
}

mkdir -p "$server"
{
  echo "PLSREMIND_SECRET_KEY=$(openssl rand -base64 32)"
  echo "PLSREMIND_PORT=${port}"
  echo "PLSREMIND_PUBLIC_URL=${base}"
} > "$server/.env"

if [ "$previous" != "none" ]; then
  bundle="${PREVIOUS_BUNDLE:-}"
  if [ -z "$bundle" ]; then
    gh release download "v${previous}" --pattern 'plsremind-deploy-*.tar.gz' --dir "$work"
    bundle="$(ls "$work"/plsremind-deploy-*.tar.gz)"
  fi
  tar -xzf "$bundle" -C "$server" --strip-components=1
  if [ -n "${PREVIOUS_HUB_IMAGE:-}" ]; then
    use_hub "$PREVIOUS_HUB_IMAGE" never
  fi
  start
  python3 upgrade-check.py seed "$base" "$work/state.json"
  echo "ok  seeded release ${previous}"
fi

# The candidate's bundle over the old folder, as a self-hoster updates.
tar -xzf "$(./bundle.sh "$work")" -C "$server" --strip-components=1
use_hub "$candidate" never
start
if [ "$previous" = "none" ]; then
  python3 upgrade-check.py seed "$base" "$work/state.json"
  echo "ok  seeded a fresh install"
fi
python3 upgrade-check.py verify "$base" "$work/state.json" || fail "data changed or went missing"
echo "upgrade: all good"
