"""The client: a gateway connection, a cache and listeners."""

from __future__ import annotations

import asyncio
import contextlib
import inspect
import logging
import signal
import typing

import aiohttp
import msgspec

from plsremind import _cache
from plsremind import _gateway
from plsremind import errors
from plsremind import events
from plsremind.enums import ClientStatus
from plsremind.enums import Intents
from plsremind.enums import intent_names
from plsremind.rest import RESTClient

if typing.TYPE_CHECKING:
    import collections.abc
    import types

    from plsremind.cache import Cache
    from plsremind.models import Principal
    from plsremind.types import ID

__all__ = ["Client", "Listener"]

LISTENER_GRACE = 5.0
"""How many seconds `Client.close` waits for running listeners before it cancels them."""

type Listener[E: events.Event] = collections.abc.Callable[[E], collections.abc.Coroutine[typing.Any, typing.Any, None]]
"""An `async def` that takes one event."""

type _AnyListener = collections.abc.Callable[[typing.Any], collections.abc.Coroutine[typing.Any, typing.Any, None]]

_LOGGER: typing.Final = logging.getLogger("plsremind")

# The events that change the cache or tell when a user's state is complete.
_STATE: typing.Final = frozenset(
    {
        "user.sync",
        "user.gone",
        "user.updated",
        "list.created",
        "list.updated",
        "list.deleted",
        "item.created",
        "item.updated",
        "item.deleted",
        "plan.updated",
    }
)


def _name(listener: object) -> str:
    return getattr(listener, "__qualname__", repr(listener))


def _annotated(listener: _AnyListener) -> type[events.Event]:
    parameters = list(inspect.signature(listener).parameters.values())
    if len(parameters) != 1:
        msg = f"{_name(listener)} must take exactly one parameter, the event."
        raise TypeError(msg)
    try:
        hints = typing.get_type_hints(listener)
    except Exception as cause:
        msg = f"The annotations of {_name(listener)} could not be read: {cause}. Name the event class in listen(...)."
        raise TypeError(msg) from cause
    kind = hints.get(parameters[0].name)
    if kind is None:
        msg = f"{_name(listener)} does not say which event it wants: annotate its parameter or pass listen(...) one."
        raise TypeError(msg)
    return kind


class _Waiter(typing.NamedTuple):
    predicate: collections.abc.Callable[[typing.Any], bool] | None
    future: asyncio.Future[typing.Any]


