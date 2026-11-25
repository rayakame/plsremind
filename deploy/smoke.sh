#!/usr/bin/env bash
# Builds the image, starts a throwaway instance with the monitoring add-on and walks through first use:
# setup, an item, a reminder that goes through the gateway to the example adapter and back,
# then that reminder in the metrics. Exits non-zero on the first surprise.
set -euo pipefail

cd "$(dirname "$0")"
export COMPOSE_PROJECT_NAME=plsremind-smoke
export PLSREMIND_PUBLIC_URL="http://localhost:18080"
PLSREMIND_SECRET_KEY="$(openssl rand -base64 32)"
export PLSREMIND_SECRET_KEY
PLSREMIND_GRAFANA_PASSWORD="$(openssl rand -hex 16)"
export PLSREMIND_GRAFANA_PASSWORD
export COMPOSE_PROFILES=
work="$(mktemp -d)"
# Its own port and network, so that the smoke test never meets an instance on the same machine.
cat > "$work/smoke.yaml" <<'YAML'
services:
  hub:
    ports: !override
      - "18080:8080"
networks:
  default:
    name: plsremind-smoke
YAML
cat > "$work/addon.yaml" <<'YAML'
services:
  grafana:
    ports: !override
      - "127.0.0.1:13000:3000"
networks:
  default:
    name: plsremind-smoke
    external: true
YAML
# Built from this checkout; the released images do not exist yet for what is being tested.
export COMPOSE_FILE="compose.yaml:compose.build.yaml:$work/smoke.yaml"
base="${PLSREMIND_PUBLIC_URL}/api/v1"
grafana="http://localhost:13000"
jar="$(mktemp)"

adapter_pid=""
adapter_log="$(mktemp)"

# The add-on runs from what a release ships, not from the repository's folder.
addon() {
  docker compose -p plsremind-smoke-monitoring -f "$work/monitoring/compose.yaml" -f "$work/addon.yaml" "$@"
}

cleanup() {
  [ -n "$adapter_pid" ] && kill "$adapter_pid" >/dev/null 2>&1 || true
  [ -f "$work/monitoring/compose.yaml" ] && addon down -v >/dev/null 2>&1 || true
  docker compose down -v --remove-orphans >/dev/null 2>&1 || true
  rm -f "$jar" "$adapter_log"
  rm -rf "$work"
}
trap cleanup EXIT

fail() {
  echo "smoke: $1" >&2
  docker compose logs hub >&2 || true
  [ -f "$work/monitoring/compose.yaml" ] && addon logs >&2 || true
  exit 1
}

expect() {
  local what="$1" got="$2" want="$3"
  [ "$got" = "$want" ] || fail "$what: expected $want, got $got"
  echo "ok  $what"
}

docker compose up -d --build --wait
addon_tarball="$(./pack-monitoring.sh "$work")"
if tar -tzf "$addon_tarball" | grep -qE 'alerts[.]test[.]yml|dashboard[.]py'; then
  fail "the add-on ships files that only the repository needs"
fi
tar -xzf "$addon_tarball" -C "$work"
addon up -d --wait

for _ in $(seq 1 30); do
  curl -fsS "$base/info" >/dev/null 2>&1 && break
  sleep 1
done

expect "the hub reports itself healthy" "$(docker inspect -f '{{.State.Health.Status}}' "$(docker compose ps -q hub)")" healthy
expect "the hub is alive" "$(curl -fsS "${PLSREMIND_PUBLIC_URL}/healthz")" ok
expect "the hub is ready" "$(curl -fsS "${PLSREMIND_PUBLIC_URL}/readyz")" ok
addon run --rm --no-deps vmalert -rule=/etc/plsremind/alerts.yml -dryRun >/dev/null 2>&1 \
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
released="$(python3 series.py)"
images="$(COMPOSE_FILE='' docker compose -f compose.yaml -f examples/another-discord-bot.yaml --profile discord config --format json \
  | python3 -c '
import json, sys
services = json.load(sys.stdin)["services"]
print(" ".join(name + "=" + str(services[name].get("image")) + ("+build" if "build" in services[name] else "") for name in ("hub", "discord", "discord-team")))
')"
expect "compose.yaml names the images of the released series" "$images" \
  "hub=ghcr.io/rayakame/plsremind:${released} discord=ghcr.io/rayakame/plsremind-discord:${released} discord-team=ghcr.io/rayakame/plsremind-discord:${released}"
expect "vmalert runs" "$(docker inspect -f '{{.State.Status}}' "$(addon ps -q vmalert)")" running
expect "the deployment runs no monitoring of its own" \
  "$(docker compose -f compose.yaml --profile '*' config --services | grep -cxE 'victoriametrics|vmalert|grafana' || true)" 0
expect "the database is PostgreSQL by its major tag" "$(docker compose config --images | grep '^postgres' || true)" postgres:18
expect "compose.yaml names its network" \
  "$(docker compose -f compose.yaml config --format json | python3 -c 'import json, sys; print(json.load(sys.stdin)["networks"]["default"]["name"])')" plsremind
