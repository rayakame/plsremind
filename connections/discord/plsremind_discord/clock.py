from __future__ import annotations

import datetime


class Clock:
    def now(self) -> datetime.datetime:
        return datetime.datetime.now(datetime.UTC)
