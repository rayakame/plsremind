from __future__ import annotations

import typing
import urllib.parse

import msgspec

ME: typing.Final = "@me"


class CompiledRoute(msgspec.Struct, frozen=True, gc=False):
    method: str
    path: str

    def __str__(self) -> str:
        return f"{self.method} {self.path}"


class Route(msgspec.Struct, frozen=True, gc=False):
    method: str
    path: str

    def compile(self, **ids: str) -> CompiledRoute:
        return CompiledRoute(self.method, self.path.format_map({name: _segment(value) for name, value in ids.items()}))


def _segment(value: str) -> str:
    if value == ME:
        return value
    # An empty or dot segment would be folded away on the way out and reach another call.
    if value in {"", ".", ".."}:
        msg = f"{value!r} is not an ID."
        raise ValueError(msg)
    return urllib.parse.quote(value, safe="")


GET_INFO: typing.Final = Route("GET", "/info")

GET_USER: typing.Final = Route("GET", "/users/{user}")
UPDATE_USER: typing.Final = Route("PATCH", "/users/{user}")
LIST_IDENTITIES: typing.Final = Route("GET", "/users/{user}/identities")
GET_IDENTITY_AVATAR: typing.Final = Route("GET", "/users/{user}/identities/{id}/avatar")

LIST_LISTS: typing.Final = Route("GET", "/users/{user}/lists")
CREATE_LIST: typing.Final = Route("POST", "/users/{user}/lists")
REORDER_LISTS: typing.Final = Route("PUT", "/users/{user}/lists/order")
RENAME_LIST: typing.Final = Route("PATCH", "/users/{user}/lists/{id}")
DELETE_LIST: typing.Final = Route("DELETE", "/users/{user}/lists/{id}")

LIST_REMINDER_PLANS: typing.Final = Route("GET", "/users/{user}/reminder-plans")
SET_REMINDER_PLAN: typing.Final = Route("PUT", "/users/{user}/reminder-plans/{priority}")
RESET_REMINDER_PLAN: typing.Final = Route("DELETE", "/users/{user}/reminder-plans/{priority}")

LIST_ITEMS: typing.Final = Route("GET", "/users/{user}/items")
CREATE_ITEM: typing.Final = Route("POST", "/users/{user}/items")
GET_ITEM: typing.Final = Route("GET", "/users/{user}/items/{id}")
UPDATE_ITEM: typing.Final = Route("PATCH", "/users/{user}/items/{id}")
DELETE_ITEM: typing.Final = Route("DELETE", "/users/{user}/items/{id}")

CREATE_SUBITEM: typing.Final = Route("POST", "/users/{user}/items/{id}/subitems")
REORDER_SUBITEMS: typing.Final = Route("PUT", "/users/{user}/items/{id}/subitems/order")
UPDATE_SUBITEM: typing.Final = Route("PATCH", "/users/{user}/items/{id}/subitems/{sid}")
DELETE_SUBITEM: typing.Final = Route("DELETE", "/users/{user}/items/{id}/subitems/{sid}")

GET_ITEM_SCHEDULE: typing.Final = Route("GET", "/users/{user}/items/{id}/schedule")
LIST_REMINDERS: typing.Final = Route("GET", "/users/{user}/items/{id}/reminders")
CREATE_REMINDER: typing.Final = Route("POST", "/users/{user}/items/{id}/reminders")
DELETE_REMINDER: typing.Final = Route("DELETE", "/users/{user}/items/{id}/reminders/{rid}")

LOOKUP_USER: typing.Final = Route("GET", "/connection/users/lookup")
LIST_USER_CONNECTIONS: typing.Final = Route("GET", "/users/{user}/connections")
SET_CONNECTION_GRANT: typing.Final = Route("PUT", "/users/{user}/connections/{id}")

LIST_DELIVERIES: typing.Final = Route("GET", "/deliveries")
ACKNOWLEDGE_DELIVERY: typing.Final = Route("PUT", "/deliveries/{id}")
REPORT_TEST: typing.Final = Route("PUT", "/deliveries/tests/{id}")

LIST_REMINDER_LOG: typing.Final = Route("GET", "/users/{user}/reminder-log")
