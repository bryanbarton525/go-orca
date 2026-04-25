# Workflow Analysis: Linear Sync (ede3d459-9529-49e7-8e1f-08703d8146d3)

**Date**: 2026-04-25  
**Request**: Create Go service to sync Linear.app issues and push to github.com/bryanbarton525/linear-sync  
**Status**: ❌ **FAILED** - Primary deliverable not achieved despite "completed" status

---

## Executive Summary

The workflow executed through all 8 personas (Director → PM → Matriarch → Architect → Implementer → QA → Refiner → Finalizer) and generated 13 code artifacts, but **failed to deliver the primary requirement**: creating a GitHub repository at `bryanbarton525/linear-sync`.

### Critical Findings

**🔴 Issue #1: Repository Never Created (CRITICAL)**
- User request explicitly stated: "pushing to github.com/bryanbarton525/linear-sync"
- Repository was never created on GitHub
- Generated code exists only in ephemeral workflow workspace
- Director persona failed to select `create-repo` finalizer action
- Workflow used `api-response` action instead, returning JSON

**🔴 Issue #2: All Validation Steps Failing (HIGH)**
- All 4 MCP toolchain validation steps failed: `go_mod_tidy`, `go_fmt`, `go_test`, `go_build`
- Error: "policy: path escapes workspace"
- Root cause: Engine passes absolute path `/var/lib/go-orca/workspaces/{id}`, MCP servers expect relative path `{id}`
- Cannot verify generated code compiles or passes tests

### Impact

- **User expectation**: Working GitHub repository with verified Go code
- **Actual delivery**: JSON response with unverified code trapped in workspace
- **Workflow appeared successful**: Status shows "completed" but primary deliverable missing
- **Code quality unknown**: Cannot confirm if code compiles or runs due to validation failure

---

## Workflow Execution Analysis

### ✅ What Worked Correctly

**Persona Execution Pattern**:
- Director correctly identified software mode
- Director correctly selected go-toolchain based on "Go" in request
- PM created appropriate requirements and constitution
- Matriarch provided pragmatic design defaults
- Architect created proper 5-task dependency graph
- Implementer completed all 5 tasks generating code artifacts
- QA detected blocking issues and triggered remediation (2 cycles)
- Refiner performed retrospective analysis
- Finalizer executed (wrong action but executed correctly)

**MCP Toolchain Architecture**:
- All 10 MCP servers healthy and connected
- 62 total tools registered in MCP registry
- go-toolchain selected and reachable (preflight passed)
- Workspace created at correct path
- MCP servers correctly mounting shared workspace PVC

**Code Generation**:
- 13 artifacts generated including:
  - `go.mod` with proper module declaration
  - `internal/model/issue.go` - Issue data model
  - `internal/client/client.go` - Linear API client
  - `cmd/linear-sync/main.go` - CLI entry point
  - `README.md` - Setup instructions
  - Multiple remediation versions from QA cycles

### ❌ What Failed

**Repository Creation**:
- Director parsed user request but did NOT extract GitHub repository intent
- `constitution.finalizer_action` remained `null` instead of being set to `"create-repo"`
- Finalizer defaulted to `api-response` action
- No git initialization, no commits, no GitHub API calls
- CreateRepoAction code exists at `internal/finalizer/actions/actions.go:747` but wasn't invoked
- GitHub token configured in environment (`GOORCA_GITHUB_TOKEN`) but unused

**Toolchain Validation**:
- All validation steps failed with policy error
- Engine bug at `internal/workflow/engine/engine.go:1439`
- Passes `ws.Execution.Workspace.Path` (absolute) instead of `ws.ID` (relative)
- MCP `policy.ResolveWorkspacePath()` rejects absolute paths as security feature
- Workflow completed anyway due to `enforce_validation_gate: false` configuration

**QA Remediation**:
- QA found 8 blocking issues (4 validation errors + 4 code issues)
- Remediation fixed code issues but couldn't fix infrastructure bug
- After 3 QA cycles, workflow proceeded to Finalizer without passing validation

---

## Artifacts Generated

| Kind | Name | Status |
|------|------|--------|
| config | go.mod | ✅ Generated |
| code | internal/model/issue.go | ✅ Generated |
| code | internal/client/client.go | ✅ Generated |
| code | cmd/linear-sync/main.go | ✅ Generated |
| markdown | README.md | ✅ Generated |
| code | initialize-module | ✅ Generated |
| code | client-fix | ✅ Generated (remediation) |
| code | linear-sync | ✅ Generated |
| code | linear-sync-source | ✅ Generated |

