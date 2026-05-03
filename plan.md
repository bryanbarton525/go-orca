# Repo-Backed Workflow And MCP Toolchain Plan

## Goal

Make software workflows return functional, validated code. The system should not report completion just because code artifacts were generated; it should materialize a real workspace/repository, run the appropriate build/test commands, checkpoint the result, and only advance when the code is demonstrably usable.

## Core Architecture

- `go-orca` remains the workflow orchestrator.
- Language/runtime/build tools live outside the `go-orca` container in MCP toolchain servers.
- The repo/workspace becomes the source of truth for software deliverables.
- Artifacts remain useful as metadata, summaries, reports, or non-software deliverables, but generated code should be written to the workspace.
- Each implementation/remediation loop validates the workspace through MCP toolchain capabilities.
- Each implementation/remediation loop can checkpoint the repository so future iterations can source the codebase directly instead of carrying large artifact stacks.

## Container And Deployment Model

The `go-orca` container should not need every compiler and package manager installed. It should not become a universal build image containing Go, Node, Python, Rust, Java, Docker, and every possible project tool.

All MCP servers in this plan are **custom, first-party MCP implementations shipped as part of the go-orca product**. They are not third-party servers. Each MCP server lives in this repository, is built from this repository, has its own Dockerfile, has its own published image, and is deployable as part of the go-orca Helm chart. This guarantees consistent capability schemas, consistent policy gating, and a single deploy story for the product.

Deployment includes the following first-party services (one binary, one image, one Dockerfile per service):

- `go-orca-api` — orchestrator, already exists
- `mcp-go-toolchain`
- `mcp-node-toolchain`
- `mcp-python-toolchain`
- `mcp-rust-toolchain`
- `mcp-java-toolchain`
- `mcp-git`
- `mcp-filesystem`
- `mcp-workspace` (governed checkout/materialize/lifecycle)
- optional `mcp-container-build`

Each MCP toolchain server owns its runtime-specific dependencies, compilers, package managers, linters, formatters, and test runners. Cross-cutting MCP servers (`mcp-git`, `mcp-filesystem`, `mcp-workspace`) own no language toolchain and stay slim.

### Repository Layout

Each MCP server is a sibling under a single root, sharing a common framework module:

```
cmd/
  go-orca-api/                  # existing
  mcp-go-toolchain/             # new — main package per server
  mcp-node-toolchain/
  mcp-python-toolchain/
  mcp-rust-toolchain/
  mcp-java-toolchain/
  mcp-git/
  mcp-filesystem/
  mcp-workspace/
internal/
  mcp/
    server/                     # shared MCP server framework (transport, schema, policy, audit)
    capabilities/               # shared capability contracts and result types
    policy/                     # allowlists, timeouts, output caps, env filter
deploy/
  docker/
    mcp-go-toolchain.Dockerfile
    mcp-node-toolchain.Dockerfile
    mcp-python-toolchain.Dockerfile
    mcp-rust-toolchain.Dockerfile
    mcp-java-toolchain.Dockerfile
    mcp-git.Dockerfile
    mcp-filesystem.Dockerfile
    mcp-workspace.Dockerfile
```

A single CI workflow builds and pushes one image per MCP server (`ghcr.io/bryanbarton525/mcp-<name>:<tag>`). Tags are pinned to the same release version as `go-orca-api` so the registry, capability contracts, and server binaries always ship together.

## Workspace Strategy

Initial implementation can use a shared workspace volume:

- `go-orca` creates or records workspace metadata.
- MCP servers mount the same workspace root.
- Capability calls receive `workspace_path`, `workflow_id`, `phase`, `toolchain_id`, and related metadata.

Longer-term preferred architecture:

- A workspace-owned MCP service manages checkout/materialization, file operations, git operations, and workspace lifecycle.
- `go-orca` only stores workspace identity, repo URL, branch, latest commit SHA, validation results, and checkpoint metadata.

## MCP Registry Plan

