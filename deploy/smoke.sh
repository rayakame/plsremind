#!/usr/bin/env bash
# Builds the image, starts a throwaway instance with monitoring and walks through first use:
# setup, an item, a reminder that goes through the gateway to the example adapter and back,
# then that reminder in the metrics. Exits non-zero on the first surprise.
set -euo pipefail

cd "$(dirname "$0")"
export COMPOSE_PROJECT_NAME=plsremind-smoke
export PLSREMIND_PORT="${PLSREMIND_PORT:-18080}"
export PLSREMIND_PUBLIC_URL="http://localhost:${PLSREMIND_PORT}"
export PLSREMIND_SECRET_KEY="$(openssl rand -base64 32)"
export PLSREMIND_GRAFANA_PORT="${PLSREMIND_GRAFANA_PORT:-13000}"
export PLSREMIND_GRAFANA_PASSWORD="$(openssl rand -hex 16)"
export COMPOSE_PROFILES=monitoring
base="${PLSREMIND_PUBLIC_URL}/api/v1"
grafana="http://localhost:${PLSREMIND_GRAFANA_PORT}"
jar="$(mktemp)"

adapter_pid=""
adapter_log="$(mktemp)"

cleanup() {
  [ -n "$adapter_pid" ] && kill "$adapter_pid" >/dev/null 2>&1 || true
  docker compose down -v --remove-orphans >/dev/null 2>&1 || true
  rm -f "$jar" "$adapter_log"
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
expect "the dashboard is there" \
  "$(curl -fsS -u "admin:${PLSREMIND_GRAFANA_PASSWORD}" "${grafana}/api/search?query=plsremind" | grep -o '"uid":"plsremind"')" '"uid":"plsremind"'

echo "smoke: all good"
