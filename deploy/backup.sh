#!/usr/bin/env bash
# Writes a dump of the plsremind database to backups/ and prints its path; restore.sh puts it back.
set -euo pipefail

cd "$(dirname "$0")"
mkdir -p backups
partial="$(mktemp backups/.partial-XXXXXX)"
trap 'rm -f "$partial"' EXIT
# The process number keeps two backups started in the same second apart.
file="backups/plsremind-$(date -u +%Y%m%d-%H%M%S)-$$.dump"

docker compose exec -T db pg_dump --username plsremind --dbname plsremind --format custom > "$partial"
[ -s "$partial" ] || { echo "backup.sh: the dump came out empty" >&2; exit 1; }
mv "$partial" "$file"
echo "$file"