Add a first-class **MCP registry** as a runtime component inside `go-orca-api`. The registry is the only path through which the workflow engine and toolchains reach MCP servers — there is no direct dial from a persona, capability, or toolchain entry to an HTTP endpoint. Toolchains resolve `mcp_server: <name>` against the registry; the registry returns a connected, health-checked client.

The registry must cover two layers, but exposes one resolution API:

- **MCP server registry** — what servers exist, their stable name, transport, endpoint, auth, the tool names they expose, capability schema version, health state, and last-seen timestamp.
- **Toolchain registry** — which MCP server supports which language/stack and which governed capabilities form validation/checkpoint profiles. A toolchain is a *binding* of capability names to `(mcp_server, tool_name)` pairs.

### Registry Responsibilities

- Load MCP server entries from config at startup (from `tools.mcp` in `go-orca.yaml`).
- Establish and maintain one client per server (lazy connect, reconnect with backoff).
- Probe each server's `tools/list` on connect and cache the schema; warn loudly if a tool referenced by a toolchain `capability_tools` mapping is not advertised by the server.
- Expose `Resolve(toolchainID, capabilityName) → (client, toolName, schema)` for the engine.
- Expose health/status to the API (`GET /api/v1/mcp/registry`) and surface it in the UI for operators.
- Emit registry events into the workflow journal: `mcp_server_connected`, `mcp_server_unreachable`, `mcp_capability_missing`, `mcp_tool_invoked`, `mcp_tool_failed`.
- Refuse to start workflows that depend on a toolchain whose required MCP server is unreachable, unless the toolchain is marked optional.

### Toolchain → Registry → MCP Call Flow

```
engine phase (validation/checkpoint)
   → toolchain.capability("run_tests")
   → registry.Resolve("go", "run_tests")
   → mcp client for "go-toolchain" calls tool "go_test"
   → structured CapabilityResult flows back into the workflow journal
```

No persona prompt, no executor, no engine code path may invoke an MCP tool except via `registry.Resolve`. This keeps capability semantics, audit, and policy enforcement in one place.

Example target configuration. Every entry under `tools.mcp` is a first-party go-orca MCP server. `image` is informational — it documents which container image backs the endpoint so operators can correlate registry entries with deployed pods.

```yaml
tools:
  mcp:
    - name: go-toolchain
      endpoint: "http://mcp-go-toolchain:3000/mcp"
      transport: streamable
      image: ghcr.io/bryanbarton525/mcp-go-toolchain
      health_path: "/healthz"
      required: true

    - name: node-toolchain
      endpoint: "http://mcp-node-toolchain:3000/mcp"
      transport: streamable
      image: ghcr.io/bryanbarton525/mcp-node-toolchain
      health_path: "/healthz"
      required: false

    - name: git
      endpoint: "http://mcp-git:3000/mcp"
      transport: streamable
      image: ghcr.io/bryanbarton525/mcp-git
      health_path: "/healthz"
      required: true

    - name: workspace
      endpoint: "http://mcp-workspace:3000/mcp"
      transport: streamable
      image: ghcr.io/bryanbarton525/mcp-workspace
      health_path: "/healthz"
      required: true

  toolchains:
    - id: go
      languages: ["go", "golang"]
      mcp_server: go-toolchain
      capabilities:
        - init_project
        - tidy_dependencies
        - format_code
        - run_tests
        - run_build
        - run_lint
        - git_status
        - git_checkpoint
        - git_push_checkpoint
      capability_tools:
        init_project: go_mod_init
        tidy_dependencies: go_mod_tidy
        format_code: go_fmt
        run_tests: go_test
        run_build: go_build
        run_lint: go_vet
        git_status: git_status
        git_checkpoint: git_checkpoint
        git_push_checkpoint: git_push_checkpoint
      validation_profiles:
        default:
          - tidy_dependencies
          - format_code
          - run_tests
          - run_build
        strict:
          - tidy_dependencies
          - format_code
          - run_tests
          - run_lint
          - run_build
      checkpoint_capability: git_checkpoint
      push_checkpoints: false

    - id: node
      languages: ["javascript", "typescript", "node"]
      mcp_server: node-toolchain
      capabilities:
        - install_dependencies
        - format_code
        - run_tests
        - run_build
        - run_lint
        - git_checkpoint
      validation_profiles:
        default:
          - install_dependencies
          - run_tests
          - run_build
        strict:
          - install_dependencies
          - format_code
          - run_lint
          - run_tests
          - run_build
```

