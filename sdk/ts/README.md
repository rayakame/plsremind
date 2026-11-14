# plsremind for TypeScript and JavaScript

A client for the [plsremind](../../README.md) hub. It connects to the gateway, keeps the
state it is sent in memory, reports every change as a typed event and offers each call of
the REST API as a method.

It runs in browsers and in Node 22 or newer and has no dependencies. The package is not
on npm yet; inside this repository it is used through the npm workspace.

```ts
import { Client } from "plsremind";

const client = new Client({
  url: "https://todo.example.com",
  token: process.env.PLSREMIND_TOKEN,
  intents: ["items", "deliveries"],
});

client.on("item.created", (event) => {
  console.log("new item:", event.data.title);
});

client.on("delivery.created", async (event) => {
  await remind(event.data);
  await client.rest.acknowledgeDelivery(event.data.id, { status: "delivered" });
});

await client.connect();
```

`connect()` returns once the state of every user the session covers is in the cache. From
then on the client reconnects and resumes on its own.

## The rules

- **Nothing is confirmed for you.** A reminder counts as delivered only when you call
  `client.rest.acknowledgeDelivery`. Until then the hub sends it again after every
  reconnect, with a growing `attempt`, and the SDK hands you every one of them. If you
  must not remind twice, remember the `id` yourself, as the
  [example adapter](examples/remind.ts) does.
- **Objects are data.** What you receive has no methods and must not be changed. To change
  something, call `client.rest`. The cache replaces objects instead of editing them, so
  `before === after` tells you whether something changed.
- **Every HTTP call goes through `client.rest`**, and its answers do not touch the cache.
  The cache changes only through the gateway events that follow.

## Names and types

The [gateway](../../docs/gateway.md) and the [API reference](../../docs/api/openapi.yaml)
describe what travels over the wire. The SDK keeps the names of events, operations and
schemas, and turns the rest into what is usual in TypeScript:

| In the API                                | In the SDK                                                             |
| ----------------------------------------- | ---------------------------------------------------------------------- |
| `snake_case` fields (`due_at`, `list_id`) | `camelCase` (`dueAt`, `listId`)                                        |
| a moment as RFC 3339 text                 | `Date`                                                                 |
| a duration as text (`1d12h`)              | a number of milliseconds; `parseDuration` and `formatDuration` convert |
| a whole day (`due_date`)                  | text, `"2026-11-13"`                                                   |
| a time of day in quiet hours              | text, `"22:00"`                                                        |
| fixed values (`status: "done"`)           | the same text, narrowed by the type                                    |
| `preferences` on the profile              | passed through untouched                                               |

The same holds in the other direction: methods of `client.rest` take `camelCase`, `Date`
and milliseconds. Field names in `ApiError.fields` are the SDK's.

## Events

`client.on(name, handler)` returns a function that unsubscribes. The name decides the type
of the event; every event type can also be imported by name, for example
`ItemCreatedEvent`.

Events of the hub keep their gateway names: `item.created`, `item.updated`,
`item.deleted`, `list.*`, `subitem.*`, `reminder.*`, `plan.updated`, `user.updated`,
`connection.updated`, `delivery.created`, `delivery.closed`, `user.sync` and `user.gone`.
A change event carries:

| Field    | Meaning                                                             |
| -------- | ------------------------------------------------------------------- |
| `userId` | the user it is about                                                |
| `data`   | the whole object after the change                                   |
| `old`    | the object as the cache held it before, if it held one              |
| `actor`  | who made the change; compare `actor?.id` with `client.principal.id` |
| `nonce`  | the `nonce` option of the REST call that made the change, or `null` |

The cache is already up to date when a handler runs. Handlers run in the order of the
events; a handler that returns a promise is not waited for.

The client adds events of its own:

| Event          | When                                                                               |
| -------------- | ---------------------------------------------------------------------------------- |
| `ready`        | after every new login, once the state is complete                                  |
| `user.ready`   | one user's state is complete                                                       |
| `resumed`      | a dropped connection was picked up again and missed nothing                        |
| `disconnected` | the connection is gone and is being opened again                                   |
| `closed`       | the client gave up for good: the token no longer works, or the intents are refused |
| `error`        | a handler threw or its promise rejected                                            |

A failing handler never ends the connection. Without an `error` handler the failure is
written to the console.

