# plsremind for Python

A client for the [plsremind](https://github.com/rayakame/plsremind) hub. It connects to the gateway, keeps the
state it is sent in memory, hands every change to listeners as a typed event and offers
each call of the REST API as a method.

If you have used [hikari](https://github.com/hikari-py/hikari), most of it will feel
familiar: event classes, `@client.listen()`, `client.rest` and `client.cache`.

It needs Python 3.12 or newer and depends on `msgspec`, `aiohttp` and `tzdata`. It works
with plsremind 0.1 or newer.

```sh
uv add plsremind        # or: pip install plsremind
```

```python
import os

import plsremind

client = plsremind.Client(
    "https://remind.example.com",
    token=os.environ["PLSREMIND_TOKEN"],
    intents=plsremind.Intents.ITEMS | plsremind.Intents.DELIVERIES,
)


@client.listen()
async def on_item(event: plsremind.ItemCreateEvent) -> None:
    print("new item:", event.item.title)


@client.listen()
async def on_delivery(event: plsremind.DeliveryCreateEvent) -> None:
    await remind(event.delivery)
    await client.rest.acknowledge_delivery(event.delivery.id, status=plsremind.DeliveryStatus.DELIVERED)


client.run()
```

`run()` blocks until SIGINT or SIGTERM and then closes the client. Inside a program with
its own event loop use `await client.start()` and `await client.close()`, or
`async with client:`. `start()` returns once the state of every user the session covers
is in the cache. From then on the client reconnects and resumes on its own. If the first
attempt fails, or the hub refuses the token or the intents, `start()` raises a
`GatewayError` and the client does not try again.

## The rules

- **Nothing is acknowledged for you.** A reminder counts as delivered only when you call
  `client.rest.acknowledge_delivery`. Until then the hub sends it again after every
  reconnect, with a growing `attempt`, and the SDK hands you every one of them. If you
  must not remind twice, remember the `id` yourself, as the
  [example adapter](https://github.com/rayakame/plsremind/blob/main/sdk/python/examples/remind.py) does.
- **Objects are frozen data.** What you receive is a `msgspec.Struct` without methods that
  cannot be changed. To change something, call `client.rest`. The cache replaces objects
  instead of editing them, so `before is after` tells you whether something changed.
- **Every HTTP call goes through `client.rest`**, and its answers do not touch the cache.
  The cache changes only through the gateway events that follow.
- **A connection names the user.** A personal token acts for its one user, so `user` can
  be left out with it. With the token of a connection, pass `user=...` to every call and
  cache read that is about a user; leaving it out raises a `TypeError` before anything is
  sent.

## Names and types

The [gateway](https://github.com/rayakame/plsremind/blob/main/docs/gateway.md) and the [API reference](https://github.com/rayakame/plsremind/blob/main/docs/api/openapi.yaml)
describe what travels over the wire. Fields keep their names, and values arrive as the
types Python has for them:

| In the API | In the SDK |
|---|---|
| an ID | `plsremind.ID`, a `str` |
| a moment as RFC 3339 text | an aware `datetime.datetime`, exact to the microsecond |
| a duration as text (`1d12h`) | `plsremind.Duration`, a `datetime.timedelta` |
| a whole day (`due_date`) | `datetime.date` |
| a time of day in quiet hours (`22:00`) | `plsremind.ClockTime`, a `datetime.time` |
| a time zone (`Europe/Berlin`) | `zoneinfo.ZoneInfo` |
| fixed values (`status`, `priority`, `kind`) | an enum, for example `ItemStatus.DONE` |
| a list | a `tuple` |
| `preferences` on the profile | a read-only mapping |

- A field with fixed values is always the enum. A value this version of the SDK does not
  know yet arrives as the member `UNKNOWN` instead of failing the event.
- A time zone the system does not know becomes UTC, with one warning per name in the log
  (logger `plsremind`). In the same way a duration or a time of day in a form this version
  cannot read becomes zero or midnight, with a warning.
- `preferences` is read-only at its top level only: lists and objects inside it can still
  be changed, which changes nothing at the hub. Before Python 3.15 it is a
  `types.MappingProxyType`, so a `User` cannot be pickled or deep-copied there.
- Fields and events the SDK does not know are skipped, so an older SDK keeps working
  against a newer hub.
- Methods of `client.rest` take the same types. `before=` and `after=` take any
  `timedelta`; inside a `ReminderOverride` or `QuietHours` write `plsremind.Duration(hours=3)`
  and `plsremind.ClockTime(22, 0)`. A `datetime` needs a time zone, and a duration is whole
  minutes, from one minute up; anything else raises a `ValueError` before it is sent.
  `format_duration` and `parse_duration` convert between a `timedelta` and the hub's text.

To keep something the SDK handed out, `plsremind.to_json(delivery)` writes it as JSON in
the hub's form and `plsremind.from_json(data, plsremind.Delivery)` reads it back. msgspec's
own encoder does not know `Duration` and `ClockTime`.

## Events

`@client.listen()` reads the event class from the annotation of the listener's parameter;
`@client.listen(plsremind.ItemCreateEvent)` names it. A listener is an `async def` with
one parameter. `client.subscribe` and `client.unsubscribe` do the same without a
decorator, and `await client.wait_for(SomeEvent, timeout=10, predicate=...)` waits for one
event.

| Gateway | Class | Carries |
|---|---|---|
| `item.created` | `ItemCreateEvent` | `item` |
| `item.updated` | `ItemUpdateEvent` | `item`, `old_item` |
| `item.deleted` | `ItemDeleteEvent` | `item_id`, `old_item` |
| `list.created`, `list.updated`, `list.deleted` | `ListCreateEvent`, `ListUpdateEvent`, `ListDeleteEvent` | `list` or `list_id`, `old_list` |
| `subitem.created`, `subitem.updated`, `subitem.deleted` | `SubitemCreateEvent`, `SubitemUpdateEvent`, `SubitemDeleteEvent` | `subitem` or `subitem_id` and `item_id`, `old_subitem` |
| `reminder.created`, `reminder.deleted` | `ReminderCreateEvent`, `ReminderDeleteEvent` | `reminder` or `reminder_id` and `item_id`, `old_reminder` |
| `plan.updated` | `PlanUpdateEvent` | `plan`, `old_plan` |
| `user.updated` | `UserUpdateEvent` | `user`, `old_user` |
| `connection.updated` | `ConnectionUpdateEvent` | `connection` |
| `delivery.created` | `DeliveryCreateEvent` | `delivery` |
| `delivery.closed` | `DeliveryCloseEvent` | `delivery_id`, `status` |
| `delivery.test` | `DeliveryTestEvent` | `test`; answer with `client.rest.report_test` |
| `user.sync` | `UserSyncEvent` | one part of a user's state |
| `user.gone` | `UserGoneEvent` | |

- Every event about a user has `user_id`. Change events also have `actor` (compare
  `actor.id` with `client.principal.id` to recognise your own changes) and `nonce`, the
  `nonce=` of the REST call that made the change. `actor` is `None` for what the hub does
  by itself, such as removing a manual reminder whose time has passed.
- `old_*` is the object the cache held before the change, or `None` if it held none.
- Listening to a base class gets every event below it: `ItemEvent`, `ListEvent`,
  `DeliveryEvent`, `ChangeEvent`, `UserEvent`, or `Event` for everything.
- The cache is already up to date when a listener runs. Each listener runs as a task of
  its own, started in the order of the events and not waited for, so a slow listener
  holds up neither the connection nor other listeners.
- An event nobody listens to, and the cache does not need, is never decoded.
- A `UserGoneEvent` also comes for a user the cache held whom a new session no longer
  names, for example one who switched a connection off while it could not resume.
- If a change arrives that this version cannot read, the client logs it and starts the
  session over, so the cache cannot quietly differ from the hub. If it cannot read the
  hub's login answer or a user's state at all, it gives up with a `ClosedEvent`.
- `wait_for` raises a `GatewayError` if the client is closed, or turned away, meanwhile.

The client adds events of its own:

| Class | When |
|---|---|
| `ReadyEvent` | after every new login, once the state is complete |
| `UserReadyEvent` | one user's state is complete |
| `ResumedEvent` | a dropped connection was picked up again and missed nothing |
| `DisconnectedEvent` | the connection is gone and is being opened again; once per outage |
| `ClosedEvent` | the client gave up: the first connect failed, or the hub refuses the token or the intents |
| `ExceptionEvent` | a listener raised |

A listener that raises never ends the connection. Without a listener for
`ExceptionEvent` the exception goes to the log with its traceback.

`client.status` is a `ClientStatus`: `IDLE`, `CONNECTING`, `CONNECTED`, `RECONNECTING` or
`CLOSED`. `close()` ends the session and every retry, waits up to five seconds for
running listeners and cancels the rest. The cache stays readable, and `start()` works
again. After a `ClosedEvent` the client has let go of its connections by itself.

## The cache

```python
cache = client.cache
cache.get_profile()
cache.get_lists()
cache.get_items()          # every open item, with subitems and manual reminders
cache.get_item(item_id)    # an open item, or a completed one from the history
cache.get_history()        # completed items, newest first; needs Intents.HISTORY
cache.get_plans()
cache.get_users()          # for a connection: the IDs of everyone it may act for
cache.get_items(user_id)
```

- What the cache holds follows from the intents: `USERS` gives the profile, `LISTS`,
  `ITEMS` and `PLANS` their parts, `HISTORY` the completed items. A client with
  `Intents.DELIVERIES` alone keeps no state at all.
- Reading is synchronous and returns the objects the cache holds; nothing is copied. A
  tuple stays the very same tuple until something of its kind changes for that user.
- `get_history()` starts with the 1000 most recently completed items and grows with every
  item completed while connected. `get_history_cursor()` continues it over REST:

  ```python
  cursor = client.cache.get_history_cursor()
  if cursor is not None:
      async for item in plsremind.paginate(
          client.rest.list_items,
          status=plsremind.ItemStatus.DONE,
          sort=plsremind.ItemSort.COMPLETED,
          order=plsremind.SortOrder.DESCENDING,
          cursor=cursor,
      ):
          ...
  ```

- The cache does not filter or sort beyond that. Build views like "overdue" on top.

## REST

Every operation of the API reference that a token may call is a method of `client.rest`,
named after its `operationId` in `snake_case`: `create_item`, `update_item`,
`list_lists`, `acknowledge_delivery`. What only a browser session may do (administration,
managing tokens, passwords, signing in) is not part of the SDK.

```python
item = await client.rest.create_item(title="Hand in the report", due_at=deadline)
await client.rest.update_item(item.id, status=plsremind.ItemStatus.DONE)
await client.rest.update_item(item.id, due_at=None)      # None clears, leaving it out keeps
await client.rest.create_reminder(item.id, after=datetime.timedelta(hours=2))
```

- IDs from the path come first, everything else is a keyword. Writes take `nonce=`, which
  comes back in every event the call causes.
- Calls about the account itself (`update_user`, `list_identities`, changing reminder
  plans, `list_user_connections`, `set_connection_grant`, `list_reminder_log`) take no
  `user`: the hub opens them to the user alone, and with the token of a connection they
  raise a `TypeError`.
- Listings return a `Page` with `data` and `next_cursor`. `plsremind.paginate` walks a
  listing to its end.
- An ID that is empty, `.` or `..` raises a `ValueError` before anything is sent, so text a
  person typed can be passed on as an ID without reaching another call.
- `client.rest` accepts the ID of a personal token's own user as `user=`. A `RESTClient`
  on its own does not know that ID, and the hub answers it with a 404: leave `user` out
  there.
- `RESTClient` also works alone, for a script that needs no gateway:

  ```python
  async with plsremind.RESTClient(url, token=token) as rest:
      for delivery in await rest.list_deliveries():
          ...
  ```

- A call the hub answers with an error raises an `APIError` with `status`, `code`,
  `message` and `fields`. Its subclasses follow the status: `BadRequestError`,
  `UnauthorizedError`, `ForbiddenError`, `NotFoundError`, `ConflictError`,
  `RateLimitedError`, `ServerError`. No answer, or one that cannot be read, raises a
  `NetworkError`. All of them derive from `PlsremindError`.
- A call turned away for the rate limit is tried again up to three times when the hub
  asks for a wait under 30 seconds; `rest_retry=False` on the client switches that off.
  Nothing else is retried. A call may take 30 seconds (`rest_timeout=`).
- `session=` on `Client` and `RESTClient` takes an `aiohttp.ClientSession` of your own,
  for example to go through a proxy.
- The client asks the hub where its gateway listens, and the hub names its public address.
  A client that reaches the hub by another address, such as one inside a container
  network, passes `gateway_url="ws://hub:8080/api/v1/gateway"`.

## A connection

```python
client = plsremind.Client(url, token=connection_token, intents=plsremind.Intents.DELIVERIES)


@client.listen()
async def on_delivery(event: plsremind.DeliveryCreateEvent) -> None:
    delivery = event.delivery
    for account in delivery.user.external_ids:   # the user's accounts at your provider
        await send(account, delivery.item.title, language=delivery.user.locale)
    await client.rest.acknowledge_delivery(delivery.id, status=plsremind.DeliveryStatus.DELIVERED)


async def add_item(account: str, title: str) -> None:
    found = await client.rest.lookup_user(account)
    await client.rest.create_item(title=title, user=found.id)
```

If a reminder cannot reach its user, say so with
`status=plsremind.DeliveryStatus.FAILED, reason="direct messages are closed"`. The reason
is shown to the user, and the delivery is not tried again.

## Working on the SDK

```sh
cd sdk/python
uv run --group nox nox                 # format, lint, type check, unit tests
uv run --group nox nox -s hub          # needs Docker and Go, starts a real hub
```
