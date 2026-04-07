// Command go-orca-api starts the go-orca workflow orchestration API server.
//
// Configuration is loaded from a YAML file (default: go-orca.yaml) and can be
// overridden with GOORCA_* environment variables.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/go-orca/go-orca/internal/api/routes"
	"github.com/go-orca/go-orca/internal/config"
	"github.com/go-orca/go-orca/internal/customization"
	"github.com/go-orca/go-orca/internal/logger"
	"github.com/go-orca/go-orca/internal/persona"
	"github.com/go-orca/go-orca/internal/persona/architect"
	"github.com/go-orca/go-orca/internal/persona/director"
	"github.com/go-orca/go-orca/internal/persona/finalizer"
	"github.com/go-orca/go-orca/internal/persona/implementer"
	"github.com/go-orca/go-orca/internal/persona/pm"
	"github.com/go-orca/go-orca/internal/persona/qa"
	"github.com/go-orca/go-orca/internal/persona/refiner"
	"github.com/go-orca/go-orca/internal/provider/common"
	copilotProvider "github.com/go-orca/go-orca/internal/provider/copilot"
	ollamaProvider "github.com/go-orca/go-orca/internal/provider/ollama"
	openaiProvider "github.com/go-orca/go-orca/internal/provider/openai"
	"github.com/go-orca/go-orca/internal/storage"
	pgStore "github.com/go-orca/go-orca/internal/storage/postgres"
	sqStore "github.com/go-orca/go-orca/internal/storage/sqlite"
	"github.com/go-orca/go-orca/internal/tenant"
	"github.com/go-orca/go-orca/internal/tools"
	"github.com/go-orca/go-orca/internal/tools/builtin"
	"github.com/go-orca/go-orca/internal/workflow/engine"
	"github.com/go-orca/go-orca/internal/workflow/scheduler"
)

func main() {
	cfgFile := flag.String("config", "", "path to go-orca.yaml config file")
	flag.Parse()

	// ── Load configuration ────────────────────────────────────────────────────
	cfg, err := config.Load(*cfgFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: config load: %v\n", err)
		os.Exit(1)
	}

	// ── Initialize logger ─────────────────────────────────────────────────────
	if err := logger.Init(cfg.Logging); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: logger init: %v\n", err)
		os.Exit(1)
	}
	log := logger.L()
	defer log.Sync() //nolint:errcheck

	log.Info("go-orca starting",
		zap.String("db_driver", string(cfg.Database.Driver)),
		zap.String("scoping_mode", string(cfg.Scoping.Mode)),
	)

	// ── Open storage ──────────────────────────────────────────────────────────
	ctx := context.Background()
	store, err := openStore(ctx, cfg)
	if err != nil {
		log.Fatal("storage init failed", zap.Error(err))
	}
	defer store.Close() //nolint:errcheck

	if err := store.Ping(ctx); err != nil {
		log.Fatal("storage ping failed", zap.Error(err))
	}
	log.Info("storage connected")

	// ── Ensure default tenant + scope (homelab / global mode) ─────────────────
	defaultTenant, defaultScope, err := tenant.EnsureDefault(ctx, store)
	if err != nil {
		log.Fatal("ensure default tenant/scope", zap.Error(err))
	}
	log.Info("default tenant ready",
		zap.String("tenant_id", defaultTenant.ID),
		zap.String("scope_id", defaultScope.ID),
	)

	// ── Register providers ────────────────────────────────────────────────────
	registerProviders(cfg, log)

	// ── Register personas ─────────────────────────────────────────────────────
	persona.Register(director.New())
	persona.Register(pm.New())
	persona.Register(architect.New())
	persona.Register(implementer.New())
	persona.Register(qa.New())
	persona.Register(finalizer.New())
	persona.Register(refiner.New())
	log.Info("personas registered", zap.Int("count", len(persona.All())))

	// ── Register built-in tools ───────────────────────────────────────────────
	toolReg := tools.NewRegistry()
	builtin.RegisterAll(toolReg)
	log.Info("builtin tools registered", zap.Int("count", len(toolReg.All())))

	// ── Build customization registry ──────────────────────────────────────────
	customReg := buildCustomizationRegistry(cfg, log)

	// ── Build workflow engine + scheduler ─────────────────────────────────────
	eng := engine.New(store, engine.Options{
		MaxQARetries:          2,
		DefaultProvider:       resolveDefaultProvider(cfg),
		DefaultModel:          resolveDefaultModel(cfg),
		CustomizationRegistry: customReg,
		HandoffTimeout:        cfg.Workflow.HandoffTimeout,
	})

	sched := scheduler.New(eng, scheduler.Options{
		Concurrency: cfg.Workflow.MaxConcurrentWorkflows,
		RetryDelay:  5 * time.Second,
		MaxRetries:  0,
	}, log)

	// ── Build HTTP server ─────────────────────────────────────────────────────
	router := routes.New(routes.Config{
		Store:                 store,
		Scheduler:             sched,
		Logger:                log,
		DefaultTenantID:       defaultTenant.ID,
		DefaultScopeID:        defaultScope.ID,
		CustomizationRegistry: customReg,
		GinMode:               cfg.Server.Mode,
	})

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	// ── Graceful shutdown ─────────────────────────────────────────────────────
	done := make(chan struct{})
	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		sig := <-quit
		log.Info("shutdown signal received", zap.String("signal", sig.String()))

		shutCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
		defer cancel()

		if err := srv.Shutdown(shutCtx); err != nil {
			log.Error("http server shutdown error", zap.Error(err))
		}
		if err := sched.Shutdown(shutCtx); err != nil {
			log.Error("scheduler shutdown error", zap.Error(err))
		}
		close(done)
	}()

	log.Info("listening", zap.String("addr", addr))
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal("server error", zap.Error(err))
	}

	<-done
	log.Info("shutdown complete")
}

