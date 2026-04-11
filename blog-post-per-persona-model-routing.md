# Per-Persona Model Routing in Multi-Agent AI Orchestration

> How to route the right compute to the right agent when building AI-native Go tooling

When building multi-agent AI orchestration systems in Go, one of the most critical architectural decisions is model routing: ensuring each persona gets the right amount of compute power for its specific tasks. This post walks through our implementation approach, catalog discovery mechanisms, routing decisions, and practical examples for Go developers.

## The Problem: One Size Doesn't Fit All

Consider a Go microservice orchestrating AI agents. You have five distinct roles:

- **Project Manager**: Defines requirements, validates scope
- **Architect**: Designs system architecture, creates component diagrams
- **Implementer**: Generates code, produces large artifacts
- **QA**: Reviews code, identifies edge cases
- **Finalizer**: Synthesizes outputs into final deliverables

If you route all requests to a 13B parameter model, you're wasting resources on simple classification tasks (Project Manager, QA). If you use a 1B model for code generation (Implementer), you'll hit quality cliffs.

The answer: **per-persona model routing** — assigning different models based on task complexity and artifact size.

## Catalog Discovery: Finding Available Models

Before routing can happen, you need to know what models are available. Our system maintains a catalog at `/models/catalog.json` that tracks:

```json
{
  "ollama": [
    {
      "name": "qwen3.5:9b",
      "params": 9.7,
      "family": "qwen35",
      "suitable_for": ["classification", "planning", "validation"]
    },
    {
      "name": "codegemma:7b",
      "params": 9,
      "family": "gemma",
      "suitable_for": ["code_generation", "light_synth"]
    },
    {
      "name": "deepseek-coder-v2:16b",
      "params": 15.7,
      "family": "deepseek2",
      "suitable_for": ["complex_synth", "code_gen"]
    }
  ]
}
```

The catalog also tracks current load per model to prevent overloading. Here's how the discovery flow works:

```go
func DiscoverAvailableModels(ctx context.Context) ([]ModelInfo, error) {
    catalog, err := loadCatalog("models/catalog.json")
    if err != nil {
        return nil, fmt.Errorf("failed to load model catalog: %w", err)
    }

    onlineModels := make([]ModelInfo, 0, len(catalog))
    for provider, models := range catalog {
        for _, model := range models {
            if isModelOnline(ctx, model.Name) {
                onlineModels = append(onlineModels, model)
            }
        }
    }

    return onlineModels, nil
}
```

**Key takeaway**: Always check model availability before routing. Never assume a model is online just because it was registered yesterday.

## The Director Routing Decision

The Director persona is the traffic cop. It doesn't generate final outputs — it makes routing decisions based on the incoming request and available resources.

Here's the routing logic:

```go
type RoutingDecision struct {
    PersonaChain     []string
    Models           []ModelAssignment
    Rationale        string
}

type ModelAssignment struct {
    Persona  string
    Model    string
    Provider string
}

func (d *Director) RouteRequest(ctx context.Context,
    request WorkflowRequest,
    availableModels []ModelInfo) (*RoutingDecision, error) {

    // Step 1: Determine required personas
    requiredPersonas := d.analyzeRequest(request)
    
    // Step 2: Match personas to models based on task profile
    assignments := make([]ModelAssignment, 0, len(requiredPersonas))
    
    for _, persona := range requiredPersonas {
        model := d.selectModelForPersona(persona, request, availableModels)
        assignments = append(assignments, ModelAssignment{
            Persona: persona,
            Model:   model.Name,
            Provider: model.Provider,
        })
    }
    
    // Step 3: Check context window sufficiency
    if !d.verifyContextWindows(ctx, assignments, request) {
        return nil, d.buildContextWindowError(assignments, request)
    }
    
    return &RoutingDecision{
        PersonaChain: requiredPersonas,
        Models:       assignments,
        Rationale:    d.generateRationale(request),
    }, nil
}
```

### Routing Decision Factors

The Director weighs multiple factors:

