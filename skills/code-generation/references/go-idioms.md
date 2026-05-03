# Go Idiomatic Patterns for go-orca

## Module Versioning (go.mod)

Go requires Major Version Suffixes in module paths for v2+ releases.

```
# CORRECT — v2+ modules include /v2, /v3, etc. in the path
require github.com/go-co-op/gocron/v2 v2.2.5
require github.com/labstack/echo/v4 v4.13.3
require github.com/jackc/pgx/v5 v5.7.2

# WRONG — these will fail with "version invalid: should be v0 or v1, not v2"
require github.com/go-co-op/gocron v2.2.5
require github.com/labstack/echo v4.13.3
```

The import path in source files must match:
```go
import (
    "github.com/go-co-op/gocron/v2"   // CORRECT for v2
    "github.com/labstack/echo/v4"      // CORRECT for v4
)
```

For recurring tasks, prefer `time.Ticker` (zero deps) over scheduler libraries:
```go
ticker := time.NewTicker(5 * time.Minute)
defer ticker.Stop()
for {
    select {
    case <-ticker.C:
        doWork(ctx)
    case <-ctx.Done():
        return
    }
}
```

This document provides curated examples of idiomatic Go patterns relevant to the go-orca workflow orchestration system. Use these patterns when implementing model registration, routing, and concurrency primitives.

---

## Error Handling

### Wrapping Errors with Context

```go
// BAD: Silently swallowing errors
func registerModel(name string, config Config) {
    if err := validateConfig(config); err != nil {
        // error ignored
        return
    }
    // ...
}

// GOOD: Wrap errors with context
func registerModel(ctx context.Context, name string, config Config) error {
    if err := validateConfig(config); err != nil {
        return fmt.Errorf("failed to validate model %q: %w", name, err)
    }
    
    if err := storage.Put(ctx, "models", name, config); err != nil {
        return fmt.Errorf("failed to store model config: %w", err)
    }
    
    return nil
}

var (
    ErrModelNotFound    = errors.New("model not found in registry")
    ErrInvalidConfig    = errors.New("invalid model configuration")
    ErrMaxModelsReached = errors.New("maximum number of models reached")
)
```

### Early Returns on Error

```go
// BAD: Deeply nested success path
func handleRequest(ctx context.Context, req *api.Request) (*api.Response, error) {
    if req.Version == "" {
        err := errors.New("version required")
        return nil, err
    }
    if req.ModelName == "" {
        err := errors.New("model name required")
        return nil, err
    }
    
    model, err := registry.Get(ctx, req.ModelName)
    if err != nil {
        return nil, err
    }
    
    resp, err := model.Handle(ctx, req)
    if err != nil {
        return nil, err
    }
    
    return resp, nil
}

// GOOD: Early returns for clarity
func handleRequest(ctx context.Context, req *api.Request) (*api.Response, error) {
    if req.Version == "" {
        return nil, errors.New("version required")
    }
    if req.ModelName == "" {
        return nil, errors.New("model name required")
    }
    
    model, err := registry.Get(ctx, req.ModelName)
    if err != nil {
        return nil, err
    }
    
    resp, err := model.Handle(ctx, req)
    if err != nil {
        return nil, err
    }
    
    // success path is the main code path
    return resp, nil
}
```

---

## Struct Definitions

### Favor Concrete Types over `any`

```go
// BAD: Using any breaks type safety
type Model interface {
    Handle(ctx context.Context, req Request) any
}

// GOOD: Use concrete types or generic constraints
type Model interface {
    Handle(ctx context.Context, req Request) (*Response, error)
}

type LLMModel interface {
    Model
    Temperature(ctx context.Context, req Request) float64
    TopP(ctx context.Context, req Request) float64
}
```

### Struct Layout for API Types