**Total**: 13 artifacts in ephemeral workspace  
**Repository Status**: ❌ Not created  
**Verification Status**: ❌ Cannot verify (validation failed)

---

## Root Cause Analysis

### Issue #1: Repository Creation Failure

**Expected Behavior**:
1. Director parses request and detects "pushing to github.com/bryanbarton525/linear-sync"
2. Director sets `constitution.finalizer_action: "create-repo"`
3. Director sets `constitution.finalizer_config: {name: "linear-sync", org: "bryanbarton525"}`
4. Finalizer invokes CreateRepoAction
5. GitHub API called to create repository
6. Artifacts committed to repository
7. User receives GitHub URL in response

**Actual Behavior**:
1. Director parses request
2. Director does NOT detect repository creation intent
3. `constitution.finalizer_action` remains `null`
4. Finalizer defaults to ApiResponseAction
5. Artifacts serialized to JSON
6. User receives JSON response
7. Repository never created

**Root Cause**: Director persona lacks pattern matching for repository creation keywords like:
- "pushing to github.com/[org]/[repo]"
- "create repo at github.com/[org]/[repo]"
- "initialize git repo and push to github.com/[org]/[repo]"

**Evidence**:
```json
{
  "request": "...Initialize git repo and prepare code for pushing to github.com/bryanbarton525/linear-sync...",
  "finalizer_action": null,  // ❌ Should be "create-repo"
  "mode": "software"
}
```

**Available Infrastructure**:
- ✅ CreateRepoAction implemented at `internal/finalizer/actions/actions.go:747-850`
- ✅ GitHub token configured via `GOORCA_GITHUB_TOKEN` env var
- ✅ Token loaded from `go-orca-api-secrets.copilot-github-token`
- ✅ GitHub API integration fully functional

### Issue #2: Validation Failure

**Expected Behavior**:
1. Engine prepares toolchain call arguments
2. Engine passes `workspace_path: <workflow-id>` (relative path)
3. MCP toolchain server receives relative path
4. MCP calls `policy.ResolveWorkspacePath(root="/var/lib/go-orca/workspaces", rel="<workflow-id>")`
5. Policy validates relative path and resolves to absolute path
6. Validation executes: `go mod tidy`, `go fmt`, `go test`, `go build`
7. Results returned to engine

**Actual Behavior**:
1. Engine prepares toolchain call arguments
2. Engine passes `workspace_path: "/var/lib/go-orca/workspaces/<workflow-id>"` (absolute path) ❌
3. MCP toolchain server receives absolute path
4. MCP calls `policy.ResolveWorkspacePath(root="/var/lib/go-orca/workspaces", rel="/var/lib/go-orca/workspaces/<workflow-id>")`
5. Policy detects absolute path and rejects: `"policy: path escapes workspace"`
6. Validation fails before executing any Go commands
7. Error returned to engine

**Root Cause**: Bug at `internal/workflow/engine/engine.go:1439`

```go
// CURRENT (BUG):
args["workspace_path"] = ws.Execution.Workspace.Path  // Absolute path

// SHOULD BE:
args["workspace_path"] = ws.ID  // Just workflow ID (relative path)
```

**Evidence from Logs**:
```
validation tidy_dependencies failed via go_mod_tidy: mcp: {"passed":false,"success":false,"error":"policy: path escapes workspace: \"/var/lib/go-orca/workspaces/ede3d459-9529-49e7-8e1f-08703d8146d3\""}
```

**Why Workflow Completed Anyway**:
- Configuration: `workflow.enforce_validation_gate: false` (line 76 in values.yaml)
- Designed for environments where validation isn't production-ready
- Allows workflows to complete even when validation fails
- After 3 QA cycles, workflow proceeded to Finalizer

---

## MCP Architecture Verification

### ✅ Confirmed Working

**All 10 MCP Servers Deployed**:
- ✅ mcp-workspace (10.43.94.166:3000)
- ✅ mcp-go-toolchain (10.43.77.7:3000)
- ✅ mcp-node-toolchain (10.43.45.155:3000)
- ✅ mcp-nextjs-toolchain (10.43.247.99:3000)
- ✅ mcp-python-toolchain (10.43.188.222:3000)
- ✅ mcp-rust-toolchain (10.43.121.208:3000)
- ✅ mcp-java-toolchain (10.43.13.85:3000)
- ✅ mcp-git (10.43.139.212:3000)
- ✅ mcp-filesystem (10.43.124.135:3000)
- ✅ mcp-container-build (10.43.96.10:3000)

