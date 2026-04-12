// Package config provides application configuration loading, validation,
// and access. Configuration can be supplied via YAML file, environment
// variables, or defaults. Viper is used as the underlying engine.
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// DatabaseDriver selects which SQL backend to use.
type DatabaseDriver string

const (
	DriverPostgres DatabaseDriver = "postgres"
	DriverSQLite   DatabaseDriver = "sqlite"
)

// ScopingMode controls which scope kinds are enabled.
type ScopingMode string

const (
	ScopingModeGlobal ScopingMode = "global" // homelab: one global scope, no org/team
	ScopingModeOrg    ScopingMode = "org"    // org under global, no teams
	ScopingModeTeam   ScopingMode = "team"   // full tree: global -> org -> team
	ScopingModeHosted ScopingMode = "hosted" // multi-tenant hosted deployment
)

// SourceType defines where a customization source is loaded from.
type SourceType string

const (
	SourceTypeFilesystem SourceType = "filesystem"
	SourceTypeRepo       SourceType = "repo"
	SourceTypeGitMirror  SourceType = "git-mirror"
	SourceTypeBuiltin    SourceType = "builtin"
)

// Config is the root configuration structure for go-orca.
type Config struct {
	Server         ServerConfig         `mapstructure:"server"`
	Database       DatabaseConfig       `mapstructure:"database"`
	Logging        LoggingConfig        `mapstructure:"logging"`
	Scoping        ScopingConfig        `mapstructure:"scoping"`
	Providers      ProvidersConfig      `mapstructure:"providers"`
	Tools          ToolsConfig          `mapstructure:"tools"`
	Customizations CustomizationsConfig `mapstructure:"customizations"`
	Workflow       WorkflowConfig       `mapstructure:"workflow"`
	GitHub         GitHubConfig         `mapstructure:"github"`
}

