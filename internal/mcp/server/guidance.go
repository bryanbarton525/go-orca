package server

import (
	"context"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// AddMCPPrompt registers an MCP prompt on the SDK server.
func AddMCPPrompt(mcp *sdkmcp.Server, name, description string, args []*sdkmcp.PromptArgument, handler sdkmcp.PromptHandler) {
	mcp.AddPrompt(&sdkmcp.Prompt{
		Name:        name,
		Description: description,
		Arguments:   args,
	}, handler)
}

// AddMCPResource registers a static MCP resource with fixed text content.
func AddMCPResource(mcp *sdkmcp.Server, uri, description, mimeType, body string) {
	mcp.AddResource(&sdkmcp.Resource{
		URI:         uri,
		Name:        uri,
		Description: description,
		MIMEType:    mimeType,
	}, func(_ context.Context, _ *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
		return &sdkmcp.ReadResourceResult{
			Contents: []*sdkmcp.ResourceContents{
				{
					URI:      uri,
					MIMEType: mimeType,
					Text:     body,
				},
			},
		}, nil
	})
}
