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

Everything else is configured through the API by an admin and stored in the database.

## First steps

1. **Create the admin.** A new instance accepts exactly one call to `POST /api/v1/setup`:

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

## Backups

All state lives in the PostgreSQL volume. Back it up with `pg_dump`, and keep
`PLSREMIND_SECRET_KEY` with the backup.