## MCP Server Packaging

Each first-party MCP server in `cmd/mcp-*` is a standalone Go binary that:

- Imports `internal/mcp/server` for transport (HTTP streamable by default), tool dispatch, schema publication, structured logging, audit, and `/healthz`.
- Imports `internal/mcp/capabilities` for the `CapabilityResult` and `CheckpointResult` types so every server returns the same shape.
- Imports `internal/mcp/policy` for the command-allowlist, timeout, output cap, env filter, and audit enforcement described in *Command Execution Policy*.
- Reads its own config from env vars (`MCP_LISTEN`, `MCP_WORKSPACE_ROOT`, `MCP_AUDIT_LOG`, plus server-specific vars like `MCP_GO_TEST_TIMEOUT`).
- Mounts the same workspace volume as `go-orca-api` at `MCP_WORKSPACE_ROOT` and constrains all filesystem operations to that root.
- Exposes only the tools listed in its server-specific capability map; no generic `run_command` is exposed unless the server explicitly opts in and the tool is itself policy-gated.

### Per-Server Dockerfiles

Each MCP server has a dedicated Dockerfile under `deploy/docker/mcp-<name>.Dockerfile`. Each Dockerfile:

- Builds the matching binary from `cmd/mcp-<name>` in a pinned Go builder stage.
- Installs *only* the runtime tools that server's capabilities need:
  - `mcp-go-toolchain` → `go`, `gofmt`, `go vet`, `git` (for checkpoints if the server bundles them; otherwise omit).
  - `mcp-node-toolchain` → `node`, `npm`/`pnpm`, `eslint`/`prettier` runtime deps via project lockfile.
  - `mcp-python-toolchain` → `python`, `pip`/`uv`, `pytest`, `ruff`/`black`.
  - `mcp-rust-toolchain` → `rustc`, `cargo`, `rustfmt`, `clippy`.
  - `mcp-java-toolchain` → JDK, `maven`/`gradle`.
  - `mcp-git` → `git` only.
  - `mcp-filesystem` → no extra runtime tools.
  - `mcp-workspace` → `git` and a small set of file utilities.
- Runs as a non-root user, reads workspace from a mount path, exposes a single port (default 3000), and defines a `HEALTHCHECK` against `/healthz`.
- Produces an image tagged `ghcr.io/bryanbarton525/mcp-<name>:<version>`. The version matches the `go-orca-api` image tag for that release.

The existing top-level [Dockerfile](Dockerfile) continues to build only `go-orca-api`; it does not change shape.

### CI / Build

`.github/workflows/build.yaml` adds a matrix step: one entry per MCP server, each builds and pushes its Dockerfile when the corresponding `cmd/mcp-<name>/**` or `deploy/docker/mcp-<name>.Dockerfile` path changes (or when the API image is published, to keep tags aligned).

## Helm Chart Plan

The Helm chart at [clusters/namespace-go-orca](../../repo/homelab/clusters/namespace-go-orca/) adds first-class deployment support for each first-party MCP server. Operators enable any MCP server with a single boolean and the registry picks it up automatically.

### values.yaml shape

Use a map of named MCP entries under `mcp:`, each independently togglable. A list is also acceptable for ordering, but the map form makes per-server overrides ergonomic and avoids list-merge pitfalls in downstream values files.