**MCP Registry**: 62 total tools, 6 toolchains configured  
**Shared Workspace**: PVC mounted at `/var/lib/go-orca/workspaces` (ReadWriteMany, 10Gi Longhorn)  
**Service Discovery**: All services reachable via ClusterIP  
**Health Checks**: All passing

### Workflow Integration

**Toolchain Selection**: ✅ Correct
- Request mentioned "Go service"
- Director selected `go` toolchain
- Engine resolved to `mcp-go-toolchain` MCP server
- Preflight check passed

**Workspace Creation**: ✅ Correct
- Workspace path: `/var/lib/go-orca/workspaces/ede3d459-9529-49e7-8e1f-08703d8146d3`
- Branch: `workflow/ede3d459-9529-49e7-8e1f-08703d8146d3`
- Created by engine, accessible to all MCP servers

**Validation Invocation**: ✅ Attempted (Failed due to bug)
- Engine correctly identified validation profile: `default`
- Steps: `[tidy_dependencies, format_code, run_tests, run_build]`
- Mapped to tools: `[go_mod_tidy, go_fmt, go_test, go_build]`
- HTTP calls made to mcp-go-toolchain service
- Failed at policy validation before executing Go commands

---

## Recommendations

### Immediate Actions

**1. Fix Engine Validation Bug** (15 minutes)
```bash
cd /Users/bbarton/go/modules/go-orca
# Edit internal/workflow/engine/engine.go line 1439
# Change: args["workspace_path"] = ws.Execution.Workspace.Path
# To: args["workspace_path"] = ws.ID

# Rebuild and deploy
make docker-build-api
docker tag go-orca-api:latest ghcr.io/bryanbarton525/go-orca-api:fix-validation
docker push ghcr.io/bryanbarton525/go-orca-api:fix-validation

cd /Users/bbarton/repo/homelab/clusters/namespace-go-orca
# Edit values.yaml: api.image.tag: "fix-validation"
git add values.yaml && git commit -m "fix: update go-orca-api to fix-validation" && git push
# Wait for ArgoCD sync or kubectl rollout restart deployment/go-orca-api -n go-orca
```

**2. Re-run Workflow with Correct Finalizer** (5 minutes)
```bash
kubectl run curl-linear-fixed --image=curlimages/curl:latest --rm -it --restart=Never -n go-orca -- \
  curl -s -X POST http://go-orca-api:8080/api/v1/workflows \
  -H "Content-Type: application/json" \
  -d '{
    "request": "Create a working Go service called linear-sync that connects to Linear.app SDK and outputs all issues. Use github.com/linear-app/linear SDK. Create repository at github.com/bryanbarton525/linear-sync.",
    "mode": "software",
    "constitution_override": {
      "finalizer_action": "create-repo",
      "finalizer_config": {
        "name": "linear-sync",
        "org": "bryanbarton525",
        "description": "Go service to fetch and display Linear.app issues",
        "private": false
      }
    }
  }'
```

**3. Verify Repository Created** (2 minutes)
```bash
# Check GitHub
curl -s https://api.github.com/repos/bryanbarton525/linear-sync | jq '{name, html_url}'

# Clone and test
git clone https://github.com/bryanbarton525/linear-sync.git
cd linear-sync
go build ./cmd/linear-sync
```

### Short-term Improvements (< 1 week)

**1. Fix Director Persona** (`prompts/personas/director.md`)
Add pattern matching for repository creation:
- Detect keywords: "pushing to github.com/", "create repo at", "initialize git repo and push"
- Extract org/username and repo name from URLs
- Set `finalizer_action: "create-repo"` with correct config
- Add test case for this scenario

**2. Add Integration Tests**
- Test workflow with repository creation request
- Test toolchain validation end-to-end
- Test all finalizer actions (create-repo, api-response, github-pr, etc.)

**3. Enhanced Error Messages**
- Update `policy.ResolveWorkspacePath()` error to include hint about relative paths
- Add validation preflight check in engine before calling MCP

### Long-term Improvements (ongoing)

