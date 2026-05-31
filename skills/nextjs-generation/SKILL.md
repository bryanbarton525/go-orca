---
name: nextjs-generation
description: Use when implementing or reviewing Next.js App Router applications in go-orca workflows.
---
# Next.js Generation Skill

Use this skill when the workflow request targets a **Next.js** (or React App Router) web application. Pair with `code-generation` layout profiles and `mcp-integration` toolchain configuration.

## Scope discipline — CRITICAL

Ship **one coherent app** per workflow. Common ORCA failure mode: multiple Pod tasks from different cycles leave overlapping artifacts (todo + RSS reader + Go backend + blog stubs in one repo).

- **Single stack**: If the request is a todo app, emit only Next.js/React files — no `go.mod`, no `pages/` Newsstand stubs, no Prisma RSS schemas unless explicitly requested.
- **Single router**: Use **App Router** (`app/`) exclusively for greenfield Next.js projects. Do not add `pages/` unless migrating an existing Pages Router repo.
- **Single entry page**: Exactly **one** `page.tsx` (or `page.js`) per route segment. Never leave both `app/page.js` and `app/page.tsx` — Next.js precedence is unpredictable and QA will block.
- **Single layout**: One root `app/layout.tsx`. Remove duplicate `layout.js` / `layout.tsx` pairs.

## Bootstrap task — REQUIRED

The Architect must schedule an early **frontend** Pod task that scaffolds:

1. `package.json` — strict JSON, real scripts (see below)
2. `tsconfig.json` and `next.config.ts` (or `.js`)
3. `app/layout.tsx` and `app/page.tsx`

No feature tasks may depend on files that reference packages not yet declared in `package.json`.

## package.json scripts — CRITICAL

Scripts must invoke real tooling. **Never** use no-op stubs:

| Script | Required value (Next.js) | Forbidden |
|--------|--------------------------|-----------|
| `dev` | `next dev` | — |
| `build` | `next build` | `echo build successful`, `true`, `: ` |
| `start` | `next start` | — |
| `test` | `jest` or `vitest run` | `echo no tests` (unless tests truly deferred and documented) |

The engine preflight rejects fake build scripts before `run_build` validation runs.

## Dependency versions — use latest stable

- Do not hardcode stale major versions in generated manifests (for example `next@13` in a new project).
- When adding new dependencies during remediation/scaffolding, prefer latest stable releases via package manager commands (`npm install <pkg>@latest` or `pnpm add <pkg>@latest`) unless the constitution explicitly pins a version.
- Skills/prompts should describe *capabilities* and compatibility constraints, not fixed version numbers.

## Dependencies must match config files

If the workspace contains any of these config files, declare matching packages in `package.json`:

| Config file | Required packages |
|-------------|-------------------|
| `postcss.config.js` with `tailwindcss` | `tailwindcss`, `autoprefixer`, `postcss` in devDependencies |
| `tailwind.config.ts` | `tailwindcss`, `postcss`, `autoprefixer` |
| `prisma/schema.prisma` | `@prisma/client`, `prisma` |
| `app/actions.ts` using Prisma | `@prisma/client`, `prisma` |
| RSS/API routes using `rss-parser` | `rss-parser` |

Missing deps cause `next dev` to fail with `Cannot find module` even when `pnpm install` succeeds.

## Client vs Server Components

- **Default**: Server Components (no directive).
- **`"use client"`** required at top of file when using: `useState`, `useEffect`, `useReducer`, event handlers, browser APIs (`localStorage`, `window`).
- A todo app with interactive checkboxes **must** be a Client Component or split: thin client wrapper + server layout.

Example minimal client page:

```tsx
"use client";

import { useState, useEffect } from "react";

export default function TodoPage() {
  // ...
}
```

## Styling choices

For **MVP/simple apps** (todo lists, dashboards):

- Prefer **plain CSS** (`app/globals.css` + class names) or inline styles to avoid Tailwind bootstrap overhead.
- If using Tailwind: add all PostCSS deps in the same scaffold task that creates `postcss.config.js`.

Do not create `postcss.config.js` referencing `tailwindcss` without adding `tailwindcss` to `package.json`.

## File naming

- Prefer `.tsx` + TypeScript for new projects (`tsconfig.json` present).
- Do not mix `.js` and `.tsx` page modules at the same route.
- Colocate components in `components/`; shared utilities in `lib/`.

## Validation expectations

When `tools.toolchains` includes `nextjs` or `node`, the engine runs:

1. **Workspace preflight** — package.json JSON, fake build scripts, PostCSS deps, route conflicts
2. **install_dependencies** — `pnpm install` / `npm install`
3. **run_build** — must execute real `next build`
4. **run_tests** / **typecheck** — when configured

Pod must fix preflight blockers before requesting another install task.

## MVP task graph guidance

For a simple todo app, cap at ~8 tasks:

1. Scaffold Next.js project (package.json, configs, layout, empty page)
2. Todo UI page (client component, localStorage)
3. Styles (globals.css)
4. README with run instructions
5. (Optional) Basic test or typecheck fix task

Do not add RSS ingestion, Go backends, Docker, or blog posts unless the constitution requires them.

## References

- `skills/code-generation/SKILL.md` — Next.js layout profile
- `skills/mcp-integration/SKILL.md` — toolchain MCP wiring
- MCP resource `orca://schemas/nextjs-preflight` — engine preflight checklist
