// Package middleware provides Gin middleware for the gorca API.
package middleware

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Logger returns a Gin middleware that logs requests using zap.
func Logger(log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()
		fields := []zap.Field{
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.String("query", query),
			zap.Int("status", status),
			zap.Duration("latency", latency),
			zap.String("client_ip", c.ClientIP()),
		}
		if requestID, ok := c.Get("X-Request-Id"); ok {
			fields = append(fields, zap.Any("request_id", requestID))
		}

		switch {
		case status >= http.StatusInternalServerError:
			log.Error("request", fields...)
		case status >= http.StatusBadRequest:
			log.Warn("request", fields...)
		default:
			log.Info("request", fields...)
		}
	}
}

// Recovery returns a Gin middleware that recovers from panics and returns 500.
func Recovery(log *zap.Logger) gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(nil, func(c *gin.Context, recovered interface{}) {
		log.Error("panic recovered", zap.Any("error", recovered))
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"error": "internal server error",
		})
	})
}

// RequestID injects a request ID into the context.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader("X-Request-Id")
		if rid == "" {
			rid = c.GetHeader("X-Correlation-Id")
		}
		if rid != "" {
			c.Set("X-Request-Id", rid)
			c.Header("X-Request-Id", rid)
		}
		c.Next()
	}
}

// TenantFromHeader extracts the tenant ID from the X-Tenant-ID header and
// stores it in the context under key "tenant_id".  This is a simplistic
// approach suitable for homelab/internal use; replace with JWT validation
// for production hosted deployments.
func TenantFromHeader(defaultTenantID string) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := c.GetHeader("X-Tenant-ID")
		if tenantID == "" {
			tenantID = defaultTenantID
		}
		c.Set("tenant_id", tenantID)
		c.Next()
	}
}

// ScopeFromHeader extracts the scope ID from the X-Scope-ID header.
func ScopeFromHeader(defaultScopeID string) gin.HandlerFunc {
	return func(c *gin.Context) {
		scopeID := c.GetHeader("X-Scope-ID")
		if scopeID == "" {
			scopeID = defaultScopeID
		}
		c.Set("scope_id", scopeID)
		c.Next()
	}
}
