from __future__ import annotations

import inspect
import pathlib
import re
import typing

import yaml

from plsremind.rest import RESTClient

SPEC = pathlib.Path(__file__).parents[3] / "docs" / "api" / "openapi.yaml"
METHODS = {"get", "post", "put", "patch", "delete"}
# Opening the gateway is the client's job, and anyone may ask a hub about itself.
NOT_A_CALL = {"connect_gateway"}
OPEN_TO_ALL = {"get_info"}


def snake(name: str) -> str:
    return re.sub(r"(?<!^)(?=[A-Z])", "_", name).lower()


def token_operations() -> set[str]:
    spec = yaml.safe_load(SPEC.read_text(encoding="utf-8"))
    default: list[dict[str, typing.Any]] = spec.get("security", [])
    names: set[str] = set()
    for path in spec["paths"].values():
        for method, operation in path.items():
            if method not in METHODS:
                continue
            security: list[dict[str, typing.Any]] = operation.get("security", default)
            if any("bearer" in scheme for scheme in security):
                names.add(snake(operation["operationId"]))
    return names


def test_every_token_operation_has_a_method() -> None:
    expected = (token_operations() - NOT_A_CALL) | OPEN_TO_ALL
    actual = {
        name
        for name, member in vars(RESTClient).items()
        if not name.startswith("_") and inspect.iscoroutinefunction(member)
    } - {"close"}
    assert actual == expected
    assert len(expected) == 31
