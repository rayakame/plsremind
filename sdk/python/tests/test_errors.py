from __future__ import annotations

from plsremind import errors


def test_api_error_carries_what_the_hub_said() -> None:
    error = errors.ConflictError(409, "delivery_closed", "The delivery is closed.", {"status": "too late"})
    assert isinstance(error, errors.APIError)
    assert isinstance(error, errors.PlsremindError)
    assert (error.status, error.code, error.message) == (409, "delivery_closed", "The delivery is closed.")
    assert error.fields == {"status": "too late"}
    assert str(error) == "The delivery is closed."
    assert errors.NotFoundError(404, "not_found", "No.").fields == {}


def test_rate_limit_carries_the_wait() -> None:
    assert errors.RateLimitedError(429, "rate_limited", "Slow down.", None, 12).retry_after == 12
    assert errors.RateLimitedError(429, "rate_limited", "Slow down.").retry_after is None


def test_gateway_error() -> None:
    error = errors.GatewayError("Authentication failed.", 4004)
    assert error.code == 4004
    assert error.reason == "Authentication failed."
    assert str(error) == "Authentication failed."
    assert errors.GatewayError("The hub did not answer.").code is None
