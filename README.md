# plsremind

plsremind keeps your todos and deadlines in one place and reminds you about them.
It is self-hostable and built around a documented API, so you can connect your own
tools to it the same way you would write a bot.

The project is in early development. What exists today is the hub: accounts, login,
lists, items and the complete REST API. Reminder delivery, the dashboard and the
Discord bot are next.

## How it fits together

- **Hub** (`hub/`): the server that holds all logic and data. Everything else talks to its API.
- **Connections**: integrations an admin installs for all users of an instance, such as a
  Discord bot.
- **Adapters**: your own tools, using a personal token that only reaches your data.

## Try it

```sh
cd deploy
echo "PLSREMIND_SECRET_KEY=$(openssl rand -base64 32)" > .env
docker compose up -d
curl http://localhost:8080/api/v1/info
```

See [docs/self-hosting.md](docs/self-hosting.md) for setup and configuration and
[docs/api/openapi.yaml](docs/api/openapi.yaml) for the API.

## Development

The hub is written in Go and uses PostgreSQL through sqlc.

```sh
cd hub
make test      # needs Docker, the tests start their own PostgreSQL
make lint
make generate  # after changing SQL in internal/db
```

## License

MIT
