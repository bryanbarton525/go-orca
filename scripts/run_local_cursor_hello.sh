#!/usr/bin/env bash
# Start go-orca-api with .cursor-orca-run/go-orca.yaml (SQLite) and submit a
# minimal software workflow using the Cursor provider.
#
# Required:
#   export GOORCA_PROVIDERS_CURSOR_API_KEY="..."   # Cursor Dashboard → Integrations
#   export GOORCA_PROVIDERS_CURSOR_REPO_URL="https://github.com/your-org/your-repo"
#
# Optional:
#   export GOORCA_PROVIDERS_CURSOR_DEFAULT_MODEL="composer-2"
#
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if [[ -z "${GOORCA_PROVIDERS_CURSOR_API_KEY:-}" ]]; then
  echo "error: set GOORCA_PROVIDERS_CURSOR_API_KEY" >&2
  exit 1
fi
if [[ -z "${GOORCA_PROVIDERS_CURSOR_REPO_URL:-}" ]]; then
  echo "error: set GOORCA_PROVIDERS_CURSOR_REPO_URL to a GitHub repo URL your key can use with Cloud Agents" >&2
  exit 1
fi

CFG="$ROOT/.cursor-orca-run/go-orca.yaml"
mkdir -p "$ROOT/.cursor-orca-run"

echo "==> building go-orca-api"
go build -o "$ROOT/.cursor-orca-run/go-orca-api" ./cmd/go-orca-api

PORT="${GOORCA_SERVER_PORT:-18080}"
export GOORCA_SERVER_PORT="$PORT"

echo "==> starting API on 127.0.0.1:${PORT} (config: ${CFG})"
"$ROOT/.cursor-orca-run/go-orca-api" -config "$CFG" &
PID=$!
trap 'kill $PID 2>/dev/null || true' EXIT

for i in $(seq 1 60); do
  if curl -sf "http://127.0.0.1:${PORT}/api/v1/healthz" >/dev/null; then
    break
  fi
  sleep 0.25
done

echo "==> resolving tenant + scope"
TENANT_JSON="$(curl -sf "http://127.0.0.1:${PORT}/api/v1/tenants")"
TENANT_ID="$(echo "$TENANT_JSON" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d["tenants"][0]["id"])')"
SCOPE_JSON="$(curl -sf "http://127.0.0.1:${PORT}/api/v1/tenants/${TENANT_ID}/scopes")"
SCOPE_ID="$(echo "$SCOPE_JSON" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d["scopes"][0]["id"])')"
echo "    tenant_id=$TENANT_ID scope_id=$SCOPE_ID"

echo "==> creating workflow (provider=cursor)"
MODEL="${GOORCA_PROVIDERS_CURSOR_DEFAULT_MODEL:-composer-2}"
WF_JSON="$(curl -sf -X POST "http://127.0.0.1:${PORT}/api/v1/workflows" \
  -H "Content-Type: application/json" \
  -H "X-Tenant-ID: ${TENANT_ID}" \
  -H "X-Scope-ID: ${SCOPE_ID}" \
  -d "$(python3 <<PY
import json
print(json.dumps({
  "title": "Hello world (Cursor)",
  "mode": "software",
  "provider": "cursor",
  "model": "${MODEL}",
  "request": (
    "Produce a minimal hello-world program as a single source file named hello.go "
    "in the repository root that prints exactly one line: Hello, World"
  ),
}))
PY
)")"

WF_ID="$(echo "$WF_JSON" | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')"
echo "    workflow_id=$WF_ID"
echo "    poll: curl -s -H X-Tenant-ID:$TENANT_ID -H X-Scope-ID:$SCOPE_ID http://127.0.0.1:${PORT}/api/v1/workflows/$WF_ID | jq .status,.error_message"

echo "==> polling status (up to ~20 minutes)..."
deadline=$((SECONDS + 1200))
while (( SECONDS < deadline )); do
  ST="$(curl -sf "http://127.0.0.1:${PORT}/api/v1/workflows/${WF_ID}" \
    -H "X-Tenant-ID: ${TENANT_ID}" \
    -H "X-Scope-ID: ${SCOPE_ID}" \
    | python3 -c 'import json,sys; w=json.load(sys.stdin); print(w.get("status",""))')"
  echo "    status=$ST"
  if [[ "$ST" == "completed" || "$ST" == "failed" || "$ST" == "cancelled" ]]; then
    curl -sf "http://127.0.0.1:${PORT}/api/v1/workflows/${WF_ID}" \
      -H "X-Tenant-ID: ${TENANT_ID}" \
      -H "X-Scope-ID: ${SCOPE_ID}" | python3 -m json.tool | head -n 80
    if [[ "$ST" == "completed" ]]; then
      exit 0
    fi
    exit 1
  fi
  sleep 10
done

echo "error: timed out waiting for workflow" >&2
exit 1
