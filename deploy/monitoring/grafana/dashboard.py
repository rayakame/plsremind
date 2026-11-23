"""Write the plsremind Grafana dashboard: python3 dashboard.py plsremind.json"""

import json
import sys

DS = {"type": "prometheus", "uid": "${datasource}"}
BOT = '{instance=~"$bot"}'

panels = []
ids = iter(range(1, 1000))
y = 0


def row(title):
    global y
    panels.append(
        {
            "type": "row",
            "title": title,
            "id": next(ids),
            "collapsed": False,
            "gridPos": {"h": 1, "w": 24, "x": 0, "y": y},
            "panels": [],
        }
    )
    y += 1


def series(title, targets, unit, x, w=8, h=8, description="", empty="0"):
    panels.append(
        {
            "type": "timeseries",
            "title": title,
            "description": description,
            "id": next(ids),
            "datasource": DS,
            "gridPos": {"h": h, "w": w, "x": x, "y": y},
            "fieldConfig": {
                "defaults": {
                    "unit": unit,
                    "noValue": empty,
                    "color": {"mode": "palette-classic"},
                    "custom": {"lineWidth": 2, "fillOpacity": 0, "showPoints": "never", "spanNulls": False},
                },
                "overrides": [],
            },
            "options": {
                "legend": {"displayMode": "list", "placement": "bottom", "showLegend": True},
                "tooltip": {"mode": "multi", "sort": "desc"},
            },
            "targets": [
                {"datasource": DS, "expr": expr, "legendFormat": legend, "refId": chr(65 + i)}
                for i, (expr, legend) in enumerate(targets)
            ],
        }
    )


def stat(title, expr, unit, x, w=4, h=4, description="", empty="0"):
    panels.append(
        {
            "type": "stat",
            "title": title,
            "description": description,
            "id": next(ids),
            "datasource": DS,
            "gridPos": {"h": h, "w": w, "x": x, "y": y},
            "fieldConfig": {
                "defaults": {"unit": unit, "noValue": empty, "color": {"mode": "fixed", "fixedColor": "text"}},
                "overrides": [],
            },
            "options": {
                "reduceOptions": {"calcs": ["lastNotNull"], "fields": "", "values": False},
                "colorMode": "none",
                "graphMode": "none",
                "textMode": "value",
                "justifyMode": "center",
            },
            "targets": [{"datasource": DS, "expr": expr, "refId": "A", "instant": True}],
        }
    )


def table(title, expr, x, w=24, h=5, description="", empty="none firing"):
    panels.append(
        {
            "type": "table",
            "title": title,
            "description": description,
            "id": next(ids),
            "datasource": DS,
            "gridPos": {"h": h, "w": w, "x": x, "y": y},
            "fieldConfig": {"defaults": {"noValue": empty}, "overrides": []},
            "options": {"showHeader": True, "cellHeight": "sm"},
            "targets": [{"datasource": DS, "expr": expr, "refId": "A", "instant": True, "format": "table"}],
            "transformations": [{"id": "organize", "options": {"excludeByName": {"Time": True, "Value": True}}}],
        }
    )


def advance(h):
    global y
    y += h


table(
    "Firing alerts",
    'sum by (alertname, severity, instance) (ALERTS{alertstate="firing"})',
    0,
    description="Alerts vmalert holds as firing right now. Empty while all is well.",
)
advance(5)

row("Reminders")
stat("Open deliveries", "sum(plsremind_deliveries_open)", "short", 0, description="Deliveries waiting for a channel to confirm them.")
stat(
    "Late, p95 over 10 min",
    "histogram_quantile(0.95, sum by (le) (rate(plsremind_reminder_lateness_seconds_bucket[10m])))",
    "s",
    4,
    description="How late the slowest five percent of reminders fired.",
    empty="-",
)
stat("Since the ticker last succeeded", "time() - max(plsremind_ticker_last_success_timestamp_seconds)", "s", 8, empty="-")
stat(
    "Waiting in the bots",
    f"sum(plsremind_discord_deliveries_waiting{BOT})",
    "short",
    12,
    description="Deliveries the chosen Discord bots try again later.",
    empty="no bot",
)
stat(
    "Bots connected to the hub",
    f"min(plsremind_discord_hub_connected{BOT})",
    "bool_yes_no",
    16,
    description="No as soon as one of the chosen bots is not connected.",
    empty="no bot",
)
stat("Hub version", "count by (version) (plsremind_build_info)", "short", 20, empty="-")
panels[-1]["options"]["textMode"] = "name"
panels[-1]["targets"][0]["legendFormat"] = "{{version}}"
advance(4)
series("Reminders by outcome", [("sum by (outcome) (rate(plsremind_reminders_total[5m]))", "{{outcome}}")], "ops", 0)
series(
    "How late reminders fire",
    [
        ("histogram_quantile(0.5, sum by (le) (rate(plsremind_reminder_lateness_seconds_bucket[5m])))", "median"),
        ("histogram_quantile(0.95, sum by (le) (rate(plsremind_reminder_lateness_seconds_bucket[5m])))", "p95"),
    ],
    "s",
    8,
    empty="-",
)
series(
    "Deliveries confirmed and expired",
    [
        ("sum by (result) (rate(plsremind_deliveries_acknowledged_total[5m]))", "{{result}}"),
        ("sum by (channel) (rate(plsremind_deliveries_expired_total[5m]))", "expired on {{channel}}"),
    ],
    "ops",
    16,
)
advance(8)