// GitHubConfig holds credentials used by delivery actions that interact with
// the GitHub API (github-pr, repo-commit-only).
// Environment variable: GOORCA_GITHUB_TOKEN
type GitHubConfig struct {
	Token string `mapstructure:"token"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
	TrustedProxies  []string      `mapstructure:"trusted_proxies"`
	Mode            string        `mapstructure:"mode"` // debug | release | test
}

// DatabaseConfig holds persistence settings.
type DatabaseConfig struct {
	Driver          DatabaseDriver `mapstructure:"driver"`
	DSN             string         `mapstructure:"dsn"`
	MaxOpenConns    int            `mapstructure:"max_open_conns"`
	MaxIdleConns    int            `mapstructure:"max_idle_conns"`
	ConnMaxLifetime time.Duration  `mapstructure:"conn_max_lifetime"`
	MigrationsPath  string         `mapstructure:"migrations_path"`
	AutoMigrate     bool           `mapstructure:"auto_migrate"`
}

// LoggingConfig controls the logger.
type LoggingConfig struct {
	Level  string `mapstructure:"level"`  // debug | info | warn | error
	Format string `mapstructure:"format"` // json | console
}

// ScopingConfig controls which scope kinds are enabled and bootstrap defaults.
type ScopingConfig struct {
	Mode                 ScopingMode `mapstructure:"mode"`
	AllowGlobal          bool        `mapstructure:"allow_global"`
	AllowOrg             bool        `mapstructure:"allow_org"`
	AllowTeam            bool        `mapstructure:"allow_team"`
	RequireTeamParentOrg bool        `mapstructure:"require_team_parent_org"`
	DefaultTenantSlug    string      `mapstructure:"default_tenant"`
	DefaultScopeSlug     string      `mapstructure:"default_scope"`
}

// ProvidersConfig holds per-provider settings.
type ProvidersConfig struct {
	OpenAI    OpenAIConfig    `mapstructure:"openai"`
	Ollama    OllamaConfig    `mapstructure:"ollama"`
	Copilot   CopilotConfig   `mapstructure:"copilot"`
	Anthropic AnthropicConfig `mapstructure:"anthropic"`
}

// AnthropicConfig holds Anthropic Claude API settings.
type AnthropicConfig struct {
	Enabled        bool          `mapstructure:"enabled"`
	APIKey         string        `mapstructure:"api_key"`
	BaseURL        string        `mapstructure:"base_url"`
	DefaultModel   string        `mapstructure:"default_model"`
	ExcludedModels []string      `mapstructure:"excluded_models"`
	MaxTokens      int           `mapstructure:"max_tokens"`
	Timeout        time.Duration `mapstructure:"timeout"`
}

// OpenAIConfig holds OpenAI / Codex settings.
type OpenAIConfig struct {
	Enabled        bool          `mapstructure:"enabled"`
	APIKey         string        `mapstructure:"api_key"`
	BaseURL        string        `mapstructure:"base_url"`
	DefaultModel   string        `mapstructure:"default_model"`
	ExcludedModels []string      `mapstructure:"excluded_models"`
	Timeout        time.Duration `mapstructure:"timeout"`
}

// OllamaConfig holds Ollama settings.
type OllamaConfig struct {
	Enabled        bool          `mapstructure:"enabled"`
	Host           string        `mapstructure:"host"`
	DefaultModel   string        `mapstructure:"default_model"`
	ExcludedModels []string      `mapstructure:"excluded_models"`
	Timeout        time.Duration `mapstructure:"timeout"`
	TLSSkipVerify  bool          `mapstructure:"tls_skip_verify"`
	// NumCtx sets the context window size (num_ctx) sent to Ollama.
	// Defaults to 0, which lets Ollama use the model's built-in default
	// (often 2048–4096). For long-form generation set this to 16384 or higher
	// to prevent the model from truncating output mid-content.
	NumCtx int `mapstructure:"num_ctx"`
}

// CopilotConfig holds GitHub Copilot SDK settings.
type CopilotConfig struct {
	Enabled        bool     `mapstructure:"enabled"`
	GitHubToken    string   `mapstructure:"github_token"`
	CLIPath        string   `mapstructure:"cli_path"`
	DefaultModel   string   `mapstructure:"default_model"`
	ExcludedModels []string `mapstructure:"excluded_models"`
}

// CustomizationsConfig controls customization discovery sources.
type CustomizationsConfig struct {
	Sources []CustomizationSource `mapstructure:"sources"`
}

// ToolsConfig holds external tool integration settings.
type ToolsConfig struct {
	MCP []MCPServerConfig `mapstructure:"mcp"`
}

// MCPServerConfig defines a single MCP server connection.
// Transport values: "streamable" (default), "sse", "command".
type MCPServerConfig struct {
	// Name is a human-readable identifier used in log messages.
	Name string `mapstructure:"name"`
	// Transport selects the connection type: "streamable", "sse", or "command".
	// Defaults to "streamable" for HTTP servers.
	Transport string `mapstructure:"transport"`
	// Endpoint is the HTTP URL for streamable / SSE transports.
	Endpoint string `mapstructure:"endpoint"`
	// Command is the executable for the "command" transport (e.g. "uvx", "npx").
	Command string `mapstructure:"command"`
	// Args are the arguments passed to Command.
	Args []string `mapstructure:"args"`
	// Env holds additional environment variables to pass to the subprocess.
	// Format: KEY=VALUE   (only used with transport: command)
	Env []string `mapstructure:"env"`
	// HTTPTimeout overrides the default 30s timeout for HTTP transports.
	HTTPTimeout time.Duration `mapstructure:"http_timeout"`
	// TLSSkipVerify disables TLS certificate verification. Dev only.
	TLSSkipVerify bool `mapstructure:"tls_skip_verify"`
}

// CustomizationSource defines one resolved source for skills/agents/prompts.
type CustomizationSource struct {
	Name           string     `mapstructure:"name"`
	Type           SourceType `mapstructure:"type"`
	Root           string     `mapstructure:"root"`
	Precedence     int        `mapstructure:"precedence"`
	EnabledTypes   []string   `mapstructure:"enabled_types"` // skills | agents | prompts
	ScopeSlug      string     `mapstructure:"scope_slug"`
	RefreshSeconds int        `mapstructure:"refresh_seconds"`
}

// WorkflowConfig controls workflow engine behaviour.
type WorkflowConfig struct {
	MaxConcurrentWorkflows  int           `mapstructure:"max_concurrent_workflows"`
	MaxConcurrentTasks      int           `mapstructure:"max_concurrent_tasks"`
	DefaultPersonaTimeoutMs int           `mapstructure:"default_persona_timeout_ms"`
	EventRetentionDays      int           `mapstructure:"event_retention_days"`
	ArtifactStoragePath     string        `mapstructure:"artifact_storage_path"`
	HandoffTimeout          time.Duration `mapstructure:"handoff_timeout"`
	// PersonaMaxRetries is the number of additional attempts after a transient
	// LLM failure (context deadline, connection error).  0 disables retries.
	PersonaMaxRetries int `mapstructure:"persona_max_retries"`
	// PersonaRetryBackoff is the base wait before the first retry.  Each
	// subsequent retry doubles the wait (exponential backoff).
	PersonaRetryBackoff time.Duration `mapstructure:"persona_retry_backoff"`
	// MaxQARetries is the maximum number of times the Implementer will be
	// re-run after QA returns blocking issues.  Defaults to 2.
	MaxQARetries int `mapstructure:"max_qa_retries"`
}

// Load reads configuration from the given file (if set) and merges with
// environment variable overrides prefixed GOORCA_.
func Load(cfgFile string) (*Config, error) {
	v := viper.New()

	// Environment variable support.
	v.SetEnvPrefix("GOORCA")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Default values.
	setDefaults(v)

	// File-based config.
	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
	} else {
		v.SetConfigName("go-orca")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("/etc/go-orca")
		v.AddConfigPath("$HOME/.go-orca")
	}

	if err := v.ReadInConfig(); err != nil {
		// File not found is acceptable; all other errors are fatal.
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("config: read error: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("config: unmarshal error: %w", err)
	}

	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("config: validation error: %w", err)
	}

	return &cfg, nil
}

// setDefaults populates sensible defaults for all config fields.
func setDefaults(v *viper.Viper) {
	// Server
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.read_timeout", 30*time.Second)
	v.SetDefault("server.write_timeout", 60*time.Second)
	v.SetDefault("server.shutdown_timeout", 15*time.Second)
	v.SetDefault("server.mode", "release")

	// Database
	v.SetDefault("database.driver", string(DriverSQLite))
	v.SetDefault("database.dsn", "go-orca.db")
	v.SetDefault("database.max_open_conns", 25)
	v.SetDefault("database.max_idle_conns", 5)
	v.SetDefault("database.conn_max_lifetime", 5*time.Minute)
	v.SetDefault("database.migrations_path", "internal/storage/migrations")
	v.SetDefault("database.auto_migrate", true)

	// Logging
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "json")

	// Scoping
	v.SetDefault("scoping.mode", string(ScopingModeGlobal))
	v.SetDefault("scoping.allow_global", true)
	v.SetDefault("scoping.allow_org", false)
	v.SetDefault("scoping.allow_team", false)
	v.SetDefault("scoping.require_team_parent_org", true)
	v.SetDefault("scoping.default_tenant", "default")
	v.SetDefault("scoping.default_scope", "global")

	// Providers
	v.SetDefault("providers.openai.enabled", false)
	v.SetDefault("providers.openai.default_model", "gpt-4o")
	v.SetDefault("providers.openai.timeout", 120*time.Second)
	v.SetDefault("providers.ollama.enabled", false)
	v.SetDefault("providers.ollama.host", "http://localhost:11434")
	v.SetDefault("providers.ollama.default_model", "llama3")
	v.SetDefault("providers.ollama.timeout", 120*time.Second)
	v.SetDefault("providers.copilot.enabled", false)
	v.SetDefault("providers.copilot.default_model", "gpt-4o")

	// Workflow engine
	v.SetDefault("workflow.max_concurrent_workflows", 10)
	v.SetDefault("workflow.max_concurrent_tasks", 50)
	v.SetDefault("workflow.default_persona_timeout_ms", 120000)
	v.SetDefault("workflow.event_retention_days", 90)
	v.SetDefault("workflow.artifact_storage_path", "./artifacts")
	v.SetDefault("workflow.handoff_timeout", 5*time.Minute)
	v.SetDefault("workflow.persona_max_retries", 3)
	v.SetDefault("workflow.persona_retry_backoff", 10*time.Second)
	v.SetDefault("workflow.max_qa_retries", 2)

	// GitHub delivery
	v.SetDefault("github.token", "")
}

// validate enforces hard constraints on the configuration.
func validate(cfg *Config) error {
	if cfg.Scoping.AllowTeam && !cfg.Scoping.AllowOrg {
		return fmt.Errorf("scoping: allow_team requires allow_org to be true")
	}
	if cfg.Database.Driver != DriverPostgres && cfg.Database.Driver != DriverSQLite {
		return fmt.Errorf("database: unsupported driver %q", cfg.Database.Driver)
	}
	return nil
}
