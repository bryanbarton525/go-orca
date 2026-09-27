# Initial validation — 2026-09-27

Source base: `6ed3c11c7b40eae0a115846dd38722c16ca88ae5`.
Branch: `current/orc-4-k3s-smoke` (the PR head identifies the added smoke files).

## Local results

- Python 3.13.5: `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s hack -p test_smoke_workflow.py -v` — PASS, 5 tests.
- Downloaded Go 1.26.1 linux/amd64 into the run-owned scratch toolchain directory.
- `CGO_ENABLED=0 go test ./internal/api/handlers -run 'TestCreateWorkflow|TestGetWorkflow' -count=1` — PASS.
- `CGO_ENABLED=0 go test ./internal/workflow/engine -run '^TestSmokeMissingProviderPersistsTerminalState$' -count=1` — PASS. Confirms the persisted failed status and exact `director phase: engine: no provider resolved for persona "director"` message with an in-memory mock store.
- Downloaded kubectl v1.34.1 (Kustomize v5.7.1) into the run-owned scratch toolchain directory.
- `kubectl kustomize deploy/k3s-smoke` — PASS, ConfigMap reference rewritten and namespaced Deployment rendered. This is rendering only, not server validation.
- `git diff --check` — PASS.

The regression test initially failed because its prompt directory omitted
`personas`, then exposed the `director phase:` prefix. Both test expectations
were corrected; no product behavior was changed.

## Exact access blocker and limits

Initial `command -v kubectl`, `command -v k3s`, `command -v docker`,
`command -v go`, and `command -v gcc` found no binaries. A client-only kubectl
and Go toolchain were obtained for local checks. No GCC/container builder is
available for the CGO SQLite API build. `KUBECONFIG` is unset;
`$HOME/.kube/config` and `/etc/rancher/k3s/k3s.yaml` are absent.
`kubectl config current-context` reports `error: current-context is not set`;
`kubectl config get-contexts -o name` prints no contexts.
Paperclip granted-secret metadata is empty and the Kubernetes connection search
returns no configured service. No credentials were requested in logs or files.

Candidate namespace: `orca-smoke-orc4`, pending platform ownership/collision
check. Cluster/server/k3s version: unavailable. Container image/ID: **not built**.
Deployment/server dry run: **not run**. Live HTTP workflow ID/terminal result:
**not run**. The mock-store engine result is not a k3s or SQLite claim.

Exact planned prompt: `Return the text ORCA_SMOKE_OK. Do not use tools or access
external data.` See README for the complete scenario commands.

Cleanup: no cluster resources, namespace, images, or live API process were
created. The downloaded tools and rendered manifest are in Paperclip run-owned
scratch, removed by the runtime after the heartbeat. Generated Python bytecode
was removed from the working tree before commit.

Next action: Chief Architect arrange namespace-scoped sandbox execution and
image build/load through the platform owner, run the documented scenario, and
attach image/runtime IDs, server version, actual terminal result, and cleanup.
Chief Architect review is required before merge; no merge/release was performed.