```go
// Model configuration struct with clear field naming
type ModelConfig struct {
    // Required fields
    Name        string          `json:"name"`
    Version     string          `json:"version"`
    Endpoint    string          `json:"endpoint"`
    
    // Optional fields with defaults
    Timeout     time.Duration   `json:"timeout,omitempty"`
    RetryCount  int             `json:"retry_count,omitempty"`
    MetricsPort uint            `json:"metrics_port,omitempty"`
    
    // Validation tags
    Validate() error
}

// Model registration entry
type ModelRegistration struct {
    // Immutable identity fields
    ID          string `json:"id"`
    Name        string `json:"name"`
    Version     string `json:"version"`
    CreatedAt   time.Time `json:"created_at"`
    
    // Mutable configuration
    Config      *ModelConfig
    Status      ModelStatus `json:"status"`
    Capabilities []Capability `json:"capabilities,omitempty"`
    
    // Metrics
    Requests     uint64 `json:"requests"`
    Errors       uint64 `json:"errors"`
    LastAccessed time.Time `json:"last_accessed,omitempty"`
}
```

### Embedding for Code Organization

```go
// GOOD: Use embedding for related behaviors
type Model struct {
    Base    // Embed base model functionality
    
    // Type-specific methods
    *llm.Handler
    
    // Type-specific fields
    modelVersion string
}

// Base provides common routing behavior
type Base struct {
    ID   string
    Name string
    
    // Protected by mutex for concurrent access
    mu     sync.RWMutex
    config atomic.Value // Use atomic.Value for safe config updates
}
```

---

## Concurrency Patterns

### Context Propagation

```go
// BAD: No context cancellation check
func processRequest(req Request) {
    time.Sleep(100 * time.Millisecond) // Will block forever
}

// GOOD: Always pass context and check cancellation
func processRequest(ctx context.Context, req Request) (*Response, error) {
    defer func() {
        log.Printf("request %s completed: %s", req.ID, status)
    }()
    
    // Check context before blocking
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
        // proceed
    }
    
    result, err := makeBlockingCall(ctx, req)
    if err != nil {
        return nil, err
    }
    
    return result, nil
}

// Blocking call that respects context
func makeBlockingCall(ctx context.Context, req Request) (*Response, error) {
    for i := 0; i < maxRetries; i++ {
        select {
        case <-ctx.Done():
            return nil, ctx.Err()
        default:
        }
        
        result, err := attemptRequest(ctx, req)
        if err == nil {
            return result, nil
        }
        
        log.Printf("attempt %d failed: %v", i+1, err)
        time.Sleep(backoffTime)
    }
    
    return nil, fmt.Errorf("max retries reached: %w", req.Err)
}
```

### Channels for Coordination

```go
// BAD: Shared memory without synchronization
var results = make(map[string]Response)

func worker(id int, wg *sync.WaitGroup) {
    defer wg.Done()
    results[workerID] = makeResponse() // UNSAFE
}

// GOOD: Channels for coordination
func processBatch(ctx context.Context, requests []Request) ([]Response, error) {
    results := make(chan Response, len(requests))
    wg := new(sync.WaitGroup)
    
    for i, req := range requests {
        wg.Add(1)
        go func(idx int, r Request) {
            defer wg.Done()
            
            resp, err := handleRequest(ctx, r)
            if err != nil {
                log.Printf("worker %d: %v", idx, err)
                return
            }
            
            results <- *resp
        }(i, req)
    }
    
    // Collect results
    go func() {
        wg.Wait()
        close(results)
    }()
    
    var responses []Response
    for resp := range results {
        responses = append(responses, resp)
    }
    
    return responses, nil
}
```

### Using errgroup for Fan-out

