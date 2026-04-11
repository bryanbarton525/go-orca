# Per-Persona Model Routing in go-orca: Enhancing Productivity and Collaboration

In today's AI-powered development workflows, why settle for a one-size-fits-all model when your personas—assistant, implementer, reviewer—have distinct needs? go-orca intelligently matches each persona to the optimal model for its workload, balancing speed, cost, and capability. In this guide, you'll discover how go-orca builds its model catalog, routes personas dynamically, and configures routing in your own workflows.

## The go-orca Discovery Engine

go-orca maintains a dynamic model catalog that knows what's available in your ecosystem. It constructs this catalog by polling registered MCP servers, loading local YAML files, and consulting remote registries. You configure the `mcp.servers` array in your `go-orca.yaml` with entries like this:

```yaml
mcp:
  servers:
    - name: fetch
      endpoint: "http://localhost:3000/mcp"
      transport: streamable
```

The MCP server broadcasts a schema describing available models: name, capabilities (code, tools, reasoning), parameter count (context window in tokens, e.g., 8192 or 32768), and provider. go-orca caches this info locally and refreshes on schedule (default interval: 5 minutes) or when you trigger a refresh via API. Local files load via `file:./models/local.yaml`, while registries can be HTTP endpoints or S3 URLs. go-orca also supports hybrid deployments, mixing MCP, files, and registries in a single catalog.

Failure modes matter. If an MCP server becomes unavailable, go-orca falls back to cached data and logs a warning. You can tune this behavior with `mcp.pollInterval` or `mcp.failureStrategy` (e.g., `retry`, `failOpen`, or `failClosed`). Edge cases include network timeouts during discovery or schema mismatches from incompatible MCP versions. go-orca reports these errors as `ModelCatalogError` structs with `ErrorCode` and `Message` fields for easy debugging.

## The Director's Routing Logic

At the heart of go-orca sits the `Director`, the routing engine that maps each persona to a model. On every request, the Director executes a two-step selection:

1. **Capability Matching**: It filters models that support the persona's required capabilities. An implementer persona might need `["code", "tools"]`; an assistant might only need `["chat"]`.
2. **Parameter Constraint**: From the capability-matched set, it chooses a model whose `parameterSize` (context window tokens) meets the persona's memory requirements.

```go
func (d *Director) RoutePersona(ctx context.Context, p *PersonaConfig) (*ModelInstance, error) {
    candidates := d.catalog.FilterByCapabilities(p.Requirements)
    for _, m := range candidates {
        if m.ParameterSize >= p.ContextTokensNeeded {
            return m, nil
        }
    }
    return nil, ErrNoModelSatisfiesConstraints
}
```

The `RegisterPersona` method allows you to add custom personas with custom capability sets. The `RoutePersona` method returns a `ModelInstance` containing model metadata and a stream channel for responses.

## Why Different Personas Need Different Models

Your personas represent distinct work roles. Each role has unique model requirements:

- **Implementer personas** build code, debug issues, or operate tools. They need models with `code` and `tools` capabilities. A 7B parameter model lacks reasoning depth here; go-orca routes them to a 14B+ model with a large context window for multi-step tasks.
- **Assistant personas** handle chat, summaries, and quick answers. They run on lighter, faster models (e.g., 4–7B parameters) for low-latency responses.
- **Reviewer personas** analyze codebases for security or correctness. They need long-context models (32K+ tokens) but not tool execution.

Think of it like hiring: you wouldn't use an entry-level assistant for a critical code review. go-orca automates this hiring decision dynamically.

## Configuring Routing: Step-by-Step

1. **Define personas in YAML**:

   ```yaml
   # personas.yaml
   personas:
     - name: implementer
       capabilities: ["code", "tools"]
       minParameterSize: 14000
     - name: assistant
       capabilities: ["chat"]
       minParameterSize: 4000
   ```

2. **Initialize the Director**:

   ```go
   import (
       "context"
       "log"

       "github.com/example/go-orca/director"
   )

   cfg := director.Config{
       PersonasPath: "./personas.yaml",
   }
   d, err := director.New(cfg)
   if err != nil {
       log.Fatal(err)
   }

   ctx := context.Background()
   inst, err := d.RoutePersona(ctx, &director.PersonaConfig{
       Name: "implementer",
   })
   if err != nil {
       log.Fatalf("Failed to route persona: %v", err)
   }
   ```

3. **Register custom models via MCP**: Add a new MCP server in `go-orca.yaml` to expose an internal model service. The `RegisterPersona` method supports custom registration for edge cases like on-prem models.

## Conclusion

go-orca's per-persona model routing lets you match workload needs to model capabilities without manual intervention. By automating catalog discovery, capability matching, and parameter sizing, you free your team to focus on building rather than configuring. Start with small persona sets, observe latency and cost metrics, then iterate.

**Take the next step**: [Explore the full go-orca documentation](https://go-orca.dev/docs) or join our [Discord community](https://go-orca.dev/discord) for live Q&A. Your next workflow upgrade awaits—configure routing today and see the difference.