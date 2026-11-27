# plsremind

plsremind keeps your todos and deadlines in one place and reminds you about them.
It is self-hostable and built around a documented API, so you can connect your own
tools to it the same way you would write a bot.

The project is in early development. What exists today is the hub (accounts, login,
lists, items, reminders with confirmed delivery, the REST API and the WebSocket gateway),
a dashboard in the browser, and SDKs for TypeScript and Python to build on. The Discord
bot is next.

## How it fits together

- **Hub** (`hub/`): the server that holds all logic and data. Everything else talks to its API.
- **Dashboard** (`web/`): the web interface, served by the hub. It keeps everything in
  memory through the gateway, so nothing waits for the network after the first load.
  See [web](web/README.md).
- **TypeScript SDK** (`sdk/ts/`): a client for browsers and Node that speaks the gateway,
  keeps the state in memory and wraps the REST API. The dashboard is built on it.
  See [sdk/ts](sdk/ts/README.md).
- **Python SDK** (`sdk/python/`): an asyncio client with listeners, a cache of frozen
  objects and the REST API as methods. See [sdk/python](sdk/python/README.md).
- **Connections**: integrations an admin installs for all users of an instance. The
  Discord bot (`connections/discord/`) sends reminders as direct messages and answers
  commands. See [connections/discord](connections/discord/README.md).
- **Adapters**: your own tools, using a personal token that only reaches your data.

## Try it

Every release ships its images and deploy files for Docker Compose and Docker Swarm; nothing
is built on your machine. [docs/deploy/compose.md](docs/deploy/compose.md#start) has the
commands to start; then open http://localhost:8080 and create the first account. Other ways,
Portainer and Kubernetes among them, are in [docs/deploy](docs/deploy/README.md). To run a
checkout instead, see [Building from source](docs/deploy/compose.md#building-from-source).

See [docs/self-hosting.md](docs/self-hosting.md) for setup and configuration,
[docs/api/openapi.yaml](docs/api/openapi.yaml) for the REST API and
[docs/gateway.md](docs/gateway.md) for the gateway. A small example adapter lives in
[hub/examples/remind](hub/examples/remind/main.go).

## Development

The hub is written in Go and uses PostgreSQL through sqlc.

```sh
cd hub
make test      # needs Docker, the tests start their own PostgreSQL
make lint
make generate  # after changing SQL in internal/store
make web       # builds the dashboard into the binary; needs "npm ci" at the root first
```

Without `make web` the binary serves a placeholder page instead of the dashboard. The
Docker image always contains it.

The TypeScript SDK and the dashboard live in one npm workspace.

```sh
npm install
cd sdk/ts
npm run check
npm test
npm run test:hub  # needs Docker and Go, starts a real hub

cd ../../web
npm run dev       # against a hub on localhost:8080
npm run check
npm test
npm run test:e2e  # needs Docker and Go, drives a browser against a real hub
```

The Python SDK uses [uv](https://docs.astral.sh/uv/).

```sh
cd sdk/python
uv run --group nox nox         # format, lint, type check, unit tests
uv run --group nox nox -s hub  # needs Docker and Go, starts a real hub
```

## License

MIT
