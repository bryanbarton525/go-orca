// Package guidance provides MCP prompts and resources shared by Node/Next.js toolchain servers.
package guidance

import (
	"context"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/go-orca/go-orca/internal/mcp/server"
)

const (
	// Prompt names exposed via MCP prompts/get.
	PromptInstallDependencies       = "install_dependencies"
	PromptImplementationRemediation = "implementation_validation_remediation"

	// Resource URIs exposed via MCP resources/read.
	URIPackageJSONSchema = "orca://schemas/package.json"
	URIPnpmWorkspace     = "orca://schemas/pnpm-workspace.yaml"
	URINextJSPreflight   = "orca://schemas/nextjs-preflight"
)

const packageJSONSchemaDoc = `# package.json (strict JSON)

pnpm and npm read package.json as **JSON only**. The file must:

- Start with { (no leading // or /* comments, no prose like "Contents of...")
- Use double-quoted keys and strings
- Omit trailing commas
- Be a single JSON object at the root

Invalid example (will break pnpm install):
` + "```" + `
// Contents of updated package.json
{"name":"app"}
` + "```" + `

Valid example:
` + "```json" + `
{
  "name": "rss-newspaper",
  "version": "0.1.0",
  "private": true,
  "scripts": {
    "dev": "next dev",
    "build": "next build",
    "start": "next start",
    "test": "jest"
  }
}
` + "```" + `

When fixing validation failures that mention "Unexpected token '/'" or "not valid JSON", edit the on-disk package.json in the workspace — do not add comment lines.
`

const nextJSPreflightDoc = `# Next.js workspace preflight

The engine rejects these before install/build when using the nextjs or node toolchain:

## Fake build scripts
scripts.build must run the real compiler (e.g. "next build"). These fail preflight:
- "echo build successful"
- "echo no tests" for build (test script is separate)
- "true", ":", bare echo commands

## PostCSS / Tailwind deps
If postcss.config.js references tailwindcss or autoprefixer, those packages must appear in package.json devDependencies.

## Route conflicts
- Only one page.* file per App Router segment (not both page.js and page.tsx in app/)
- Do not mix app/page.* and pages/index.* for the same root route

## Client components
Interactive pages using useState/useEffect/localStorage need "use client" at the top of the file.

## Scope
Ship one stack per workflow. A "simple todo app" should not accumulate Go backends, RSS readers, or blog stubs from prior tasks.
`

const installPromptBody = `You are about to run package installation (pnpm install or npm install) in a go-orca workflow workspace.

Before install:
1. Read resource orca://schemas/package.json if unsure about manifest format.
2. Ensure package.json is strict JSON (no // comments, no prose prefixes).
3. Use plain pnpm install during implementation validation — do not require a lockfile on greenfield workspaces.

If install fails with JSON parse errors, fix package.json first; do not assign another install-only task without editing the file.
`

const remediationPromptBody = `Implementation validation failed (install, test, build, lint, or typecheck).

Rules for remediation tasks:
1. Read the Latest Validation Result and Blocking Issues in the handoff context.
2. If stderr mentions "Unexpected token" or "not valid JSON" for package.json, assign ONE pod task to rewrite package.json as strict JSON (remove all comment lines). Do not assign duplicate "Install Dependencies" or "Re-run Scaffolding" tasks unless the manifest is already valid JSON.
3. Do not re-plan the whole project — minimal tasks only, assigned to pod.
4. Read MCP resource orca://schemas/package.json for the required manifest shape.
`

// RegisterNode registers prompts and schema resources on a Node toolchain MCP server.
func RegisterNode(s *server.Server) {
	registerCommon(s, "Node.js")
}

// RegisterNextJS registers prompts and schema resources on a Next.js toolchain MCP server.
func RegisterNextJS(s *server.Server) {
	registerCommon(s, "Next.js")
}

func registerCommon(s *server.Server, stack string) {
	mcp := s.MCPServer()
	stackInstall := strings.ReplaceAll(installPromptBody, "package installation", stack+" package installation")
	stackRemediation := remediationPromptBody + "\nStack: " + stack + ".\n"

	server.AddMCPPrompt(mcp, PromptInstallDependencies,
		"Guidance before running install_dependencies / next_install / node_install.",
		nil, staticPrompt(stackInstall))

	server.AddMCPPrompt(mcp, PromptImplementationRemediation,
		"Guidance for Architect remediation after implementation validation failures.",
		[]*sdkmcp.PromptArgument{
			{Name: "cycle", Description: "Remediation cycle number", Required: false},
		},
		func(_ context.Context, req *sdkmcp.GetPromptRequest) (*sdkmcp.GetPromptResult, error) {
			cycle := "unknown"
			if req.Params != nil && req.Params.Arguments != nil {
				if v := req.Params.Arguments["cycle"]; v != "" {
					cycle = v
				}
			}
			body := stackRemediation + "\nRemediation cycle: " + cycle + ".\n"
			return promptResult(body), nil
		})

	server.AddMCPResource(mcp, URIPackageJSONSchema,
		"Strict JSON schema and examples for package.json in workflow workspaces.",
		"application/json", packageJSONSchemaDoc)

	server.AddMCPResource(mcp, URIPnpmWorkspace,
		"Notes for pnpm workspace layout (optional monorepos).",
		"text/markdown", "# pnpm workspace\n\nOptional. Single-package Next.js apps usually only need package.json at the workspace root.\n")

	server.AddMCPResource(mcp, URINextJSPreflight,
		"Next.js workspace preflight checklist (build scripts, deps, route conflicts).",
		"text/markdown", nextJSPreflightDoc)
}

// ContextForToolchain returns guidance text for engine handoff packets when MCP resources are not fetched live.
func ContextForToolchain(toolchainID string) string {
	switch strings.ToLower(strings.TrimSpace(toolchainID)) {
	case "nextjs", "node", "javascript", "typescript":
		var sb strings.Builder
		sb.WriteString("## Toolchain MCP guidance (")
		sb.WriteString(toolchainID)
		sb.WriteString(")\n")
		sb.WriteString(installPromptBody)
		sb.WriteString("\n\n")
		sb.WriteString(remediationPromptBody)
		sb.WriteString("\n\n### package.json schema\n")
		sb.WriteString(packageJSONSchemaDoc)
		return sb.String()
	default:
		return ""
	}
}

func staticPrompt(body string) sdkmcp.PromptHandler {
	return func(_ context.Context, _ *sdkmcp.GetPromptRequest) (*sdkmcp.GetPromptResult, error) {
		return promptResult(body), nil
	}
}

func promptResult(body string) *sdkmcp.GetPromptResult {
	return &sdkmcp.GetPromptResult{
		Description: "go-orca toolchain guidance",
		Messages: []*sdkmcp.PromptMessage{
			{Role: "user", Content: &sdkmcp.TextContent{Text: body}},
		},
	}
}
