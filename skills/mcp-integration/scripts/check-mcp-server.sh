#!/usr/bin/env bash
# check-mcp-server.sh — verify connectivity to a Streamable HTTP MCP server
# Usage: ./check-mcp-server.sh <endpoint>
# Example: ./check-mcp-server.sh http://localhost:3000/mcp
set -euo pipefail

ENDPOINT="${1:-}"
if [[ -z "$ENDPOINT" ]]; then
  echo "Usage: $0 <endpoint>" >&2
  exit 1
fi

# Send MCP initialize request and check for protocol version in response.
RESPONSE=$(curl --silent --fail --max-time 10 \
  -X POST "$ENDPOINT" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{
    "jsonrpc":"2.0",
    "id":1,
    "method":"initialize",
    "params":{
      "protocolVersion":"2025-03-26",
      "clientInfo":{"name":"check-script","version":"1.0"},
      "capabilities":{}
    }
  }' 2>&1)

if echo "$RESPONSE" | grep -q '"protocolVersion"'; then
  echo "✓ MCP server at $ENDPOINT is reachable and returned a valid initialize response."
  exit 0
else
  echo "✗ Unexpected response from $ENDPOINT:" >&2
  echo "$RESPONSE" >&2
  exit 1
fi
