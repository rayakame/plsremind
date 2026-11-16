"""What can go wrong."""

from __future__ import annotations

import types
import typing

if typing.TYPE_CHECKING:
    import collections.abc

__all__ = [
    "APIError",
    "BadRequestError",
    "ConflictError",
    "ForbiddenError",
    "GatewayError",
    "NetworkError",
    "NotFoundError",
    "PlsremindError",
    "RateLimitedError",
    "ServerError",
    "UnauthorizedError",
]

_NO_FIELDS: typing.Final[collections.abc.Mapping[str, str]] = types.MappingProxyType({})


class PlsremindError(Exception):
    """The root of every error the SDK raises."""


class APIError(PlsremindError):
    """The hub answered a call with an error."""

    def __init__(
        self, status: int, code: str, message: str, fields: collections.abc.Mapping[str, str] | None = None
    ) -> None:
        super().__init__(message)
        self.status: typing.Final = status
        """The HTTP status of the answer."""
        self.code: typing.Final = code
        """The hub's name for what went wrong, for example `delivery_closed`."""
        self.message: typing.Final = message
        self.fields: typing.Final = _NO_FIELDS if fields is None else types.MappingProxyType(dict(fields))
        """What is wrong with which field of the request."""


class BadRequestError(APIError):
    """The hub did not accept what the call sent."""


class UnauthorizedError(APIError):
    """The token is missing, wrong or no longer valid."""


class ForbiddenError(APIError):
    """The token may not do this."""


class NotFoundError(APIError):
    """There is no such thing, or not for this token."""


class ConflictError(APIError):
    """The call does not fit the state of what it is about."""


class RateLimitedError(APIError):
    """Too many calls in a short time."""

    def __init__(
        self,
        status: int,
        code: str,
        message: str,
        fields: collections.abc.Mapping[str, str] | None = None,
        retry_after: float | None = None,
    ) -> None:
        super().__init__(status, code, message, fields)
        self.retry_after: typing.Final = retry_after
        """How many seconds the hub asks to wait, if it said."""


class ServerError(APIError):
    """Something went wrong inside the hub."""


class NetworkError(PlsremindError):
    """The hub did not answer, or not in a way that could be read."""


class GatewayError(PlsremindError):
    """The gateway connection ended and will not be tried again."""

    def __init__(self, reason: str, code: int | None = None) -> None:
        super().__init__(reason)
        self.reason: typing.Final = reason
        self.code: typing.Final = code
        """The close code, if the hub gave one."""


_BY_STATUS: typing.Final[dict[int, type[APIError]]] = {
    400: BadRequestError,
    401: UnauthorizedError,
    403: ForbiddenError,
    404: NotFoundError,
    409: ConflictError,
    422: BadRequestError,
}
_TOO_MANY: typing.Final = 429
_SERVER: typing.Final = 500


def api_error(
    status: int,
    code: str,
    message: str,
    fields: collections.abc.Mapping[str, str] | None = None,
    retry_after: float | None = None,
) -> APIError:
    """Build the error for an answer of the hub."""
    if status == _TOO_MANY:
        return RateLimitedError(status, code, message, fields, retry_after)
    kind = _BY_STATUS.get(status, ServerError if status >= _SERVER else APIError)
    return kind(status, code, message, fields)
