from __future__ import annotations

import nox

nox.options.sessions = ["reformat", "ruff", "pyright", "pytest"]
nox.options.default_venv_backend = "uv"

PYTHON_PATHS = ["noxfile.py", "plsremind_discord/", "tests/"]
IMPORT_RULES = "I,RUF022,RUF023"
GENERATED = "plsremind_discord/db"


def sync(session: nox.Session, *groups: str) -> None:
    flags = [flag for group in groups for flag in ("--group", group)]
    session.run_install(
        "uv",
        "sync",
        "--locked",
        "--python",
        str(session.python or "3.15"),
        *flags,
        silent=True,
        env={"UV_PROJECT_ENVIRONMENT": session.virtualenv.location},
    )


@nox.session(reuse_venv=True)
def reformat(session: nox.Session) -> None:
    sync(session, "ruff")
    session.run("ruff", "format", *PYTHON_PATHS, *session.posargs)
    session.run("ruff", "check", *PYTHON_PATHS, "--select", IMPORT_RULES, "--fix", *session.posargs)


@nox.session(name="format-check", reuse_venv=True)
def format_check(session: nox.Session) -> None:
    sync(session, "ruff")
    session.run("ruff", "format", "--check", *PYTHON_PATHS, *session.posargs)
    session.run("ruff", "check", *PYTHON_PATHS, "--select", IMPORT_RULES, *session.posargs)


@nox.session(reuse_venv=True)
def ruff(session: nox.Session) -> None:
    sync(session, "ruff")
    session.run("ruff", "check", *PYTHON_PATHS, *session.posargs)


@nox.session(reuse_venv=True)
def pyright(session: nox.Session) -> None:
    # The pytest group is needed so that tests/ type-checks too.
    sync(session, "pyright", "pytest")
    session.run("pyright")


@nox.session(reuse_venv=True)
def pytest(session: nox.Session) -> None:
    sync(session, "pytest")
    session.run("coverage", "run", "-m", "pytest", "-m", "not hub", *session.posargs)
    session.run("coverage", "report", "--skip-covered")


@nox.session(reuse_venv=True)
def hub(session: nox.Session) -> None:
    sync(session, "pytest")
    session.run("pytest", "-m", "hub", *session.posargs)


@nox.session(reuse_venv=True)
def generate(session: nox.Session) -> None:
    sync(session, "sqlc")
    session.run("sqlc", "generate")


@nox.session(name="generated-check", reuse_venv=True)
def generated_check(session: nox.Session) -> None:
    sync(session, "sqlc")
    session.run("sqlc", "generate")
    session.run("git", "diff", "--exit-code", "--", GENERATED, external=True)
    session.run("git", "status", "--porcelain", "--", GENERATED, external=True)
