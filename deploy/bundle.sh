#!/usr/bin/env bash
# Packs what a server needs into plsremind-deploy-<version>.tar.gz and prints its path.
set -euo pipefail

out="${1:?usage: ./bundle.sh <directory to write the bundle to>}"
mkdir -p "$out"
# Resolved before leaving the caller's directory, so that the printed path works where it was asked.
out="$(cd "$out" && pwd)"
cd "$(dirname "$0")"
python3 - "$out" <<'PY'
import gzip
import io
import json
import pathlib
import sys
import tarfile

version = json.loads(pathlib.Path("../.release-please-manifest.json").read_text())["."]
shipped = [
    "compose.yaml",
    ".env.example",
    "UPDATING.md",
    "backup.sh",
    "restore.sh",
    "monitoring/scrape.yml",
    "monitoring/alerts.yml",
    "monitoring/targets/discord.yml",
    "monitoring/grafana/provisioning/dashboards/plsremind.yml",
    "monitoring/grafana/provisioning/datasources/plsremind.yml",
    "monitoring/grafana/plsremind.json",
    *sorted(str(path) for path in pathlib.Path("examples").glob("*.yaml")),
]
target = pathlib.Path(sys.argv[1]) / f"plsremind-deploy-{version}.tar.gz"
raw = io.BytesIO()
# Fixed times and owners, so that the same commit always gives the same bundle.
with tarfile.open(fileobj=raw, mode="w", format=tarfile.PAX_FORMAT) as tar:
    for name in sorted(shipped):
        path = pathlib.Path(name)
        info = tarfile.TarInfo(f"plsremind/{name}")
        data = path.read_bytes()
        info.size = len(data)
        info.mode = 0o755 if path.stat().st_mode & 0o111 else 0o644
        info.mtime = 0
        info.uid = info.gid = 0
        info.uname = info.gname = ""
        tar.addfile(info, io.BytesIO(data))
with gzip.GzipFile(target, "wb", mtime=0) as packed:
    packed.write(raw.getvalue())
print(target)
PY
