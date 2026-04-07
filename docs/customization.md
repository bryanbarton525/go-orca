# Customization

go-orca's customization system lets you inject skills, agent personas, and prompt overlays into every workflow run. Customizations are loaded from one or more sources, filtered by scope, and snapshotted once at workflow start so live source changes do not affect running workflows.

## File Types

go-orca recognises three kinds of customization files:

| File Pattern | Kind | Purpose |
|---|---|---|
| `SKILL.md` | `skill` | Describe capabilities or domain knowledge available to all personas |
| `*.agent.md` | `agent` | Override or augment the system prompt for a persona role |
| `*.prompt.md` | `prompt` | Inject additional context or instructions into prompts |

File matching is case-insensitive. Any file not matching these patterns is silently ignored.

### Name Derivation

The name of a customization item is derived from the filename:

| Filename | Kind | Derived Name |
|---|---|---|
| `SKILL.md` | skill | `skill` |
| `senior-dev.agent.md` | agent | `senior-dev` |
| `go-style.prompt.md` | prompt | `go-style` |

---

## Source Types

Each source has a `type` field:

| Type | Description |
|---|---|
| `filesystem` | Scans a local directory tree recursively |
| `repo` | Scans a checked-out repository (same scan as `filesystem`; semantic distinction only) |
| `git-mirror` | Mirror of a remote git repository cloned locally; scanned the same way |
| `builtin` | In-process items registered at startup via `Registry.RegisterBuiltin` |

For `filesystem`, `repo`, and `git-mirror`, go-orca walks the `root` directory recursively. Missing roots are silently skipped.

---

## Source Configuration

Add sources under `customizations.sources` in your config file:

```yaml
customizations:
  sources:
    - name: "global-skills"
      type: "filesystem"
      root: "./customizations/global"
      precedence: 10
      enabled_types:
        - "skill"
        - "prompt"
      scope_slug: "global"

    - name: "team-agents"
      type: "repo"
      root: "./customizations/team-engineering"
      precedence: 5
      enabled_types:
        - "agent"
      scope_slug: "engineering"
      refresh_seconds: 60
```

### Source Fields

| Field | Required | Description |
|---|---|---|
| `name` | Yes | Display name used in logs and API responses |
| `type` | Yes | `filesystem` \| `repo` \| `git-mirror` \| `builtin` |
| `root` | Yes (non-builtin) | Path to scan. Relative paths are resolved from the working directory |
| `precedence` | No | Integer priority. **Lower value = higher priority.** Default: `0` |
| `enabled_types` | No | Which kinds to load from this source: `skill`, `agent`, `prompt`. Empty = all three |
| `scope_slug` | No | Restrict this source to a specific scope slug. Empty = available to all scopes |
| `refresh_seconds` | No | Informational rescan interval. The snapshot is always taken fresh at workflow start |

---

## Precedence and Deduplication

When multiple sources provide an item with the same `(Kind, Name)` combination, the item with the **lowest `Precedence` number** (highest priority) wins. Others are discarded.

Resolution order (highest to lowest priority by convention):

```
workflow/repo  (precedence 0–9)
  → team       (precedence 10–19)
    → org      (precedence 20–29)
      → global (precedence 30–39)
        → builtin (precedence 40+)
```

You define the actual numbers — use this convention to keep it predictable.

---

## Scope Filtering

At workflow start, `Registry.Snapshot(scopeSlug)` is called with the workflow's scope ID slug. A source is included in the snapshot if:

- The source has no `scope_slug` (applies to all scopes), **or**
- The source's `scope_slug` matches the workflow's scope slug.

This lets you maintain separate customization trees for different teams or environments.

---

## How Context is Injected

The snapshot produces three strings that are injected into every `HandoffPacket`:

| Packet Field | Snapshot Method | Content |
|---|---|---|
| `SkillsContext` | `Snapshot.SkillsContext()` | All skill items concatenated with `## <name>\n<content>` headers |
| `CustomAgentMD` | `Snapshot.AgentsContext()` | The **highest-precedence** agent item (only one) |
| `PromptsContext` | `Snapshot.PromptsContext()` | All prompt items concatenated |

Each persona receives these strings and uses them to augment its LLM prompts.

---

## Example Directory Layout

```
./customizations/
├── global/
│   ├── SKILL.md               # global skill context
│   └── safety.prompt.md       # appended to all prompts
├── team-engineering/
│   ├── senior-dev.agent.md    # overrides agent persona for this team
│   └── go-style.prompt.md     # Go coding standards
└── team-content/
    ├── SKILL.md               # content team's skill context
    └── brand-voice.prompt.md  # brand writing guidelines
```

Corresponding config:

```yaml
customizations:
  sources:
    - name: "global"
      type: "filesystem"
      root: "./customizations/global"
      precedence: 30

    - name: "team-engineering"
      type: "filesystem"
      root: "./customizations/team-engineering"
      precedence: 10
      scope_slug: "engineering"

    - name: "team-content"
      type: "filesystem"
      root: "./customizations/team-content"
      precedence: 10
      scope_slug: "content"
```

---

## Inspecting Active Customizations

Use the API to see what's resolved for the current scope:

```bash
curl -s \
  -H 'X-Scope-ID: <scope-uuid>' \
  http://localhost:8080/customizations/resolve | jq .
```

Response:

```json
{
  "scope_id": "uuid",
  "skills": [
    { "name": "skill", "source": "global", "precedence": 30, "path": "./customizations/global/SKILL.md" }
  ],
  "agents": [
    { "name": "senior-dev", "source": "team-engineering", "precedence": 10, "path": "..." }
  ],
  "prompts": [
    { "name": "safety", "source": "global", "precedence": 30, "path": "..." },
    { "name": "go-style", "source": "team-engineering", "precedence": 10, "path": "..." }
  ]
}
```

---

## Builtin Registration

You can register in-process customization items at startup using `Registry.RegisterBuiltin`:

```go
reg.RegisterBuiltin(&customization.Item{
    Kind:    customization.KindSkill,
    Name:    "core-skill",
    Content: "...",
})
```

Builtin items appear in snapshots for any source of type `builtin`. They are filtered by the source's `enabled_types` and the source's own `Precedence`.