1. **Task complexity** — Is this classification (light) or synthesis (heavy)?
2. **Artifact size** — Will the Implementer need 8K or 32K tokens?
3. **Persona role** — Does this role typically need reasoning or generation?
4. **Model availability** — Is the preferred model currently overloaded?
5. **Cost constraints** — Are we in cost-saving mode?

## Per-Persona Model Assignments

### Project Manager (Light Tasks)

The Project Manager defines requirements and validates scope. Tasks are typically classification-heavy, not generation-heavy.

**Recommended model**: `qwen3.5:9b` or `phi3:mini`

```go
func (d *Director) SelectModelForProjectManager(
    ctx context.Context,
    request WorkflowRequest) *ollama.Model {
    
    // Use lightweight model for classification tasks
    return &ollama.Model{
        Name:        "qwen3.5:9b",
        Family:      "qwen35",
        Params:      9700,
        SuitableFor: []string{"classification", "planning", "validation"},
    }
}
```

### Architect (Design Work)

The Architect creates component diagrams and designs system topology. This requires good spatial reasoning but moderate generation capacity.

**Recommended model**: `qwen3.5:9b` or `codegemma:7b`

```go
func (d *Director) SelectModelForArchitect(
    ctx context.Context,
    request WorkflowRequest) *ollama.Model {
    
    // Balance reasoning and generation capability
    if request.ArtifactType == "mermaid_diagram" {
        // Light diagrams can use phi3
        return &ollama.Model{
            Name: "phi3:mini",
            Family: "phi3",
        }
    }
    
    return &ollama.Model{
        Name:        "qwen3.5:9b",
        Family:      "qwen35",
        Params:      9700,
    }
}
```

### Implementer (Synthesis Heavy)

This is where routing matters most. The Implementer generates code, blog posts, or large artifacts.

**Recommended model**: `deepseek-coder-v2:16b` or `qwen2.5-coder:14b`

```go
func (d *Director) SelectModelForImplementer(
    ctx context.Context,
    request WorkflowRequest) *ollama.Model {
    
    // Code generation needs larger context and strong coding knowledge
    switch {
    case request.Mode == "content":
        // Blog posts: use coder model for quality
        return &ollama.Model{
            Name:        "qwen2.5-coder:14b",
            Family:      "qwen2",
            Params:      14800,
            Reasoning:   true,
        }
    case request.Mode == "software":
        // Code generation: prefer coder family
        return &ollama.Model{
            Name:        "deepseek-coder-v2:16b",
            Family:      "deepseek2",
            Params:      15700,
            Reasoning:   true,
        }
    default:
        // Fall back to gemma
        return &ollama.Model{
            Name:        "codegemma:7b",
            Family:      "gemma",
            Params:      9000,
        }
    }
}
```

### QA (Validation Tasks)

QA reviews code and identifies issues. This is classification-heavy with some reasoning.

**Recommended model**: `qwen3.5:9b` or `phi3:mini`

```go
func (d *Director) SelectModelForQA(
    ctx context.Context,
    request WorkflowRequest) *ollama.Model {
    
    // Validation is classification-heavy
    return &ollama.Model{
        Name:        "qwen3.5:9b",
        Family:      "qwen35",
        Params:      9700,
        SuitableFor: []string{"validation", "classification"},
    }
}
```

### Finalizer (Synthesis and Polish)

The Finalizer synthesizes outputs from all personas into final deliverables.

**Recommended model**: `qwen3.5:9b` or `deepseek-coder-v2:16b` depending on synthesis complexity

```go
func (d *Director) SelectModelForFinalizer(
    ctx context.Context,
    request WorkflowRequest) *ollama.Model {
    
    // Simple finalization: lighter model
    // Complex synthesis: larger model
    switch {
    case len(request.PersonaOutputs) > 3:
        // Multiple artifacts need synthesis
        return &ollama.Model{
            Name:        "qwen3.5:9b",
            Family:      "qwen35",
            Params:      9700,
        }
    default:
        return &ollama.Model{
            Name:        "qwen3.5:9b",
            Family:      "qwen35",
            Params:      9700,
        }
    }
}
```

