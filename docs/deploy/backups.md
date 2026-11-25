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
docker compose exec -T db pg_dump --username plsremind --dbname plsremind --format custom > plsremind.dump.partial &&
  mv plsremind.dump.partial plsremind.dump
```

The dump is written under a temporary name first, so that a dump that breaks off never takes
the place of a good one. For a backup every night, with the date in the file's name, a cron
entry like this works (cron needs the `%` escaped):

```sh
0 3 * * * cd /path/to/plsremind && docker compose exec -T db pg_dump --username plsremind --dbname plsremind --format custom > "backups/plsremind-$(date +\%Y\%m\%d).dump"
```

Old dumps are not deleted; remove them yourself.

### Restoring

This replaces everything in the database with `plsremind.dump`:

<!-- snippet: compose-restore -->
```sh
psql() { docker compose exec -T db psql --username plsremind --dbname postgres --quiet -v ON_ERROR_STOP=1 "$@"; }
psql -c 'DROP DATABASE IF EXISTS plsremind_restore' -c 'CREATE DATABASE plsremind_restore OWNER plsremind' &&
  docker compose exec -T db pg_restore --username plsremind --dbname plsremind_restore \
    --no-owner --exit-on-error --single-transaction < plsremind.dump &&
  docker compose stop &&
  docker compose up -d --wait db &&
  psql -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = 'plsremind'" \
    -c 'ALTER DATABASE plsremind RENAME TO plsremind_replaced; ALTER DATABASE plsremind_restore RENAME TO plsremind;' &&
  docker compose up -d &&
  psql -c 'DROP DATABASE plsremind_replaced'
```

What it does, step by step:

1. It restores the dump into a new database, `plsremind_restore`, next to the one in use. If
   the dump cannot be restored completely, it stops here and nothing has changed; the hub has
   kept running.
2. It stops everything that writes to the database (the hub and every bot), starts the
   database alone again, and swaps the two databases in one statement, so that there is never
   a moment without a `plsremind` database.
3. It starts everything again and drops the database it replaced.

If it stops after step 2 has begun, start everything again with `docker compose up -d`. If the
last line fails, the old database is still there as `plsremind_replaced`; drop it once you no
longer need it.

### Going back after an update

The hub updates its database when it starts, and an older hub refuses to start against a
database a newer one has updated. Going back is therefore a restore: set the image tags in
`compose.yaml` back to the series you had, then restore the dump you made before the update,
as above. The restore starts the older hub against the older database.
