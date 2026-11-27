# Deploying plsremind

plsremind is two images and a PostgreSQL database:

- `ghcr.io/rayakame/plsremind`: the hub, with the dashboard. It needs PostgreSQL.
- `ghcr.io/rayakame/plsremind-discord`: the Discord bot. It is optional and talks to the hub
  like any other app.

Both run for amd64 and arm64. How you run them is up to you; these are the ways described here:

| Way | What you get | Tested |
|---|---|---|
| [Docker Compose](compose.md) | `compose.yaml` from each release | on every change, in CI |
| [Docker Swarm and Portainer](swarm.md) | `stack.yaml` from each release | on every change, in CI |
| [Kubernetes](kubernetes.md) | a guide with plain manifests | no |
| [`docker run`](docker-run.md) | a guide | no |

## What every way needs

- **An address people reach.** The hub speaks plain HTTP on port 8080 and expects something in
  front of it that terminates HTTPS: a reverse proxy such as Caddy, Traefik or nginx, or a
  tunnel such as Cloudflare Tunnel. See [reverse proxies and tunnels](proxies.md). The address
  people type into their browser is `PLSREMIND_PUBLIC_URL`; the examples use
  `https://todo.example.com`. To try plsremind on your own machine first, leave it empty in
  the Compose and Swarm files, which then use `http://localhost:8080`; with `docker run` or on
  Kubernetes, set it to that yourself.
- **A secret key.** `PLSREMIND_SECRET_KEY`, 32 random bytes, base64 encoded, for example from
  `openssl rand -base64 32`. Keep it with your backups.
- **PostgreSQL 18**, as in the deploy files.
- **One hub.** It updates the database when it starts, and two hubs must never do that at the
  same time.
- **One bot per Discord application**, each with a volume of its own for its SQLite file.

## The deploy files are templates

`compose.yaml` and `stack.yaml` are meant to be adapted to your setup by editing them: the
port, the network's name, where the services run, whether the bot runs. What the programs
themselves read, the secret key, the addresses, the tokens, are environment variables (see
[settings](settings.md)), so the files hold no secrets and can live in a Git repository of
your own.

Within a release series the files never change in a way you need to follow. A change you need
comes with a new series, and its release notes name it.

## Image tags

| Tag | Points at |
|---|---|
| `0.2` | the newest release of the series 0.2, for example; a series tag is what the deploy files name |
| `0.2.3` | that one release, for good |
| `latest` | the newest release, also of a newer series |
| `1` | from 1.0 on, the newest release of 1.x |
| `edge` | the newest build of `main`; not for an instance anyone relies on |

Use the series. Pulling it again brings every fix of that series, and nothing that asks you
to do anything. Before 1.0 a series is the first two numbers, because a release with new
features or breaking changes raises the middle one; from 1.0 on it is the first number alone.
[`docs/releases.md`](../releases.md) explains the numbers.

## More

- [Updating](updating.md)
- [Backups](backups.md)
- [Monitoring](monitoring.md)
- [Settings](settings.md)
- [Reverse proxies and tunnels](proxies.md)
- After the start: the first account, login providers, apps and the Discord bot's side in
  Discord are in [docs/self-hosting.md](../self-hosting.md).