## The cache

```ts
const me = client.cache.user(); // the user of a personal token or a login
me.profile;
me.lists();
me.items(); // every open item, with subitems and manual reminders
me.item(id);
me.history(); // completed items, newest first; needs the "history" intent
me.plans();

const off = client.cache.subscribe(({ userId, kinds }) => {
  if (kinds.includes("items")) render(client.cache.user(userId)?.items());
});
```

- What the cache holds follows from the intents: `users` gives the profile, `lists`,
  `items` and `plans` their parts, `history` the completed items.
- Reading is synchronous. An array stays the very same array until something of its kind
  changes for that user, which makes it safe to use as a snapshot in UI frameworks.
- The cache does not filter or sort beyond that. Build views like "overdue" on top.
- `history()` starts with the 1000 most recently completed items and grows with every
  item completed while connected. `historyCursor` continues it over REST:

  ```ts
  const older = await client.rest.listItems({
    status: "done",
    sort: "completed",
    order: "desc",
    cursor: me.historyCursor,
  });
  ```

  Pages loaded that way are yours to keep; they do not enter the cache.

## Connections

A connection token covers many users. Ask the cache for a user by id and tell every REST
call whom it is for:

```ts
const bot = new Client({ url, token: "plsr_con_...", intents: ["users", "items"] });
await bot.connect();

for (const user of bot.cache.users()) {
  console.log(user.profile?.displayName, user.items().length);
}

const { id } = await bot.rest.lookupUser(discordUserId);
await bot.rest.createItem({ title: "Hand in the report" }, { user: id });
```

Leaving `user` out with a connection token is refused before the hub is asked.

## REST without the gateway

`RestClient` works on its own. A script that runs every few minutes needs nothing else:

```ts
import { RestClient } from "plsremind";

const rest = new RestClient({ url, token });
for (const delivery of await rest.listDeliveries()) {
  await remind(delivery);
  await rest.acknowledgeDelivery(delivery.id, { status: "delivered" });
}
```

- Every operation of the API reference is a method of the same name: `createItem`,
  `updateItem`, `listReminderPlans` and so on. Ids from the path come first, then the
  body, then options.
- Options: `user` (whom to act for, `@me` by default), `signal` to abort, and on writes
  `nonce`, which comes back in every event the call causes.
- Listings that page return `{ data, nextCursor }`. `rest.pages` walks them:

  ```ts
  for await (const page of rest.pages((cursor) => rest.listItems({ status: "done", cursor }))) {
    // ...
  }
  ```

- An error answer is thrown as `ApiError` with `status`, `code`, `message` and `fields`.
  No answer at all is a `NetworkError`. A `409` with the code `delivery_closed` on
  `acknowledgeDelivery` means the hub withdrew the reminder: treat it as done.
- A `429` is waited out and tried again, up to three times, when the hub asks for less
  than 30 seconds. `retry: false` turns that off.
- In the dashboard, `auth: "session"` uses the login cookie instead of a token, and `url`
  may be left out.

## Showing changes before the hub confirmed them

`plsremind/optimistic` is a separate entry point, so an adapter that only reminds does not
load it.

```ts
import { optimistic } from "plsremind/optimistic";

const view = optimistic(client);

await view.apply(
  [{ kind: "item", id, update: (item) => ({ ...item, status: "done", completedAt: new Date() }) }],
  (nonce) => client.rest.updateItem(id, { status: "done" }, { nonce }),
);
```

- `view` reads like `client.cache` and has the same `subscribe`. It shows the confirmed
  state with the pending changes on top. `client.cache` itself always stays confirmed.
- A change creates, updates or removes an `item` or a `list`, or updates a `plan` or the
  `profile`. For a new object, take an id from `view.tempId()`; `view.resolveId(tempId)`
  gives the real one once it is known.
- If the call rejects, exactly its changes are taken back and the error is rethrown.
- If it succeeds, a change stays until the event with its nonce arrived, or the confirmed
  state already shows it, and for five seconds at most. Pass the nonce on to REST as in
  the example; without it the view cannot tell your event from someone else's.

## Development

```sh
npm install            # in the repository root
cd sdk/ts
npm run check          # types and formatting
npm test               # unit tests
npm run test:hub       # against a real hub; needs Docker and Go
```
