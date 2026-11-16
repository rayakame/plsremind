"""The Python SDK for plsremind.

`Client` connects to a hub's gateway, keeps the state it is sent in `client.cache` and
hands events to listeners. Every call of the REST API is a method of `client.rest`.
"""

from __future__ import annotations

from plsremind._about import __version__ as __version__
from plsremind.enums import *
from plsremind.errors import *
from plsremind.events import *
from plsremind.models import *
from plsremind.rest import *
from plsremind.types import *
