# docker run

Without Compose or Swarm, the same containers can be started by hand. This guide is not tested
in CI; the [Compose file](compose.md) is the tested form of the same setup.

```sh
docker network create plsremind
docker run -d --name db --network plsremind --restart unless-stopped \
  -e POSTGRES_USER=plsremind -e POSTGRES_PASSWORD="$POSTGRES_PASSWORD" -e POSTGRES_DB=plsremind \
  -v plsremind-db:/var/lib/postgresql \
  postgres:18
docker run -d --name hub --network plsremind --restart unless-stopped -p 8080:8080 \
  -e PLSREMIND_DATABASE_URL='postgres://plsremind@db:5432/plsremind?sslmode=disable' \
  -e PGPASSWORD="$POSTGRES_PASSWORD" \
  -e PLSREMIND_PUBLIC_URL=https://todo.example.com \
  -e PLSREMIND_SECRET_KEY="$PLSREMIND_SECRET_KEY" \
  -e PLSREMIND_PROXY_HEADER=X-Forwarded-For \
  ghcr.io/rayakame/plsremind:0.0
```

The hub ends while the database is not ready yet and `--restart` starts it again, so the order
does not matter. The container names are the names the others reach them by.

The Discord bot, after the Discord side is set up as in
[docs/self-hosting.md](../self-hosting.md#the-discord-bot):

```sh
docker run -d --name discord --network plsremind --restart unless-stopped --stop-timeout 20 \
  -e DISCORD_TOKEN="$DISCORD_TOKEN" \
  -e PLSREMIND_TOKEN="$PLSREMIND_DISCORD_TOKEN" \
  -e PLSREMIND_URL=http://hub:8080 \
  -e PLSREMIND_PUBLIC_URL=https://todo.example.com \
  -v plsremind-discord:/data \
  ghcr.io/rayakame/plsremind-discord:0.0
```

## Updating

Within a series, pull the image again, then remove the container and run the same command as
before:

```sh
docker pull ghcr.io/rayakame/plsremind:0.0
docker rm -f hub
docker run -d --name hub ...
```

The volumes keep the data. For a new series, see [updating](updating.md).

## Backups

The commands of [backups](backups.md) work with `docker exec -i db` in place of
`docker compose exec -T db`, and `docker stop hub discord` and `docker start hub discord` in
place of stopping and starting the Compose services.
