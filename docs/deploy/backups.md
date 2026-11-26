# Backups

Everything the hub keeps is in its PostgreSQL database, so a backup is a dump of that database,
made with PostgreSQL's own `pg_dump` and put back with `pg_restore`. Back up before every
update to a new series, and regularly besides.

Keep `PLSREMIND_SECRET_KEY` with the backups. The sign-in providers' secrets and the linked
accounts' tokens are encrypted with it; without it, a restored database cannot read them.

The Discord bot keeps a small SQLite file on a volume of its own, which these backups do not
cover. Restored without it, the bot works on: it opens the DM channels again, a reminder still
waiting for its confirmation may arrive a second time, and older messages catch up only when
someone clicks them.

## Docker Compose

Run these in the folder that holds `compose.yaml`.

### Backing up

<!-- snippet: compose-backup -->
```sh
rm -f plsremind.dump.partial &&
  (umask 077 && docker compose exec -T db pg_dump --username plsremind --dbname plsremind --format custom > plsremind.dump.partial) &&
  mv plsremind.dump.partial plsremind.dump
```

The dump holds everything in the database, so only its owner may read it. It is written under
a temporary name first, so that a dump that breaks off never takes the place of a good one.
For a backup every night, with the date in the file's name, create a `backups` folder next to
`compose.yaml` and add a cron entry like this (cron needs the `%` escaped):

```sh
0 3 * * * cd /path/to/plsremind && rm -f backups/partial && (umask 077 && docker compose exec -T db pg_dump --username plsremind --dbname plsremind --format custom > backups/partial) && mv backups/partial "backups/plsremind-$(date +\%Y\%m\%d).dump"
```

Old dumps are not deleted; remove them yourself.

### Restoring

This replaces everything in the database with `plsremind.dump`; to restore another dump, copy
it to that name first:

<!-- snippet: compose-restore -->
```sh
psql() { docker compose exec -T db psql --username plsremind --dbname postgres --quiet -v ON_ERROR_STOP=1 "$@"; }
psql -c 'DROP DATABASE IF EXISTS plsremind_restore' -c 'CREATE DATABASE plsremind_restore OWNER plsremind' &&
  docker compose exec -T db pg_restore --username plsremind --dbname plsremind_restore \
    --no-owner --exit-on-error --single-transaction < plsremind.dump &&
  docker compose stop &&
  docker compose up -d --wait db &&
  psql -c 'DROP DATABASE IF EXISTS plsremind_replaced' \
    -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = 'plsremind'" \
    -c 'ALTER DATABASE plsremind RENAME TO plsremind_replaced; ALTER DATABASE plsremind_restore RENAME TO plsremind;' &&
  docker compose up -d
```

What it does, step by step:

1. It restores the dump into a new database, `plsremind_restore`, next to the one in use. If
   the dump cannot be restored completely, it stops here and nothing has changed; the hub has
   kept running.
2. It stops everything that writes to the database (the hub and every bot), starts the
   database alone again, and swaps the two databases in one statement, so that there is never
   a moment without a `plsremind` database.
3. It starts everything again. The database it replaced stays as `plsremind_replaced`, so that
   you can go back to it if the restored one turns out wrong.

Something that uses the database but runs outside these Compose services keeps running; stop
it before the restore and start it again afterwards, since the databases cannot be swapped
while it holds a connection.

If it stops after step 2 has begun, start everything again with `docker compose up -d`. Once the
hub runs as it should on the restored database, drop the old one (the next restore drops it
too):

```sh
docker compose exec -T db psql --username plsremind --dbname postgres -c 'DROP DATABASE plsremind_replaced'
```

## Docker Swarm

