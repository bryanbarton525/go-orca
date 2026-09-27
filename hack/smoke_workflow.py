#!/usr/bin/env python3
"""Credential-free API/scheduler/storage smoke; expects a specific terminal failure."""
import argparse
import json
import sys
import time
from urllib.error import URLError
from urllib.parse import quote
from urllib.request import Request, urlopen

PROMPT = "Return the text ORCA_SMOKE_OK. Do not use tools or access external data."
EXPECTED_ERROR = 'director phase: engine: no provider resolved for persona "director"'


def request(base, path, payload=None, timeout=10):
    data = None if payload is None else json.dumps(payload).encode()
    # The API middleware resolves the default tenant and global scope to their
    # persisted UUIDs. The slugs "default" and "global" are not database IDs.
    req = Request(base.rstrip("/") + path, data=data, headers={
        "Content-Type": "application/json",
    })
    with urlopen(req, timeout=timeout) as response:
        expected = 200 if payload is None else 201
        if response.status != expected:
            raise RuntimeError(f"expected HTTP {expected}, got {response.status}")
        return json.load(response)


def smoke(base, timeout=60, interval=0.5):
    if timeout <= 0 or interval <= 0:
        raise ValueError("timeout and interval must be positive")
    deadline = time.monotonic() + timeout

    def call(path, payload=None):
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise TimeoutError("workflow did not reach expected terminal state")
        return request(base, path, payload, timeout=min(10, remaining))

    call("/readyz")
    created = call("/api/v1/workflows", {"request": PROMPT})
    workflow_id = created.get("id")
    if not isinstance(workflow_id, str) or not workflow_id:
        raise RuntimeError("create response has no workflow id")
    print(json.dumps({"workflow_id": workflow_id, "prompt": PROMPT}), flush=True)
    while True:
        state = call("/api/v1/workflows/" + quote(workflow_id, safe=""))
        if state.get("id") != workflow_id:
            raise RuntimeError("poll returned a different workflow")
        status = state.get("status")
        if status == "failed":
            if EXPECTED_ERROR != state.get("error_message", ""):
                raise RuntimeError("workflow failed for an unexpected reason: " +
                                   str(state.get("error_message")))
            print(json.dumps({"workflow_id": workflow_id, "status": status,
                              "error_message": state["error_message"]}), flush=True)
            return state
        if status not in ("pending", "running"):
            raise RuntimeError(f"unexpected workflow status: {status!r}")
        time.sleep(min(interval, max(0, deadline - time.monotonic())))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", default="http://127.0.0.1:18080")
    parser.add_argument("--timeout", type=float, default=60)
    args = parser.parse_args()
    try:
        smoke(args.base_url, args.timeout)
    except (URLError, RuntimeError, ValueError, TimeoutError) as exc:
        print(f"smoke failed: {exc}", file=sys.stderr)
        sys.exit(1)
