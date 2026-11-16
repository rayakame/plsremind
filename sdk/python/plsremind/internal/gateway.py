from __future__ import annotations

import asyncio
import logging
import random as random_module
import typing

import aiohttp
import msgspec

from plsremind import errors
from plsremind.enums import ClientStatus

if typing.TYPE_CHECKING:
    import collections.abc

FIRST_WAIT = 1.0
LONGEST_WAIT = 60.0
WAIT_AFTER_TOO_MANY = 5.0
HANDSHAKE_SILENCE = 15.0
CLOSE_GRACE = 2.0

_LOGGER: typing.Final = logging.getLogger("plsremind")

_DISPATCH: typing.Final = 0
_HEARTBEAT: typing.Final = 1
_IDENTIFY: typing.Final = 2
_RESUME: typing.Final = 6
_RECONNECT: typing.Final = 7
_INVALID_SESSION: typing.Final = 9
_HELLO: typing.Final = 10
_HEARTBEAT_ACK: typing.Final = 11

_CLOSE_NORMAL: typing.Final = 1000
_CLOSE_RESUMING: typing.Final = 4000
_NO_CLOSE_FRAME: typing.Final = 1006
_BAD_SEQUENCE: typing.Final = 4007
_TOO_MANY: typing.Final = 4008
_FATAL: typing.Final = frozenset({4004, 4013, 4014})
_RECONNECT_REQUESTS: typing.Final = 4
_DOUBLINGS: typing.Final = 6


