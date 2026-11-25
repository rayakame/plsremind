# Settings

The hub and the bot read their settings from environment variables. Everything else is
configured by an admin in the dashboard under Settings, Administration, or through the API,
and stored in the database.

## The hub

| Variable | Required | Meaning |
|---|---|---|
| `PLSREMIND_DATABASE_URL` | yes | PostgreSQL connection string. Set by the deploy files. |
| `PLSREMIND_PUBLIC_URL` | yes | The address users reach the hub at, without a trailing slash. Login redirects and the browser security check depend on it being exact. |
| `PLSREMIND_SECRET_KEY` | yes | 32 random bytes, base64 encoded. The sign-in providers' secrets and the linked accounts' tokens are encrypted with it; keep it with your backups. |
| `PLSREMIND_LISTEN_ADDR` | no | Defaults to `:8080`. |
| `PLSREMIND_METRICS_ADDR` | no | Where the hub serves `/metrics`, for example `:9090`. Off when empty. The deploy files set it inside their network only. See [monitoring](monitoring.md). |
| `PLSREMIND_PROXY_HEADER` | behind a proxy | The header that carries the client's address: `X-Forwarded-For` behind most reverse proxies, `CF-Connecting-IP` behind a Cloudflare Tunnel. Without it every visitor appears to come from the proxy and they all share one rate limit, so logging in fails for everyone once a few people try at the same time. The header is only believed from loopback and private addresses. See [reverse proxies and tunnels](proxies.md). |

The hub also reads PostgreSQL's own variables, such as `PGPASSWORD`. The deploy files pass the
database's password that way, so that it may contain any character.

## The Discord bot

| Variable | Required | Meaning |
|---|---|---|
| `DISCORD_TOKEN` | yes | The bot token of the Discord application. |
| `PLSREMIND_TOKEN` | yes | The token of the app in plsremind (`plsr_con_...`). The deploy files read it from `PLSREMIND_DISCORD_TOKEN`. |
| `PLSREMIND_URL` | yes | Where the bot reaches the hub. The deploy files set it to `http://hub:8080`. |
| `PLSREMIND_PUBLIC_URL` | yes | The address of the dashboard, for the links in the bot's messages. |
| `PLSREMIND_DISCORD_DB` | no | Where the bot keeps its SQLite file. In the image it is `/data/discord.db`, on a volume. |
| `PLSREMIND_DISCORD_METRICS_ADDR` | no | `host:port` for the bot's `/metrics`, `/healthz` and `/readyz`, for example `0.0.0.0:9091`. Off when empty. The deploy files set it inside their network only. |

## The deploy files

`compose.yaml` and `stack.yaml` read these and hand them on. With Compose they go into `.env`
next to `compose.yaml`; with Swarm into the environment `docker stack deploy` runs in, or into
Portainer's environment variables.

| Variable | Meaning |
|---|---|
| `PLSREMIND_SECRET_KEY` | Required. Handed to the hub. |
| `PLSREMIND_PUBLIC_URL` | Handed to the hub and the bot. Defaults to `http://localhost:8080`. |
| `PLSREMIND_PROXY_HEADER` | Handed to the hub. |
| `POSTGRES_PASSWORD` | The database's password, for PostgreSQL and the hub. Set it before the first start: the database keeps the password it was created with. |
| `DISCORD_TOKEN`, `PLSREMIND_DISCORD_TOKEN` | Handed to the bot. |
| `COMPOSE_PROFILES` | Compose only: `discord` runs the bot. |

The port, the network's name and where the services run are written into the files; change
them there.
