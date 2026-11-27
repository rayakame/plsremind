#!/usr/bin/env bash
# Packs the monitoring add-on, as a release ships it, into plsremind-monitoring.tar.gz and prints its path.
set -euo pipefail

out="${1:?usage: ./pack-monitoring.sh <directory to write the add-on to>}"
mkdir -p "$out"
# Resolved before leaving the caller's directory, so that the printed path works where it was asked.
out="$(cd "$out" && pwd)"
cd "$(dirname "$0")"
# macOS tar would otherwise add a ._ file of extended attributes next to each file.
COPYFILE_DISABLE=1 tar -czf "$out/plsremind-monitoring.tar.gz" \
  monitoring/compose.yaml \
  monitoring/.env.example \
  monitoring/scrape.yml \
  monitoring/alerts.yml \
  monitoring/targets/discord.yml \
  monitoring/grafana/plsremind.json \
  monitoring/grafana/provisioning/dashboards/plsremind.yml \
  monitoring/grafana/provisioning/datasources/plsremind.yml
echo "$out/plsremind-monitoring.tar.gz"
