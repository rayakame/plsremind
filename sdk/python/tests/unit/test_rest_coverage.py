from __future__ import annotations

import inspect
import pathlib
import re
import typing

import yaml

from plsremind.internal import routes
from plsremind.internal.routes import Route
from plsremind.rest import RESTClient

SPEC = pathlib.Path(__file__).parents[4] / "docs" / "api" / "openapi.yaml"
METHODS = {"get", "post", "put", "patch", "delete"}
# Opening the gateway is the client's job, and anyone may ask a hub about itself.
NOT_A_CALL = {"connect_gateway"}
OPEN_TO_ALL = {"get_info"}


def snake(name: str) -> str:
    return re.sub(r"(?<!^)(?=[A-Z])", "_", name).lower()


def token_operations() -> dict[str, tuple[str, str]]:
    spec = yaml.safe_load(SPEC.read_text(encoding="utf-8"))
    default: list[dict[str, typing.Any]] = spec.get("security", [])
    names: dict[str, tuple[str, str]] = {}
    for path, operations in spec["paths"].items():
        for method, operation in operations.items():
            if method not in METHODS:
                continue
            security: list[dict[str, typing.Any]] = operation.get("security", default)
            if any("bearer" in scheme for scheme in security) or snake(operation["operationId"]) in OPEN_TO_ALL:
                names[snake(operation["operationId"])] = (method.upper(), path)
    return names


def test_every_token_operation_has_a_method() -> None:
    expected = set(token_operations()) - NOT_A_CALL
    actual = {
        name
        for name, member in vars(RESTClient).items()
        if not name.startswith("_") and inspect.iscoroutinefunction(member)
    } - {"close"}
    assert actual == expected
    assert len(expected) == 31


def test_every_route_is_a_call_of_the_api_and_the_other_way_round() -> None:
    expected = {name.upper(): call for name, call in token_operations().items() if name not in NOT_A_CALL}
    actual = {name: (route.method, route.path) for name, route in vars(routes).items() if isinstance(route, Route)}
    assert actual == expected