row("Hub")
series("Requests by status", [("sum by (status) (rate(plsremind_http_requests_total[5m]))", "{{status}}")], "reqps", 0)
series(
    "Slowest routes, p95",
    [
        (
            "topk(5, histogram_quantile(0.95, sum by (le, route) (rate(plsremind_http_request_duration_seconds_bucket[5m]))))",
            "{{route}}",
        )
    ],
    "s",
    8,
    empty="-",
)
series("Ticker rounds", [("sum by (result) (rate(plsremind_ticker_runs_total[5m]))", "{{result}}")], "ops", 16)
advance(8)
series("Gateway sessions", [("sum by (kind) (plsremind_gateway_sessions)", "{{kind}}")], "short", 0)
series("Gateway connections ended, by code", [("sum by (code) (rate(plsremind_gateway_closes_total[5m]))", "{{code}}")], "ops", 8)
series("Database connections", [("sum by (state) (plsremind_db_pool_connections)", "{{state}}")], "short", 16)
advance(8)
series(
    "Server errors by route",
    [('sum by (route) (rate(plsremind_http_requests_total{status=~"5.."}[5m]))', "{{route}}")],
    "reqps",
    0,
)
series(
    "Waits for a database connection",
    [("sum(rate(plsremind_db_pool_empty_acquires_total[5m]))", "had to wait")],
    "ops",
    8,
    description="Requests that found no connection free and waited for one to be released or made.",
)
series(
    "Time spent waiting for a connection",
    [("sum(rate(plsremind_db_pool_acquire_wait_seconds_total[5m]))", "waited")],
    "s",
    16,
    description="Seconds per second that requests waited because no connection was free.",
)
advance(8)

row("Discord bots")
series(
    "Tries to send a reminder",
    [(f"sum by (instance, result) (rate(plsremind_discord_delivery_attempts_total{BOT}[5m]))", "{{instance}} {{result}}")],
    "ops",
    0,
    empty="no bot data",
)
series(
    "Interactions",
    [(f"sum by (instance, kind) (rate(plsremind_discord_interactions_total{BOT}[5m]))", "{{instance}} {{kind}}")],
    "ops",
    8,
    empty="no bot data",
)
series(
    "Interactions that failed",
    [(f"sum by (instance, kind) (rate(plsremind_discord_interaction_failures_total{BOT}[5m]))", "{{instance}} {{kind}}")],
    "ops",
    16,
    empty="no bot data",
)
advance(8)
series(
    "Messages drawn again",
    [(f"sum by (instance, result) (rate(plsremind_discord_redraws_total{BOT}[5m]))", "{{instance}} {{result}}")],
    "ops",
    0,
    empty="no bot data",
)
series(
    "DM channels opened",
    [(f"sum by (instance) (rate(plsremind_discord_dm_channels_opened_total{BOT}[5m]))", "{{instance}}")],
    "ops",
    8,
    empty="no bot data",
    description="A channel is kept once opened, so this stays near zero after the first reminder to each account.",
)
series(
    "Heartbeat latency to Discord",
    [(f"max by (instance) (plsremind_discord_discord_latency_seconds{BOT})", "{{instance}}")],
    "s",
    16,
    empty="no bot data",
)
advance(8)
series(
    "Writes whose event did not come back",
    [(f"sum by (instance) (rate(plsremind_discord_write_echo_timeouts_total{BOT}[5m]))", "{{instance}}")],
    "ops",
    0,
    empty="no bot data",
    description="The bot's own writes the hub's gateway did not report within two seconds.",
)
series(
    "Test reminders",
    [(f"sum by (instance, result) (rate(plsremind_discord_tests_total{BOT}[5m]))", "{{instance}} {{result}}")],
    "ops",
    8,
    empty="no bot data",
    description="Tests users asked for to see that a bot reaches them.",
)
advance(8)

dashboard = {
    "uid": "plsremind",
    "title": "plsremind",
    "tags": ["plsremind"],
    "editable": False,
    "schemaVersion": 41,
    "time": {"from": "now-6h", "to": "now"},
    "refresh": "30s",
    "timezone": "",
    "graphTooltip": 1,
    "panels": panels,
    "templating": {
        "list": [
            {
                "name": "datasource",
                "label": "Data source",
                "type": "datasource",
                "query": "prometheus",
                "current": {"text": "VictoriaMetrics", "value": "victoriametrics"},
                "hide": 0,
                "refresh": 1,
            },
            {
                "name": "bot",
                "label": "Bot",
                "type": "query",
                "datasource": DS,
                "definition": 'label_values(up{job="plsremind-discord"}, instance)',
                "query": {"query": 'label_values(up{job="plsremind-discord"}, instance)', "refId": "bots"},
                "multi": True,
                "includeAll": True,
                "allValue": ".*",
                "current": {"text": "All", "value": "$__all"},
                "hide": 0,
                "refresh": 2,
                "sort": 1,
            },
        ]
    },
    "annotations": {"list": []},
    "links": [],
}

with open(sys.argv[1], "w") as out:
    json.dump(dashboard, out, indent=2)
    out.write("\n")
print(len(panels), "panels")
