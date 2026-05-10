#!/usr/bin/env bash
# Start go-orca-api with .cursor-orca-run/go-orca.yaml (SQLite) and submit a
# minimal software workflow using the Cursor provider.
#
# The workflow uses delivery action "create-repo" so the engine creates a
# fresh disposable GitHub repository before the Director runs; Cursor then
# targets that repo (not the go-orca checkout).
#
# Required:
#   GOORCA_PROVIDERS_CURSOR_API_KEY   — Cursor Dashboard → Integrations
#   GOORCA_GITHUB_TOKEN               — classic PAT with repo scope (create repository)
#
# Optional:
#   GOORCA_PROVIDERS_CURSOR_DEFAULT_MODEL
#   GOORCA_PROVIDERS_CURSOR_STARTING_REF   — default main (GitHub auto_init)
#   GOORCA_CURSOR_SMOKE_REPO_PREFIX        — default: orca-cursor-smoke
#   GOORCA_GITHUB_ORG                      — if set, repo is created under this org
#
# Advanced (skip repo creation; you supply an existing repo Cursor can access):
#   GOORCA_PROVIDERS_CURSOR_REPO_URL=https://github.com/org/existing-empty-repo
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

USE_EXISTING_REPO=0
if [[ -n "${GOORCA_PROVIDERS_CURSOR_REPO_URL:-}" ]]; then
  USE_EXISTING_REPO=1
fi

if [[ "$USE_EXISTING_REPO" -eq 0 ]]; then
  if [[ -z "${GOORCA_GITHUB_TOKEN:-}" ]]; then
    echo "error: GOORCA_GITHUB_TOKEN is required so go-orca can create a throwaway repo (delivery create-repo)." >&2
    echo "      Create a classic PAT with repo scope, or set GOORCA_PROVIDERS_CURSOR_REPO_URL to an existing empty repo you control." >&2
    exit 1
  fi
fi

PREFIX="${GOORCA_CURSOR_SMOKE_REPO_PREFIX:-orca-cursor-smoke}"
UNIQ="$(python3 -c 'import uuid; print(uuid.uuid4().hex[:10])')"
SMOKE_REPO_NAME="${PREFIX}-${UNIQ}"

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
export MODEL="${GOORCA_PROVIDERS_CURSOR_DEFAULT_MODEL:-composer-2}"
export ORG="${GOORCA_GITHUB_ORG:-}"
export SMOKE_REPO_NAME
if [[ "$USE_EXISTING_REPO" -eq 1 ]]; then
  echo "    using existing GOORCA_PROVIDERS_CURSOR_REPO_URL (no create-repo)"
  WF_PAYLOAD="$(python3 <<'PY'
import json, os
print(json.dumps({
  "title": "Hello world (Cursor)",
  "mode": "software",
  "provider": "cursor",
  "model": os.environ["MODEL"],
  "request": (
    "Produce a minimal hello-world program as a single source file named hello.go "
    "in the repository root that prints exactly one line: Hello, World"
  ),
}))
PY
)"
else
  echo "    disposable GitHub repo name: ${SMOKE_REPO_NAME}"
  WF_PAYLOAD="$(python3 <<'PY'
import json, os
org = (os.environ.get("ORG") or "").strip()
name = os.environ["SMOKE_REPO_NAME"]
cfg = {"name": name, "description": "Ephemeral go-orca Cursor smoke-test repo (safe to delete).", "private": False}
if org:
    cfg["org"] = org
print(json.dumps({
  "title": "Hello world (Cursor)",
  "mode": "software",
  "provider": "cursor",
  "model": os.environ["MODEL"],
  "request": (
    "Produce a minimal hello-world program as a single source file named hello.go "
    "in the repository root that prints exactly one line: Hello, World"
  ),
  "delivery": {"action": "create-repo", "config": cfg},
}))
PY
)"
fi

WF_JSON="$(curl -sf -X POST "http://127.0.0.1:${PORT}/api/v1/workflows" \
  -H "Content-Type: application/json" \
  -H "X-Tenant-ID: ${TENANT_ID}" \
  -H "X-Scope-ID: ${SCOPE_ID}" \
  -d "$WF_PAYLOAD")"

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
