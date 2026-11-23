# Metrics

The hub and the Discord bot can expose metrics in the Prometheus text format. Every tool
that reads it works: Prometheus, VictoriaMetrics, Grafana Alloy, the Datadog agent and
others. They are off until you set an address, and they are never served on the hub's
public port. No label ever holds a user, an item, a token or any other id, so the numbers
say how the instance is doing without saying who is doing what.

## Hub

Set `PLSREMIND_METRICS_ADDR`, for example to `:9090`. The hub then serves `GET /metrics`
on that address. An address the hub cannot listen on stops it at the start.

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `plsremind_build_info` | gauge | `version` | Always 1; tells which version runs. |
| `plsremind_http_requests_total` | counter | `method`, `route`, `status` | HTTP requests. `route` is the pattern, such as `/api/v1/users/:user/items/:id`; `web` is the dashboard, `unmatched` a path no route knows. |
| `plsremind_http_request_duration_seconds` | histogram | `method`, `route` | How long requests took. The gateway's WebSocket is left out, since it lasts as long as its connection. |
| `plsremind_reminders_total` | counter | `kind`, `outcome` | Reminders whose time came. `kind` is `before`, `due`, `follow_up` or `manual`; `outcome` is `fired`, `superseded` (a newer one of the same item fired at once) or `missed` (more than a day late, after the hub was away). |
| `plsremind_reminder_lateness_seconds` | histogram | | How long after its time a reminder fired. Quiet hours move that time on purpose, so they do not count as lateness. This is the number to watch. |
| `plsremind_deliveries_open` | gauge | | Deliveries still waiting for a channel to confirm them, counted at each scrape. Left out when the database does not answer within two seconds. |
| `plsremind_deliveries_acknowledged_total` | counter | `result` | Deliveries a channel confirmed, `delivered` or `failed`. |
| `plsremind_deliveries_expired_total` | counter | | Deliveries nobody confirmed within a day. |
| `plsremind_ticker_runs_total` | counter | `result` | Rounds of the reminder ticker, `ok` or `error`. It runs every five seconds. |
| `plsremind_ticker_duration_seconds` | histogram | | How long a round took. |
| `plsremind_ticker_last_success_timestamp_seconds` | gauge | | When the ticker last finished a round without an error. |
| `plsremind_gateway_sessions` | gauge | `kind` | Gateway sessions with a client attached, by credential: `personal_token`, `connection` or `session` (the dashboard). |
| `plsremind_gateway_closes_total` | counter | `code` | Gateway connections that ended, by close code. `1000` is a normal end; the others are listed in `docs/gateway.md`. |
| `plsremind_gateway_dispatches_total` | counter | `event` | Dispatches produced for sessions, by event, such as `READY` or `item.created`. |
| `plsremind_db_pool_connections` | gauge | `state` | Database connections: `acquired`, `idle`, `total` and the pool's `max`. |
| `plsremind_db_pool_acquire_seconds_total` | counter | | Time spent getting a connection from the pool. |
| `plsremind_db_pool_empty_acquires_total` | counter | | Requests for a connection that had to wait because none was free. A steady rise means the pool is too small. |

The Go runtime (`go_*`) and the process (`process_*`) are included.

## Health

The hub answers two paths on its public port, without a login and outside the rate
limits, so that a load balancer or an orchestrator can ask:

- `GET /healthz` is `200 ok` while the process serves.
- `GET /readyz` is `200 ok` when the database answers within two seconds, otherwise
  `503 unavailable`.

The image has no shell, so `plsremind health` asks `/readyz` on the hub's own listener
and exits with 0 or 1. The compose file uses it as the hub's healthcheck.

## Discord bot

Set `PLSREMIND_DISCORD_METRICS_ADDR` to `host:port`, for example `0.0.0.0:9091`. The bot
then serves three paths on it:

- `GET /metrics` with the metrics below.
- `GET /healthz`, `200 ok` while the bot runs.
- `GET /readyz`, `200 ok` while the bot is connected to both the hub and Discord and lets
  through what the hub sends, otherwise `503 unavailable`.

An address the bot cannot listen on ends it at the start with exit code 1 and the
variable named in the log.

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `plsremind_discord_build_info` | gauge | `version` | Always 1; tells which version runs. |
| `plsremind_discord_delivery_attempts_total` | counter | `result` | Tries to send a reminder: `sent`, `closed` (DMs closed), `refused` (Discord would not take the message), `no_account` (no Discord account linked), `passing` (trouble that is tried again), `unreadable` (the item could not be read yet). |
| `plsremind_discord_deliveries_waiting` | gauge | | Deliveries that are tried again later. |
| `plsremind_discord_dm_channels_opened_total` | counter | | DM channels the bot had to open; a kept one is used again. |
| `plsremind_discord_interactions_total` | counter | `kind` | Interactions from Discord: `command`, `component` (a click), `modal` or `autocomplete`. |
| `plsremind_discord_interaction_failures_total` | counter | `kind` | Interactions that failed unexpectedly. A click while the bot starts or by an unlinked account is not a failure. |
| `plsremind_discord_redraws_total` | counter | `result` | Reminder messages drawn again after a change: `redrawn`, `forgotten` (the message is gone or out of reach) or `failed`. |
| `plsremind_discord_write_echo_timeouts_total` | counter | | Writes of the bot whose event did not come back from the gateway within two seconds. |
| `plsremind_discord_hub_connected` | gauge | | 1 while the bot is connected to the hub. |
| `plsremind_discord_discord_latency_seconds` | gauge | | Heartbeat latency to Discord; missing until the first heartbeat came back. |

The process (`process_*`) and the Python version (`python_info`) are included.
