# Storage

go-orca persists all workflow state, events, tenants, and scopes through a single `Store` interface. Two implementations are provided: SQLite (default) and PostgreSQL.

## Store Interface

Package: `internal/storage`

The `Store` interface composes four sub-interfaces:

```
Store
  ├── WorkflowStore    — workflow state CRUD and upsert
  ├── EventStore       — event journal queries
  ├── TenantStore      — tenant CRUD
  └── ScopeStore       — scope CRUD
```

Plus `Ping(ctx)` and `Close()`.

### WorkflowStore

| Method | Description |
|---|---|
| `CreateWorkflow(ctx, ws)` | Insert a new workflow (must not already exist) |
| `GetWorkflow(ctx, id)` | Retrieve a workflow by UUID |
| `SaveWorkflow(ctx, ws)` | Full upsert of workflow state (used by the engine after each phase) |
| `ListWorkflows(ctx, tenantID, limit, offset)` | List workflows for a tenant, newest first |
| `UpdateWorkflowStatus(ctx, id, status, errMsg)` | Targeted status-only update (avoids full upsert contention) |
| `AppendEvents(ctx, evts...)` | Atomically append one or more events to the journal |

### EventStore

| Method | Description |
|---|---|
| `ListEvents(ctx, workflowID)` | All events for a workflow in chronological order |
| `ListEventsByType(ctx, workflowID, evtType)` | Events of a specific type for a workflow |
| `EventsSince(ctx, tenantID, after)` | All events across a tenant after a given timestamp |

### TenantStore

| Method | Description |
|---|---|
| `CreateTenant(ctx, t)` | Insert a new tenant |
| `GetTenant(ctx, id)` | Retrieve by UUID |
| `GetTenantBySlug(ctx, slug)` | Retrieve by slug (used for default tenant lookup) |
| `ListTenants(ctx)` | All tenants |
| `UpdateTenant(ctx, t)` | Replace mutable fields (name, slug) |
| `DeleteTenant(ctx, id)` | Delete by UUID |

### ScopeStore

| Method | Description |
|---|---|
| `CreateScope(ctx, s)` | Insert a new scope |
| `GetScope(ctx, id)` | Retrieve by UUID |
| `ListScopes(ctx, tenantID)` | All scopes for a tenant |
| `UpdateScope(ctx, s)` | Replace mutable fields (name, slug) |
| `DeleteScope(ctx, id)` | Delete by UUID |

---

## SQLite

Package: `internal/storage/sqlite`

SQLite is the default backend. It stores everything in a single `.db` file on disk. Ideal for homelab, local development, and single-node deployments.

### Configuration

```yaml
database:
  driver: "sqlite"
  dsn: "go-orca.db"
  auto_migrate: true
```

The `dsn` is the file path. Relative paths are resolved from the working directory. Use an absolute path in production.

### Connection Details

SQLite does not use `max_open_conns` or `max_idle_conns` in the same way as PostgreSQL — the sqlite driver manages its own internal locking. The connection pool settings in config are accepted but have limited effect on SQLite.

### Migrations

When `auto_migrate: true`, the SQLite store runs `s.Migrate()` at startup. This applies the base DDL and then idempotently adds any new columns via `ALTER TABLE` statements — duplicate-column errors are silently ignored, so it is safe to run against an existing database.

**Schema versions**

| Version | Change |
|---|---|
| v001 | Initial schema: `tenants`, `scopes`, `workflows`, `workflow_events` |
| v002 | `scope_settings` table |
| v003 | `task_edges` on `workflows` |
| v004 | `all_suggestions`, `persona_prompt_snapshot`, `required_personas`, `finalizer_action` columns |
| v005 | `execution` column (`TEXT NOT NULL DEFAULT '{}'`) — stores in-flight progress |

---

## PostgreSQL

Package: `internal/storage/postgres`

PostgreSQL is the recommended backend for multi-user, multi-node, or production deployments.

### Configuration

```yaml
database:
  driver: "postgres"
  dsn: "postgres://go-orca:secret@localhost:5432/go-orca?sslmode=disable"
  max_open_conns: 25
  max_idle_conns: 5
  conn_max_lifetime: "5m"
  migrations_path: "internal/storage/migrations"
  auto_migrate: true
```

### DSN Format

Standard PostgreSQL connection string or URL:

```
postgres://user:password@host:port/dbname?sslmode=disable
```

Also accepts `GOORCA_DATABASE_DSN` environment variable:

```bash
export GOORCA_DATABASE_DSN="postgres://go-orca:secret@db.internal:5432/go-orca?sslmode=require"
```

### Connection Pool

| Setting | Default | Description |
|---|---|---|
| `max_open_conns` | 25 | Maximum open connections |
| `max_idle_conns` | 5 | Maximum idle connections kept in pool |
| `conn_max_lifetime` | 5m | Maximum lifetime of a single connection |

### Migrations

When `auto_migrate: true`, the PostgreSQL store runs `s.Migrate(migrationsPath)` at startup, applying SQL files from `migrations_path` in lexicographic order. The default path is `internal/storage/migrations`.

To manage migrations manually, set `auto_migrate: false` and apply the SQL files yourself.

---

## Default Tenant and Scope Bootstrap

At startup, `tenant.EnsureDefault(ctx, store)` is called unconditionally. It:

1. Looks up the tenant by the configured `default_tenant` slug (default: `"default"`)
2. Creates it if it does not exist
3. Looks up (or creates) the default scope with slug `"global"` under that tenant
4. Returns the tenant and scope IDs used as API defaults

The default tenant and scope IDs are injected into every request via middleware when `X-Tenant-ID` / `X-Scope-ID` headers are absent.

---

## Choosing a Backend

| Consideration | SQLite | PostgreSQL |
|---|---|---|
| Setup complexity | None | Requires a running PostgreSQL server |
| Single-node deployment | Excellent | Good |
| Multi-node / horizontal scaling | Not supported | Supported |
| Concurrent writes | Serialised (WAL mode) | Fully concurrent |
| Production readiness | Suitable for low-traffic / homelab | Recommended for production |
| Backup | Copy the `.db` file | Use `pg_dump` / WAL archiving |

---

## Switching Backends

To migrate from SQLite to PostgreSQL:

1. Export your data (or start fresh — workflows are ephemeral)
2. Update `database.driver` to `"postgres"` and set `dsn`
3. Restart — `auto_migrate: true` will create the schema automatically