Run these on the node that runs the database, for a stack deployed as `plsremind`. They find the
database's container there by its service, `plsremind_db`. Restoring also scales services,
which only a manager can do; the stack keeps the database on one (see
[several nodes](swarm.md#adapting-stackyaml)).

### Backing up

<!-- snippet: swarm-backup -->
```sh
db="$(docker ps -q --filter label=com.docker.swarm.service.name=plsremind_db)" &&
  rm -f plsremind.dump.partial &&
  (umask 077 && docker exec "$db" pg_dump --username plsremind --dbname plsremind --format custom > plsremind.dump.partial) &&
  mv plsremind.dump.partial plsremind.dump
```

### Restoring

<!-- snippet: swarm-restore -->
```sh
db="$(docker ps -q --filter label=com.docker.swarm.service.name=plsremind_db)"
psql() { docker exec -i "$db" psql --username plsremind --dbname postgres --quiet -v ON_ERROR_STOP=1 "$@"; }
counts="$(docker stack services plsremind --format '{{.Name}} {{.Mode}}' \
  | awk '$2 == "replicated" && $1 != "plsremind_db" { print $1 }' \
  | xargs docker service inspect --format '{{.Spec.Name}}={{.Spec.Mode.Replicated.Replicas}}')"
psql -c 'DROP DATABASE IF EXISTS plsremind_restore' -c 'CREATE DATABASE plsremind_restore OWNER plsremind' &&
  docker exec -i "$db" pg_restore --username plsremind --dbname plsremind_restore \
    --no-owner --exit-on-error --single-transaction < plsremind.dump &&
  echo "$counts" | sed 's/=.*/=0/' | xargs docker service scale &&
  until running="$(echo "$counts" | sed 's/=.*//' | xargs docker service ps --format '{{.CurrentState}}')" &&
    ! printf '%s\n' "$running" | grep -Eq '^(Starting|Running)'; do sleep 1; done &&
  psql -c 'DROP DATABASE IF EXISTS plsremind_replaced' \
    -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = 'plsremind'" \
    -c 'ALTER DATABASE plsremind RENAME TO plsremind_replaced; ALTER DATABASE plsremind_restore RENAME TO plsremind;' &&
  echo "$counts" | xargs docker service scale --detach
```

It works like the one for Compose: it restores next to the database in use and stops there if
that fails; then it scales the stack's other replicated services (the hub, every bot and those
of your own) to 0, waits until their containers have stopped (`docker service scale` returns
earlier), swaps the databases, and scales them back to what they were, keeping the replaced
database as `plsremind_replaced`. Services of your own that run in global mode, a tunnel for
instance, keep running, as does everything outside the stack. The databases cannot be swapped
while one of those holds a connection, so give such a service in the stack replicated mode, and
stop one outside it before the restore and start it again afterwards. If it stops after the
scaling has begun, scale them back: in the same shell with
`echo "$counts" | xargs docker service scale`, or, once that shell is gone, by deploying the
stack again, which sets every service to the replicas its file names. Once the hub runs as it
should, drop the old database:

```sh
docker exec "$(docker ps -q --filter label=com.docker.swarm.service.name=plsremind_db)" \
  psql --username plsremind --dbname postgres -c 'DROP DATABASE plsremind_replaced'
```

## Going back after an update

The hub updates its database when it starts, and an older hub refuses to start against a
database a newer one has updated. Going back is therefore a restore of the dump you made
before the update:

- **Compose:** set the image tags in `compose.yaml` back to the series you had and run the
  restore. It starts the older images itself, against the older database. Do not run
  `docker compose up -d` first: with the bot, it would wait for an older hub that cannot
  start against the newer database.
- **Swarm:** set the image tags in `stack.yaml` back, deploy it, then run the restore. The older
  hub keeps restarting until the restore has swapped the databases.
- **docker run:** run the restore as [docker run](docker-run.md#backups) describes, but at the
  end, in place of `docker start hub`, remove the container and run it again with the older
  tag; the same for the bot. `docker start` would start the newer image, which updates the
  restored database once more.
- **Kubernetes:** while the Deployments are scaled to 0 for the restore, set their images back,
  `kubectl set image deployment/hub hub=ghcr.io/rayakame/plsremind:<series>` and the same for
  `deployment/discord`, then run the restore and scale them back to 1.

If the restore stops before it has swapped the databases, the database is still the newer one,
which the older hub cannot start against. Start the newer series again and go back once the
cause is fixed: set the tags back to it and run `docker compose up -d` or deploy the stack; with
docker run, start the containers you stopped; on Kubernetes, set the images back to it and
scale to 1.

The bot may refuse its own file the same way; deleting its volume lets it start over, with
the consequences described at the top.
