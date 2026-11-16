from __future__ import annotations

import asyncio
import base64
import dataclasses
import os
import pathlib
import secrets
import shutil
import signal
import socket
import subprocess
import tempfile
import time
import typing
import urllib.request

import asyncpg

HUB_SOURCE: typing.Final = pathlib.Path(__file__).parents[4] / "hub"
# Clients behind this header count as separate addresses for the hub's rate limit.
PROXY_HEADER: typing.Final = "X-Forwarded-For"


class MissingToolError(RuntimeError):
    pass


def _run(command: list[str], cwd: pathlib.Path | None = None) -> str:
    try:
        done = subprocess.run(command, cwd=cwd, check=False, capture_output=True, text=True)  # ruff: ignore[subprocess-without-shell-equals-true]
    except OSError as cause:
        msg = f"The hub tests need Docker and Go: {command[0]} could not be started."
        raise MissingToolError(msg) from cause
    if done.returncode != 0:
        failed = f'"{" ".join(command[:2])}" failed:\n{done.stderr}'
        # A daemon that is not running is a missing tool; a hub that does not build is a defect.
        if command[0] == "docker" and "daemon" in done.stderr.lower():
            raise MissingToolError(failed)
        raise RuntimeError(failed)
    return done.stdout


def _free_port() -> int:
    with socket.socket() as probe:
        probe.bind(("127.0.0.1", 0))
        return int(probe.getsockname()[1])


class _GaveUpError(RuntimeError):
    pass


def _until(what: str, attempt: typing.Callable[[], bool], seconds: float = 60.0) -> None:
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        try:
            if attempt():
                return
        except _GaveUpError:
            raise
        except Exception:  # ruff: ignore[blind-except, try-except-pass]
            pass
        time.sleep(0.25)
    msg = f"Timed out waiting for {what}."
    raise TimeoutError(msg)


def _database_answers(url: str) -> bool:
    async def connect() -> bool:
        connection = await asyncpg.connect(url, timeout=3)
        await connection.close()
        return True

    return asyncio.run(connect())


@dataclasses.dataclass
class LaunchedHub:
    url: str
    database_url: str
    _binary: pathlib.Path
    _work: pathlib.Path
    _container: str
    environment: dict[str, str]
    _process: subprocess.Popen[bytes] | None = None

    def start(self) -> None:
        log = None if os.environ.get("PLSREMIND_TEST_LOG") else subprocess.DEVNULL
        self._process = subprocess.Popen([str(self._binary)], env=self.environment, stdout=log, stderr=log)  # ruff: ignore[subprocess-without-shell-equals-true]
        process = self._process

        def answers() -> bool:
            if process.poll() is not None:
                msg = f"The hub exited with {process.returncode} while starting. Set PLSREMIND_TEST_LOG=1 to see why."
                raise _GaveUpError(msg)
            with urllib.request.urlopen(f"{self.url}/api/v1/info", timeout=2) as response:  # ruff: ignore[suspicious-url-open-usage]
                return bool(response.status == 200)

        _until("the hub", answers)

    def halt(self, how: signal.Signals = signal.SIGTERM) -> None:
        process, self._process = self._process, None
        if process is None or process.poll() is not None:
            return
        process.send_signal(how)
        try:
            process.wait(15)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait()

    def restart(self) -> None:
        self.halt()
        self.start()

    def stop(self) -> None:
        self.halt(signal.SIGKILL)
        subprocess.run(["docker", "rm", "-f", "-v", self._container], check=False, capture_output=True)  # ruff: ignore[subprocess-without-shell-equals-true, start-process-with-partial-path]
        shutil.rmtree(self._work, ignore_errors=True)


def launch_hub() -> LaunchedHub:
    work = pathlib.Path(tempfile.mkdtemp(prefix="plsremind-hub-"))
    binary = work / "plsremind"
    try:
        _run(["go", "build", "-o", str(binary), "./cmd/plsremind"], HUB_SOURCE)
        container = _run(
            [
                "docker",
                "run",
                "-d",
                "--rm",
                "-e",
                "POSTGRES_PASSWORD=plsremind",
                "-e",
                "POSTGRES_DB=plsremind",
                "-p",
                "127.0.0.1::5432",
                "postgres:18",
            ]
        ).strip()
    except BaseException:
        shutil.rmtree(work, ignore_errors=True)
        raise
    port = _free_port()
    url = f"http://127.0.0.1:{port}"
    hub = LaunchedHub(url, "", binary, work, container, {})
    try:
        mapped = _run(["docker", "port", container, "5432/tcp"]).strip().splitlines()[0]
        database_port = mapped.rsplit(":", 1)[1]
        hub.database_url = f"postgres://postgres:plsremind@127.0.0.1:{database_port}/plsremind?sslmode=disable"
        _until("PostgreSQL", lambda: _database_answers(hub.database_url))
        hub.environment = {
            "PATH": os.environ.get("PATH", ""),
            "PLSREMIND_DATABASE_URL": hub.database_url,
            "PLSREMIND_PUBLIC_URL": url,
            "PLSREMIND_LISTEN_ADDR": f"127.0.0.1:{port}",
            "PLSREMIND_SECRET_KEY": base64.b64encode(secrets.token_bytes(32)).decode(),
            "PLSREMIND_PROXY_HEADER": PROXY_HEADER,
        }
        hub.start()
    except BaseException:
        hub.stop()
        raise
    return hub
