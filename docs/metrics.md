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

The Go runtime (`go_*`) and the process (`process_*`) are included.

## Health

The hub answers two paths on its public port, without a login and outside the rate
limits, so that a load balancer or an orchestrator can ask:

- `GET /healthz` is `200 ok` while the process serves.
- `GET /readyz` is `200 ok` when the database answers within two seconds, otherwise
  `503 unavailable`.

The image has no shell, so `plsremind health` asks `/readyz` on the hub's own listener
and exits with 0 or 1. The compose file uses it as the hub's healthcheck.
