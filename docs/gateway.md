# Gateway

The gateway is a WebSocket connection through which the hub tells you what changed, so
you do not have to ask. It follows Discord's gateway closely: if you have written a
Discord bot, most of this will look familiar.

Everything that changes something still goes through the [REST API](api/openapi.yaml).
The gateway only carries events from the hub to you, plus the three messages a client
needs to stay connected.

## Connecting

Ask `GET /api/v1/info` for `gateway_url` and open a WebSocket there. Every message is a
JSON text frame of this shape:

```json
{"op": 0, "d": {}, "s": 42, "t": "item.created"}
```

| Field | Meaning |
|---|---|
| `op` | The opcode, see below |
| `d` | The payload |
| `s` | The sequence number of a dispatch, otherwise `null` |
| `t` | The event name of a dispatch, otherwise `null` |

### Opcodes

| Op | Name | Sent by | Purpose |
|---|---|---|---|
| 0 | Dispatch | hub | An event. `t` names it. |
| 1 | Heartbeat | client | Keeps the connection alive. `d` is the last sequence number you saw, or `null`. |
| 2 | Identify | client | Starts a new session. |
| 6 | Resume | client | Continues a session after a dropped connection. |
| 7 | Reconnect | hub | Please reconnect and resume. Sent before the hub shuts down. |
| 9 | Invalid Session | hub | The session cannot be resumed. Identify again. `d` is `false`. |
| 10 | Hello | hub | The first message. Tells you the heartbeat interval. |
| 11 | Heartbeat ACK | hub | The answer to a heartbeat. |

### The handshake

1. The hub sends **Hello**: `{"op": 10, "d": {"heartbeat_interval": 45000}}`. The interval
   is in milliseconds.
2. Within 10 seconds, send **Identify**:

   ```json
   {"op": 2, "d": {"token": "plsr_pat_...", "intents": ["items", "deliveries"]}}
   ```

3. The hub answers with the `READY` dispatch:

   ```json
   {"op": 0, "t": "READY", "s": 1, "d": {
     "session_id": "f3Zk...",
     "intents": ["items", "deliveries"],
     "principal": {"kind": "personal_token", "user_id": "0193..."},
     "users": ["0193..."]
   }}
   ```

   `principal.kind` is `personal_token`, `connection` or `session`. A connection gets
   `principal.connection` with its `id`, `name` and `provider` instead of `user_id`.
   `users` lists the users whose state follows.

4. For every user in `users` the hub sends their state as `user.sync`.
5. From then on every change arrives as a dispatch.
6. Send **Heartbeat** once per interval. If none arrives for one and a half intervals, the
   hub closes the connection.

A token goes into Identify, never into a header. The dashboard has no token: it leaves
`token` out and is recognised by its session cookie, which only works from pages of the
hub itself.

## Intents

You only get what you ask for. `intents` in Identify is required and must not be empty.

| Intent | Part of `user.sync` | Events |
|---|---|---|
| `users` | `user` | `user.updated`, and in a user's own stream `connection.updated` |
| `lists` | `lists` | `list.created`, `list.updated`, `list.deleted` |
| `items` | `items` | `item.created`, `item.updated`, `item.deleted`, `subitem.created`, `subitem.updated`, `subitem.deleted`, `reminder.created`, `reminder.deleted` |
| `plans` | `plans` | `plan.updated` |
| `deliveries` | none | `delivery.created`, `delivery.closed` |

- An adapter that only reminds asks for `deliveries` and keeps no state at all. Every
  delivery carries the item and the user it is about.
- Without any of `users`, `lists`, `items` and `plans` there is no `user.sync` and no
  `user.gone`, and `READY.users` is empty.
- Intents hold for the whole session. To change them, identify again.

For a connection, the admin decides which intents it may have at all. Asking for one that
is not allowed closes the connection with code 4014. The same limit applies to the REST
API: a connection without `items` can neither receive items nor read or write them.

## Who a stream covers

- A personal token or a dashboard session: the one user it belongs to.
- A connection token: every user the connection may act for. That is everyone who has an
  account at the connection's provider and has not switched the connection off.

The protocol is the same for both. Every event names the user it is about in `user_id`.

## State

The gateway is built so that a client can hold the complete state in memory and use
REST only to change things.

### `user.sync`

```json
{"op": 0, "t": "user.sync", "s": 2, "d": {
  "user_id": "0193...",
  "chunk": {"index": 0, "count": 1},
  "user": {"id": "0193...", "display_name": "Mia", "timezone": "Europe/Berlin", "quiet_hours": null},
  "lists": [{"id": "...", "name": "Inbox", "position": 0, "is_inbox": true}],
  "items": [{"id": "...", "title": "Hand in the report", "status": "open", "subitems": [], "reminders": []}],
  "plans": [{"priority": "low", "before": ["1d"], "follow_up": "never", "is_default": true}]
}}
```

