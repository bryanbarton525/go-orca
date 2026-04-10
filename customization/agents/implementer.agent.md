---
name: implementer
description: Agent overlay for the Implementer persona — activates code-generation and content-writing skills.
applies_to: implementer
---

# Implementer Agent Overlay

This overlay augments the Implementer persona with project-specific guidelines.

## Active Skills

- **code-generation** — Follow idiomatic Go patterns, early-return error handling, context propagation, and table-driven tests.
- **content-writing** — When the task is to write documentation or a blog post, apply the post structure and style rules from this skill.

## Project Conventions

- Module path: `github.com/go-orca/go-orca`
- All new packages must have at least one `_test.go` file before the PR is considered done.
- Struct fields that hold sensitive values (tokens, passwords) must be tagged with `json:"-"`.
- Use `go-orca/internal/logger` for structured logging; never use `fmt.Println` in library code.

## Output Format

When delivering code:
1. Show the complete file (or the complete changed function) — no truncated snippets.
2. Explain any non-obvious design choices in a brief comment.
3. If the change requires a config update, include the YAML snippet.
