# Docker Swarm and Portainer

`stack.yaml` runs the hub, PostgreSQL and the Discord bot as a Docker Swarm stack. Every release
carries the file. It differs from the Compose file where Swarm differs from Compose:

- Swarm has no start order. The hub ends while the database is not there yet, the bot while
  the hub is not, and Swarm starts them again until it is.
- The hub's port is published in host mode, so that the hub sees the client's address. Swarm's
  usual way would show it an address of Swarm's own.
- The database and the bot keep their data on volumes, which stay on the node they were made
  on, so the two run on a manager node.
- `docker stack deploy` does not read a `.env` file; the variables come from the environment it
  runs in, or from Portainer.

The commands in the guides, the backups for instance, take the stack to be called `plsremind`.

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

`COMPOSE_PROFILES` in that file means nothing to Swarm; the bot runs when `stack.yaml` has it.

## With Portainer

**From the web editor.** Under Stacks, add a stack named `plsremind`, paste `stack.yaml` into
the web editor and adapt it there. Add the variables under Environment variables, or load the
filled-in `env.example` as a `.env` file. Deploy.

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
- **Several nodes.** Their volumes keep the database and the bot on the node they first ran on.
  The file keeps them on a manager, which is enough with one manager. With several, label the
  node that should hold them and pin both services to it:

  ```sh
  docker node update --label-add plsremind.data=true <node>
  ```

  and in `stack.yaml`, for `db` and `discord`:

  ```yaml
  placement:
    constraints: ["node.labels.plsremind.data == true"]
  ```

  Moving them to another node later means moving their data: back up, deploy on the new
  node, restore.

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

and `discord-team:` under `volumes`. With the [monitoring add-on](monitoring.md#further-bots),
give it a target file.
