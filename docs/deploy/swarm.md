# Docker Swarm and Portainer

`stack.yaml` runs the hub, PostgreSQL and the Discord bot as a Docker Swarm stack. Every release
carries the file. It differs from the Compose file where Swarm differs from Compose:

- Swarm has no start order. The hub ends while the database is not there yet, the bot while
  the hub is not, and Swarm starts them again until it is.
- The hub's port is published in host mode, so that the hub sees the client's address. Swarm's
  usual way would show it an address of Swarm's own.
- The database and the bot keep their data on volumes, which stay on the node they were made
  on. Swarm does not move a volume with its service: started on another node, a service finds
  a new, empty volume there. So the file keeps the two on a manager node.
- `docker stack deploy` does not read a `.env` file; the variables come from the environment it
  runs in, or from Portainer.

The commands in the guides, the backups for instance, take the stack to be called `plsremind`.

## The first account

Until the first account exists, whoever reaches the hub first becomes its admin, and
`stack.yaml` publishes the hub on every address of the node it runs on. So keep port 8080
closed to others until then, with a firewall in front of the node: one on the node itself, ufw
for instance, does not hold back ports that Docker publishes. Create the account on that node,
at `http://localhost:8080`, as described in
[docs/self-hosting.md](../self-hosting.md#first-steps). Start a proxy or tunnel
([reverse proxies and tunnels](proxies.md)), open the port or take it away only afterwards.

## With the docker CLI

```sh
mkdir plsremind && cd plsremind
curl -fsSLO https://github.com/rayakame/plsremind/releases/latest/download/stack.yaml
curl -fsSLo .env https://github.com/rayakame/plsremind/releases/latest/download/env.example
sed -i.bak "s|^PLSREMIND_SECRET_KEY=.*|PLSREMIND_SECRET_KEY=$(openssl rand -base64 32)|" .env && rm .env.bak
# Then set PLSREMIND_PUBLIC_URL, PLSREMIND_PROXY_HEADER and the bot's tokens in .env,
# and adapt stack.yaml as below.
set -a; . ./.env; set +a
docker stack deploy -c stack.yaml plsremind
```

Sourcing the file runs it as shell, so put every value in single quotes that holds anything but
letters, digits and `_-.:/+=` (Compose reads such values the same way). A value with a single
quote in it cannot be written so that both read it alike; choose passwords without one,
`openssl rand -hex 24` makes such a password. `COMPOSE_PROFILES` in that file means nothing to
Swarm; the bot runs when `stack.yaml` has it. [Settings](settings.md#the-deploy-files) lists the
variables. The files of one particular release are at
`https://github.com/rayakame/plsremind/releases/download/v0.2.3/stack.yaml` and so on.

## With Portainer

**From the web editor.** Under Stacks, add a stack named `plsremind`, paste `stack.yaml` into
the web editor and adapt it there. Add the variables under Environment variables (see
[settings](settings.md#the-deploy-files)), or load the filled-in `env.example` as a `.env` file.
Deploy.

**From a Git repository.** Keep your adapted copy of `stack.yaml` in a repository of your own
and add the stack from that repository. With Portainer's automatic updates, by polling or by
webhook, a change you push to the file is deployed on its own; with re-pulling images on, each
redeploy also takes the newest image of the series. Keep the secret key and the tokens in
Portainer's environment variables, not in the repository.

## Adapting stack.yaml

The file is a template; change it to fit your setup:

- **Without a bot**, delete the `discord` service. Left in without its tokens, it ends and is
  started again over and over.
- **No published port.** Reached only through a tunnel, or a proxy in the swarm on the
  `plsremind` network, the hub needs no port on the node: delete its `ports`. See
  [reverse proxies and tunnels](proxies.md).
- **The port.** Change `published: 8080` for another one.
- **The network.** Hub, database and bot share the overlay network `plsremind`, which other
  stacks and containers can join. If you rename it, rename it wherever it is joined too.
- **Several nodes.** The file keeps the database and the bot on a manager, which is enough
  with one manager. With several, pin both to one node, or a restart may bring them up on
  another manager with empty volumes. Label a manager, since restoring a backup scales
  services, which only a manager can do:

  ```sh
  docker node update --label-add plsremind.data=true '<node>'
  ```

  and in `stack.yaml`, for `db` and `discord`:

  ```yaml
  placement:
    constraints: ["node.labels.plsremind.data == true"]
  ```

  Moving them to another node later means moving their data: back up, deploy on the new
  node, restore. A published port exists only on the node that runs the hub, and moves with
  it; pin the hub to that node as well, or reach it through a tunnel or a proxy on the
  `plsremind` network.

### Another Discord bot

A second bot is a second app with its own Discord application, token and SQLite file. Add a
service of its own to your copy of `stack.yaml`, next to `discord`:

```yaml
  discord-team:
    image: ghcr.io/rayakame/plsremind-discord:0.0
    stop_grace_period: 20s
    environment:
      DISCORD_TOKEN: ${DISCORD_TEAM_TOKEN:-}
      PLSREMIND_TOKEN: ${PLSREMIND_DISCORD_TEAM_TOKEN:-}
      PLSREMIND_URL: http://hub:8080
      PLSREMIND_PUBLIC_URL: ${PLSREMIND_PUBLIC_URL:-http://localhost:8080}
      PLSREMIND_DISCORD_METRICS_ADDR: "0.0.0.0:9091"
    volumes:
      - discord-team:/data
    deploy:
      replicas: 1
      placement:
        constraints: ["node.role == manager"]
      update_config:
        order: stop-first
      restart_policy:
        condition: any
        delay: 5s
```

and `discord-team:` under `volumes`, and set `DISCORD_TEAM_TOKEN` and
`PLSREMIND_DISCORD_TEAM_TOKEN` where the other variables are. With several managers, pin it to
the labelled node like `discord` (see [several nodes](#adapting-stackyaml)), or a restart may
bring it up elsewhere with an empty volume. With the
[monitoring add-on](monitoring.md#further-bots), give it a target file.
