from __future__ import annotations

import http.cookiejar
import json
import sys
import time
import urllib.error
import urllib.request

PASSWORD = "upgrade test password"
# Everything a user or an admin keeps, read back the way the dashboard reads it.
READS = [
    "/users/@me",
    "/users/@me/lists",
    "/users/@me/items?status=all",
    "/users/@me/reminder-plans",
    "/users/@me/tokens",
    "/users/@me/connections",
    "/users/@me/identities",
    "/users/@me/reminder-log",
    "/admin/settings",
    "/admin/connections",
    "/admin/invites",
    "/admin/users",
]


class Hub:
    def __init__(self, base: str, cookies: http.cookiejar.CookieJar) -> None:
        self.base = base.rstrip("/")
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cookies))

    def call(self, method: str, path: str, body: object = None, token: str | None = None) -> object:
        data = None if body is None else json.dumps(body).encode()
        request = urllib.request.Request(self.base + "/api/v1" + path, data=data, method=method)
        request.add_header("Origin", self.base)
        if data is not None:
            request.add_header("Content-Type", "application/json")
        if token is not None:
            request.add_header("Authorization", "Bearer " + token)
        try:
            with self.opener.open(request, timeout=30) as answer:
                raw = answer.read()
        except urllib.error.HTTPError as error:
            raise SystemExit(f"{method} {path}: {error.code} {error.read().decode(errors='replace')}") from None
        return json.loads(raw) if raw else None


def seed(hub: Hub) -> dict[str, object]:
    hub.call("POST", "/setup", {"username": "admin", "password": PASSWORD})
    hub.call("PATCH", "/admin/settings", {"name": "Upgrade test"})
    hub.call("POST", "/admin/invites", {})
    hub.call("PATCH", "/users/@me", {"display_name": "Ada", "timezone": "Europe/Berlin", "locale": "de"})
    work = hub.call("POST", "/users/@me/lists", {"name": "Work"})
    hub.call("POST", "/users/@me/lists", {"name": "Home"})
    item = hub.call(
        "POST",
        "/users/@me/items",
        {"title": "Report", "list_id": work["id"], "priority": "high", "due_at": "2031-03-01T09:00:00Z"},
    )
    hub.call("POST", f"/users/@me/items/{item['id']}/subitems", {"title": "Draft"})
    hub.call("POST", f"/users/@me/items/{item['id']}/reminders", {"in": "48h"})
    done = hub.call("POST", "/users/@me/items", {"title": "Old task"})
    hub.call("PATCH", f"/users/@me/items/{done['id']}", {"status": "done"})
    hub.call("PUT", "/users/@me/reminder-plans/low", {"before": ["2h"], "follow_up": "daily"})
    token = hub.call("POST", "/users/@me/tokens", {"name": "script", "remind": True})["token"]
    app = hub.call("POST", "/admin/connections", {"name": "Bot", "provider": "discord", "install_url": ""})
    hub.call("PUT", f"/users/@me/connections/{app['id']}", {"enabled": True})
    # A reminder that fires now, so that the update meets a delivery waiting for its adapter.
    soon = hub.call("POST", "/users/@me/items", {"title": "Ping"})
    hub.call("POST", f"/users/@me/items/{soon['id']}/reminders", {"in": "1m"})
    deadline = time.monotonic() + 120
    while not hub.call("GET", "/users/@me/reminder-log")["data"]:
        if time.monotonic() > deadline:
            raise SystemExit("the reminder did not fire within two minutes")
        time.sleep(3)
    return {"token": token, "reads": {path: hub.call("GET", path) for path in READS}}


def still_there(before: object, after: object, where: str) -> list[str]:
    # Fields an update adds are fine; what was there must still be there and the same.
    if isinstance(before, dict):
        if not isinstance(after, dict):
            return [f"{where}: was an object"]
        return [
            problem
            for key, value in before.items()
            for problem in still_there(value, after.get(key, Missing), f"{where}.{key}")
        ]
    if isinstance(before, list):
        if not isinstance(after, list) or len(after) != len(before):
            return [f"{where}: had {len(before)} entries, now {len(after) if isinstance(after, list) else after!r}"]
        return [
            problem
            for index, pair in enumerate(zip(before, after))
            for problem in still_there(*pair, f"{where}[{index}]")
        ]
    return [] if before == after else [f"{where}: was {before!r}, now {after!r}"]


class _Missing:
    def __repr__(self) -> str:
        return "missing"


Missing = _Missing()


def verify(hub: Hub, state: dict[str, object]) -> list[str]:
    problems = [
        problem
        for path, before in state["reads"].items()
        for problem in still_there(before, hub.call("GET", path), path)
    ]
    me = hub.call("GET", "/users/@me", token=state["token"])
    if me.get("display_name") != "Ada":
        problems.append("the personal token no longer signs in")
    return problems


def main() -> None:
    if len(sys.argv) != 4 or sys.argv[1] not in ("seed", "verify"):
        raise SystemExit("usage: upgrade-check.py seed|verify <base url> <state file>")
    command, base, state_file = sys.argv[1:4]
    cookies = http.cookiejar.LWPCookieJar(state_file + ".cookies")
    if command == "seed":
        hub = Hub(base, cookies)
        state = seed(hub)
        cookies.save(ignore_discard=True)
        with open(state_file, "w") as out:
            json.dump(state, out)
        print(f"seeded {len(READS)} reads")
        return
    cookies.load(ignore_discard=True)
    with open(state_file) as source:
        problems = verify(Hub(base, cookies), json.load(source))
    if problems:
        print("\n".join(problems), file=sys.stderr)
        raise SystemExit(1)
    print("everything is still there")


if __name__ == "__main__":
    main()
