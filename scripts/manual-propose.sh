#!/usr/bin/env bash
set -euo pipefail
payload="${1:-manual proposal from script}"
target_url="${PQ_FABRIC_PROPOSE_URL:-http://127.0.0.1:8081/propose}"
body="$(python3 -c 'import json, sys; print(json.dumps({"payload": sys.argv[1]}))' "$payload")"
curl -sS -X POST "$target_url" \
  -H 'Content-Type: application/json' \
  -d "$body" | python3 -m json.tool
