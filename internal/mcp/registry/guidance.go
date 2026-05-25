package registry

import (
	"context"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"

	"github.com/go-orca/go-orca/internal/mcp/guidance"
)

// GuidanceForToolchain returns MCP prompt/resource text for personas. It prefers
// content cached at connect time from the toolchain's MCP server, and falls back
// to embedded guidance when the server did not expose prompts/resources.
func (r *Registry) GuidanceForToolchain(toolchainID string) string {
	tc, ok := r.Toolchain(toolchainID)
	if !ok {
		return guidance.ContextForToolchain(toolchainID)
	}
	r.mu.RLock()
	entry, ok := r.servers[tc.MCPServer]
	cached := ""
	if ok && entry != nil {
		cached = entry.guidanceText
	}
	r.mu.RUnlock()
	if strings.TrimSpace(cached) != "" {
		return cached
	}
	return guidance.ContextForToolchain(toolchainID)
}

func (r *Registry) cacheServerGuidance(ctx context.Context, serverName string, session *sdkmcp.ClientSession) {
	if session == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	var sb strings.Builder
	sb.WriteString("## MCP server guidance (")
	sb.WriteString(serverName)
	sb.WriteString(")\n")

	for _, promptName := range []string{guidance.PromptInstallDependencies, guidance.PromptImplementationRemediation} {
		res, err := session.GetPrompt(ctx, &sdkmcp.GetPromptParams{Name: promptName})
		if err != nil {
			r.logger.Debug("mcp prompt fetch skipped",
				zap.String("server", serverName),
				zap.String("prompt", promptName),
				zap.Error(err))
			continue
		}
		sb.WriteString("\n### Prompt: ")
		sb.WriteString(promptName)
		sb.WriteString("\n")
		for _, msg := range res.Messages {
			if tc, ok := msg.Content.(*sdkmcp.TextContent); ok && tc.Text != "" {
				sb.WriteString(tc.Text)
				sb.WriteString("\n")
			}
		}
	}

	for _, uri := range []string{guidance.URIPackageJSONSchema, guidance.URIPnpmWorkspace} {
		res, err := session.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
		if err != nil {
			r.logger.Debug("mcp resource fetch skipped",
				zap.String("server", serverName),
				zap.String("uri", uri),
				zap.Error(err))
			continue
		}
		sb.WriteString("\n### Resource: ")
		sb.WriteString(uri)
		sb.WriteString("\n")
		for _, c := range res.Contents {
			if c.Text != "" {
				sb.WriteString(c.Text)
				sb.WriteString("\n")
			}
		}
	}

	text := strings.TrimSpace(sb.String())
	if text == "" {
		return
	}
	r.mu.Lock()
	if entry, ok := r.servers[serverName]; ok && entry != nil {
		entry.guidanceText = text
	}
	r.mu.Unlock()
}