- Only the parts of your intents are present. The objects are the same as in the REST API.
- `items` holds all **open** items, each with its subitems and manual reminders. Completed
  items are not part of the state; fetch them over REST if you need them.
- A user with many items is sent in several parts of at most 500 items. `chunk` tells you
  which part this is. `user`, `lists` and `plans` come in part 0. Part 0 replaces whatever
  you knew about the user; later parts add items.
- For a connection, `user` is a reduced profile: `id`, `display_name`, `timezone`,
  `quiet_hours`, the user's `external_ids` at the connection's provider, and `remind`
  (whether the user wants reminders through this connection).

### `user.gone`

```json
{"op": 0, "t": "user.gone", "s": 17, "d": {"user_id": "0193..."}}
```

A connection receives this when a user switches it off, is disabled or is deleted. Drop
everything you hold about the user. It also stands in for the `user.sync` of a user that
`READY` named but who went out of reach before their state was sent. A `delivery.closed`
for a reminder you were sent earlier can still follow it. When a user comes into reach later, for example by
linking their account, the connection receives a `user.sync` for them. These two are the
counterparts of Discord's `GUILD_DELETE` and `GUILD_CREATE`.

### Change events

```json
{"op": 0, "t": "item.updated", "s": 18, "d": {
  "user_id": "0193...",
  "actor": {"kind": "session", "id": "0193..."},
  "data": {"id": "...", "title": "Hand in the report", "status": "done"}
}}
```

- `data` is always the whole object after the change, so you replace your copy. For
  `*.deleted` it is `{"id": "..."}`, with `item_id` added for subitems and reminders.
- `actor` says who made the change: `kind` is `session`, `personal_token` or `connection`,
  and `id` identifies it. Compare it with your own to recognise your own changes. It is
  `null` when the hub tells a connection that something about a user's access changed,
  for example that they switched reminders through it off.
- Every change to a subitem or a manual reminder is followed by `item.updated` with the
  whole item. If you keep items in memory you can ignore the `subitem.*` and `reminder.*`
  events and just replace the item.
- An item that is completed arrives as `item.updated` with `status: "done"`. Since the
  state only holds open items, remove it from your cache.
- `user.updated` carries the profile, in the same form as in `user.sync`.
- `plan.updated` carries one reminder plan. `connection.updated` tells a user's own stream
  that they changed what a connection may do; connections never receive it.

**The guarantee:** after the last part of a `user.sync`, every later change to that user
arrives, in order and without gaps, for as long as the session lives or is resumed.

## Sequence numbers and resuming

Every dispatch that belongs to the state carries a sequence number `s`. It starts at 1
with `READY` and goes up by one. Remember the last one you saw.

When the connection drops, connect again and send **Resume** instead of Identify:

```json
{"op": 6, "d": {"token": "plsr_pat_...", "session_id": "f3Zk...", "seq": 18}}
```

The hub sends everything after `seq` again and finishes with the `RESUMED` dispatch.
You then continue as if nothing had happened.

The hub keeps a session for 5 minutes after its connection dropped and buffers up to
5000 events for it. If the session is gone, the buffer overflowed, or the hub was
restarted, it answers with **Invalid Session** (`{"op": 9, "d": false}`). The connection
stays open: send Identify, and build your state again from the fresh `user.sync`.

## Reminders

When the time of a reminder comes, the hub creates a **delivery** for each way the user
is reminded through: every connection that reminds them, and every personal token marked
with `remind`. Each delivery waits until that channel confirms it.

```json
{"op": 0, "t": "delivery.created", "s": null, "d": {
  "user_id": "0193...",
  "actor": null,
  "data": {
    "id": "0193...",
    "attempt": 1,
    "late": false,
    "reminder": {"id": "...", "kind": "before", "offset": "1d", "scheduled_for": "2026-11-12T18:00:00Z"},
    "item": {"id": "...", "title": "Hand in the report", "due_at": "2026-11-13T18:00:00Z"},
    "user": {"id": "0193...", "display_name": "Mia", "timezone": "Europe/Berlin", "external_ids": ["1234"]}
  }
}}
```

| `reminder.kind` | Meaning |
|---|---|
| `before` | Ahead of a deadline, as the reminder plan says. `offset` tells how far ahead. |
| `due` | The deadline is now. |
| `follow_up` | The deadline has passed and the item is still open. |
| `manual` | The user asked to be reminded about this item at this time. |

What to do with a delivery:

1. Remind the person. For a connection, `user.external_ids` says whom to address.
2. Once the reminder has actually reached them, confirm it:
   `PUT /api/v1/deliveries/{id}` with `{"status": "delivered"}`.
