from __future__ import annotations

import pytest

from plsremind import errors


@pytest.mark.parametrize(
    ("status", "kind"),
    [
        (400, errors.BadRequestError),
        (401, errors.UnauthorizedError),
        (403, errors.ForbiddenError),
        (404, errors.NotFoundError),
        (409, errors.ConflictError),
        (422, errors.BadRequestError),
        (429, errors.RateLimitedError),
        (500, errors.ServerError),
        (503, errors.ServerError),
        (418, errors.APIError),
    ],
)
def test_status_picks_the_class(status: int, kind: type[errors.APIError]) -> None:
    error = errors.api_error(status, "some_code", "Something is wrong.", {"title": "too long"})
    assert type(error) is kind
    assert isinstance(error, errors.PlsremindError)
    assert (error.status, error.code, error.message) == (status, "some_code", "Something is wrong.")
    assert error.fields == {"title": "too long"}
    assert str(error) == "Something is wrong."


def test_rate_limit_carries_the_wait() -> None:
    error = errors.api_error(429, "rate_limited", "Slow down.", {}, retry_after=12)
    assert isinstance(error, errors.RateLimitedError)
    assert error.retry_after == 12


def test_gateway_error() -> None:
    error = errors.GatewayError("Authentication failed.", 4004)
    assert error.code == 4004
    assert error.reason == "Authentication failed."
    assert errors.GatewayError("The hub did not answer.").code is None
