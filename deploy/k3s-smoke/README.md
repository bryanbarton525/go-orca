# Isolated k3s API smoke

Base source: `6ed3c11c7b40eae0a115846dd38722c16ca88ae5`. Build the reviewed
commit containing this directory and record its full SHA and image ID below.
This scenario submits one harmless workflow and verifies the **expected failed
terminal state**, including the exact missing-provider error. It exercises API
creation, scheduler execution, and persisted state without LLM credentials,
external model calls, or delivery actions. It does not prove successful model
execution, artifact generation, durability across pod replacement, or production readiness.

## Check access first

Use only the existing authorized sandbox context. Do not use production.
The proposed namespace is `orca-smoke-orc4`; the platform owner must confirm it
is dedicated to this test and provision it if absent. This overlay deliberately
creates no Namespace, RBAC, Service, Ingress, PVC, or shared resource.

```bash
kubectl version -o yaml
kubectl config current-context
kubectl get namespace orca-smoke-orc4
kubectl -n orca-smoke-orc4 get deployments,configmaps,pods
kubectl auth can-i --list --namespace orca-smoke-orc4
```

Stop on missing access, unexpected context, or a namespace/resource collision.
Request only namespace access: deployments/configmaps create, get, list, watch,
patch, update, delete; pods get, list, watch; pods/log get; pods/portforward create.
Ask the platform owner to confirm namespace ownership if namespace reads are
not permitted. No secrets or cluster-admin are needed. Record the server's k3s
version, kubectl version, container builder version, and Python version.

## Build and load

From a clean checkout of the reviewed PR commit:

```bash
COMMIT=$(git rev-parse HEAD)
IMAGE="go-orca-smoke:$COMMIT"
git status --short
docker version
python3 --version
docker build --label "org.opencontainers.image.revision=$COMMIT" -t "$IMAGE" .
docker image inspect "$IMAGE" --format '{{.Id}}'
docker save "$IMAGE" -o go-orca-smoke.tar
```

The existing Dockerfile needs CGO and downloads the Copilot CLI; this smoke does
not enable Copilot. Image build is not byte-reproducible because upstream base
tags are mutable, so retain the built image ID/digest as well as the source SHA.
Have the authorized platform owner import that exact tar into the sandbox k3s
node containerd image store (`k3s ctr images import go-orca-smoke.tar`) on every
eligible node, or provide a sandbox registry digest. Node image import requires
separate platform access; do not request cluster-admin for the API pod.

## Run

Use a disposable copy of the overlay so the source stays unchanged. `RUN_DIR`
should be a task-owned scratch directory (Paperclip: `$PAPERCLIP_RUN_SCRATCH_DIR`).
Set `COMMIT` to the full reviewed commit, `IMAGE` to the exact loaded tag or
registry digest, and verify both match the build record.

```bash
mkdir -p "$RUN_DIR/overlay"
cp deploy/k3s-smoke/*.yaml "$RUN_DIR/overlay/"
python3 - "$RUN_DIR/overlay/kustomization.yaml" "$IMAGE" <<'PY'
import json, pathlib, sys
p = pathlib.Path(sys.argv[1])
s = p.read_text().split('images:\n')[0]
s += 'patches:\n  - target:\n      kind: Deployment\n      name: go-orca-smoke\n'
s += '    patch: |-\n      - op: replace\n        path: /spec/template/spec/containers/0/image\n'
s += '        value: ' + json.dumps(sys.argv[2]) + '\n'
p.write_text(s)
PY
kubectl kustomize "$RUN_DIR/overlay" > "$RUN_DIR/rendered.yaml"
kubectl -n orca-smoke-orc4 apply --dry-run=server -f "$RUN_DIR/rendered.yaml"
kubectl -n orca-smoke-orc4 apply -f "$RUN_DIR/rendered.yaml"
kubectl -n orca-smoke-orc4 rollout status deployment/go-orca-smoke --timeout=120s
kubectl -n orca-smoke-orc4 get pods -l app=go-orca-smoke \
  -o jsonpath='{range .items[*]}{.metadata.name}{" "}{.status.containerStatuses[0].imageID}{"\n"}{end}'
kubectl -n orca-smoke-orc4 port-forward deployment/go-orca-smoke 18080:8080
```

Run the last command in a separate terminal; it binds localhost only. Then:

```bash
python3 hack/smoke_workflow.py --base-url http://127.0.0.1:18080 --timeout 60
kubectl -n orca-smoke-orc4 logs deployment/go-orca-smoke --tail=100
```

The exact prompt is `Return the text ORCA_SMOKE_OK. Do not use tools or access
external data.` The checker prints the created workflow ID and verifies a GET
of the same ID reaches `failed` with exactly `director phase: engine: no provider
resolved for persona "director"`. An unrelated failure, cancellation, unexpected
completion, HTTP error, or timeout is nonzero. Use this only with the supplied
provider-free configuration. The API has no authentication in this fixture;
keep it inside the dedicated sandbox namespace and use localhost port-forward.

## Cleanup and evidence

Stop port-forward with Ctrl-C. Save output before deleting only this run's
resources, using the same rendered file:

```bash
kubectl -n orca-smoke-orc4 delete -f "$RUN_DIR/rendered.yaml" --wait=true --timeout=120s
kubectl -n orca-smoke-orc4 get deployments,configmaps,pods
```

The SQLite database/artifacts use emptyDir and are removed with the pod. Do not
delete the namespace or unrelated resources. Let the platform owner remove the
imported image if required; remove the local image tar after evidence collection.
Record date, context, namespace ownership confirmation, source SHA, built image
ID and runtime imageID, tool/server versions, exact commands and exit codes,
workflow ID/status/error, logs, and cleanup result. If access is blocked, record
the exact missing tool/credential/permission and leave cluster result **not run**.

## Focused local checks

```bash
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s hack -p test_smoke_workflow.py -v
CGO_ENABLED=0 go test ./internal/workflow/engine -run '^TestSmokeMissingProviderPersistsTerminalState$' -count=1
CGO_ENABLED=0 go test ./internal/api/handlers -run 'TestCreateWorkflow|TestGetWorkflow' -count=1
kubectl kustomize deploy/k3s-smoke
```

These Go tests use mock stores. They do not substitute for running the real
SQLite-backed API or for server-side Kubernetes validation.
