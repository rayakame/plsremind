from __future__ import annotations

import asyncio
import collections.abc

# Discord gives an interaction three seconds; measured, the way there and back takes up to about one.
PATIENCE = 1.0


async def unhurried[T](
    work: collections.abc.Coroutine[object, object, T],
    wait: collections.abc.Callable[[], collections.abc.Awaitable[object]],
) -> T:
    """Do `work`. If it takes longer than Discord waits for an answer, call `wait` to make it wait."""
    task = asyncio.ensure_future(work)
    try:
        done, _ = await asyncio.wait({task}, timeout=PATIENCE)
        if not done:
            await wait()
    except BaseException:
        task.cancel()
        raise
    return await task