```go
// GOOD: Use errgroup for bounded fan-out
func registerModels(ctx context.Context, configs []*ModelConfig) error {
    ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
    defer cancel()
    
    g, ctx := errgroup.WithContext(ctx)
    
    for _, cfg := range configs {
        cfg := cfg // capture loop variable
        g.Go(func() error {
            select {
            case <-ctx.Done():
                return ctx.Err()
            default:
            }
            
            if err := validateModel(cfg); err != nil {
                return fmt.Errorf("model %q: %w", cfg.Name, err)
            }
            
            if err := registry.Store(cfg); err != nil {
                return fmt.Errorf("failed to register model %q: %w", cfg.Name, err)
            }
            
            log.Printf("registered model %q version %q", cfg.Name, cfg.Version)
            return nil
        })
    }
    
    return g.Wait()
}
```

---

## Mutex Usage

### Compound Operations Require Mutex

```go
// BAD: Mixing atomic with mutex protects partial state
type ModelBucket struct {
    tokens       int
    tokenCount   int64 // protected by atomic
    burst        int
    mutex        sync.Mutex
}

func (b *ModelBucket) consumeToken() bool {
    // UNSAFE: atomic while mutex held
    b.mutex.Lock()
    atomic.AddInt64(&b.tokenCount, -1)
    b.mutex.Unlock()
    
    return b.tokenCount >= 0
}

// GOOD: Use mutex for compound operations
type ModelBucket struct {
    tokens       int
    burst        int
    mutex        sync.Mutex
    lastReplenish time.Time
    rate         int // tokens per 1000ms
}

func (b *ModelBucket) consumeToken() bool {
    b.mutex.Lock()
    defer b.mutex.Unlock()
    
    if b.tokens > 0 {
        b.tokens--
        return true
    }
    
    // Replenish if allowed
    now := time.Now().UnixMilli()
    elapsed := now - b.lastReplenish
    tokensToAdd := elapsed * b.rate / 1000
    
    if tokensToAdd > 0 {
        b.lastReplenish = time.UnixMilli(now)
        b.tokens = min(b.tokens+tokensToAdd, b.burst)
        b.tokens--
    }
    
    return b.tokens >= 0
}

func (b *ModelBucket) allow(ctx context.Context, bucket *Bucket) bool {
    b.mutex.Lock()
    defer b.mutex.Unlock()
    
    if b.tokens >= 1 {
        return true
    }
    
    // Check context while waiting
    for {
        if b.tokens >= 1 {
            return true
        }
        
        select {
        case <-ctx.Done():
            return false
        default:
            time.Sleep(time.Millisecond)
        }
    }
}
```

---

## Validation Patterns

### Struct Validation with Methods

```go
// GOOD: Validation methods on types
type ModelConfig struct {
    Name   string `json:"name" validate:"required"`
    Version string `json:"version" validate:"required"`
    Endpoint string `json:"endpoint"`
}

func (c *ModelConfig) Validate() error {
    if c.Name == "" {
        return fmt.Errorf("model name is required")
    }
    if c.Version == "" {
        return fmt.Errorf("model version is required")
    }
    if c.Endpoint == "" {
        return fmt.Errorf("model endpoint is required")
    }
    
    // Validate endpoint URL
    u, err := url.Parse(c.Endpoint)
    if err != nil {
        return fmt.Errorf("invalid endpoint URL: %w", err)
    }
    
    // Validate version semver
    if !semver.IsValid(c.Version) {
        return fmt.Errorf("invalid semver version: %q", c.Version)
    }
    
    return nil
}

type ModelRegistration struct {
    Config *ModelConfig `json:"config"`
    Capabilities []Capability `json:"capabilities,omitempty"`
}

func (r *ModelRegistration) Validate() error {
    if r.Config == nil {
        return ErrInvalidConfig
    }
    
    if err := r.Config.Validate(); err != nil {
        return fmt.Errorf("invalid config: %w", err)
    }
    
    // Validate capabilities
    for i, cap := range r.Capabilities {
        if cap == "" {
            return fmt.Errorf("capability at index %d cannot be empty", i)
        }
    }
    
    return nil
}
```

---

## Logging Patterns

### Structured Logging with Context