```yaml
mcp:
  goToolchain:
    enabled: false
    image:
      repository: ghcr.io/bryanbarton525/mcp-go-toolchain
      tag: ""           # defaults to .Chart.AppVersion when empty
      pullPolicy: IfNotPresent
    service:
      port: 3000
    resources:
      requests: { cpu: 100m, memory: 256Mi }
      limits:   { cpu: 1000m, memory: 1Gi }
    env: {}
    workspaceMount: "/var/lib/go-orca/workspaces"

  nodeToolchain:
    enabled: false
    image:
      repository: ghcr.io/bryanbarton525/mcp-node-toolchain
      tag: ""
    service: { port: 3000 }
    resources:
      requests: { cpu: 100m, memory: 256Mi }
      limits:   { cpu: 2000m, memory: 2Gi }

  pythonToolchain: { enabled: false, image: { repository: ghcr.io/bryanbarton525/mcp-python-toolchain } }
  rustToolchain:   { enabled: false, image: { repository: ghcr.io/bryanbarton525/mcp-rust-toolchain } }
  javaToolchain:   { enabled: false, image: { repository: ghcr.io/bryanbarton525/mcp-java-toolchain } }
  git:             { enabled: false, image: { repository: ghcr.io/bryanbarton525/mcp-git } }
  filesystem:      { enabled: false, image: { repository: ghcr.io/bryanbarton525/mcp-filesystem } }
  workspace:       { enabled: false, image: { repository: ghcr.io/bryanbarton525/mcp-workspace } }
  containerBuild:  { enabled: false, image: { repository: ghcr.io/bryanbarton525/mcp-container-build } }
```

### Templates

Add one shared template that renders a Deployment + Service per enabled MCP server, driven by a list iteration in `_helpers.tpl`. Suggested files:

- `templates/mcp-deployment.yaml` — `range` over the `mcp` map, gated on `.enabled`. Each pod mounts the existing `go-orca-workspace` PVC at `workspaceMount` (read-write). Pod name and service name follow `mcp-<name>` to match `tools.mcp[].endpoint` defaults.
- `templates/mcp-service.yaml` — companion `ClusterIP` service per enabled MCP, exposing `service.port` as `3000`.
- `templates/mcp-registry-configmap.yaml` *(optional convenience)* — auto-generates the `tools.mcp` block consumed by `go-orca-api` from the same `mcp.*.enabled` flags so operators don't have to keep two lists in sync. The API's `tools.mcp` list in [api-configmap.yaml](../../repo/homelab/clusters/namespace-go-orca/templates/api-configmap.yaml) is populated from this generated block.
- `_helpers.tpl` — adds `go-orca.mcp.enabledList` returning the set of enabled MCP server keys, used by both the deployment loop and the registry configmap.

The existing [workspace-pvc.yaml](../../repo/homelab/clusters/namespace-go-orca/templates/workspace-pvc.yaml) is reused. If multi-pod read-write is required (PVC `ReadWriteOnce` on `longhorn` will not allow this across nodes), the chart documents that the workspace must use `ReadWriteMany` (e.g., NFS/Longhorn-RWX) when more than one MCP server runs alongside `go-orca-api`. Until then, MCP pods schedule with affinity for the same node as `go-orca-api`.

### Wiring `go-orca-api` to the Registry

`api-configmap.yaml` sources its `tools.mcp` block either from operator-supplied `api.config.tools.mcp` or from the auto-generated `mcp-registry-configmap.yaml`. Toolchains under `api.config.tools.toolchains` reference MCP servers by `name`, matching what the deployment templates render. Sample toolchain blocks (Go, Node, Python, Rust, Java) are shipped commented-out in `values.yaml` so an operator who flips `mcp.goToolchain.enabled: true` has a one-line uncomment to wire a working `go` toolchain.

## Capability Contract

Prefer governed, semantic capabilities over raw unrestricted shell execution.

Recommended standard capabilities:

- `init_project`
- `install_dependencies`
- `tidy_dependencies`
- `format_code`
- `run_tests`
- `run_build`
- `run_lint`
- `typecheck`
- `security_scan`
- `git_status`
- `git_checkpoint`
- `git_push_checkpoint`

