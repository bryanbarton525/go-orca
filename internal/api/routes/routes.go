// Package routes wires the Gin router for the gorca API.
package routes

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/go-orca/go-orca/docs"
	"github.com/go-orca/go-orca/internal/api/handlers"
	"github.com/go-orca/go-orca/internal/api/middleware"
	"github.com/go-orca/go-orca/internal/customization"
	"github.com/go-orca/go-orca/internal/storage"
	"github.com/go-orca/go-orca/internal/workflow/scheduler"
)

// Config holds the dependencies required to wire all routes.
type Config struct {
	Store                 storage.Store
	Scheduler             *scheduler.Scheduler
	Logger                *zap.Logger
	DefaultTenantID       string
	DefaultScopeID        string
	CustomizationRegistry *customization.Registry
	// GinMode sets gin.SetMode; defaults to "release".
	GinMode string
}

// New creates and returns a fully wired Gin engine.
func New(cfg Config) *gin.Engine {
	if cfg.GinMode == "" {
		cfg.GinMode = gin.ReleaseMode
	}
	gin.SetMode(cfg.GinMode)

	r := gin.New()

	// ── Global middleware ────────────────────────────────────────────────────
	r.Use(middleware.Recovery(cfg.Logger))
	r.Use(middleware.Logger(cfg.Logger))
	r.Use(middleware.RequestID())
	r.Use(middleware.TenantFromHeader(cfg.DefaultTenantID))
	r.Use(middleware.ScopeFromHeader(cfg.DefaultScopeID))

	// ── Health probes ────────────────────────────────────────────────────────
	r.GET("/healthz", handlers.Healthz())
	r.GET("/readyz", handlers.Readyz(cfg.Store))

	// ── Workflows ────────────────────────────────────────────────────────────
	wf := r.Group("/workflows")
	{
		wf.GET("", handlers.ListWorkflows(cfg.Store))
		wf.POST("", handlers.CreateWorkflow(cfg.Store, cfg.Scheduler, cfg.Logger))
		wf.GET("/:id", handlers.GetWorkflow(cfg.Store))
		wf.GET("/:id/events", handlers.GetWorkflowEvents(cfg.Store))
		wf.GET("/:id/stream", handlers.StreamWorkflowEvents(cfg.Store))
		wf.POST("/:id/cancel", handlers.CancelWorkflow(cfg.Store, cfg.Logger))
		wf.POST("/:id/resume", handlers.ResumeWorkflow(cfg.Store, cfg.Scheduler, cfg.Logger))
	}

	// ── Providers ────────────────────────────────────────────────────────────
	prov := r.Group("/providers")
	{
		prov.GET("", handlers.ListProviders())
		prov.POST("/:name/test", handlers.TestProvider(cfg.Logger))
	}

	// ── Scopes ───────────────────────────────────────────────────────────────
	r.GET("/scopes/:id/effective-config", handlers.GetEffectiveConfig(cfg.Store))

	// ── Tenants ──────────────────────────────────────────────────────────────
	tenants := r.Group("/tenants")
	{
		tenants.GET("", handlers.ListTenants(cfg.Store))
		tenants.POST("", handlers.CreateTenant(cfg.Store, cfg.Logger))
		tenant := tenants.Group("/:id")
		{
			tenant.GET("", handlers.GetTenant(cfg.Store))
			tenant.PATCH("", handlers.UpdateTenant(cfg.Store, cfg.Logger))
			tenant.DELETE("", handlers.DeleteTenant(cfg.Store, cfg.Logger))
			tenant.POST("/scopes", handlers.CreateScope(cfg.Store, cfg.Logger))
			tenant.GET("/scopes", handlers.ListScopesForTenant(cfg.Store))
			tenant.PATCH("/scopes/:scopeId", handlers.UpdateScope(cfg.Store, cfg.Logger))
			tenant.DELETE("/scopes/:scopeId", handlers.DeleteScope(cfg.Store, cfg.Logger))
		}
	}

	// ── Customizations ───────────────────────────────────────────────────────
	r.GET("/customizations/resolve", handlers.ResolveCustomizations(cfg.CustomizationRegistry))

	// ── API Docs (Swagger UI) ─────────────────────────────────────────────────
	r.GET("/docs/openapi.yaml", func(c *gin.Context) {
		c.Data(200, "application/yaml; charset=utf-8", docs.OpenAPISpec)
	})
	r.GET("/docs", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(200, swaggerHTML)
	})

	return r
}

// swaggerHTML is a minimal Swagger UI page that loads the spec from the API.
const swaggerHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>go-orca API Docs</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
<div id="swagger-ui"></div>
<script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>
  SwaggerUIBundle({
    url: "/docs/openapi.yaml",
    dom_id: "#swagger-ui",
    presets: [SwaggerUIBundle.presets.apis, SwaggerUIBundle.SwaggerUIStandalonePreset],
    layout: "BaseLayout",
    deepLinking: true,
  });
</script>
</body>
</html>`
