# Self-hosting plsremind

The hub is a single program that needs a PostgreSQL database; the Discord bot is a second,
optional one. How to run them, with Docker Compose, Docker Swarm and Portainer, Kubernetes or
plain `docker run`, is in [docs/deploy](deploy/README.md), together with the settings, updating,
backups, monitoring and reverse proxies. This page covers what comes once the hub runs: the
first account, login providers, apps and the Discord bot. The examples use
`https://todo.example.com` as the hub's address.

## First steps

1. **Create the admin.** Open the hub's address in a browser. A new instance asks for its
   first account, which becomes the admin. The same can be done without a browser: a new
   instance accepts exactly one call to `POST /api/v1/setup`:

   ```sh
   curl -c cookies.txt -H 'Content-Type: application/json' \
     -d '{"username":"admin","password":"choose a long one"}' \
     https://todo.example.com/api/v1/setup
   ```

   The response sets a session cookie. Requests that change something with that cookie
   must carry an `Origin` header equal to `PLSREMIND_PUBLIC_URL`, as a browser would send.

2. **Decide who may register.** New instances are invite-only. Create invites with
   `POST /api/v1/admin/invites`, or open registration with
   `PATCH /api/v1/admin/settings` and `{"registration":"open"}`.

3. **Add login providers.** Username and password work out of the box. GitHub, Discord and
   any OpenID Connect provider are configured with `PUT /api/v1/admin/providers/{key}`.

## Login providers

Register the hub as an OAuth application at the provider and use this redirect address:

```
{PLSREMIND_PUBLIC_URL}/api/v1/auth/oauth/{key}/callback
```

| Provider | Key | Where to register |
|---|---|---|
| GitHub | `github` | Settings → Developer settings → OAuth Apps |
| Discord | `discord` | Developer Portal → your application → OAuth2 |
| OpenID Connect | your choice | Your identity provider, for example Authentik or Keycloak |

Then hand the hub the credentials:

```sh
curl -b cookies.txt -X PUT -H 'Content-Type: application/json' \
  -H 'Origin: https://todo.example.com' \
  -d '{"client_id":"...","client_secret":"...","login_enabled":true,"link_enabled":true}' \
  https://todo.example.com/api/v1/admin/providers/discord
```

An OpenID Connect provider additionally needs `"type":"oidc"`, a `"name"` and its
`"issuer_url"`.

`login_enabled` lets people log in through the provider. `link_enabled` lets logged-in users
attach their account there, which is how an app such as the Discord bot recognises them.
The two are independent. One provider serves every app of its kind: a single Discord
provider links accounts for all Discord bots of the instance.

## Apps

An app is an integration for all users of the instance, such as a Discord bot. There can
be any number of them, several of the same kind too. An admin adds them in the dashboard
under Settings, Administration, Apps, or with `POST /api/v1/admin/connections` (in the API
an app is a connection), and gives the returned token to the app. The token is shown
once; `POST /api/v1/admin/connections/{id}/token` issues a new one. An `install_url` tells
users where to add the app on the provider's side; for a Discord bot the dashboard makes it
from the bot's Application ID.

Each user decides per app what it may do for them, under Settings, Apps, and can send a
test reminder through it to see that it reaches them. Users who want something of their
own build it with a personal token instead.

## The Discord bot

The bot in `connections/discord` sends reminders as direct messages and answers commands
such as `/remind` and `/todos`. It is a connection like any other: it talks to the hub
through the API and the gateway only, and keeps a small SQLite file of its own.

1. **Create a Discord application** in the
   [Developer Portal](https://discord.com/developers/applications). Under Installation,
   allow both "User Install" and "Guild Install" and keep the scope `applications.commands`
   for both. Set the Install Link to "Custom URL" with `https://todo.example.com/link/discord`,
   so that adding the app in Discord links the account in plsremind too. Under Bot, create a
   token. The bot needs no privileged intents.
2. **Let people link their Discord account** with the same application. Configure the
   login provider `discord` as described above with `"link_enabled": true`, with the client
   ID and secret under OAuth2. Once the bot is an app with this Application ID (next step),
   signing in and linking with Discord ask for the bot's scope as well, so linking adds the
   bot to the user's account. It has to be the same application: Discord keeps only the
   newest grant a user gave an application, and a sign-in through another one would not add
   the bot. A Discord sign-in without a bot asks for `identify` alone.
3. **Add the bot as an app** in the dashboard under Settings, Administration, Apps, with
   its Application ID, or with the API, and keep the token it returns:

   ```sh
   curl -b cookies.txt -X POST -H 'Content-Type: application/json' \
     -H 'Origin: https://todo.example.com' \
     -d '{"name":"Discord","provider":"discord","allowed_intents":["users","lists","items","history","deliveries"],"install_url":"https://discord.com/oauth2/authorize?client_id=<Application ID>&integration_type=1&scope=applications.commands"}' \
     https://todo.example.com/api/v1/admin/connections
   ```

   A bot that was added without its Application ID, for example before apps had an
   install link, shows users no button, and linking does not add it: enter the ID once
   under Settings, Administration, Apps.

4. **Start the bot** next to the hub, with `DISCORD_TOKEN` and `PLSREMIND_DISCORD_TOKEN` (the
   `plsr_con_...` token): with Compose, add `discord` to `COMPOSE_PROFILES`
   ([Compose](deploy/compose.md#the-discord-bot)); in Swarm it is part of `stack.yaml`
   ([Swarm](deploy/swarm.md)). Its variables are listed under
   [settings](deploy/settings.md#the-discord-bot).

Users then link their Discord account in the dashboard under Settings, Apps, which adds the
bot to their Discord account, and get their reminders as direct messages.

The bot's SQLite file holds which reminders went out, which message shows which item and the DM
channels of the accounts reminded in the last 30 days. If it is lost, the DM channels are
opened again, a reminder that was still waiting for its confirmation may arrive a second
time, and older messages only catch up when someone clicks them. Nothing else depends on it.

### Another Discord bot

A second bot is a second app with its own Discord application, token and SQLite file. The
Discord sign-in stays with the first bot's application, so users add the second bot with its
own button under Settings, Apps. Running it next to the first is described for
[Compose](deploy/compose.md#another-discord-bot) and for
[Swarm](deploy/swarm.md#another-discord-bot).

## API documentation

The REST API is described in [`docs/api/openapi.yaml`](api/openapi.yaml). Run `make docs` in
`hub/` to render it to `docs/api/index.html`. The gateway is described in
[`docs/gateway.md`](gateway.md).
