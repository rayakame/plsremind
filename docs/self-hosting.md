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
| `PLSREMIND_PROXY_HEADER` | behind a proxy | Set to `X-Forwarded-For` whenever the hub runs behind a reverse proxy. Without it every visitor appears to come from the proxy and they all share one rate limit, so logging in fails for everyone once a few people try at the same time. The header is only trusted from loopback and private addresses. |

Compose also reads `POSTGRES_PASSWORD` and `PLSREMIND_PORT` from `.env`. Set
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
attach their account there, which is how a connection such as the Discord bot recognises
them. The two are independent.

## Connections

A connection is an integration for all users of the instance. Create one with
`POST /api/v1/admin/connections` and give the returned token to the integration. The token
is shown once; `POST /api/v1/admin/connections/{id}/token` issues a new one.

## The Discord bot

The bot in `connections/discord` sends reminders as direct messages and answers commands
such as `/remind` and `/todos`. It is a connection like any other: it talks to the hub
through the API and the gateway only, and keeps a small SQLite file of its own.

1. **Create a Discord application** in the
   [Developer Portal](https://discord.com/developers/applications). Under Installation,
   allow both "User Install" and "Guild Install" and keep the scope `applications.commands`
   for both. Under Bot, create a token. The bot needs no privileged intents.
2. **Let people link their Discord account.** Configure the login provider `discord` as
   described above with `"link_enabled": true`. The same Discord application can serve
   for this: its client ID and secret are under OAuth2.
3. **Create the connection** in the hub and keep the token it returns:

   ```sh
   curl -b cookies.txt -X POST -H 'Content-Type: application/json' \
     -H 'Origin: https://todo.example.com' \
     -d '{"name":"Discord","provider":"discord","allowed_intents":["users","lists","items","history","deliveries"]}' \
     https://todo.example.com/api/v1/admin/connections
   ```

4. **Start the bot** next to the hub:

   ```sh
   echo "DISCORD_TOKEN=..." >> .env
   echo "PLSREMIND_DISCORD_TOKEN=plsr_con_..." >> .env
   docker compose --profile discord up -d
   ```

Users then add the application to their account with its install link, link their Discord
account in the dashboard under Settings, and get their reminders as direct messages.

| Variable | Required | Meaning |
|---|---|---|
| `DISCORD_TOKEN` | yes | The bot token of the Discord application. |
| `PLSREMIND_TOKEN` | yes | The token of the connection. Compose reads it from `PLSREMIND_DISCORD_TOKEN`. |
| `PLSREMIND_URL` | yes | Where the bot reaches the hub. Compose sets it to `http://hub:8080`. |
| `PLSREMIND_PUBLIC_URL` | yes | The address of the dashboard, for the links in the bot's messages. |
| `PLSREMIND_DISCORD_DB` | no | Where the bot keeps its SQLite file. In the image it is `/data/discord.db`, on a volume. |

The file holds which reminders went out, which message shows which item and the DM
channels of the accounts reminded in the last 30 days. If it is lost, the DM channels are
opened again, a reminder that was still waiting for its confirmation may arrive a second
time, and older messages only catch up when someone clicks them. Nothing else depends on it.

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

With the Discord bot, name its profile, or the bot keeps running its old image:

```sh
docker compose --profile discord up -d --build
```

## Backups

The hub's state lives in the PostgreSQL volume. Back it up with `pg_dump`, and keep
`PLSREMIND_SECRET_KEY` with the backup. The Discord bot keeps its SQLite file on a volume
of its own, which this backup does not cover. Restored without it, the bot works on, as
described in its section: a reminder still waiting for its confirmation may arrive a
second time, and older messages catch up only when someone clicks them.
