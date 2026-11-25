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

The hub now listens on port 8080 and applies its database migrations on every start. Open its
address and create the first account, as described in
[docs/self-hosting.md](../self-hosting.md#first-steps). The files of one particular release are
at `https://github.com/rayakame/plsremind/releases/download/v0.2.3/compose.yaml` and so on.

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

- **The port.** The hub is published on port 8080 of the machine. Change the first number of
  `"8080:8080"` for another one.
- **Behind a proxy on the same machine**, publish it on that machine only, as
  `"127.0.0.1:8080:8080"`. See [reverse proxies and tunnels](proxies.md#a-proxy-on-the-same-machine)
  for why.
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