class Client:
    """A connection to a hub: events for listeners, state in `cache`, every call in `rest`.

    ```python
    client = plsremind.Client(
        url, token=token, intents=plsremind.Intents.DELIVERIES
    )


    @client.listen()
    async def on_delivery(event: plsremind.DeliveryCreateEvent) -> None: ...


    client.run()
    ```
    """

    def __init__(
        self,
        url: str,
        *,
        token: str,
        intents: Intents = Intents.NONE,
        rest_timeout: float = 30.0,
        rest_retry: bool = True,
        session: aiohttp.ClientSession | None = None,
    ) -> None:
        """Make a client for the hub at `url`. Nothing connects until `start` or `run`.

        Parameters
        ----------
        url
            The address of the hub, without `/api/v1`.
        token
            A personal token or the token of a connection.
        intents
            What to ask the gateway for. Without any, the client can only use `rest`.
        rest_timeout
            How many seconds a REST call may take.
        rest_retry
            Whether a REST call the hub turned away for its rate limit is tried again.
        session
            A session for REST and the gateway instead of the client's own, for example
            to go through a proxy. It is left open on `close`.
        """
        self.rest: typing.Final = RESTClient(url, token=token, timeout=rest_timeout, retry=rest_retry, session=session)
        """Every call of the REST API."""
        self._token: typing.Final = token
        self._given_session: typing.Final = session
        self._asked: typing.Final = intents
        self._granted: Intents | None = None
        self._store: typing.Final = _cache.CacheStore()
        self._listeners: dict[type[events.Event], list[_AnyListener]] = {}
        self._waiters: dict[type[events.Event], list[_Waiter]] = {}
        self._tasks: set[asyncio.Task[None]] = set()
        self._gateway: _gateway.Gateway | None = None
        self._socket_session: aiohttp.ClientSession | None = None
        self._status = ClientStatus.IDLE
        self._principal: Principal | None = None
        self._ready: events.Ready | None = None
        self._waiting: set[ID] = set()
        self._starting: asyncio.Future[None] | None = None
        self._ended: asyncio.Event | None = None
        self._failure: errors.GatewayError | None = None
        self._closing: asyncio.Future[None] | None = None

    @property
    def cache(self) -> Cache:
        """What the gateway has sent about each user. It stays readable after `close`."""
        return self._store

    @property
    def principal(self) -> Principal | None:
        """Who the client is logged in as, once the gateway has said so."""
        return self._principal

    @property
    def status(self) -> ClientStatus:
        """Where the client stands with its gateway connection."""
        return self._status

    @property
    def intents(self) -> Intents:
        """The intents of the session, or the ones asked for while there is none yet."""
        return self._asked if self._granted is None else self._granted

    def listen[E: events.Event](
        self, event_type: type[E] | None = None
    ) -> collections.abc.Callable[[Listener[E]], Listener[E]]:
        """Subscribe the decorated `async def` to an event class and its subclasses.

        Without an argument the class is read from the annotation of the listener's parameter.
        """

        def decorator(listener: Listener[E]) -> Listener[E]:
            kind = event_type if event_type is not None else typing.cast("type[E]", _annotated(listener))
            self.subscribe(kind, listener)
            return listener

        return decorator

    def subscribe[E: events.Event](self, event_type: type[E], listener: Listener[E]) -> None:
        """Call `listener` with every event of the class or a subclass of it.

        Raises
        ------
        TypeError
            If `event_type` is no event class, or `listener` is not an `async def` with one parameter.
        """
        if not (isinstance(event_type, type) and issubclass(event_type, events.Event)):  # pyright: ignore[reportUnnecessaryIsInstance]
            msg = f"{_name(listener)} wants {event_type!r}, which is not an event class."
            raise TypeError(msg)
        if not inspect.iscoroutinefunction(listener):
            msg = f"{_name(listener)} must be an async def."
            raise TypeError(msg)
        if len(inspect.signature(listener).parameters) != 1:
            msg = f"{_name(listener)} must take exactly one parameter, the event."
            raise TypeError(msg)
        self._listeners.setdefault(event_type, []).append(listener)

    def unsubscribe[E: events.Event](self, event_type: type[E], listener: Listener[E]) -> None:
        """Stop calling `listener` for the class. Unknown pairs are ignored."""
        listeners = self._listeners.get(event_type)
        if listeners is not None and listener in listeners:
            listeners.remove(listener)
            if not listeners:
                del self._listeners[event_type]

    async def wait_for[E: events.Event](
        self,
        event_type: type[E],
        *,
        timeout: float | None,  # ruff: ignore[async-function-with-timeout]
        predicate: collections.abc.Callable[[E], bool] | None = None,
    ) -> E:
        """Wait for the next event of the class, or a subclass, that the predicate accepts.

        Raises
        ------
        TimeoutError
            If none arrives within `timeout` seconds. `None` waits without limit.
        """
        waiter = _Waiter(predicate, asyncio.get_running_loop().create_future())
        self._waiters.setdefault(event_type, []).append(waiter)
        try:
            async with asyncio.timeout(timeout):
                return await waiter.future
        finally:
            waiters = self._waiters[event_type]
            waiters.remove(waiter)
            if not waiters:
                del self._waiters[event_type]

    async def start(self) -> None:
        """Connect, and return once the state of every user of the session is in the cache.

        Later drops are reconnected without the caller.

        Raises
        ------
        RuntimeError
            If the client has no intents or is already started.
        GatewayError
            If the hub cannot be reached or turns the client away.
        """
        if self._asked is Intents.NONE:
            msg = "start() needs intents: pass intents=... to the client."
            raise RuntimeError(msg)
        if self._gateway is not None:
            msg = "The client is already started."
            raise RuntimeError(msg)
        loop = asyncio.get_running_loop()
        self._failure = None
        self._ended = asyncio.Event()
        self._starting = starting = loop.create_future()
        session = self._given_session
        if session is None:
            self._socket_session = session = aiohttp.ClientSession()
        self._gateway = _gateway.Gateway(
            url=self._gateway_url,
            token=self._token,
            intents=intent_names(self._asked),
            session=lambda: session,
            on_dispatch=self._on_dispatch,
            on_status=self._on_status,
            on_disconnected=self._on_disconnected,
            on_closed=self._on_closed,
        )
        self._gateway.start()
        try:
            await starting
        except BaseException:
            await self.close()
            raise

    async def close(self) -> None:
        """End the session and every retry. The cache stays readable, and `start` works again."""
        if self._closing is not None:
            await asyncio.shield(self._closing)
            return
        self._closing = asyncio.get_running_loop().create_future()
        try:
            gateway, self._gateway = self._gateway, None
            if gateway is not None:
                await gateway.stop()
            starting, self._starting = self._starting, None
            if starting is not None and not starting.done():
                starting.set_exception(errors.GatewayError("The client was closed."))
                # Nobody may be waiting for it any more.
                starting.exception()
            self._ready = None
            self._waiting.clear()
            await self._finish_listeners()
            session, self._socket_session = self._socket_session, None
            if session is not None:
                await session.close()
            await self.rest.close()
            self._status = ClientStatus.IDLE if self._failure is None else ClientStatus.CLOSED
            if self._ended is not None:
                self._ended.set()
        finally:
            closing, self._closing = self._closing, None
            closing.set_result(None)

    async def join(self) -> None:
        """Wait until the client is closed or the hub has turned it away."""
        if self._ended is not None:
            await self._ended.wait()

    def run(self) -> None:
        """Start the client and block until SIGINT or SIGTERM, then close it.

        For programs that do nothing else. Inside a running event loop use `start` and `close`.

        Raises
        ------
        GatewayError
            If the hub cannot be reached or turns the client away.
        """
        with asyncio.Runner() as runner:
            runner.run(self._run())

    async def __aenter__(self) -> typing.Self:
        await self.start()
        return self

    async def __aexit__(
        self, kind: type[BaseException] | None, error: BaseException | None, trace: types.TracebackType | None
    ) -> None:
        await self.close()

    async def _run(self) -> None:
        loop = asyncio.get_running_loop()
        stop = asyncio.Event()
        handled: list[signal.Signals] = []
        for number in (signal.SIGINT, signal.SIGTERM):
            # Windows has no signal handlers on its event loop; Ctrl+C still ends run() there.
            with contextlib.suppress(NotImplementedError):
                loop.add_signal_handler(number, stop.set)
                handled.append(number)
        try:
            await self.start()
            waits: list[asyncio.Task[typing.Any]] = [
                asyncio.create_task(stop.wait()),
                asyncio.create_task(self.join()),
            ]
            try:
                await asyncio.wait(waits, return_when=asyncio.FIRST_COMPLETED)
            finally:
                for wait in waits:
                    wait.cancel()
                await asyncio.gather(*waits, return_exceptions=True)
        finally:
            failure = self._failure
            await self.close()
            for number in handled:
                loop.remove_signal_handler(number)
        if failure is not None:
            raise failure

    async def _gateway_url(self) -> str:
        return (await self.rest.get_info()).gateway_url

    async def _finish_listeners(self) -> None:
        tasks = self._tasks - {typing.cast("asyncio.Task[None]", asyncio.current_task())}
        if not tasks:
            return
        _, running = await asyncio.wait(tasks, timeout=LISTENER_GRACE)
        for task in running:
            task.cancel()
        await asyncio.gather(*running, return_exceptions=True)

    def _on_status(self, status: ClientStatus) -> None:
        self._status = status

    def _on_disconnected(self, code: int | None, reason: str) -> None:
        self._emit(events.DisconnectedEvent(code=code, reason=reason))

    def _on_closed(self, error: errors.GatewayError) -> None:
        self._status = ClientStatus.CLOSED
        self._failure = error
        self._gateway = None
        self._ready = None
        self._waiting.clear()
        self._emit(events.ClosedEvent(error=error))
        starting, self._starting = self._starting, None
        if starting is not None and not starting.done():
            starting.set_exception(error)
        if self._ended is not None:
            self._ended.set()

    def _on_dispatch(self, name: str, payload: msgspec.Raw) -> None:
        if name == "READY":
            ready = events.decode_ready(payload)
            self._principal = ready.principal
            self._granted = ready.intents
            self._ready = ready
            self._waiting = set(ready.users)
            self._store.begin(ready.principal, ready.users, ready.intents)
            self._arrived(None)
            return
        if name == "RESUMED":
            self._emit(events.ResumedEvent())
            return
        kind = events.EVENT_CLASSES.get(name)
        if kind is None or not (name in _STATE or self._wanted(kind)):
            return
        try:
            event = events.decode_event(name, payload, connection=self.rest.is_connection)
        except msgspec.MsgspecError:
            _LOGGER.exception("The event %s could not be read and was skipped.", name)
            return
        if event is None:
            return
        applied = self._store.apply(event)
        self._emit(applied.event)
        if applied.ready is not None:
            self._emit(events.UserReadyEvent(user_id=applied.ready))
            self._arrived(applied.ready)
        elif isinstance(event, events.UserGoneEvent):
            self._arrived(event.user_id)

    def _arrived(self, user: ID | None) -> None:
        if user is not None:
            if user not in self._waiting:
                return
            self._waiting.discard(user)
        if self._waiting or self._ready is None:
            return
        ready, self._ready = self._ready, None
        starting, self._starting = self._starting, None
        if starting is not None and not starting.done():
            starting.set_result(None)
        self._emit(events.ReadyEvent(principal=ready.principal, users=ready.users, intents=ready.intents))

    def _wanted(self, kind: type[events.Event]) -> bool:
        return any(base in self._listeners or base in self._waiters for base in kind.__mro__)

    def _emit(self, event: events.Event) -> None:
        for base in type(event).__mro__:
            for waiter in tuple(self._waiters.get(base, ())):
                self._offer(waiter, event)
            for listener in tuple(self._listeners.get(base, ())):
                task = asyncio.create_task(self._call(listener, event), name=f"plsremind-listener-{_name(listener)}")
                self._tasks.add(task)
                task.add_done_callback(self._tasks.discard)

    @staticmethod
    def _offer(waiter: _Waiter, event: events.Event) -> None:
        if waiter.future.done():
            return
        try:
            if waiter.predicate is None or waiter.predicate(event):
                waiter.future.set_result(event)
        except Exception as error:  # ruff: ignore[blind-except]
            waiter.future.set_exception(error)

    async def _call(self, listener: _AnyListener, event: events.Event) -> None:
        try:
            await listener(event)
        except Exception as error:
            if isinstance(event, events.ExceptionEvent) or events.ExceptionEvent not in self._listeners:
                _LOGGER.error(  # ruff: ignore[error-instead-of-exception]
                    "The listener %s raised while handling a %s.", _name(listener), type(event).__name__, exc_info=error
                )
                return
            self._emit(events.ExceptionEvent(exception=error, failed_event=event, failed_listener=listener))