```go
// GOOD: Include relevant context in log lines
func registerModel(ctx context.Context, cfg *ModelConfig) error {
    log := log.With(
        "model", cfg.Name,
        "version", cfg.Version,
        "event", "register",
    )
    
    log.Infow("starting model registration", 
        "duration", time.Since(start))
    
    if err := registry.Store(cfg); err != nil {
        log.Errorw("failed to register model",
            "error", err,
            "stack", strings.TrimPrefix(debug.Stack(), "\n"))
        return err
    }
    
    log.Infow("model registered successfully")
    return nil
}
```

### Error Context in Logs

```go
// BAD: Losing context in error logs
func processRequest(req *api.Request) error {
    if err := process(); err != nil {
        return err // Lost request ID
    }
    return nil
}

// GOOD: Preserve context
func processRequest(ctx context.Context, req *api.Request) error {
    if err := process(ctx, req); err != nil {
        // Include request context in error log
        log.Errorw("request processing failed",
            "request_id", req.ID,
            "error", err,
            "method", req.Method,
            "path", req.Path,
        )
        return err
    }
    return nil
}
```

---

## Testing Patterns

### Table-Driven Tests

```go
func TestModelRegistration(t *testing.T) {
    tests := []struct {
        name string
        cfg  *ModelConfig
        wantErr error
    }{
        {
            name: "valid config",
            cfg: &ModelConfig{
                Name: "test-model",
                Version: "v1.0.0",
                Endpoint: "http://localhost:8080",
            },
            wantErr: nil,
        },
        {
            name: "missing name",
            cfg: &ModelConfig{
                Name: "",
                Version: "v1.0.0",
                Endpoint: "http://localhost:8080",
            },
            wantErr: ErrInvalidConfig,
        },
        {
            name: "missing version",
            cfg: &ModelConfig{
                Name: "test-model",
                Version: "",
                Endpoint: "http://localhost:8080",
            },
            wantErr: ErrInvalidConfig,
        },
        {
            name: "invalid endpoint",
            cfg: &ModelConfig{
                Name: "test-model",
                Version: "v1.0.0",
                Endpoint: "not-a-url",
            },
            wantErr: fmt.Errorf("invalid endpoint URL: %w", urlParseError),
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := tt.cfg.Validate()
            if (err != nil) != (tt.wantErr != nil) {
                t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}
```

### Test Server Cleanup

```go
func TestModelEndpoint(t *testing.T) {
    // GOOD: Always use fresh mux and close test server
    mux := http.NewServeMux()
    mux.HandleFunc("/v1/models", handleRegister)
    
    ts := httptest.NewServer(mux)
    defer ts.Close() // CRITICAL: Prevent goroutine leaks
    
    resp, err := http.Post(ts.URL+"/v1/models", "application/json", nil)
    if err != nil {
        t.Fatal(err)
    }
    
    defer resp.Body.Close()
}
```

---

## References

- [Go Project Layout](https://github.com/golang-standards/project-layout)
- [Go Error Wrapping Best Practices](https://github.com/golang/go/wiki/CommonMistakes#using-a-variable-of-type-error)
- [Go Concurrency Patterns](https://go.dev/blog/pipelines)
- [Go Testing](https://go.dev/doc/testing)

---

## Variadic Slice Expansion — VALID SYNTAX, DO NOT FLAG

`append(dst, src...)` is a **core Go language feature** (spec §Built-in functions).
The `...` spread operator expands a slice argument into variadic parameters.
This is **never invalid syntax**.  QA must NOT flag it as an error.

```go
// CORRECT – append all elements from one slice into another
models = append(models, additionalModels...)

// CORRECT – expand a method-returned slice
catalog = append(catalog, source.Models()...)

// CORRECT – three-argument form is identical
result = append(result, items...)
```

Incorrect QA complaint to reject: *"`append(models, source.Models()...)` is not valid Go syntax because
`Models()` returns a slice and `...` cannot be used directly"* — this claim is **wrong**.
The variadic spread works on any slice expression, including function call return values.
