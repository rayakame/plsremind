#!/usr/bin/env bash
# Deploys stack.yaml on a swarm of one node and checks what Swarm does differently from Compose,
# the guide's Swarm backup and restore, and the monitoring add-on next to the stack.
set -euo pipefail

cd "$(dirname "$0")"
usage="usage: ./swarm-test.sh <hub image> <bot image>"
hub_image="${1:?$usage}"
bot_image="${2:?$usage}"
stack=plsremind
port=18100
base="http://localhost:${port}/api/v1"
export PLSREMIND_PUBLIC_URL="http://localhost:${port}"
PLSREMIND_SECRET_KEY="$(openssl rand -base64 32)"
export PLSREMIND_SECRET_KEY
PLSREMIND_GRAFANA_PASSWORD="$(openssl rand -hex 16)"
export PLSREMIND_GRAFANA_PASSWORD

# The guide's commands name the stack plsremind, so the test may not share a machine with one.
if docker stack ls --format '{{.Name}}' 2>/dev/null | grep -qx "$stack"; then
  echo "swarm: a stack named $stack runs here already" >&2
  exit 1
fi
if docker network inspect plsremind >/dev/null 2>&1; then
  echo "swarm: a network named plsremind exists here already" >&2
  exit 1
fi
# A Compose instance in a folder named plsremind leaves volumes of exactly these names.
for volume in "${stack}_db" "${stack}_discord"; do
  if docker volume inspect "$volume" >/dev/null 2>&1; then
    echo "swarm: a volume named $volume exists here already" >&2
    exit 1
  fi
done

work="$(mktemp -d)"
jar="$work/cookies"
initialised=""

addon() {
  docker compose -p plsremind-swarm-monitoring -f "$work/monitoring/compose.yaml" "$@"
}

cleanup() {
  [ -f "$work/monitoring/compose.yaml" ] && addon down -v >/dev/null 2>&1 || true
  docker stack rm "$stack" >/dev/null 2>&1 || true
  for _ in $(seq 1 60); do
    docker network inspect plsremind >/dev/null 2>&1 || break
    sleep 1
  done
  # A volume can only go once the last container that used it is gone.
  for _ in $(seq 1 30); do
    docker volume rm "${stack}_db" "${stack}_discord" >/dev/null 2>&1 && break
    sleep 1
  done
  if [ -n "$initialised" ]; then
    docker swarm leave --force >/dev/null 2>&1 || true
  fi
  rm -rf "$work"
}
trap cleanup EXIT

fail() {
  echo "swarm: $1" >&2
  docker service ps --no-trunc "${stack}_hub" >&2 || true
  docker service logs --tail 50 "${stack}_hub" >&2 || true
  exit 1
}

expect() {
  local what="$1" got="$2" want="$3"
  [ "$got" = "$want" ] || fail "$what: expected $want, got $got"
  echo "ok  $what"
}

wait_ready() {
  for _ in $(seq 1 "$1"); do
    curl -fsS "${PLSREMIND_PUBLIC_URL}/readyz" >/dev/null 2>&1 && return 0
    sleep 1
  done
  return 1
}

docker stack config -c stack.yaml >/dev/null || fail "stack.yaml is no valid stack file"
if env -u PLSREMIND_SECRET_KEY docker stack config -c stack.yaml >/dev/null 2>&1; then
  fail "stack.yaml starts without PLSREMIND_SECRET_KEY"
fi
echo "ok  stack.yaml is a valid stack file that requires the secret key"

if [ "$(docker info --format '{{.Swarm.LocalNodeState}}')" != active ]; then
  docker swarm init >/dev/null 2>&1 || docker swarm init --advertise-addr 127.0.0.1 >/dev/null
  initialised=1
fi

# A copy on another port, so that the test does not need port 8080 of the machine.
[ "$(grep -c 'published: 8080' stack.yaml)" = 1 ] || fail "stack.yaml does not publish port 8080 exactly once"
sed "s/published: 8080/published: ${port}/" stack.yaml > "$work/stack.yaml"
printf 'services:\n  hub:\n    image: %s\n  discord:\n    image: %s\n' "$hub_image" "$bot_image" > "$work/images.yaml"
printf 'services:\n  db:\n    deploy:\n      replicas: 0\n' > "$work/no-db.yaml"
# Services of one's own in the stack: a global one, as a tunnel or proxy often is, and one that keeps
# holding a connection to the database while it takes its time to stop, as the hub's pool can.
cat > "$work/own.yaml" <<'EOF'
services:
  sidecar:
    image: busybox:1.37
    command: [sleep, "86400"]
    deploy:
      mode: global
  writer:
    image: postgres:18
    command: [sh, -c, 'trap "" INT TERM; while true; do psql -h db -U plsremind -d plsremind -c "SELECT pg_sleep(60)" >/dev/null 2>&1; done']
    environment:
      PGPASSWORD: plsremind
    stop_grace_period: 30s
EOF
deploy() {
  docker stack deploy --detach=true --resolve-image never -c "$work/stack.yaml" -c "$work/images.yaml" -c "$work/own.yaml" "$@" "$stack" >/dev/null
}

# Swarm has no start order: a hub without its database ends and is started again until it is there.
deploy -c "$work/no-db.yaml"
tasks=0
for _ in $(seq 1 90); do
  tasks="$(docker service ps -q "${stack}_hub" | wc -l | tr -d ' ')"
  [ "$tasks" -ge 2 ] && break
  sleep 1
