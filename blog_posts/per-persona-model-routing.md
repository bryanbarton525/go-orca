# Per-Persona Model Routing in go-orca: Building Intelligent Workflows

If you've been exploring go-orca, you might be wondering how the system decides which AI model to call when orchestrating a multi-persona workflow. This guide explains the routing mechanism, why different personas need different models, and how to configure your own routing strategy.

## The Model Catalog: Your Foundation

go-orca doesn't arbitrarily pick models. It discovers a catalog at startup through a `tools.mcp` configuration block. Each MCP server exposes a `ListTools` endpoint that registers available tools in the global registry.

```yaml
tools:
  mcp:
    - name: fetch
      endpoint: "http://localhost:3000/mcp"
      transport: streamable

    - name: local-tools
      command: uvx
      args: ["mcp-server-fetch"]
      transport: command
```

This simple config tells go-orca to connect to two MCP servers. On startup, go-orca calls `ListTools` on each endpoint and builds an index. From that index, the Director can query available models.

## Why Route Models by Persona?

Not all personas have the same requirements. The routing system considers three factors:

1. **Tool capability** — Some personas must call external tools. Only models with `tools=yes` qualify.
2. **Parameter count** — Larger models handle synthesis better; smaller models excel at classification.
3. **Task type** — Planning tasks need compact reasoning; synthesis tasks need broad knowledge.

### Implementer: The Code Generator

The implementer writes code and generates content. This role produces large artifacts and needs a model that can reason through complex tasks. We assign a coder-focused, larger-parameter model:

- `qwen2.5-coder:14b` (14.8B params, tools=yes)

The Coder family is trained for programming tasks. Even if a workflow is purely content, using a coder model for the implementer yields better technical writing and code examples.

### Finalizer: The Synthesizer

The finalizer polishes and packages the output. This is synthesis-heavy: it must read many intermediate artifacts and produce a cohesive final piece. We again use a larger model:

- `qwen2.5-coder:14b` or `qwen2.5-coder:7b`

The Coder family handles long-context summarization better than base models.

### Project Manager & Architect: The Planners

These personas define requirements and design. Their outputs are compact (JSON plans, ADRs), so we don't need massive capacity. Smaller models work well:

- `qwen3.5:9b` (9.7B params, tools=yes) or
- `qwen3:1.7b` (2.0B params, tools=yes)

The 9B model offers a good balance; the 1.7B model is fine for simple classification tasks.

### QA: The Validator

QA reviews code, checks requirements, and validates the design. It needs a model that can catch edge cases and reason about correctness. We use:

- `qwen3.5:9b`

This model handles code review tasks without the overhead of a 14B+ model.

## Configuring Your Routing

Routing happens in your go-orca.yaml. Under the `models` section, you define each persona's assignment:

```yaml
models:
  project_manager: qwen3.5:9b
  architect: qwen3.5:9b
  implementer: qwen2.5-coder:14b
  qa: qwen3.5:9b
  finalizer: qwen2.5-coder:14b

  # Provider is inferred from the tool set
  provider: ollama
```

The `provider` field maps to your MCP endpoint. Since you're using `ollama` with `streamable` transport, the system auto-detects.

If a persona doesn't need special handling, you can omit it. The default model (`qwen3.5:9b`) applies.

### Tool Capability Matters

Remember: if a persona must call tools, the model must have `tools=yes`. Check each model's metadata:

| Model | Params | Tools |
|-------|--------|-------|
| `qwen2.5-coder:14b` | 14.8B | yes |
| `qwen2.5-coder:7b` | 7.6B | yes |
| `qwen3.5:9b` | 9.7B | yes |
| `qwen3:1.7b` | 2.0B | yes |
| `gemma4:e4b` | 8.0B | yes |
| `llama3.2:1b` | 1.2B | yes |

Models like `codegemma:7b`, `phi3:mini`, or `nomic-embed-text` lack tool support and can't be assigned to tool-using personas.

## Advanced Routing Strategies

### Role-Specific Overrides

Some workflows benefit from role overrides. For instance, if you want QA to be more strict on security checks, assign it a stronger model:

```yaml
models:
  qa: qwen2.5-coder:14b  # stricter validation
```

But be mindful: larger models increase latency. If you're serving many concurrent workflows, the extra tokens per turn can add up.

### Dynamic Routing

go-orca doesn't support runtime model switching. Choose wisely at startup. However, you can expose model selection via configuration files that your deployment pipeline manages.

### Fallback Chains

You can define fallback models if the primary is unavailable:

```yaml
models:
  implementer:
    primary: qwen2.5-coder:14b
    fallback: qwen2.5-coder:7b
```

The Director will retry with the fallback if the primary times out or fails.

## Performance Trade-offs

### Latency vs. Quality

Larger models produce better outputs but take longer. If you're running on a constrained VPS, smaller models reduce latency. For local dev, `qwen2.5-coder:14b` on a GPU with 12GB+ VRAM is acceptable.

### Token Consumption

Synthesis-heavy roles (finalizer) consume more tokens because they process many intermediate artifacts. If you hit context limits, switch to a 7B model for finalizer and rely on prompt engineering to keep the output concise.

### Cost Considerations

If you're paying per token (cloud deployment), the routing strategy directly affects your bill. For example:

- Implementer on 14B model at $0.002/1k tokens → $0.02 for 10k tokens
- Implementer on 1.7B model at $0.0001/1k tokens → $0.001 for 10k tokens

Choose the smallest model that meets your quality threshold.

## Example Workflow

Let's walk through a sample workflow:

1. **User request**: "Write a blog post about Go concurrency patterns."
2. **Director** creates a plan with `mode=content`.
3. **Project Manager** (qwen3.5:9b) defines the constitution: "Target 1200 words, include code examples."
4. **Architect** (qwen3.5:9b) designs the task graph: hook → context → body → conclusion.
5. **Implementer** (qwen2.5-coder:14b) writes the blog post, calling tools to fetch documentation if needed.
6. **QA** (qwen3.5:9b) reviews the draft, checks word count, validates code examples.
7. **Finalizer** (qwen2.5-coder:14b) polishes the post, adds a call-to-action, and returns the result.

If QA finds blocking issues (e.g., missing code examples), the Architect re-designs the task graph and the Implementer rewrites. The workflow iterates until QA approves.

## Debugging Routing Issues

If a persona returns an error like "model not found," check:

1. Your `go-orca.yaml` models section.
2. That the model exists in the MCP tool catalog.
3. That your ollama instance has the model loaded (`ollama list`).

To list available models in go-orca, query the `/tools` endpoint or inspect the tool catalog response.

## Conclusion

Per-persona model routing is about matching models to roles. Use larger, coder-focused models for implementation and synthesis; use compact models for planning and validation. Configure your routing at startup, and iterate on model choices based on latency and quality metrics.

By understanding the routing mechanism, you can optimize go-orca workflows for your specific use case—whether that's local development, cloud deployment, or edge scenarios.

[Get the full configuration example](#configuring-your-routing) · [Read the model catalog documentation](/docs/models.md)

---

**CTA**: Start routing models in your go-orca workflow today. Share your routing configurations in our Discord or open an issue on GitHub.
