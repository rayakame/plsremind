# Docker Compose

`compose.yaml` runs the hub and PostgreSQL, and the Discord bot when you want it. Every release
carries the file; nothing is built on your machine.

## Start

```sh
mkdir plsremind && cd plsremind
curl -fsSLO https://github.com/rayakame/plsremind/releases/latest/download/compose.yaml
curl -fsSLo .env https://github.com/rayakame/plsremind/releases/latest/download/env.example
# The hub encrypts provider secrets with this key. Keep it with your backups: without it
# the stored secrets cannot be read again.
sed -i.bak "s|^PLSREMIND_SECRET_KEY=.*|PLSREMIND_SECRET_KEY=$(openssl rand -base64 32)|" .env && rm .env.bak
# Then set PLSREMIND_PUBLIC_URL and, behind a proxy, PLSREMIND_PROXY_HEADER in .env.
docker compose up -d
```

The hub now listens on port 8080 of this machine alone and applies its database migrations on
every start. Until the first account exists, whoever reaches the hub first becomes its admin, so
create it from this machine before you start a proxy or tunnel or take the port away: with the
call in [docs/self-hosting.md](../self-hosting.md#first-steps) against `http://localhost:8080`,
or in a browser at that address. The hub refuses a browser whose page is at another address than
`PLSREMIND_PUBLIC_URL`, so for the browser leave that empty until the account exists, then set
it and run `docker compose up -d` again. The files of one particular release are at
`https://github.com/rayakame/plsremind/releases/download/v0.2.3/compose.yaml` and so on.

## The Discord bot

Set up the Discord side first, as described in
[docs/self-hosting.md](../self-hosting.md#the-discord-bot). Then fill in `DISCORD_TOKEN` and
`PLSREMIND_DISCORD_TOKEN` (the `plsr_con_...` token of the app) in `.env`, set
`COMPOSE_PROFILES=discord`, and run `docker compose up -d`. Every `docker compose` command in
the folder then includes the bot, those of an update too.

### Another Discord bot

A second bot is a second app with its own Discord application, token and SQLite file.
[`another-discord-bot.yaml`](https://github.com/rayakame/plsremind/blob/main/deploy/examples/another-discord-bot.yaml)
adds one next to the first, on the same image. Save it next to `compose.yaml`, name it in
`COMPOSE_FILE` in `.env`, and give it its tokens there:

```sh
curl -fsSLo discord-team.yaml https://raw.githubusercontent.com/rayakame/plsremind/main/deploy/examples/another-discord-bot.yaml
echo "COMPOSE_FILE=compose.yaml:discord-team.yaml" >> .env
echo "DISCORD_TEAM_TOKEN=..." >> .env
echo "PLSREMIND_DISCORD_TEAM_TOKEN=plsr_con_..." >> .env
docker compose up -d
```

Once `COMPOSE_FILE` is set, Compose reads only the files it names: if you also have a
`compose.override.yaml`, list it too, as `compose.yaml:compose.override.yaml:discord-team.yaml`.

Every further bot works the same way with its own service name, variables and volume. With
the [monitoring add-on](monitoring.md#further-bots), give each one a target file.

## Adapting compose.yaml

The file is a template; change it to fit your setup:

- **The port.** The hub is published on port 8080 of this machine alone, where a proxy on the
  same machine reaches it. Change the 8080 after `127.0.0.1:` for another port.
- **A proxy on another machine** needs the port on every address, `"8080:8080"`. Open it once
  the first account exists, and to the proxy alone; see
  [reverse proxies and tunnels](proxies.md#a-proxy-on-the-same-machine) for why.
- **No published port.** Reached only through a tunnel, or a proxy on the `plsremind`
  network, the hub needs no port on the machine: delete its `ports`.
- **The network.** Hub, database and bot share the network `plsremind`, which other Compose
  projects can join: a tunnel, a reverse proxy, the monitoring add-on. If you rename it, rename
  it wherever it is joined too. A second instance on the same machine needs a network name of
  its own, or the two would reach each other's database.

Changes can also go into `compose.override.yaml` instead, which Compose reads on its own and
which keeps `compose.yaml` as it came. To take the port away there, `ports: !reset []` under
`hub` works with Compose 2.24.4 or newer.

Another Compose project joins the network like this:

```yaml
networks:
  default:
    name: plsremind
    external: true
```

## Building from source

To run what is in a checkout of the repository instead of a release, for example while working
on plsremind, build the images with the extra compose file. Its network is `plsremind-dev`, so
that it never meets an instance from the release files on the same machine:

```sh
git clone https://github.com/rayakame/plsremind.git
cd plsremind/deploy
echo "PLSREMIND_SECRET_KEY=$(openssl rand -base64 32)" > .env
docker compose -f compose.yaml -f compose.build.yaml up -d --build
```

An instance from the release files on the same machine holds port 8080. Stop it first, or give
this hub another port with a third file, `dev-port.yaml`:

```yaml
services:
  hub:
    ports: !override ["8081:8080"]
```

Add `PLSREMIND_PUBLIC_URL=http://localhost:8081` to `.env` and `-f dev-port.yaml` to the command.
