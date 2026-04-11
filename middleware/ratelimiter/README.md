# Rate Limiter Middleware

A token-bucket rate limiter middleware for Go HTTP servers.

## Usage

```go
router.Use(ratelimiter.New(10, 20)) // 10 tokens, 20 burst capacity
```

## Features

- Token-bucket algorithm for smooth rate limiting
- Configurable rate and burst capacity
- Thread-safe implementation
- Works with any HTTP router
