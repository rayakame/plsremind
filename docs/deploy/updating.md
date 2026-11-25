# Updating

Read the release notes of every release between yours and the new one on the
[releases page](https://github.com/rayakame/plsremind/releases). A release marked as breaking
says what you have to do. [`docs/releases.md`](../releases.md) explains what the numbers mean.

The hub updates its database when it starts; on a large instance that can take a while.

## Within a series

The deploy files name a series, such as `0.2`, and every release of that series is a fix that
asks nothing of you. Updating is pulling the images again:

- **Compose:** `docker compose pull && docker compose up -d`. This covers the bot when
  `COMPOSE_PROFILES` in `.env` names it.
- **Swarm:** run `docker stack deploy -c stack.yaml plsremind` again, with the variables loaded
  as at the start. It asks the registry for the newest image of the series and updates what
  changed.
- **Portainer:** redeploy the stack with re-pulling images on, or call the stack's webhook.

## To a new series

Going from `0.2` to `0.3`, or later from `1` to `2`, is a step you choose:

1. Read the notes of every release in between.
2. [Back up](backups.md).
3. Change the series in both image tags of your deploy file, the hub's and the bot's, from
   `:0.2` to `:0.3`.
4. If the notes name a change to the deploy file, make it in your copy too. Changes like that
   are rare: most new series only change the number.
5. Deploy as within a series.

## Going back

An older hub refuses to start against a database that a newer one has updated, and says so;
the Discord bot does the same with its own file. Going back is restoring the backup from before
the update, see [backups](backups.md#going-back-after-an-update). A bot that refuses can also
start over from nothing: delete its volume, with the consequences described under
[backups](backups.md).