// ─── Bootstrap helpers ────────────────────────────────────────────────────────

func openStore(ctx context.Context, cfg *config.Config) (storage.Store, error) {
	switch cfg.Database.Driver {
	case config.DriverPostgres:
		s, err := pgStore.New(ctx, cfg.Database.DSN)
		if err != nil {
			return nil, err
		}
		if cfg.Database.AutoMigrate {
			if err := s.Migrate(cfg.Database.MigrationsPath); err != nil {
				return nil, fmt.Errorf("postgres migrate: %w", err)
			}
		}
		return s, nil
	case config.DriverSQLite:
		s, err := sqStore.New(cfg.Database.DSN)
		if err != nil {
			return nil, err
		}
		if cfg.Database.AutoMigrate {
			if err := s.Migrate(); err != nil {
				return nil, fmt.Errorf("sqlite migrate: %w", err)
			}
		}
		return s, nil
	default:
		return nil, fmt.Errorf("unknown database driver: %s", cfg.Database.Driver)
	}
}

func registerProviders(cfg *config.Config, log *zap.Logger) {
	if cfg.Providers.OpenAI.Enabled {
		p, err := openaiProvider.New(cfg.Providers.OpenAI)
		if err != nil {
			log.Warn("openai provider init failed", zap.Error(err))
		} else {
			common.Register(p)
			log.Info("provider registered", zap.String("name", "openai"))
		}
	}

	if cfg.Providers.Ollama.Enabled {
		p, err := ollamaProvider.New(cfg.Providers.Ollama)
		if err != nil {
			log.Warn("ollama provider init failed", zap.Error(err))
		} else {
			common.Register(p)
			log.Info("provider registered", zap.String("name", "ollama"))
		}
	}

	if cfg.Providers.Copilot.Enabled {
		p, err := copilotProvider.New(cfg.Providers.Copilot)
		if err != nil {
			log.Warn("copilot provider init failed", zap.Error(err))
		} else {
			common.Register(p)
			log.Info("provider registered", zap.String("name", "copilot"))
		}
	}
}

func resolveDefaultProvider(cfg *config.Config) string {
	if cfg.Providers.OpenAI.Enabled {
		return "openai"
	}
	if cfg.Providers.Ollama.Enabled {
		return "ollama"
	}
	if cfg.Providers.Copilot.Enabled {
		return "copilot"
	}
	return "openai"
}

func resolveDefaultModel(cfg *config.Config) string {
	if cfg.Providers.OpenAI.Enabled && cfg.Providers.OpenAI.DefaultModel != "" {
		return cfg.Providers.OpenAI.DefaultModel
	}
	if cfg.Providers.Ollama.Enabled && cfg.Providers.Ollama.DefaultModel != "" {
		return cfg.Providers.Ollama.DefaultModel
	}
	if cfg.Providers.Copilot.Enabled && cfg.Providers.Copilot.DefaultModel != "" {
		return cfg.Providers.Copilot.DefaultModel
	}
	return "gpt-4o"
}

// buildCustomizationRegistry converts config-defined sources into a live Registry.
func buildCustomizationRegistry(cfg *config.Config, log *zap.Logger) *customization.Registry {
	reg := customization.NewRegistry()
	for _, src := range cfg.Customizations.Sources {
		kinds := make([]customization.Kind, 0, len(src.EnabledTypes))
		for _, t := range src.EnabledTypes {
			kinds = append(kinds, customization.Kind(t))
		}
		reg.AddSource(customization.Source{
			Name:         src.Name,
			Type:         string(src.Type),
			Root:         src.Root,
			Precedence:   src.Precedence,
			EnabledTypes: kinds,
			ScopeSlug:    src.ScopeSlug,
		})
	}
	log.Info("customization registry built",
		zap.Int("sources", len(cfg.Customizations.Sources)))
	return reg
}