class _Frame(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    op: int
    d: msgspec.Raw = msgspec.Raw(b"null")
    s: int | None = None
    t: str | None = None


class _Hello(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    heartbeat_interval: int


class _Session(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    session_id: str


class _Lost(typing.NamedTuple):
    code: int | None
    reason: str
    reconnect_now: bool = False
    cause: BaseException | None = None


def _sent_code(code: int | None) -> int | None:
    # 1006 is what a library reports when no close frame came at all.
    return None if code is None or code == _NO_CLOSE_FRAME else int(code)


_FRAME: typing.Final = msgspec.json.Decoder(_Frame)
_STOPPED_ANSWERING: typing.Final = _Lost(None, "The hub stopped answering heartbeats.")


class Gateway:
    def __init__(
        self,
        *,
        url: collections.abc.Callable[[], collections.abc.Awaitable[str]],
        token: str,
        intents: collections.abc.Sequence[str],
        session: collections.abc.Callable[[], aiohttp.ClientSession],
        on_dispatch: collections.abc.Callable[[str, msgspec.Raw], None],
        on_status: collections.abc.Callable[[ClientStatus], None],
        on_disconnected: collections.abc.Callable[[int | None, str], None],
        on_closed: collections.abc.Callable[[errors.GatewayError], None],
        sync_after_ready: bool = False,
        random: collections.abc.Callable[[], float] = random_module.random,  # ruff: ignore[suspicious-non-cryptographic-random-usage]
        sleep: collections.abc.Callable[[float], collections.abc.Awaitable[None]] = asyncio.sleep,
    ) -> None:
        self._url: typing.Final = url
        self._token: typing.Final = token
        self._intents: typing.Final = list(intents)
        self._http: typing.Final = session
        self._on_dispatch: typing.Final = on_dispatch
        self._on_status: typing.Final = on_status
        self._on_disconnected: typing.Final = on_disconnected
        self._on_closed: typing.Final = on_closed
        self._sync_after_ready: typing.Final = sync_after_ready
        self._syncing = False
        self._logged_in = False
        self._random: typing.Final = random
        self._sleep: typing.Final = sleep
        self._task: asyncio.Task[None] | None = None
        self._running = False
        self._address: str | None = None
        self._session: str | None = None
        self._seq: int | None = None
        self._established = False
        self._online = False
        self._attempts = 0
        self._acked = True
        self._silenced: _Lost | None = None
        self._heart: asyncio.Task[None] | None = None
        self._aborted: errors.GatewayError | None = None
        self._restart = False

    def start(self) -> None:
        if self._running:
            return
        self._running = True
        self._task = asyncio.create_task(self._run(), name="plsremind-gateway")

    async def stop(self) -> None:
        self._running = False
        task, self._task = self._task, None
        if task is not None and not task.done() and task is not asyncio.current_task():
            task.cancel()
            await asyncio.gather(task, return_exceptions=True)
        self._reset()

    def _reset(self) -> None:
        self._syncing = False
        self._session = None
        self._seq = None
        self._established = False
        self._online = False
        self._attempts = 0

    def _end(self, error: errors.GatewayError) -> None:
        self._running = False
        self._restart = False
        self._reset()
        self._on_closed(error)

    def abort(self, error: errors.GatewayError) -> None:
        self._aborted = error

    def restart_session(self) -> None:
        self._restart = True

    def settle(self) -> None:
        if self._syncing:
            self._syncing = False
            self._online_now()

    def _online_now(self) -> None:
        self._logged_in = True
        self._established = True
        self._online = True
        self._attempts = 0
        self._on_status(ClientStatus.CONNECTED)

    async def _run(self) -> None:
        try:
            await self._loop()
        except errors.GatewayError as error:
            self._end(error)
        except Exception as cause:  # ruff: ignore[blind-except]
            if self._running:
                error = errors.GatewayError(f"The gateway failed: {cause}")
                error.__cause__ = cause
                self._end(error)

    async def _loop(self) -> None:  # ruff: ignore[complex-structure]
        self._on_status(ClientStatus.CONNECTING)
        while self._running:
            lost = await self._connection()
            if not self._running:
                return
            if lost.code in _FATAL:
                raise errors.GatewayError(lost.reason or f"The gateway closed with {lost.code}.", lost.code)
            if not self._established:
                if not lost.reconnect_now:
                    reason = lost.reason or "The gateway connection could not be established."
                    raise errors.GatewayError(reason, lost.code) from lost.cause
                if self._attempts >= _RECONNECT_REQUESTS:
                    msg = "The hub kept asking to reconnect before it let the client in."
                    raise errors.GatewayError(msg)
            # A session whose state never fully arrived cannot be resumed into a complete one.
            if lost.code == _BAD_SEQUENCE or self._syncing:
                self._syncing = False
                self._session = None
                self._seq = None
            if self._online:
                self._online = False
                self._on_status(ClientStatus.RECONNECTING)
                self._on_disconnected(lost.code, lost.reason)
                if not self._running:
                    return
            if lost.reconnect_now and self._established:
                continue
            wait = min(LONGEST_WAIT, FIRST_WAIT * 2 ** min(self._attempts, _DOUBLINGS)) * (0.5 + 0.5 * self._random())
            self._attempts += 1
            if lost.code == _TOO_MANY:
                wait = max(wait, WAIT_AFTER_TOO_MANY)
            await self._sleep(wait)

    async def _connection(self) -> _Lost:
        try:
            if self._address is None:
                self._address = await self._url()
        except errors.PlsremindError as cause:
            return _Lost(None, f"The gateway could not be reached: {cause}", cause=cause)
        self._silenced = None
        self._heart = None
        self._logged_in = False
        socket: aiohttp.ClientWebSocketResponse | None = None
        try:
            # A socket that goes quiet before the session stands would otherwise be waited on forever.
            async with asyncio.timeout(HANDSHAKE_SILENCE) as silence:
                try:
                    socket = await self._http().ws_connect(
                        self._address,
                        max_msg_size=0,
                        timeout=aiohttp.ClientWSTimeout(ws_close=CLOSE_GRACE),  # pyright: ignore[reportCallIssue]
                    )
                except (aiohttp.ClientError, OSError) as cause:
                    return _Lost(None, f"The gateway could not be reached: {cause}", cause=cause)
                while True:
                    lost = await self._receive(socket, await socket.receive(), silence)
                    if lost is not None:
                        return lost
        except TimeoutError:
            return _Lost(None, "The hub did not finish the handshake.")
        except msgspec.MsgspecError as cause:
            return _Lost(None, f"The hub sent something the gateway could not read: {cause}")
        finally:
            heart, self._heart = self._heart, None
            if heart is not None:
                heart.cancel()
                await asyncio.gather(heart, return_exceptions=True)
            if socket is not None and not socket.closed:
                await socket.close(code=_CLOSE_RESUMING if self._running else _CLOSE_NORMAL)

    async def _receive(  # ruff: ignore[complex-structure]
        self, socket: aiohttp.ClientWebSocketResponse, message: aiohttp.WSMessage, silence: asyncio.Timeout
    ) -> _Lost | None:
        if message.type is aiohttp.WSMsgType.CLOSE:
            return _Lost(int(message.data), message.extra or "")
        if message.type is not aiohttp.WSMsgType.TEXT:
            return self._silenced or _Lost(_sent_code(socket.close_code), "")
        try:
            frame = _FRAME.decode(message.data)
        except msgspec.MsgspecError:
            return None
        if frame.op == _HELLO:
            interval = msgspec.json.decode(frame.d, type=_Hello).heartbeat_interval / 1000
            await self._identify(socket)
            if self._heart is not None:
                self._heart.cancel()
            self._heart = asyncio.create_task(self._beat(socket, interval), name="plsremind-heartbeat")
        elif frame.op == _HEARTBEAT_ACK:
            self._acked = True
        elif frame.op == _RECONNECT:
            return _Lost(None, "The hub asked to reconnect.", reconnect_now=True)
        elif frame.op == _INVALID_SESSION:
            self._session = None
            self._seq = None
            self._syncing = False
            self._logged_in = False
            await self._identify(socket)
        elif frame.op == _DISPATCH and frame.t is not None:
            self._dispatch(frame.t, frame)
            if self._restart:
                self._restart = False
                self._session = None
                self._seq = None
                return _Lost(None, "The session is started over.", reconnect_now=True)
        # An answered heartbeat says nothing about a login or a state that is still due.
        if frame.op != _HEARTBEAT_ACK:
            silence.reschedule(None if self._logged_in else asyncio.get_running_loop().time() + HANDSHAKE_SILENCE)
        return None

    def _dispatch(self, name: str, frame: _Frame) -> None:
        if frame.s is not None:
            self._seq = frame.s
        if name == "READY":
            self._syncing = True
            try:
                self._session = msgspec.json.decode(frame.d, type=_Session).session_id
            except msgspec.MsgspecError as cause:
                self._running = False
                msg = "The hub's READY could not be read."
                raise errors.GatewayError(msg) from cause
        try:
            self._on_dispatch(name, frame.d)
        except Exception:
            _LOGGER.exception("The event %s could not be handled.", name)
        aborted, self._aborted = self._aborted, None
        if aborted is not None:
            self._running = False
            raise aborted
        if name == "RESUMED" or (name == "READY" and not self._sync_after_ready):
            self._syncing = False
            self._online_now()

    async def _identify(self, socket: aiohttp.ClientWebSocketResponse) -> None:
        if self._session is None:
            await self._send(socket, _IDENTIFY, {"token": self._token, "intents": self._intents})
        else:
            await self._send(socket, _RESUME, {"token": self._token, "session_id": self._session, "seq": self._seq})

    async def _beat(self, socket: aiohttp.ClientWebSocketResponse, interval: float) -> None:
        self._acked = True
        await asyncio.sleep(interval * self._random())
        while True:
            if not self._acked:
                self._silenced = _STOPPED_ANSWERING
                await socket.close(code=_CLOSE_RESUMING)
                return
            self._acked = False
            await self._send(socket, _HEARTBEAT, self._seq)
            await asyncio.sleep(interval)

    @staticmethod
    async def _send(socket: aiohttp.ClientWebSocketResponse, op: int, d: object) -> None:
        try:
            await socket.send_str(msgspec.json.encode({"op": op, "d": d}).decode())
        except (aiohttp.ClientError, OSError, RuntimeError):
            # The close that follows a dead socket starts the reconnect.
            return
