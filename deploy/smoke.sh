#!/usr/bin/env bash
# Builds the image, starts a throwaway instance with monitoring and walks through first use:
# setup, an item, a reminder that goes through the gateway to the example adapter and back,
# then that reminder in the metrics. Exits non-zero on the first surprise.
set -euo pipefail

cd "$(dirname "$0")"
export COMPOSE_PROJECT_NAME=plsremind-smoke
export PLSREMIND_PORT="${PLSREMIND_PORT:-18080}"
export PLSREMIND_PUBLIC_URL="http://localhost:${PLSREMIND_PORT}"
PLSREMIND_SECRET_KEY="$(openssl rand -base64 32)"
export PLSREMIND_SECRET_KEY
export PLSREMIND_GRAFANA_PORT="${PLSREMIND_GRAFANA_PORT:-13000}"
PLSREMIND_GRAFANA_PASSWORD="$(openssl rand -hex 16)"
export PLSREMIND_GRAFANA_PASSWORD
export COMPOSE_PROFILES=monitoring
# Built from this checkout; the released images do not exist yet for what is being tested.
export COMPOSE_FILE=compose.yaml:compose.build.yaml
base="${PLSREMIND_PUBLIC_URL}/api/v1"
grafana="http://localhost:${PLSREMIND_GRAFANA_PORT}"
jar="$(mktemp)"

adapter_pid=""
adapter_log="$(mktemp)"

bundle_dir="$(mktemp -d)"

cleanup() {
  [ -n "$adapter_pid" ] && kill "$adapter_pid" >/dev/null 2>&1 || true
  docker compose down -v --remove-orphans >/dev/null 2>&1 || true
  rm -f "$jar" "$adapter_log" monitoring/targets/smoke-extra.yml
  rm -rf "$bundle_dir"
}
trap cleanup EXIT

fail() {
  echo "smoke: $1" >&2
  docker compose logs hub victoriametrics vmalert grafana >&2 || true
  exit 1
}

expect() {
  local what="$1" got="$2" want="$3"
  [ "$got" = "$want" ] || fail "$what: expected $want, got $got"
  echo "ok  $what"
}

# The deploy bundle holds what a server needs, and a new one unpacks over an old one without
# touching what belongs to the self-hoster.
bundle="$(./bundle.sh "$bundle_dir")"
mkdir -p "$bundle_dir/server/backups" "$bundle_dir/server/monitoring/targets"
printf 'MINE=1\n' > "$bundle_dir/server/.env"
printf 'services: {}\n' > "$bundle_dir/server/compose.override.yaml"
printf 'dump\n' > "$bundle_dir/server/backups/old.dump"
printf -- '- targets: ["mine:9091"]\n' > "$bundle_dir/server/monitoring/targets/mine.yml"
printf 'changed\n' > "$bundle_dir/server/compose.yaml"
tar -xzf "$bundle" -C "$bundle_dir/server" --strip-components=1
for kept in .env compose.override.yaml backups/old.dump monitoring/targets/mine.yml; do
  [ -s "$bundle_dir/server/$kept" ] || fail "unpacking the bundle lost $kept"
done
cmp -s compose.yaml "$bundle_dir/server/compose.yaml" || fail "unpacking the bundle did not bring its compose.yaml back"
[ ! -e "$bundle_dir/server/compose.build.yaml" ] || fail "the bundle ships compose.build.yaml"
(cd "$bundle_dir/server" && env -u COMPOSE_FILE docker compose --profile discord --profile monitoring config --quiet) \
  || fail "the unpacked bundle is no valid compose project"
echo "ok  the bundle unpacks over an installation and keeps its own files"

docker compose up -d --build --wait

for _ in $(seq 1 30); do
  curl -fsS "$base/info" >/dev/null 2>&1 && break
  sleep 1
done

expect "the hub reports itself healthy" "$(docker inspect -f '{{.State.Health.Status}}' "$(docker compose ps -q hub)")" healthy
expect "the hub is alive" "$(curl -fsS "${PLSREMIND_PUBLIC_URL}/healthz")" ok
expect "the hub is ready" "$(curl -fsS "${PLSREMIND_PUBLIC_URL}/readyz")" ok
docker compose run --rm --no-deps vmalert -rule=/etc/plsremind/alerts.yml -dryRun >/dev/null 2>&1 \
  || fail "the alert rules do not load"
