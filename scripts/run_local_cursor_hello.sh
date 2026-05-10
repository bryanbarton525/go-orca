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
#   export GOORCA_PROVIDERS_CURSOR_STARTING_REF="main"   # branch/tag; omit to auto-detect from git
#
# If `.env` exists in the repo root, it is sourced automatically (use
# `KEY=value` or `export KEY=value`; both work with `set -a`).
#
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if [[ -f "$ROOT/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "$ROOT/.env"
  set +a
fi

if [[ -z "${GOORCA_PROVIDERS_CURSOR_API_KEY:-}" ]]; then
  echo "error: set GOORCA_PROVIDERS_CURSOR_API_KEY in the environment or in .env" >&2
  exit 1
fi
if [[ -z "${GOORCA_PROVIDERS_CURSOR_REPO_URL:-}" ]]; then
  if git -C "$ROOT" remote get-url origin &>/dev/null; then
    origin="$(git -C "$ROOT" remote get-url origin)"
    case "$origin" in
      https://github.com/*/*)
        export GOORCA_PROVIDERS_CURSOR_REPO_URL="${origin%.git}"
        echo "==> using GOORCA_PROVIDERS_CURSOR_REPO_URL from git origin: ${GOORCA_PROVIDERS_CURSOR_REPO_URL}"
        ;;
      git@github.com:*/*)
        r="${origin#git@github.com:}"
        r="${r%.git}"
        export GOORCA_PROVIDERS_CURSOR_REPO_URL="https://github.com/${r}"
        echo "==> using GOORCA_PROVIDERS_CURSOR_REPO_URL from git origin: ${GOORCA_PROVIDERS_CURSOR_REPO_URL}"
        ;;
    esac
  fi
fi
if [[ -z "${GOORCA_PROVIDERS_CURSOR_REPO_URL:-}" ]]; then
  echo "error: GOORCA_PROVIDERS_CURSOR_REPO_URL is required (GitHub repo URL for Cloud Agents)." >&2
  echo "      Add to .env, e.g.: GOORCA_PROVIDERS_CURSOR_REPO_URL=https://github.com/your-org/your-repo" >&2
  exit 1
fi

if [[ -z "${GOORCA_PROVIDERS_CURSOR_STARTING_REF:-}" ]]; then
  if sym="$(git -C "$ROOT" symbolic-ref -q --short refs/remotes/origin/HEAD 2>/dev/null)"; then
    export GOORCA_PROVIDERS_CURSOR_STARTING_REF="${sym#origin/}"
    echo "==> using GOORCA_PROVIDERS_CURSOR_STARTING_REF from origin/HEAD: ${GOORCA_PROVIDERS_CURSOR_STARTING_REF}"
  elif br="$(git -C "$ROOT" rev-parse --abbrev-ref HEAD 2>/dev/null)" && [[ "$br" != "HEAD" ]]; then
    export GOORCA_PROVIDERS_CURSOR_STARTING_REF="$br"
    echo "==> using GOORCA_PROVIDERS_CURSOR_STARTING_REF from current branch: ${GOORCA_PROVIDERS_CURSOR_STARTING_REF}"
  fi
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
      -H "X-Scope-ID: ${SCOPE_ID}" \
      | python3 -c 'import json,sys; print(json.dumps(json.load(sys.stdin), indent=2)[:12000])'
    if [[ "$ST" == "completed" ]]; then
      exit 0
    fi
    exit 1
  fi
  sleep 10
done

echo "error: timed out waiting for workflow" >&2
exit 1