## Context-Window Considerations

Context windows are finite resources. You must plan for them carefully.

### Understanding the Trade-Offs

| Model | Context Window | Best For |
|-------|---------------|----------|
| `phi3:mini` | 4K | Simple classification, lightweight tasks |
| `qwen3.5:9b` | 32K | Multi-turn conversations, moderate synthesis |
| `deepseek-coder-v2:16b` | 32K+ | Code generation, complex synthesis |

### When to Up-Level

The Director should upgrade a persona's model when:

- Context window would be exceeded
- Synthesis artifact > 4K tokens
- Code generation complexity exceeds 100 lines
- Error rate on current model > 5%

Here's how we detect context exhaustion:

```go
func (d *Director) CheckContextWindow(
    ctx context.Context,
    persona string,
    currentAssignment ModelAssignment,
    newContextTokens int) bool {
    
    modelInfo := d.getModelInfo(currentAssignment.Model)
    maxContext := modelInfo.MaxContextTokens
    
    if newContextTokens > maxContext {
        // Find an alternative model with larger context
        for _, model := range d.availableModels {
            if model.Params > modelInfo.Params && 
               model.MaxContextTokens > newContextTokens {
                return false // Need to upgrade
            }
        }
        return false // No suitable model available
    }
    
    return true // Context is fine
}
```

## Concrete Example: Blog Post Workflow

Let's walk through a real example: generating a 1200-word technical blog post.

```go
func GenerateBlogPost(ctx context.Context,
    topic string,
    wordCount int) (*WorkflowResponse, error) {

    request := &WorkflowRequest{
        Mode:          "content",
        Topic:         topic,
        WordCount:     wordCount,
        Personas:      []string{"project_manager", "architect", "implementer", "qa", "finalizer"},
    }
    
    // Step 1: Get available models
    availableModels, err := DiscoverAvailableModels(ctx)
    if err != nil {
        return nil, err
    }
    
    // Step 2: Route request
    routingDecision, err := d.RouteRequest(ctx, *request, availableModels)
    if err != nil {
        return nil, err
    }
    
    // Step 3: Execute persona chain with assigned models
    for _, assignment := range routingDecision.Models {
        response, err := executePersona(ctx, assignment, request)
        if err != nil {
            return nil, fmt.Errorf("persona %s failed: %w", assignment.Persona, err)
        }
        
        // Store for next persona
        request = updateRequestWithResponse(request, response)
    }
    
    return routingDecision, nil
}
```

In this workflow:

- **Project Manager** (qwen3.5:9b): Validates blog post requirements, identifies target audience
- **Architect** (qwen3.5:9b): Designs outline, creates h2/h3 structure
- **Implementer** (qwen2.5-coder:14b): Generates ~1000 words of content
- **QA** (qwen3.5:9b): Reviews for factual accuracy and completeness
- **Finalizer** (qwen3.5:9b): Synthesizes into polished final version

## Best Practices for Go Developers

1. **Always check model availability before routing** — Never assume a model is online.
2. **Route based on task profile, not just persona name** — A Project Manager task might need a larger model if it's complex requirements gathering.
3. **Respect context windows** — Calculate token counts before sending requests.
4. **Use model caching** — Don't reload models on every request if you know they're stable.
5. **Implement fallback chains** — If the preferred model is unavailable, route to alternatives.
6. **Monitor error rates** — High error rates indicate the need for model upgrades or load balancing.

## Conclusion

Per-persona model routing is about matching compute resources to task requirements. By understanding each persona's role and the models available, you can build cost-effective AI orchestration systems that deliver high quality without wasting resources.

The key insight: **not all personas need the same model**. Lightweight tasks get lightweight models. Synthesis-heavy tasks get larger, smarter models. This approach can reduce compute costs by 30-50% while maintaining or improving quality.

Start by cataloging your available models, then implement the routing logic shown above. As your system grows, add monitoring and fallback mechanisms to handle failures gracefully.

---

*Build your own AI orchestration system and share your routing strategies in the comments.*