**1. Improve Director Intelligence**
- Better natural language parsing for repository creation intent
- Support more GitHub URL formats
- Detect intent for other finalizer actions (PRs, issues, etc.)

**2. Enable Validation Gate**
- Once validation bug fixed, set `workflow.enforce_validation_gate: true`
- Prevent unverified code from reaching Finalizer
- Add configuration for validation profiles (default, strict, none)

**3. Monitoring and Metrics**
- Track finalizer action selection accuracy
- Monitor validation success rates
- Alert on repeated QA failures

---

## Conclusion

The Linear sync workflow demonstrated that go-orca's **multi-persona architecture and MCP toolchain integration work correctly**, but revealed two critical bugs:

1. **Repository creation not triggered** - Director persona missing pattern matching
2. **Validation blocked by policy** - Engine passing wrong path format

Both issues have clear fixes documented in [REMEDIATION_PLAN.md](file:///Users/bbarton/go/modules/go-orca/REMEDIATION_PLAN.md).

**Next Steps**: Apply both fixes and re-run workflow to achieve the original goal of creating a verified Go service at github.com/bryanbarton525/linear-sync.

---

## Remediation Status: Addressed in Current Patch

### ✅ Issue #1: Repository Never Created

Addressed in `internal/workflow/engine/engine.go`:

- Engine now detects explicit GitHub repository URLs in software workflow requests, e.g. `github.com/bryanbarton525/linear-sync`.
- Repository creation/attachment now happens during workspace setup, immediately after Director and before PM/Architect/Implementer work proceeds.
- The engine no longer depends on the Director choosing `create-repo` as a finalizer action for initial repository materialization.
- Existing repositories are treated as attachable targets instead of hard-failing the workflow on GitHub `422 already exists` responses.

Important behavior change:

- Finalizer is no longer responsible for initial repo creation. This aligns with `plan.md`: the repo/workspace is the source of truth before implementation and remediation loops begin.

### ✅ Issue #2: MCP Toolchain Validation Failure

Addressed in `internal/workflow/engine/engine.go`:

- `toolchainArgs()` now sends `workspace_path: <workflow-id>` instead of the absolute workspace path.
- MCP servers can now resolve the path safely through `policy.ResolveWorkspacePath(root, rel)`.
- This fixes the `policy: path escapes workspace` validation failure.

### ✅ Per-Pass Checkpointing Gap

The analysis did not call this out as a separate root cause, but it explains why later workflow passes were not being stored in git:

- Helm values had `checkpoint_capability: ""` for every language toolchain.
- With that config, the engine skipped checkpointing even though checkpoint code existed.

Addressed in `/Users/bbarton/repo/homelab/clusters/namespace-go-orca/values.yaml`:

- Enabled `checkpoint_capability: git_push_checkpoint` for Go, Next.js, Node, Python, Rust, and Java toolchains.
- Enabled `push_checkpoints: true` so implementation and remediation passes are pushed to the configured GitHub repository branch.
- Added a dedicated `git` toolchain entry mapped to `mcp-git`.
- Enabled `workflow.enforce_validation_gate: true` so software workflows do not finalize after failed validation.

Addressed in `internal/workflow/engine/engine.go`:

- Checkpoint calls for `git_*` capabilities now fall back to the dedicated `git` toolchain when the selected language MCP server does not advertise git tools.

Addressed in `cmd/mcp-git/main.go`:

- `mcp-git` now checks out the workflow branch and configures `origin` from `repo_url` before committing.
- `mcp-git` now supports non-interactive GitHub HTTPS pushes using `GOORCA_GITHUB_TOKEN` via a temporary `GIT_ASKPASS` helper.

Addressed in `/Users/bbarton/repo/homelab/clusters/namespace-go-orca/templates/mcp-deployment.yaml`:

- The `mcp-git` deployment now receives `GOORCA_GITHUB_TOKEN` from the existing `go-orca-api-secrets` secret, allowing `git_push_checkpoint` to push without interactive credentials.

### Verification

Focused tests pass:

```bash
GOCACHE=/tmp/go-orca-gocache go test ./internal/workflow/engine ./cmd/mcp-git
```

### Remaining Deployment Step

These fixes require rebuilding and redeploying both:

- `go-orca-api`, for engine changes.
- `mcp-git`, for branch/origin checkpoint behavior.

Existing in-flight workflows will not automatically pick up these code changes; re-run the Linear sync workflow after deployment.