echo "ok  the alert rules load"
docker run --rm -v "$PWD/monitoring:/rules:ro" victoriametrics/vmalert-tool:v1.156.0 unittest --files=/rules/alerts.test.yml >/dev/null 2>&1 \
  || fail "the alert rules do not pass their cases in monitoring/alerts.test.yml"
echo "ok  the alert rules pass their cases"
generated="$(mktemp)"
python3 monitoring/grafana/dashboard.py "$generated" >/dev/null
cmp -s "$generated" monitoring/grafana/plsremind.json \
  || fail "monitoring/grafana/plsremind.json differs from what monitoring/grafana/dashboard.py writes"
rm -f "$generated"
echo "ok  the dashboard is what its generator writes"
docker compose -f compose.yaml -f examples/another-discord-bot.yaml --profile discord config --quiet \
  || fail "the example for another Discord bot does not fit the compose file"
echo "ok  the example for another Discord bot fits"
released="$(python3 -c 'import json; print(json.load(open("../.release-please-manifest.json"))["."])')"
images="$(COMPOSE_FILE='' docker compose -f compose.yaml -f examples/another-discord-bot.yaml --profile discord config --format json \
  | python3 -c '
import json, sys
services = json.load(sys.stdin)["services"]
print(" ".join(name + "=" + str(services[name].get("image")) + ("+build" if "build" in services[name] else "") for name in ("hub", "discord", "discord-team")))
')"
expect "compose.yaml names the released images" "$images" \
  "hub=ghcr.io/rayakame/plsremind:${released} discord=ghcr.io/rayakame/plsremind-discord:${released} discord-team=ghcr.io/rayakame/plsremind-discord:${released}"
expect "vmalert runs" "$(docker inspect -f '{{.State.Status}}' "$(docker compose ps -q vmalert)")" running

expect "fresh instance needs setup" \
  "$(curl -fsS "$base/info" | grep -o '"setup_required":[a-z]*')" '"setup_required":true'

expect "setup creates the admin" \
  "$(curl -sS -o /dev/null -w '%{http_code}' -c "$jar" -H 'Content-Type: application/json' \
      -d '{"username":"admin","password":"smoke test password"}' "$base/setup")" 201

expect "the admin can create an item" \
  "$(curl -sS -o /dev/null -w '%{http_code}' -b "$jar" -H 'Content-Type: application/json' \
      -H "Origin: ${PLSREMIND_PUBLIC_URL}" -d '{"title":"first item","due_date":"2030-01-01"}' "$base/users/@me/items")" 201

expect "the item is listed" \
  "$(curl -fsS -b "$jar" "$base/users/@me/items" | grep -o '"title":"first item"')" '"title":"first item"'

expect "setup is closed afterwards" \
  "$(curl -fsS "$base/info" | grep -o '"setup_required":[a-z]*')" '"setup_required":false'

asset="$(curl -fsS "${PLSREMIND_PUBLIC_URL}/lists/any" | grep -o '/assets/[^"]*\.js' | head -n 1)"
[ -n "$asset" ] || fail "the page does not load the dashboard"
echo "ok  the dashboard page is served on every path"

expect "its script is served to be cached for good" \
  "$(curl -fsS -o /dev/null -D - "${PLSREMIND_PUBLIC_URL}${asset}" | tr -d '\r' | grep -i '^cache-control:' | cut -d' ' -f2-)" \
  "public, max-age=31536000, immutable"

# A reminder, end to end: the example adapter waits on the gateway, an item comes due,
# the adapter confirms the delivery, and the log shows it as delivered.
token="$(curl -fsS -b "$jar" -H 'Content-Type: application/json' -H "Origin: ${PLSREMIND_PUBLIC_URL}" \
  -d '{"name":"smoke adapter","remind":true}' "$base/users/@me/tokens" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')"
[ -n "$token" ] || fail "could not create a personal token"

(cd ../hub && PLSREMIND_URL="$PLSREMIND_PUBLIC_URL" PLSREMIND_TOKEN="$token" go run ./examples/remind -once) >"$adapter_log" 2>&1 &
adapter_pid=$!
for _ in $(seq 1 60); do
  grep -q "waiting for reminders" "$adapter_log" && break
  sleep 1
done
grep -q "waiting for reminders" "$adapter_log" || { cat "$adapter_log" >&2; fail "the example adapter did not connect"; }

