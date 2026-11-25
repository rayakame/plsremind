# Monitoring

The hub and the Discord bot expose metrics in the Prometheus format, and the hub answers
`/healthz` and `/readyz` on its public port. [`docs/metrics.md`](../metrics.md) lists every
metric. Nothing is sent anywhere by the services themselves, no label names a user or an
item, and the deploy files keep the metrics ports inside the `plsremind` network.

Monitoring is not part of the default deployment. It comes as an add-on that runs next to
it, with everything needed: VictoriaMetrics to collect the metrics, vmalert with the alert
rules, and Grafana with a dashboard.

## Running the add-on

The add-on is a Compose project of its own. It joins the `plsremind` network, so it works
next to the Compose deployment and next to a Swarm stack alike; on a swarm, run it on a node
of that swarm. Every server release carries it as `plsremind-monitoring.tar.gz`:

```sh
curl -fsSL https://github.com/rayakame/plsremind/releases/latest/download/plsremind-monitoring.tar.gz | tar -xz
cd monitoring
cp .env.example .env
sed -i.bak "s|^PLSREMIND_GRAFANA_PASSWORD=.*|PLSREMIND_GRAFANA_PASSWORD=$(openssl rand -hex 16)|" .env && rm .env.bak
docker compose up -d
```

Grafana does not start without the password. If you renamed the `plsremind` network in your
deploy file, rename it in the add-on's `compose.yaml` too.

The add-on has three services:

- **VictoriaMetrics** scrapes the hub and the bots every 15 seconds and keeps 30 days.
- **vmalert** evaluates [`alerts.yml`](../../deploy/monitoring/alerts.yml): the hub or the
  bot down, the ticker stalled, reminders more than two minutes late, server errors, expired
  deliveries to a connection, the bot not connected to the hub, Discord refusing reminders,
  errors in the bot nobody foresaw. With several bots, each is watched and reported on its own.
- **Grafana** opens on `http://127.0.0.1:3000` with the plsremind dashboard, which shows the
  bots one line each and can be narrowed to some of them. Log in as `admin` with
  `PLSREMIND_GRAFANA_PASSWORD`; the password is set when Grafana first starts, so change it
  there afterwards. Grafana is only reachable from the machine itself; to reach it from
  elsewhere, change the port in the add-on's `compose.yaml` or put it behind your reverse
  proxy. Its own telemetry, update checks and plugin downloads are off.

To update the add-on, take the files of the new release in place of the old ones and run
`docker compose up -d` again. Keep your `.env` and your own files in `targets/`. This never
touches the deployment itself.

## Further bots

Every Discord bot is a file in `targets/`. The add-on comes with `targets/discord.yml` for the
bot of the deploy files. For each further bot, add a file of its own with the bot's service
name:

```sh
echo '- targets: ["discord-team:9091"]' > targets/discord-team.yml
```

VictoriaMetrics picks it up within 30 seconds. If you take a further bot away for good,
delete its file, or its alert keeps firing until its last scrape has left the 30 days. If you
run no bot at all, delete `targets/discord.yml` for the same reason.

## Alerts

Firing alerts show at the top of the dashboard and in Grafana's alert list. To have them
sent, run an Alertmanager and, in the add-on's `compose.yaml`, replace vmalert's
`-notifier.blackhole` with `-notifier.url=http://alertmanager:9093` (or wherever yours
listens). vmalert runs inside the `plsremind` network, so the address must be reachable from
there.

## Monitoring you already have

Prometheus, Grafana Alloy or any other tool that reads the Prometheus format works as well.
Inside the `plsremind` network, the add-on's [`scrape.yml`](../../deploy/monitoring/scrape.yml)
serves as its scrape configuration as it is, with the files of `targets/` mounted at
`/etc/plsremind/targets/`. Outside it, publish the metrics ports yourself and use the host's
address and those ports instead of `hub:9090` and `discord:9091`, since the service names
only resolve inside the network. The rules in `alerts.yml` work as they are, and the
dashboard, [`grafana/plsremind.json`](../../deploy/monitoring/grafana/plsremind.json), asks
which Prometheus data source to use when you import it.