done
[ "$tasks" -ge 2 ] || fail "the hub without a database was not started again"
echo "ok  a hub without its database is started again"
deploy
wait_ready 180 || fail "the hub was not ready three minutes after the database came"
hub_container="$(docker ps -q --filter "label=com.docker.swarm.service.name=${stack}_hub")"
health=""
for _ in $(seq 1 60); do
  health="$(docker inspect -f '{{.State.Health.Status}}' "$hub_container")"
  [ "$health" = healthy ] && break
  sleep 1
done
expect "the hub turns healthy once its database is there" "$health" healthy

expect "the network can be joined" "$(docker network inspect plsremind --format '{{.Attachable}}')" true
expect "a container on the network reaches the hub by its name" \
  "$(docker run --rm --network plsremind busybox:1.37 wget -qO- http://hub:8080/readyz)" ok
expect "the port is published on the host, so that the hub sees client addresses" \
  "$(docker service inspect "${stack}_hub" --format '{{range .Endpoint.Ports}}{{.PublishMode}}{{end}}')" host
expect "the hub stops before its successor starts" \
  "$(docker service inspect "${stack}_hub" --format '{{.Spec.UpdateConfig.Order}}')" stop-first
for service in db discord; do
  expect "the $service stays on a manager" \
    "$(docker service inspect "${stack}_${service}" --format '{{json .Spec.TaskTemplate.Placement.Constraints}}')" \
    '["node.role == manager"]'
done
expect "the bot runs as one" "$(docker service inspect "${stack}_discord" --format '{{.Spec.Mode.Replicated.Replicas}}')" 1

curl -fsS -o /dev/null -c "$jar" -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"swarm test password"}' "$base/setup" || fail "setup failed"
add_item() {
  curl -fsS -o /dev/null -b "$jar" -H 'Content-Type: application/json' -H "Origin: ${PLSREMIND_PUBLIC_URL}" \
    -d "{\"title\":\"$1\"}" "$base/users/@me/items" || fail "could not create the item $1"
}
add_item "before the backup"

# The backup and restore of docs/deploy/backups.md, run as the guide has them.
mkdir "$work/guide"
guide() {
  local commands
  commands="$(python3 snippet.py ../docs/deploy/backups.md "$1")" || fail "docs/deploy/backups.md has no snippet $1"
  (cd "$work/guide" && bash -euo pipefail -c "$commands")
}
guide swarm-backup >/dev/null
[ -s "$work/guide/plsremind.dump" ] || fail "the guide's backup wrote no dump"
expect "the guide's dump is readable by its owner alone" \
  "$(python3 -c 'import os, sys; print(oct(os.stat(sys.argv[1]).st_mode & 0o777))' "$work/guide/plsremind.dump")" 0o600
echo "ok  the guide's backup wrote a dump"
add_item "after the backup"
writer="$(docker ps -q --filter "label=com.docker.swarm.service.name=${stack}_writer")"
guide swarm-restore >/dev/null 2>"$work/restore.err" || { tail -n 5 "$work/restore.err" >&2; fail "the guide's restore failed"; }
restored="$(python3 -c 'import time; print(time.time())')"
for _ in $(seq 1 60); do
  [ "$(docker inspect -f '{{.State.Running}}' "$writer")" = false ] && break
  sleep 1
done
# docker service scale returns while a stopped service's containers still run.
expect "the restore swaps the databases only once the services it stopped have ended" \
  "$(python3 -c 'import calendar, sys, time
stamp, _, fraction = sys.argv[1].rstrip("Z").partition(".")
ended = calendar.timegm(time.strptime(stamp, "%Y-%m-%dT%H:%M:%S")) + float("0." + (fraction or "0"))
late = ended - float(sys.argv[2])
print("yes" if late <= 0 else f"no, ended {late:.3f}s after the restore")' \
    "$(docker inspect -f '{{.State.FinishedAt}}' "$writer")" "$restored")" yes
wait_ready 120 || fail "the hub was not ready two minutes after the restore"
titles="$(curl -fsS -b "$jar" "$base/users/@me/items" | grep -o '"title":"[^"]*"' | sort | tr '\n' ' ')"
case "$titles" in *'"title":"after the backup"'*) fail "the restore kept what came after the backup";; esac
case "$titles" in *'"title":"before the backup"'*) ;; *) fail "the restore lost the item from before the backup: $titles";; esac
expect "the database the restore replaced is kept until it is dropped by hand" \
  "$(docker exec "$(docker ps -q --filter "label=com.docker.swarm.service.name=${stack}_db")" psql --username plsremind \
    --dbname postgres -tA -c "SELECT count(*) FROM pg_database WHERE datname = 'plsremind_replaced'")" 1
expect "the restore scaled the bot back" \
  "$(docker service inspect "${stack}_discord" --format '{{.Spec.Mode.Replicated.Replicas}}')" 1
echo "ok  the guide's restore brought the backup back"

# The monitoring add-on, as a release ships it, joins the stack's network and scrapes the hub.
tar -xzf "$(./pack-monitoring.sh "$work")" -C "$work"
addon up -d victoriametrics >/dev/null 2>&1 || fail "the monitoring add-on did not start next to the stack"
scraped=""
for _ in $(seq 1 60); do
  scraped="$(docker run --rm --network plsremind busybox:1.37 \
    wget -qO- 'http://victoriametrics:8428/api/v1/query?query=up%7Bjob%3D%22plsremind-hub%22%7D' 2>/dev/null \
    | grep -o '"value":\[[^]]*\]' || true)"
  [ "${scraped##*,}" = '"1"]' ] && break
  sleep 2
done
expect "the monitoring add-on scrapes the hub of the stack" "${scraped##*,}" '"1"]'

echo "swarm: all good"
