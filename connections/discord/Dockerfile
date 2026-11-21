# Build from the root of the repository: the bot depends on the SDK next to it.
FROM python:3.15-slim AS build
COPY --from=ghcr.io/astral-sh/uv:0.13.7 /uv /bin/uv
ENV UV_COMPILE_BYTECODE=1 UV_LINK_MODE=copy UV_PYTHON_DOWNLOADS=never
WORKDIR /app/connections/discord
COPY sdk/python /app/sdk/python
COPY connections/discord/pyproject.toml connections/discord/uv.lock connections/discord/README.md ./
RUN uv sync --locked --no-dev --no-editable --no-install-project
COPY connections/discord/plsremind_discord plsremind_discord
RUN uv sync --locked --no-dev --no-editable

FROM python:3.15-slim
RUN useradd --system --uid 10001 --create-home --home-dir /data plsremind
COPY --from=build /app/connections/discord/.venv /app/connections/discord/.venv
ENV PLSREMIND_DISCORD_DB=/data/discord.db PYTHONUNBUFFERED=1
VOLUME /data
USER plsremind
ENTRYPOINT ["/app/connections/discord/.venv/bin/python", "-m", "plsremind_discord"]
