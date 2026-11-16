from __future__ import annotations

import pytest

from plsremind.internal import routes
from plsremind.internal.routes import Route


def test_a_route_is_compiled_with_its_ids() -> None:
    route = Route("PATCH", "/users/{user}/items/{id}/subitems/{sid}")
    compiled = route.compile(user="u1", id="i1", sid="s1")
    assert compiled.method == "PATCH"
    assert compiled.path == "/users/u1/items/i1/subitems/s1"
    assert str(compiled) == "PATCH /users/u1/items/i1/subitems/s1"


def test_a_route_without_ids_compiles_to_itself() -> None:
    assert routes.GET_INFO.compile().path == "/info"


def test_an_id_stays_one_segment() -> None:
    compiled = routes.GET_ITEM.compile(user=routes.ME, id="a/b?c#d e")
    assert compiled.path == "/users/@me/items/a%2Fb%3Fc%23d%20e"


@pytest.mark.parametrize("value", ["", ".", ".."])
def test_an_id_that_would_reach_another_call_is_refused(value: str) -> None:
    with pytest.raises(ValueError, match="is not an ID"):
        routes.DELETE_SUBITEM.compile(user=routes.ME, id="i1", sid=value)


def test_a_route_names_what_it_was_not_given() -> None:
    with pytest.raises(KeyError, match="sid"):
        routes.DELETE_SUBITEM.compile(user=routes.ME, id="i1")
