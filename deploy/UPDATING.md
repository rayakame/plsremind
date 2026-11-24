# Updating plsremind

1. Read the release notes of every release between yours and the new one:
   https://github.com/rayakame/plsremind/releases. Releases marked as breaking say what to do.
2. Back up the database: `./backup.sh`. It prints the file it wrote.
3. Unpack the new bundle over this folder:
   `tar -xzf plsremind-deploy-<version>.tar.gz --strip-components=1`
   This replaces the files that came with plsremind and keeps yours: `.env`,
   `compose.override.yaml`, `backups/` and your own files in `monitoring/targets/`.
4. `docker compose pull` and `docker compose up -d`, with the profiles you use
   (for example `docker compose --profile discord --profile monitoring up -d`).
   The hub updates the database when it starts; that can take a while on a large instance.

If something goes wrong, go back: unpack the bundle you had before, then
`./restore.sh backups/<the file from step 2>` and `docker compose up -d`. An older hub does not
start against a database a newer one has updated; restoring the backup is the way back.

Change none of the files that came with plsremind: settings go into `.env`, own services into
`compose.override.yaml`, scrape targets for further bots into `monitoring/targets/`.
