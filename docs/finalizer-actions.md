# Finalizer Actions

The Finalizer persona is the last phase in every workflow. Its job is to select and execute a *delivery action* — packaging and shipping the workflow's artifacts to their destination.

## Action Registry

All delivery actions implement the `Action` interface (`internal/finalizer/actions`):

```go
type Action interface {
    Kind()        ActionKind
    Description() string
    Execute(ctx context.Context, in Input) (*Output, error)
}
```

A global registry (`actions.Global`) is pre-populated at process startup with all six built-in actions. Custom actions can be registered with `Global.Register(a)`.

### Input

```go
type Input struct {
    Workflow  *state.WorkflowState
    Artifacts []state.Artifact
    Config    json.RawMessage   // action-specific configuration
}
```

### Output

```go
type Output struct {
    Action   ActionKind
    Success  bool
    Links    []string            // URLs produced (e.g. PR URL, file URLs)
    Metadata map[string]string   // action-specific key/value pairs
    Message  string
    Error    string
}
```

---

## Built-in Actions

### github-pr

Opens a GitHub pull request containing all workflow artifacts as files.

**Flow:**
1. Resolve the SHA of the base branch using the GitHub Contents API
2. Create the head branch from that SHA (`POST /git/refs`)
3. Commit each artifact as a file via the Contents API (`PUT /contents/<path>`)
4. Open the PR (`POST /pulls`)

**Config fields:**

| Field | Required | Default | Description |
|---|---|---|---|
| `repo` | Yes | — | `"owner/repo"` |
| `head_branch` | Yes | — | New branch to create |
| `base_branch` | No | `"main"` | Branch to merge into |
| `title` | No | Workflow title | PR title |
| `body` | No | `""` | PR description |
| `path` | No | `""` | Directory prefix for all artifact files |
| `token` | No | `$GITHUB_TOKEN` | GitHub PAT; falls back to `GITHUB_TOKEN` env var |

**Output links:** `["https://github.com/owner/repo/pull/N"]`

**Output metadata keys:** `pr_url`, `head_branch`, `base_branch`

**Example config (JSON):**
```json
{
  "repo": "acme/backend",
  "head_branch": "feature/generated-api",
  "base_branch": "main",
  "title": "Generated REST API",
  "path": "generated",
  "token": "ghp_..."
}
```

---

### repo-commit-only

Commits artifacts directly to a branch without creating a pull request.

**Flow:**
1. For each artifact: `PUT /repos/:owner/:repo/contents/<path>`
2. Uses the GitHub Contents API; each file is committed individually

**Config fields:**

| Field | Required | Default | Description |
|---|---|---|---|
| `repo` | Yes | — | `"owner/repo"` |
| `branch` | No | `"main"` | Target branch |
| `path` | No | `""` | Directory prefix for artifact files |
| `token` | No | `$GITHUB_TOKEN` | GitHub PAT |

**Output links:** GitHub HTML URLs for each committed file

**Output metadata keys:** `repo`, `branch`, `artifact:<name>` (file path for each artifact)

---

### markdown-export

Renders all artifacts as a single markdown document in-memory.

**Format:**
```markdown
# <workflow title>

## Vision
<constitution.vision>

## Artifacts

### <artifact name>

<artifact content>
```

**Config fields:** None required.

**Output metadata key:** `content` — the full rendered markdown string

This action does not write to disk or call any external API. The rendered document is returned in `Output.Metadata["content"]` and can be retrieved from the `FinalizationResult`.

---

### artifact-bundle

Creates a manifest of all artifact names and kinds. Does not write files — full bundling requires storage integration.

**Config fields:** None required.

**Output metadata:** One entry per artifact: `"<artifact name>" → "<artifact kind>"`

**Output message:** `"Bundle manifest created (N artifacts)."`

---

### blog-draft

Extracts the first artifact with kind `blog_post` and returns it as a publication-ready draft.

**Config fields:** None required.

**Output metadata key:** `draft` — the blog post content

Returns `success: false` with an error if no `blog_post` artifact exists in the workflow.

---

### webhook-dispatch

POSTs workflow metadata and all artifacts as a JSON payload to a configured URL.

**Request body sent to webhook:**
```json
{
  "workflow_id": "uuid",
  "tenant_id": "uuid",
  "title": "workflow title",
  "status": "completed",
  "artifacts": [
    { "name": "main.go", "kind": "code", "content": "..." }
  ]
}
```

**Config fields:**

| Field | Required | Description |
|---|---|---|
| `url` | Yes | HTTP/HTTPS URL to POST to |

**Response handling:**
- HTTP `< 300` → success
- HTTP `>= 300` → failure with the status code and response body in `Output.Error`

**Output links:** `["<webhook URL>"]`

---

## Action Selection and Execution

### How an Action is Selected

The engine determines the delivery action in this order:

1. **Caller override** — if the `POST /workflows` request includes a `delivery.action` field, that value is used unconditionally and the Finalizer's own LLM-based selection is skipped.
2. **Finalizer LLM selection** — if no caller override is present, the Finalizer persona reads the workflow content and selects an action itself. The decision is stored in `FinalizationResult.Action`.

```json
POST /workflows
{
  "title": "Generate REST API",
  "description": "...",
  "delivery": {
    "action": "github-pr",
    "config": {
      "repo": "acme/backend",
      "head_branch": "feature/generated-api",
      "base_branch": "main"
    }
  }
}
```

The `delivery.config` object is passed as-is to `Action.Execute` as `Input.Config`. Only non-secret values should be placed here — tokens and passwords must come from environment variables and are **never stored in the database**.

### Action Execution

After the Finalizer persona completes its LLM phase, the engine calls `actions.Global.Execute(ctx, kind, input)`. The resolved action runs synchronously inside the finalizer phase.

**If the action fails, the workflow fails.** A non-nil error from `Execute`, or `Output.Success == false`, transitions the workflow to `failed` status with the action error in `Output.Error`. The workflow may be resumed via `POST /workflows/:id/resume` once the underlying cause is fixed (e.g. bad token, missing repo).

On success, `Output.Links` and `Output.Metadata` are merged into `WorkflowState.Finalization` and are available via `GET /workflows/:id` and the final `workflow.completed` SSE event.

### Influencing LLM Action Selection (no caller override)

When no `delivery.action` is provided you can still influence the Finalizer by wording your request:

```
"Build a Go REST API and open a GitHub PR to the acme/backend repo"
```

Or via a prompt overlay in your customization files:

```markdown
# delivery.prompt.md
Always use the markdown-export action for documentation workflows.
```

---

## Artifact Kinds

The action receives all artifacts produced during the workflow. Artifact kinds:

| Kind | Description |
|---|---|
| `code` | Source code files |
| `document` | General documents |
| `diagram` | Architecture or flow diagrams |
| `markdown` | Markdown content |
| `config` | Configuration files (YAML, TOML, JSON, etc.) |
| `report` | Analysis or research reports |
| `blog_post` | Publication-ready blog content |
| `bundle_ref` | Reference to an exported bundle |

Each artifact has `name`, `path` (on-disk path if persisted), and `content` (inline string).