expect "building from source gets a network of its own" \
  "$(docker compose -f compose.yaml -f compose.build.yaml config --format json | python3 -c 'import json, sys; print(json.load(sys.stdin)["networks"]["default"]["name"])')" plsremind-dev
docker network inspect plsremind-smoke >/dev/null || fail "the network plsremind-smoke is missing"
echo "ok  the add-on joined the deployment's network"

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
printf -- '- targets: ["smoke-extra:9091"]\n' > "$work/monitoring/targets/smoke-extra.yml"
extra=""
for _ in $(seq 1 45); do
  extra="$(query 'up{job="plsremind-discord",instance="smoke-extra:9091"}' 2>/dev/null | grep -o '"instance":"smoke-extra:9091"' || true)"
  [ -n "$extra" ] && break
  sleep 2
done
expect "an extra target file is scraped" "$extra" '"instance":"smoke-extra:9091"'
expect "the dashboard is there" \
  "$(curl -fsS -u "admin:${PLSREMIND_GRAFANA_PASSWORD}" "${grafana}/api/search?query=plsremind" | grep -o '"uid":"plsremind"')" '"uid":"plsremind"'
# The backup and restore of docs/deploy/backups.md, run as the guide has them, in a folder of their own.
export COMPOSE_FILE="$PWD/compose.yaml:$PWD/compose.build.yaml:$work/smoke.yaml"
mkdir "$work/guide"
guide() {
  local commands
  commands="$(python3 snippet.py ../docs/deploy/backups.md "$1")" || fail "docs/deploy/backups.md has no snippet $1"
  (cd "$work/guide" && bash -euo pipefail -c "$commands")
}
guide compose-backup >/dev/null
[ -s "$work/guide/plsremind.dump" ] || fail "the guide's backup wrote no dump"
expect "the guide's dump is readable by its owner alone" \
  "$(python3 -c 'import os, sys; print(oct(os.stat(sys.argv[1]).st_mode & 0o777))' "$work/guide/plsremind.dump")" 0o600
cp "$work/guide/plsremind.dump" "$work/backup.dump"
echo "ok  the guide's backup wrote a dump"
curl -fsS -o /dev/null -b "$jar" -H 'Content-Type: application/json' -H "Origin: ${PLSREMIND_PUBLIC_URL}" \
  -d '{"title":"after the backup"}' "$base/users/@me/items"
# A dump that breaks off must leave the database, and the hub serving it, as they were.
head -c 4000 "$work/backup.dump" > "$work/guide/plsremind.dump"
if guide compose-restore >/dev/null 2>&1; then
  fail "the guide's restore took a dump that breaks off"
fi
curl -fsS -b "$jar" "$base/users/@me/items" | grep -q '"title":"after the backup"' \
  || fail "a failed restore changed the database"
expect "a broken dump leaves the database and the hub as they were" "$(curl -fsS "${PLSREMIND_PUBLIC_URL}/readyz")" ok
# Going back from an update whose hub keeps crashing: the restore must replace that container as well.
cp "$work/backup.dump" "$work/guide/plsremind.dump"
printf 'services:\n  hub:\n    environment:\n      PLSREMIND_SECRET_KEY: broken\n' > "$work/crashing.yaml"
COMPOSE_FILE="$COMPOSE_FILE:$work/crashing.yaml" docker compose up -d --no-deps hub >/dev/null 2>&1
restarts=0
for _ in $(seq 1 30); do
  restarts="$(docker inspect -f '{{.RestartCount}}' "$(docker compose ps -a -q hub)")"
  [ "$restarts" -ge 2 ] && break
  sleep 1
done
[ "$restarts" -ge 2 ] || fail "the broken hub did not keep restarting"
guide compose-restore >/dev/null 2>&1 || fail "the guide's restore failed"
for _ in $(seq 1 60); do
  curl -fsS "${PLSREMIND_PUBLIC_URL}/readyz" >/dev/null 2>&1 && break
  sleep 1
done
curl -fsS "${PLSREMIND_PUBLIC_URL}/readyz" >/dev/null 2>&1 || fail "the hub was not ready a minute after the restore"
titles="$(curl -fsS -b "$jar" "$base/users/@me/items" | grep -o '"title":"[^"]*"' | sort | tr '\n' ' ')"
case "$titles" in *'"title":"after the backup"'*) fail "the restore kept what came after the backup";; esac
case "$titles" in *'"title":"first item"'*) echo "ok  the guide's restore brought the backup back past a crashing hub";; *) fail "the restore lost the first item: $titles";; esac
expect "the database the restore replaced is kept until it is dropped by hand" \
  "$(docker compose exec -T db psql --username plsremind --dbname postgres -tA -c "SELECT count(*) FROM pg_database WHERE datname = 'plsremind_replaced'")" 1
guide compose-restore >/dev/null 2>&1 || fail "a second restore failed on the database the first one kept"
for _ in $(seq 1 60); do
  curl -fsS "${PLSREMIND_PUBLIC_URL}/readyz" >/dev/null 2>&1 && break
  sleep 1
done
curl -fsS -b "$jar" "$base/users/@me/items" | grep -q '"title":"first item"' || fail "the second restore lost the first item"
echo "ok  a second restore takes the place of what the first one kept"

echo "smoke: all good"
