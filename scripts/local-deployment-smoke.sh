#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
  docker compose -f docker-compose.yml config >/dev/null
  echo "compose config: pass"
else
  echo "compose config: skipped, docker compose unavailable"
fi

go run ./cmd/demo >/tmp/pq-fabric-deploy-local-smoke-demo.log
echo "demo smoke: pass"

go run ./cmd/deployment-evidence >/tmp/pq-fabric-deployment-evidence.log
cat /tmp/pq-fabric-deployment-evidence.log

echo "local deployment smoke complete; no long-running services left active"