# The reminder is set relative to the hub's own clock, so a clock that differs
# between this machine and the container cannot make it "already past".
item="$(curl -fsS -b "$jar" -H 'Content-Type: application/json' -H "Origin: ${PLSREMIND_PUBLIC_URL}" \
  -d '{"title":"smoke reminder"}' "$base/users/@me/items" | sed -n 's/^{"id":"\([^"]*\)".*/\1/p')"
[ -n "$item" ] || fail "could not create the item to be reminded about"
expect "a reminder in one minute" \
  "$(curl -sS -o /dev/null -w '%{http_code}' -b "$jar" -H 'Content-Type: application/json' \
      -H "Origin: ${PLSREMIND_PUBLIC_URL}" -d '{"in":"1m"}' "$base/users/@me/items/$item/reminders")" 201

for _ in $(seq 1 100); do
  kill -0 "$adapter_pid" >/dev/null 2>&1 || break
  sleep 1
done
if kill -0 "$adapter_pid" >/dev/null 2>&1; then
  cat "$adapter_log" >&2
  curl -sS -b "$jar" "$base/users/@me/reminder-log" >&2 || true
  fail "the adapter never received the reminder"
fi
adapter_pid=""
expect "the adapter was reminded" "$(grep -c 'smoke reminder' "$adapter_log")" 1
expect "the hub recorded the confirmation" \
  "$(curl -fsS -b "$jar" "$base/users/@me/reminder-log" | grep -o '"status":"delivered"')" '"status":"delivered"'

# What VictoriaMetrics has, asked through Grafana's data source, which checks that one too.
query() {
  curl -fsS -u "admin:${PLSREMIND_GRAFANA_PASSWORD}" --get \
    --data-urlencode "query=$1" "${grafana}/api/datasources/proxy/uid/victoriametrics/api/v1/query"
}
scraped=""
for _ in $(seq 1 60); do
  scraped="$(query 'sum(plsremind_reminders_total{outcome="fired"})' 2>/dev/null | grep -o '"value":\[[^]]*\]' || true)"
  [ -n "$scraped" ] && [ "${scraped##*,}" != '"0"]' ] && break
  sleep 2
done
expect "the fired reminder shows in the metrics" "${scraped##*,}" '"1"]'
expect "the hub is scraped" "$(query 'up{job="plsremind-hub"}' | grep -o '"value":\[[^]]*\]' | sed 's/.*,//')" '"1"]'
# A target of one's own goes in a file of its own, and is picked up without a restart.
printf -- '- targets: ["smoke-extra:9091"]\n' > monitoring/targets/smoke-extra.yml
extra=""
for _ in $(seq 1 45); do
  extra="$(query 'up{job="plsremind-discord",instance="smoke-extra:9091"}' 2>/dev/null | grep -o '"instance":"smoke-extra:9091"' || true)"
  [ -n "$extra" ] && break
  sleep 2
done
expect "an extra target file is scraped" "$extra" '"instance":"smoke-extra:9091"'
expect "the dashboard is there" \
  "$(curl -fsS -u "admin:${PLSREMIND_GRAFANA_PASSWORD}" "${grafana}/api/search?query=plsremind" | grep -o '"uid":"plsremind"')" '"uid":"plsremind"'

# A backup taken now and restored later brings back exactly what was there.
backup="$(./backup.sh)"
[ -s "$backup" ] || fail "backup.sh wrote no dump"
echo "ok  backup.sh wrote $backup"
curl -fsS -o /dev/null -b "$jar" -H 'Content-Type: application/json' -H "Origin: ${PLSREMIND_PUBLIC_URL}" \
  -d '{"title":"after the backup"}' "$base/users/@me/items"
./restore.sh --yes "$backup" >/dev/null
for _ in $(seq 1 60); do
  curl -fsS "${PLSREMIND_PUBLIC_URL}/readyz" >/dev/null 2>&1 && break
  sleep 1
done
titles="$(curl -fsS -b "$jar" "$base/users/@me/items" | grep -o '"title":"[^"]*"' | sort | tr '\n' ' ')"
case "$titles" in *'"title":"after the backup"'*) fail "restore.sh kept what came after the backup";; esac
case "$titles" in *'"title":"first item"'*) echo "ok  restore.sh brought the backup back";; *) fail "restore.sh lost the first item: $titles";; esac
rm -f "$backup"

echo "smoke: all good"
