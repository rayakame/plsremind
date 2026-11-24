# Self-hosting plsremind

The hub is a single program that needs a PostgreSQL database. The quickest way to run
both is Docker Compose.

## Before you start

The hub speaks plain HTTP on port 8080 and expects a reverse proxy in front of it that
terminates HTTPS, for example Caddy, Traefik or nginx. Point the proxy at port 8080 and
use the address people will type into their browser as `PLSREMIND_PUBLIC_URL`. The
examples below use `https://todo.example.com`.

To try the hub on your own machine first, leave `PLSREMIND_PUBLIC_URL` out. It then
defaults to `http://localhost:8080`, and every example works with that address instead.

## Start

```sh
git clone https://github.com/rayakame/plsremind.git
cd plsremind/deploy

# The hub encrypts provider secrets with this key. Keep it: without it the stored
# secrets cannot be read again.
echo "PLSREMIND_SECRET_KEY=$(openssl rand -base64 32)" > .env
echo "PLSREMIND_PUBLIC_URL=https://todo.example.com" >> .env
echo "PLSREMIND_PROXY_HEADER=X-Forwarded-For" >> .env

docker compose up -d
```

The hub now listens on port 8080 and applies its database migrations on every start.

## Settings

| Variable | Required | Meaning |
|---|---|---|
| `PLSREMIND_DATABASE_URL` | yes | PostgreSQL connection string. Set by the compose file. |
| `PLSREMIND_PUBLIC_URL` | yes | The address users reach the hub at, without a trailing slash. Login redirects and the browser security check depend on it being exact. |
| `PLSREMIND_SECRET_KEY` | yes | 32 random bytes, base64 encoded. |
| `PLSREMIND_LISTEN_ADDR` | no | Defaults to `:8080`. |
| `PLSREMIND_METRICS_ADDR` | no | Where the hub serves `/metrics`, for example `:9090`. Off when empty. Compose sets it inside its network only. See [Monitoring](#monitoring). |
| `PLSREMIND_PROXY_HEADER` | behind a proxy | Set to `X-Forwarded-For` whenever the hub runs behind a reverse proxy. Without it every visitor appears to come from the proxy and they all share one rate limit, so logging in fails for everyone once a few people try at the same time. The header is only trusted from loopback and private addresses. |

Compose also reads `POSTGRES_PASSWORD` and `PLSREMIND_PORT` from `.env`, and the variables
of the monitoring profile listed under [Monitoring](#monitoring). Set
`POSTGRES_PASSWORD` before the first start: the database keeps the password it was
created with.

Everything else is configured by an admin in the dashboard under Settings, Administration,
or through the API, and stored in the database.

## First steps

1. **Create the admin.** Open the hub's address in a browser. A new instance asks for its
   first account, which becomes the admin. The same can be done without a browser: a new
   instance accepts exactly one call to `POST /api/v1/setup`:

   ```sh
   curl -c cookies.txt -H 'Content-Type: application/json' \
     -d '{"username":"admin","password":"choose a long one"}' \
     https://todo.example.com/api/v1/setup
   ```

   The response sets a session cookie. Requests that change something with that cookie
   must carry an `Origin` header equal to `PLSREMIND_PUBLIC_URL`, as a browser would send.

2. **Decide who may register.** New instances are invite-only. Create invites with
   `POST /api/v1/admin/invites`, or open registration with
   `PATCH /api/v1/admin/settings` and `{"registration":"open"}`.

3. **Add login providers.** Username and password work out of the box. GitHub, Discord and
   any OpenID Connect provider are configured with `PUT /api/v1/admin/providers/{key}`.

## Login providers

Register the hub as an OAuth application at the provider and use this redirect address:

```
{PLSREMIND_PUBLIC_URL}/api/v1/auth/oauth/{key}/callback
```

| Provider | Key | Where to register |
|---|---|---|
| GitHub | `github` | Settings → Developer settings → OAuth Apps |
| Discord | `discord` | Developer Portal → your application → OAuth2 |
| OpenID Connect | your choice | Your identity provider, for example Authentik or Keycloak |

Then hand the hub the credentials:

```sh
curl -b cookies.txt -X PUT -H 'Content-Type: application/json' \
  -H 'Origin: https://todo.example.com' \
  -d '{"client_id":"...","client_secret":"...","login_enabled":true,"link_enabled":true}' \
  https://todo.example.com/api/v1/admin/providers/discord
```

An OpenID Connect provider additionally needs `"type":"oidc"`, a `"name"` and its
`"issuer_url"`.

`login_enabled` lets people log in through the provider. `link_enabled` lets logged-in users
attach their account there, which is how an app such as the Discord bot recognises them.
The two are independent. One provider serves every app of its kind: a single Discord
provider links accounts for all Discord bots of the instance.

## Apps

An app is an integration for all users of the instance, such as a Discord bot. There can
be any number of them, several of the same kind too. An admin adds them in the dashboard
under Settings, Administration, Apps, or with `POST /api/v1/admin/connections` (in the API
an app is a connection), and gives the returned token to the app. The token is shown
once; `POST /api/v1/admin/connections/{id}/token` issues a new one. An `install_url` tells
users where to add the app on the provider's side; for a Discord bot the dashboard makes it
from the bot's Application ID.

Each user decides per app what it may do for them, under Settings, Apps, and can send a
test reminder through it to see that it reaches them. Users who want something of their
own build it with a personal token instead.

## The Discord bot

The bot in `connections/discord` sends reminders as direct messages and answers commands
such as `/remind` and `/todos`. It is a connection like any other: it talks to the hub
through the API and the gateway only, and keeps a small SQLite file of its own.

1. **Create a Discord application** in the
   [Developer Portal](https://discord.com/developers/applications). Under Installation,
   allow both "User Install" and "Guild Install" and keep the scope `applications.commands`
   for both. Set the Install Link to "Custom URL" with `https://todo.example.com/link/discord`,
   so that adding the app in Discord links the account in plsremind too. Under Bot, create a
   token. The bot needs no privileged intents.
2. **Let people link their Discord account** with the same application. Configure the
   login provider `discord` as described above with `"link_enabled": true`, with the client
   ID and secret under OAuth2. Once the bot is an app with this Application ID (next step),
   signing in and linking with Discord ask for the bot's scope as well, so linking adds the
   bot to the user's account. It has to be the same application: Discord keeps only the
   newest grant a user gave an application, and a sign-in through another one would not add
   the bot. A Discord sign-in without a bot asks for `identify` alone.
3. **Add the bot as an app** in the dashboard under Settings, Administration, Apps, with
   its Application ID, or with the API, and keep the token it returns:

   ```sh
   curl -b cookies.txt -X POST -H 'Content-Type: application/json' \
     -H 'Origin: https://todo.example.com' \
     -d '{"name":"Discord","provider":"discord","allowed_intents":["users","lists","items","history","deliveries"],"install_url":"https://discord.com/oauth2/authorize?client_id=<Application ID>&integration_type=1&scope=applications.commands"}' \
     https://todo.example.com/api/v1/admin/connections
   ```

   A bot that was added without its Application ID, for example before apps had an
   install link, shows users no button, and linking does not add it: enter the ID once
   under Settings, Administration, Apps.

4. **Start the bot** next to the hub:

   ```sh
   echo "DISCORD_TOKEN=..." >> .env
   echo "PLSREMIND_DISCORD_TOKEN=plsr_con_..." >> .env
   docker compose --profile discord up -d
   ```

Users then link their Discord account in the dashboard under Settings, Apps, which adds the
bot to their Discord account, and get their reminders as direct messages.

| Variable | Required | Meaning |
|---|---|---|
| `DISCORD_TOKEN` | yes | The bot token of the Discord application. |
| `PLSREMIND_TOKEN` | yes | The token of the connection. Compose reads it from `PLSREMIND_DISCORD_TOKEN`. |
| `PLSREMIND_URL` | yes | Where the bot reaches the hub. Compose sets it to `http://hub:8080`. |
| `PLSREMIND_PUBLIC_URL` | yes | The address of the dashboard, for the links in the bot's messages. |
| `PLSREMIND_DISCORD_DB` | no | Where the bot keeps its SQLite file. In the image it is `/data/discord.db`, on a volume. |
| `PLSREMIND_DISCORD_METRICS_ADDR` | no | `host:port` for the bot's `/metrics`, `/healthz` and `/readyz`, for example `0.0.0.0:9091`. Off when empty. Compose sets it inside its network only. |

The file holds which reminders went out, which message shows which item and the DM
channels of the accounts reminded in the last 30 days. If it is lost, the DM channels are
opened again, a reminder that was still waiting for its confirmation may arrive a second
time, and older messages only catch up when someone clicks them. Nothing else depends on it.

### Another Discord bot

A second bot is a second app with its own Discord application, token and SQLite file. The
Discord sign-in stays with the first bot's application, so users add the second bot with its
own button under Settings, Apps.
[`deploy/examples/another-discord-bot.yaml`](../deploy/examples/another-discord-bot.yaml)
adds one next to the first. Copy it to `compose.override.yaml`, which Compose reads on its
own, so that the bot is also rebuilt when you update:

```sh
cp examples/another-discord-bot.yaml compose.override.yaml
echo "DISCORD_TEAM_TOKEN=..." >> .env
echo "PLSREMIND_DISCORD_TEAM_TOKEN=plsr_con_..." >> .env
docker compose --profile discord up -d
```

For the monitoring, give it a file of its own next to
[`deploy/monitoring/targets/discord.yml`](../deploy/monitoring/targets/discord.yml):

```sh
echo '- targets: ["discord-team:9091"]' > monitoring/targets/discord-team.yml
```

VictoriaMetrics picks it up within 30 seconds. Every further bot works the same way with its
own service name, variables, volume and target file.

## Monitoring

The hub and the bot can expose metrics in the Prometheus format, and the hub answers
`/healthz` and `/readyz` on its public port. [`docs/metrics.md`](metrics.md) lists every
metric. Nothing is sent anywhere by the services themselves, no label names a user or an
item, and the compose file keeps the metrics ports inside its own network.

The compose file uses `/readyz` as the hub's healthcheck, and the Discord bot starts only
once the hub is healthy.

For a dashboard and alerts, set a password for Grafana and start the `monitoring`
profile, alone or beside `discord`. Without the password Grafana does not start:

```sh
echo "PLSREMIND_GRAFANA_PASSWORD=$(openssl rand -hex 16)" >> .env
docker compose --profile monitoring up -d
```

It adds three services:

- **VictoriaMetrics** scrapes the hub and the bots every 15 seconds and keeps 30 days.
- **vmalert** evaluates [`deploy/monitoring/alerts.yml`](../deploy/monitoring/alerts.yml):
  the hub or the bot down, the ticker stalled, reminders more than two minutes late,
  server errors, expired deliveries to a connection, the bot not connected to the hub,
  Discord refusing reminders, errors in the bot nobody foresaw. With several bots, each is
  watched and reported on its own. If you take a further bot away for good, delete its
  file in `monitoring/targets/`, or its alert keeps firing until its last scrape has left
  the 30 days. The bot of `compose.yaml` keeps `targets/discord.yml`; if you stop running
  it, its alert ends once that last scrape is 30 days old, or at once if you delete the
  file (an update brings it back).
- **Grafana** opens on `http://127.0.0.1:3000` (`PLSREMIND_GRAFANA_PORT` to change it)
  with the plsremind dashboard, which shows the bots one line each and can be narrowed to
  some of them. Log in as `admin` with `PLSREMIND_GRAFANA_PASSWORD`; the
  password is set when Grafana first starts, so change it there afterwards. Grafana is
  only reachable from the machine itself; put it behind your reverse proxy to reach it
  from elsewhere. Its own telemetry, update checks and plugin downloads are off.

Firing alerts show at the top of the dashboard and in Grafana's alert list. To have them
sent, run an Alertmanager and set `PLSREMIND_ALERTMANAGER_URL`; vmalert then passes every
alert on to it. vmalert runs inside the compose network, so the address must be reachable
from there: a public address, or a service added in an override file, such as
`http://alertmanager:9093`.

| Variable | Required | Meaning |
|---|---|---|
| `PLSREMIND_GRAFANA_PASSWORD` | for the profile | The password of Grafana's `admin`. Grafana does not start without it. |
| `PLSREMIND_GRAFANA_PORT` | no | The port Grafana is published on, on `127.0.0.1`. Defaults to `3000`. |
| `PLSREMIND_ALERTMANAGER_URL` | no | Where vmalert sends alerts. Without it they are only shown. |

Prometheus, Grafana Alloy or any other tool that reads the Prometheus format works as
well. Inside the compose network,
[`deploy/monitoring/scrape.yml`](../deploy/monitoring/scrape.yml) serves as its scrape
configuration as it is, with the target files of `monitoring/targets/` mounted at
`/etc/plsremind/targets/`. Outside it, publish the metrics ports yourself and replace
`hub:9090` and `discord:9091` with the host's address and those ports, since the service
names only resolve inside the network. The rules work as they are, and the dashboard asks which
Prometheus data source to use.

## Behind a reverse proxy

The gateway is a WebSocket at `/api/v1/gateway`. The proxy has to pass the upgrade
through and must not cut idle connections after less than a minute; clients send a
heartbeat every 45 seconds. Caddy does both on its own. For nginx, set
`proxy_http_version 1.1`, forward the `Upgrade` and `Connection` headers and raise
`proxy_read_timeout` to at least 75 seconds.

## API documentation

The REST API is described in [`docs/api/openapi.yaml`](api/openapi.yaml). Run `make docs` in
`hub/` to render it to `docs/api/index.html`. The gateway is described in
[`docs/gateway.md`](gateway.md).

## Updating

```sh
git pull
docker compose up -d --build
```

Name the profiles you use, or their services keep running their old images. With the
Discord bot and the monitoring, for example:

```sh
docker compose --profile discord --profile monitoring up -d --build
```

## Backups

The hub's state lives in the PostgreSQL volume. Back it up with `pg_dump`, and keep
`PLSREMIND_SECRET_KEY` with the backup. The Discord bot keeps its SQLite file on a volume
of its own, which this backup does not cover. Restored without it, the bot works on, as
described in its section: it opens the DM channels again, a reminder still waiting for its
confirmation may arrive a second time, and older messages catch up only when someone
clicks them.
