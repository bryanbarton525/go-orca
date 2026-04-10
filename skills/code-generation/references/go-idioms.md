# Go Idioms Reference

Curated patterns with before/after examples for writing idiomatic Go.

## Error Wrapping

```go
// ✗ Loses origin
return errors.New("operation failed")

// ✓ Wraps and preserves origin for errors.Is / errors.As
return fmt.Errorf("load config: %w", err)
```

## Early Return

```go
// ✗ Nested
if err == nil {
    if result != nil {
        process(result)
    }
}

// ✓ Early return
if err != nil {
    return err
}
if result == nil {
    return errors.New("no result")
}
process(result)
```

## Context Propagation

```go
// ✓ Always first param, never stored in structs
func FetchUser(ctx context.Context, id string) (*User, error) {
    req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
    ...
}
```

## Table-Driven Tests

```go
func TestAdd(t *testing.T) {
    cases := []struct {
        name string
        a, b int
        want int
    }{
        {"positive", 1, 2, 3},
        {"negative", -1, -2, -3},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            if got := Add(tc.a, tc.b); got != tc.want {
                t.Errorf("Add(%d,%d) = %d; want %d", tc.a, tc.b, got, tc.want)
            }
        })
    }
}
```

## Generic Constraints

```go
// ✓ Use type parameter with constraint rather than any
func MapSlice[T, U any](s []T, fn func(T) U) []U {
    out := make([]U, len(s))
    for i, v := range s {
        out[i] = fn(v)
    }
    return out
}
```