3. If it cannot reach them, say so: `{"status": "failed", "reason": "direct messages are closed"}`.
   The reason is shown to the user. A failed delivery is not tried again.

A `409` with the code `delivery_closed` means the hub closed the delivery in the meantime,
for example because the item was completed. Treat it as done.

The hub never confirms anything on its own, and neither should a library on your behalf.
Until a delivery is confirmed it is sent again **every time you identify or resume**, with
a growing `attempt`. It is not repeated while you stay connected. After 24 hours it expires.

- `late` is true when the reminder reaches you more than a minute after it should have
  gone out: you or the hub were away, or it had already fired when quiet hours began and
  waited for them to end. A reminder that quiet hours moved and that then left on time
  is not late. `reminder.scheduled_for` is the moment it was meant for.
- If you were away while several reminders for the same item came due, you only get the
  newest.
- While a user is in their quiet hours, `before` and `follow_up` deliveries are held back
  until the window ends. `due` and `manual` always go out. You do not need to know about
  quiet hours.
- If you have several connections open with the same token, each gets every delivery.

### `delivery.closed`

```json
{"op": 0, "t": "delivery.closed", "s": null, "d": {"user_id": "0193...", "actor": null, "data": {"id": "0193...", "status": "cancelled"}}}
```

An open delivery was withdrawn. `status` is `superseded` (a newer reminder for the same
item replaced it), `cancelled` (the item was completed or deleted, or reminders through
you were switched off) or `expired`. If you have not reminded yet, don't.

You are only told about deliveries you were sent at least once. One that was withdrawn
before it ever reached you, while you were offline or during quiet hours, just never arrives.

Delivery events carry `s: null`. They are not part of the sequence that Resume replays,
because open deliveries are sent again after every login anyway.

### Without the gateway

`GET /api/v1/deliveries` returns the open deliveries of the calling token in the same
form. A script that runs every few minutes can remind people with that alone.

## Close codes

| Code | Meaning | Reconnect? |
|---|---|---|
| 1011 | Something went wrong inside the hub, or it could not reach its database | yes |
| 1012 | The hub is shutting down or restarting | yes |
| 4001 | Unknown opcode, or one that only the hub may send | yes |
| 4002 | The message was not valid JSON for the gateway | yes |
| 4004 | Authentication failed, or your access was withdrawn | no, the token will not work again |
| 4005 | You sent Identify or Resume twice, or the session was resumed elsewhere | yes |
| 4007 | Resume named a sequence number the hub never sent | yes, with Identify |
| 4008 | Too many messages or sessions, or you were too slow to read | yes, after a pause |
| 4009 | No heartbeat in time, or no Identify within 10 seconds | yes |
| 4013 | Unknown or missing intents | no, fix the intents |
| 4014 | An intent that the admin does not allow this connection | no, ask for less |

Limits: 10 connected sessions per token and 25 per user across all of their tokens and
logins, 60 client messages per minute. A client may only
send Heartbeat, Identify and Resume.

When you reconnect, pause first and let the pause grow while the hub stays unreachable.
The handshake counts against the hub's rate limit for unauthenticated requests.

Your access is checked again whenever something about it changes and on every heartbeat.
A deleted token, an ended login, a disabled user or a disabled connection closes the
connection with 4004. That code is only sent when the hub knows your access is gone; if
it merely could not check, the session stays. A connection whose allowed intents no longer cover the session's
is closed with 4014.

## Events at a glance

| Event | Intent | `data` |
|---|---|---|
| `READY` | always | see the handshake |
| `RESUMED` | always | `{}` |
| `user.sync` | any state intent | see above |
| `user.gone` | any state intent | none, only `user_id` |
| `user.updated` | `users` | the profile |
| `connection.updated` | `users`, user streams only | the connection as the user sees it |
| `list.created`, `list.updated` | `lists` | the list |
| `list.deleted` | `lists` | `{"id"}` |
| `item.created`, `item.updated` | `items` | the item with subitems and reminders |
| `item.deleted` | `items` | `{"id"}` |
| `subitem.created`, `subitem.updated` | `items` | the subitem |
| `subitem.deleted` | `items` | `{"id", "item_id"}` |
| `reminder.created` | `items` | the manual reminder |
| `reminder.deleted` | `items` | `{"id", "item_id"}` |
| `plan.updated` | `plans` | the reminder plan |
| `delivery.created` | `deliveries` | the delivery |
| `delivery.closed` | `deliveries` | `{"id", "status"}` |

## Example

[`hub/examples/remind`](../hub/examples/remind/main.go) is a complete adapter in about
200 lines of Go: it connects, prints every reminder and confirms it.

```sh
PLSREMIND_URL=https://todo.example.com PLSREMIND_TOKEN=plsr_pat_... go run ./hub/examples/remind
```