Each capability should return a structured result:

```json
{
  "passed": true,
  "success": true,
  "stdout": "...",
  "stderr": "...",
  "output": "short summary",
  "error": "",
  "metadata": {
    "command": "go test ./...",
    "duration_ms": 1234
  }
}
```

Checkpoint capabilities should return:

```json
{
  "commit_sha": "abc123",
  "branch": "workflow/<id>",
  "message": "checkpoint after implementation",
  "pushed": false
}
```

## Command Execution Policy

Raw command execution should not be the default interface.

If an MCP server exposes `run_command`, it must be policy-gated:

- workspace-contained working directory
- allowlisted commands only
- no destructive commands by default
- timeout per command
- max output size
- audit trail for every invocation
- environment filtering
- no secret persistence
- explicit approval for high-risk operations

For example, a Go toolchain MCP can internally allow:

- `go mod init`
- `go mod tidy`
- `gofmt`
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./...`

But the workflow should call `run_tests` or `tidy_dependencies`, not ask the model to invoke arbitrary shell commands.

## Workflow Phases

Target phase order:

1. Director classifies request, selects required personas, identifies delivery/repo intent, and allows engine toolchain selection.
2. Engine creates or attaches a workspace/repo before implementation.
3. Project Manager creates constitution and requirements.
4. Engineer Proxy captures pragmatic engineering defaults and flags product-sensitive questions.
5. Architect designs the solution and creates concrete file-oriented tasks.
6. PM reviews Architect plan for completeness.
7. PM and Architect loop until the plan is complete.
8. Implementer writes actual files into the workspace.
9. Engine invokes toolchain validation profile through MCP.
10. Engine creates a checkpoint commit if files changed and checkpointing is configured.
11. QA reviews the workspace state, latest validation output, requirements, design, and implementation summary.
12. If QA finds blockers, route to PM triage first.
13. PM classifies blockers as requirement gap, design gap, implementation defect, or environment/validation issue.
14. Architect creates targeted remediation tasks from the PM brief.
15. Implementer fixes the workspace.
16. Engine validates and checkpoints again.
17. QA repeats until pass or retry limit.
18. Finalizer reports/publishes the final repo/workspace state and validation/checkpoint metadata.

## Repository Creation Responsibility

- Director decides repo strategy and delivery intent.
- Engine or a governed workspace/repo MCP creates or attaches the repository early.
- Finalizer must not create the initial repository.
- Finalizer may open a PR, publish links, summarize final state, or dispatch final metadata.

## Context Reduction Strategy

To keep model context low:

- Store code in the repo/workspace, not in large handoff artifacts.
- Pass concise summaries, validation results, file path lists, and commit SHAs.
- After every implementation/remediation phase, checkpoint the code.
- Subsequent iterations source the workspace/repo state rather than receiving all previous code artifacts inline.

Push behavior should be configurable:

- default: local checkpoint only
- optional: push after each phase when remote persistence is configured
- final: push/open PR/publish according to finalizer delivery action

## QA Policy

For software workflows, QA should treat validation as primary evidence.

QA should block when:

- required validation failed
- tests fail
- build fails
- formatting/dependency checks fail when required by profile
- code was only generated as artifacts and not written to the workspace
- files contain human instructions such as "split this later"
- generated Go files contain multiple packages in one file
- test files and implementation files are improperly mixed
- package/module structure is inconsistent

QA should not pass solely by visual inspection when validation failed.

## Engineer Proxy Persona

The Engineer Proxy persona mimics pragmatic engineering judgment.

It should:

- prefer minimal correct implementations
- prefer idiomatic language conventions
- avoid speculative abstractions
- prefer standard library and existing dependencies
- require real validation
- flag product-sensitive decisions for escalation
- never invent product requirements

It should not replace the real user for ambiguous product decisions.

## Implemented Foundation

The current implementation includes a foundation for this plan:

- `tools.toolchains` config exists.
- Workflow execution state records workspace, selected toolchain, validation runs, and checkpoints.
- Engine creates workspace metadata after Director when a software/mixed/ops workflow has configured toolchains.
- Engine invokes configured validation capabilities after implementation and remediation phases.
- Engine invokes configured checkpoint capability after implementation and remediation phases.
- Validation and checkpoint events exist.
- QA remediation now routes through PM triage before Architect remediation.
- `engineer_proxy` persona and prompt exist and are registered before Architect.
- Persona prompts were updated for repo-backed workspace behavior.
- `docs/tools.md` documents the current toolchain config shape.
- Engine tests cover validation/checkpoint metadata.

## Not Yet Implemented

### MCP Registry (in `go-orca-api`)
- `internal/mcp/registry` package: server catalog, lazy clients, health probes, `Resolve(toolchainID, capability)` API.
- Registry events in the workflow journal (`mcp_server_connected`, `mcp_server_unreachable`, `mcp_capability_missing`, `mcp_tool_invoked`, `mcp_tool_failed`).
- `GET /api/v1/mcp/registry` endpoint and a UI panel showing per-server health, advertised tools, and toolchain bindings.
- Engine refusal to start a workflow whose required toolchain has an unreachable required MCP server.
- Wire the engine's existing validation/checkpoint paths to call `registry.Resolve` instead of any direct dispatch.

### First-Party MCP Servers (custom, in-tree)
- `cmd/mcp-go-toolchain` — `init_project`, `tidy_dependencies`, `format_code`, `run_tests`, `run_build`, `run_lint`.
- `cmd/mcp-node-toolchain` — `install_dependencies`, `format_code`, `run_tests`, `run_build`, `run_lint`, `typecheck`.
- `cmd/mcp-python-toolchain` — `install_dependencies`, `format_code`, `run_tests`, `run_lint`, `typecheck`.
- `cmd/mcp-rust-toolchain` — `tidy_dependencies`, `format_code`, `run_tests`, `run_build`, `run_lint`.
- `cmd/mcp-java-toolchain` — `install_dependencies`, `run_tests`, `run_build`.
- `cmd/mcp-git` — `git_status`, `git_checkpoint`, `git_push_checkpoint`.
- `cmd/mcp-filesystem` — workspace-scoped read/write/list/stat.
- `cmd/mcp-workspace` — checkout, materialize, lifecycle (replaces engine-managed workspace metadata long-term).
- Optional `cmd/mcp-container-build`.
- Shared `internal/mcp/server`, `internal/mcp/capabilities`, `internal/mcp/policy` packages.

### Packaging
- Per-server Dockerfiles under `deploy/docker/mcp-<name>.Dockerfile`.
- CI matrix in `.github/workflows/build.yaml` to build and publish each MCP image to GHCR with tags aligned to the `go-orca-api` release.

### Helm Chart (in `homelab/clusters/namespace-go-orca`)
- `mcp.<name>.enabled` toggles in `values.yaml` for every first-party MCP server.
- `templates/mcp-deployment.yaml` and `templates/mcp-service.yaml` rendering one workload per enabled MCP, mounting the existing `go-orca-workspace` PVC.
- `templates/mcp-registry-configmap.yaml` (optional) auto-generating the `tools.mcp` block from the same enable flags.
- Sample commented toolchain definitions in `values.yaml` for Go, Node, Python, Rust, Java.
- Documentation note about RWX storage when multiple MCP pods need concurrent workspace access.

### Workflow / Quality Gates
- Full workspace/repo creation through `mcp-workspace` instead of engine-managed workspace metadata.
- PM/Architect pre-implementation plan-review loop.
- Hard finalization gate that prevents software workflows from completing when required validation fails.
- Rich language-specific validation profiles for Node, Python, Rust, Java.
- Remote checkpoint push policy and PR flow integration with phase checkpoints.

## Verification

- Passed: `go test ./cmd/... ./internal/... ./middleware/... ./docs`
- Full `go test ./...` is blocked by pre-existing generated Go files under `artifacts/attachments/...` that do not compile.
