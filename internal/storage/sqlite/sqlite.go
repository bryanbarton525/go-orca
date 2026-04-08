// Package sqlite provides a SQLite-backed implementation of storage.Store
// using the mattn/go-sqlite3 driver via database/sql.
//
// Use this for homelab / single-node deployments.  For production multi-tenant
// deployments use the Postgres implementation instead.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/go-orca/go-orca/internal/events"
	"github.com/go-orca/go-orca/internal/state"
)

// Store implements storage.Store against a SQLite database file.
type Store struct {
	db *sql.DB
}

// New opens (or creates) a SQLite database at the given path and returns a Store.
// Use ":memory:" for tests.
func New(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("sqlite: open %s: %w", path, err)
	}
	// SQLite is not safe for concurrent writes from multiple goroutines without
	// serialization.  Limit to one writer.
	db.SetMaxOpenConns(1)
	return &Store{db: db}, nil
}

// Ping verifies the connection.
func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}

// Migrate runs the embedded schema DDL against the database.
// In production you should use golang-migrate; this is a convenience helper
// for homelab / test bootstrapping.
//
// For existing databases it also attempts to add any new columns introduced
// after the initial schema via ALTER TABLE statements.  SQLite does not
// support IF NOT EXISTS on ALTER TABLE, so each statement is executed
// individually and "duplicate column" errors are silently ignored.
func (s *Store) Migrate() error {
	if _, err := s.db.Exec(sqliteDDL); err != nil {
		return err
	}
	// Idempotently add columns introduced in schema v004.
	for _, stmt := range sqliteAlterV004 {
		if _, err := s.db.Exec(stmt); err != nil {
			// "duplicate column name" is the expected error when the column
			// already exists from a previous migration run — ignore it.
			if !isDuplicateColumnError(err) {
				return fmt.Errorf("sqlite migrate v004: %w", err)
			}
		}
	}
	// Idempotently add columns introduced in schema v005.
	for _, stmt := range sqliteAlterV005 {
		if _, err := s.db.Exec(stmt); err != nil {
			if !isDuplicateColumnError(err) {
				return fmt.Errorf("sqlite migrate v005: %w", err)
			}
		}
	}
	return nil
}

// isDuplicateColumnError returns true when the SQLite error message indicates
// that a column with that name already exists.
func isDuplicateColumnError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return len(msg) >= 22 && msg[:22] == "duplicate column name:"
}

// ─── WorkflowStore ────────────────────────────────────────────────────────────

func (s *Store) CreateWorkflow(ctx context.Context, ws *state.WorkflowState) error {
	p, err := marshalWorkflow(ws)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO workflows
		  (id, tenant_id, scope_id, status, mode, title, request,
		   provider_name, model_name, constitution, requirements, design,
		   tasks, artifacts, finalization, summaries, blocking_issues,
		   all_suggestions, persona_prompt_snapshot, required_personas, finalizer_action,
		   created_at, updated_at, execution)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		ws.ID, ws.TenantID, ws.ScopeID, ws.Status, ws.Mode, ws.Title, ws.Request,
		ws.ProviderName, ws.ModelName,
		p.constitution, p.requirements, p.design,
		p.tasks, p.artifacts, p.finalization, p.summaries, p.blockingIssues,
		p.allSuggestions, p.personaPromptSnapshot, p.requiredPersonas, ws.FinalizerAction,
		ws.CreatedAt.Unix(), ws.UpdatedAt.Unix(), p.execution,
	)
	return err
}

func (s *Store) GetWorkflow(ctx context.Context, id string) (*state.WorkflowState, error) {
	row := s.db.QueryRowContext(ctx, selectWorkflowSQL+" WHERE id=?", id)
	return scanWorkflow(row)
}

func (s *Store) SaveWorkflow(ctx context.Context, ws *state.WorkflowState) error {
	p, err := marshalWorkflow(ws)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO workflows
		  (id, tenant_id, scope_id, status, mode, title, request,
		   provider_name, model_name, error_message,
		   constitution, requirements, design, tasks, artifacts,
		   finalization, summaries, blocking_issues,
		   all_suggestions, persona_prompt_snapshot, required_personas, finalizer_action,
		   created_at, updated_at, started_at, completed_at, execution)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
		  status=excluded.status, mode=excluded.mode, title=excluded.title,
		  provider_name=excluded.provider_name, model_name=excluded.model_name,
		  error_message=excluded.error_message,
		  constitution=excluded.constitution, requirements=excluded.requirements,
		  design=excluded.design, tasks=excluded.tasks, artifacts=excluded.artifacts,
		  finalization=excluded.finalization, summaries=excluded.summaries,
		  blocking_issues=excluded.blocking_issues,
		  all_suggestions=excluded.all_suggestions,
		  persona_prompt_snapshot=excluded.persona_prompt_snapshot,
		  required_personas=excluded.required_personas,
		  finalizer_action=excluded.finalizer_action,
		  execution=excluded.execution,
		  updated_at=excluded.updated_at,
		  started_at=excluded.started_at, completed_at=excluded.completed_at`,
		ws.ID, ws.TenantID, ws.ScopeID, ws.Status, ws.Mode, ws.Title, ws.Request,
		ws.ProviderName, ws.ModelName, ws.ErrorMessage,
		p.constitution, p.requirements, p.design,
		p.tasks, p.artifacts, p.finalization, p.summaries, p.blockingIssues,
		p.allSuggestions, p.personaPromptSnapshot, p.requiredPersonas, ws.FinalizerAction,
		ws.CreatedAt.Unix(), ws.UpdatedAt.Unix(),
		nullableUnix(ws.StartedAt), nullableUnix(ws.CompletedAt), p.execution,
	)
	return err
}

func (s *Store) ListWorkflows(ctx context.Context, tenantID string, limit, offset int) ([]*state.WorkflowState, error) {
	rows, err := s.db.QueryContext(ctx,
		selectWorkflowSQL+" WHERE tenant_id=? ORDER BY created_at DESC LIMIT ? OFFSET ?",
		tenantID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*state.WorkflowState
	for rows.Next() {
		ws, err := scanWorkflow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ws)
	}
	return out, rows.Err()
}

func (s *Store) UpdateWorkflowStatus(ctx context.Context, id string, status state.WorkflowStatus, errMsg string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE workflows SET status=?, error_message=?, updated_at=? WHERE id=?`,
		status, errMsg, time.Now().UTC().Unix(), id)
	return err
}

func (s *Store) AppendEvents(ctx context.Context, evts ...*events.Event) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO workflow_events
		  (id, workflow_id, tenant_id, scope_id, type, persona, payload, occurred_at)
		VALUES (?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, e := range evts {
		payloadJSON, _ := json.Marshal(e.Payload)
		_, err := stmt.ExecContext(ctx,
			e.ID, e.WorkflowID, e.TenantID, e.ScopeID,
			e.Type, string(e.Persona), string(payloadJSON),
			e.OccurredAt.Unix())
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ─── EventStore ───────────────────────────────────────────────────────────────

func (s *Store) ListEvents(ctx context.Context, workflowID string) ([]*events.Event, error) {
	rows, err := s.db.QueryContext(ctx,
		selectEventSQL+" WHERE workflow_id=? ORDER BY occurred_at", workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

func (s *Store) ListEventsByType(ctx context.Context, workflowID string, evtType events.EventType) ([]*events.Event, error) {
	rows, err := s.db.QueryContext(ctx,
		selectEventSQL+" WHERE workflow_id=? AND type=? ORDER BY occurred_at",
		workflowID, evtType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

func (s *Store) EventsSince(ctx context.Context, tenantID string, after time.Time) ([]*events.Event, error) {
	rows, err := s.db.QueryContext(ctx,
		selectEventSQL+" WHERE tenant_id=? AND occurred_at > ? ORDER BY occurred_at",
		tenantID, after.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

// ─── TenantStore ──────────────────────────────────────────────────────────────

func (s *Store) CreateTenant(ctx context.Context, t *state.Tenant) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tenants (id, slug, name, created_at, updated_at) VALUES (?,?,?,?,?)`,
		t.ID, t.Slug, t.Name, t.CreatedAt.Unix(), t.UpdatedAt.Unix())
	return err
}

func (s *Store) GetTenant(ctx context.Context, id string) (*state.Tenant, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, slug, name, created_at, updated_at FROM tenants WHERE id=?`, id)
	return scanTenant(row)
}

func (s *Store) GetTenantBySlug(ctx context.Context, slug string) (*state.Tenant, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, slug, name, created_at, updated_at FROM tenants WHERE slug=?`, slug)
	return scanTenant(row)
}

func (s *Store) ListTenants(ctx context.Context) ([]*state.Tenant, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, slug, name, created_at, updated_at FROM tenants ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*state.Tenant
	for rows.Next() {
		t, err := scanTenant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) UpdateTenant(ctx context.Context, t *state.Tenant) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE tenants SET slug=?, name=?, updated_at=? WHERE id=?`,
		t.Slug, t.Name, t.UpdatedAt.Unix(), t.ID)
	return err
}

func (s *Store) DeleteTenant(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM tenants WHERE id=?`, id)
	return err
}

// ─── ScopeStore ───────────────────────────────────────────────────────────────

func (s *Store) CreateScope(ctx context.Context, sc *state.Scope) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO scopes (id, tenant_id, kind, name, slug, parent_scope_id, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?)`,
		sc.ID, sc.TenantID, sc.Kind, sc.Name, sc.Slug,
		nullableStringVal(sc.ParentScopeID), sc.CreatedAt.Unix(), sc.UpdatedAt.Unix())
	return err
}

func (s *Store) GetScope(ctx context.Context, id string) (*state.Scope, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, kind, name, slug, COALESCE(parent_scope_id,''), created_at, updated_at
		 FROM scopes WHERE id=?`, id)
	return scanScope(row)
}

func (s *Store) ListScopes(ctx context.Context, tenantID string) ([]*state.Scope, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, kind, name, slug, COALESCE(parent_scope_id,''), created_at, updated_at
		 FROM scopes WHERE tenant_id=? ORDER BY created_at`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*state.Scope
	for rows.Next() {
		sc, err := scanScope(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

func (s *Store) UpdateScope(ctx context.Context, sc *state.Scope) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE scopes SET name=?, slug=?, updated_at=? WHERE id=?`,
		sc.Name, sc.Slug, sc.UpdatedAt.Unix(), sc.ID)
	return err
}

func (s *Store) DeleteScope(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM scopes WHERE id=?`, id)
	return err
}

// sqliteAlterV004 adds the workflow-planning columns to existing databases.
// Each statement is run individually so that a "duplicate column name" error
// for an already-present column can be silently ignored.
var sqliteAlterV004 = []string{
	`ALTER TABLE workflows ADD COLUMN all_suggestions       TEXT NOT NULL DEFAULT '[]'`,
	`ALTER TABLE workflows ADD COLUMN persona_prompt_snapshot TEXT NOT NULL DEFAULT '{}'`,
	`ALTER TABLE workflows ADD COLUMN required_personas     TEXT NOT NULL DEFAULT '[]'`,
	`ALTER TABLE workflows ADD COLUMN finalizer_action      TEXT NOT NULL DEFAULT ''`,
}

// sqliteAlterV005 adds the execution-progress column to existing databases.
var sqliteAlterV005 = []string{
	`ALTER TABLE workflows ADD COLUMN execution TEXT NOT NULL DEFAULT '{}'`,
}

// ─── SQL constants ────────────────────────────────────────────────────────────

const selectWorkflowSQL = `
	SELECT id, tenant_id, scope_id, status, mode, title, request,
	       provider_name, model_name, error_message,
	       constitution, requirements, design, tasks, artifacts,
	       finalization, summaries, blocking_issues,
	       all_suggestions, persona_prompt_snapshot, required_personas, finalizer_action,
	       created_at, updated_at, started_at, completed_at, execution
	FROM workflows`

const selectEventSQL = `
	SELECT id, workflow_id, tenant_id, scope_id, type, persona, payload, occurred_at
	FROM workflow_events`

// ─── DDL ─────────────────────────────────────────────────────────────────────

const sqliteDDL = `
CREATE TABLE IF NOT EXISTS tenants (
    id         TEXT NOT NULL PRIMARY KEY,
    slug       TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS scopes (
    id              TEXT NOT NULL PRIMARY KEY,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    kind            TEXT NOT NULL CHECK (kind IN ('global','org','team')),
    name            TEXT NOT NULL,
    slug            TEXT NOT NULL,
    parent_scope_id TEXT REFERENCES scopes(id) ON DELETE RESTRICT,
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    UNIQUE(tenant_id, slug)
);

CREATE TABLE IF NOT EXISTS workflows (
    id                    TEXT    NOT NULL PRIMARY KEY,
    tenant_id             TEXT    NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    scope_id              TEXT    NOT NULL REFERENCES scopes(id),
    status                TEXT    NOT NULL DEFAULT 'pending',
    mode                  TEXT    NOT NULL DEFAULT 'software',
    title                 TEXT    NOT NULL DEFAULT '',
    request               TEXT    NOT NULL,
    provider_name         TEXT,
    model_name            TEXT,
    error_message         TEXT,
    constitution          TEXT,
    requirements          TEXT,
    design                TEXT,
    tasks                 TEXT    NOT NULL DEFAULT '[]',
    artifacts             TEXT    NOT NULL DEFAULT '[]',
    finalization          TEXT,
    summaries             TEXT    NOT NULL DEFAULT '{}',
    blocking_issues       TEXT    NOT NULL DEFAULT '[]',
    all_suggestions       TEXT    NOT NULL DEFAULT '[]',
    persona_prompt_snapshot TEXT  NOT NULL DEFAULT '{}',
    required_personas     TEXT    NOT NULL DEFAULT '[]',
    finalizer_action      TEXT    NOT NULL DEFAULT '',
    execution             TEXT    NOT NULL DEFAULT '{}',
    created_at            INTEGER NOT NULL,
    updated_at            INTEGER NOT NULL,
    started_at            INTEGER,
    completed_at          INTEGER
);

CREATE TABLE IF NOT EXISTS workflow_events (
    id          TEXT    NOT NULL PRIMARY KEY,
    workflow_id TEXT    NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    tenant_id   TEXT    NOT NULL,
    scope_id    TEXT    NOT NULL,
    type        TEXT    NOT NULL,
    persona     TEXT,
    payload     TEXT,
    occurred_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_workflow ON workflow_events(workflow_id, occurred_at);
CREATE INDEX IF NOT EXISTS idx_events_tenant   ON workflow_events(tenant_id, occurred_at);

CREATE TABLE IF NOT EXISTS scope_settings (
    id         TEXT    NOT NULL PRIMARY KEY,
    scope_id   TEXT    NOT NULL REFERENCES scopes(id) ON DELETE CASCADE,
    tenant_id  TEXT    NOT NULL,
    key        TEXT    NOT NULL,
    value      TEXT    NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(scope_id, key)
);

CREATE TABLE IF NOT EXISTS scope_component_sources (
    id              TEXT    NOT NULL PRIMARY KEY,
    scope_id        TEXT    NOT NULL REFERENCES scopes(id) ON DELETE CASCADE,
    tenant_id       TEXT    NOT NULL,
    name            TEXT    NOT NULL,
    source_type     TEXT    NOT NULL,
    root            TEXT    NOT NULL,
    precedence      INTEGER NOT NULL DEFAULT 50,
    enabled_types   TEXT    NOT NULL DEFAULT '[]',
    refresh_seconds INTEGER NOT NULL DEFAULT 300,
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    UNIQUE(scope_id, name)
);

CREATE TABLE IF NOT EXISTS scope_provider_policies (
    id                TEXT    NOT NULL PRIMARY KEY,
    scope_id          TEXT    NOT NULL REFERENCES scopes(id) ON DELETE CASCADE,
    tenant_id         TEXT    NOT NULL,
    allowed_providers TEXT    NOT NULL DEFAULT '[]',
    default_provider  TEXT,
    default_model     TEXT,
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL,
    UNIQUE(scope_id)
);

CREATE TABLE IF NOT EXISTS scope_tool_policies (
    id            TEXT    NOT NULL PRIMARY KEY,
    scope_id      TEXT    NOT NULL REFERENCES scopes(id) ON DELETE CASCADE,
    tenant_id     TEXT    NOT NULL,
    allowed_tools TEXT    NOT NULL DEFAULT '[]',
    denied_tools  TEXT    NOT NULL DEFAULT '[]',
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    UNIQUE(scope_id)
);

CREATE TABLE IF NOT EXISTS scope_finalizer_policies (
    id               TEXT    NOT NULL PRIMARY KEY,
    scope_id         TEXT    NOT NULL REFERENCES scopes(id) ON DELETE CASCADE,
    tenant_id        TEXT    NOT NULL,
    allowed_actions  TEXT    NOT NULL DEFAULT '[]',
    default_action   TEXT,
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL,
    UNIQUE(scope_id)
);
`

// ─── Scan helpers ─────────────────────────────────────────────────────────────

type scanner interface {
	Scan(dest ...any) error
}

func scanWorkflow(row scanner) (*state.WorkflowState, error) {
	ws := &state.WorkflowState{}
	var (
		constitution, requirements, design sql.NullString
		tasks, artifacts, finalization     sql.NullString
		summaries, blockingIssues          sql.NullString
		allSuggestions                     sql.NullString
		personaPromptSnapshot              sql.NullString
		requiredPersonas                   sql.NullString
		execution                          sql.NullString
		finalizerAction                    sql.NullString
		providerName, modelName            sql.NullString
		errorMessage                       sql.NullString
		createdAt, updatedAt               int64
		startedAt, completedAt             sql.NullInt64
	)

	err := row.Scan(
		&ws.ID, &ws.TenantID, &ws.ScopeID, &ws.Status, &ws.Mode, &ws.Title, &ws.Request,
		&providerName, &modelName, &errorMessage,
		&constitution, &requirements, &design, &tasks, &artifacts,
		&finalization, &summaries, &blockingIssues,
		&allSuggestions, &personaPromptSnapshot, &requiredPersonas, &finalizerAction,
		&createdAt, &updatedAt, &startedAt, &completedAt, &execution,
	)
	if err != nil {
		return nil, err
	}

	ws.ProviderName = providerName.String
	ws.ModelName = modelName.String
	ws.ErrorMessage = errorMessage.String
	ws.FinalizerAction = finalizerAction.String

	ws.CreatedAt = time.Unix(createdAt, 0).UTC()
	ws.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	if startedAt.Valid {
		t := time.Unix(startedAt.Int64, 0).UTC()
		ws.StartedAt = &t
	}
	if completedAt.Valid {
		t := time.Unix(completedAt.Int64, 0).UTC()
		ws.CompletedAt = &t
	}

	unmarshal := func(ns sql.NullString, v interface{}) {
		if ns.Valid && ns.String != "" && ns.String != "null" {
			_ = json.Unmarshal([]byte(ns.String), v)
		}
	}

	unmarshal(constitution, &ws.Constitution)
	unmarshal(requirements, &ws.Requirements)
	unmarshal(design, &ws.Design)
	unmarshal(tasks, &ws.Tasks)
	unmarshal(artifacts, &ws.Artifacts)
	unmarshal(finalization, &ws.Finalization)
	unmarshal(summaries, &ws.Summaries)
	unmarshal(blockingIssues, &ws.BlockingIssues)
	unmarshal(allSuggestions, &ws.AllSuggestions)
	unmarshal(personaPromptSnapshot, &ws.PersonaPromptSnapshot)
	unmarshal(requiredPersonas, &ws.RequiredPersonas)
	unmarshal(execution, &ws.Execution)

	return ws, nil
}

type rowScanner interface {
	Scan(dest ...any) error
	Next() bool
	Err() error
}

func scanEvents(rows rowScanner) ([]*events.Event, error) {
	var out []*events.Event
	for rows.Next() {
		e := &events.Event{}
		var payload sql.NullString
		var occurredAt int64
		if err := rows.Scan(
			&e.ID, &e.WorkflowID, &e.TenantID, &e.ScopeID,
			&e.Type, &e.Persona, &payload, &occurredAt,
		); err != nil {
			return nil, err
		}
		e.OccurredAt = time.Unix(occurredAt, 0).UTC()
		if payload.Valid {
			e.Payload = json.RawMessage(payload.String)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func scanTenant(row scanner) (*state.Tenant, error) {
	t := &state.Tenant{}
	var createdAt, updatedAt int64
	if err := row.Scan(&t.ID, &t.Slug, &t.Name, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	t.CreatedAt = time.Unix(createdAt, 0).UTC()
	t.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	return t, nil
}

func scanScope(row scanner) (*state.Scope, error) {
	sc := &state.Scope{}
	var createdAt, updatedAt int64
	if err := row.Scan(&sc.ID, &sc.TenantID, &sc.Kind, &sc.Name, &sc.Slug,
		&sc.ParentScopeID, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	sc.CreatedAt = time.Unix(createdAt, 0).UTC()
	sc.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	return sc, nil
}

// ─── Marshal helpers ──────────────────────────────────────────────────────────

type workflowPayload struct {
	constitution, requirements, design []byte
	tasks, artifacts, finalization     []byte
	summaries, blockingIssues          []byte
	allSuggestions                     []byte
	personaPromptSnapshot              []byte
	requiredPersonas                   []byte
	execution                          []byte
}

func marshalWorkflow(ws *state.WorkflowState) (workflowPayload, error) {
	marshal := func(v interface{}) ([]byte, error) {
		if v == nil {
			return []byte("null"), nil
		}
		return json.Marshal(v)
	}
	var p workflowPayload
	var err error
	if p.constitution, err = marshal(ws.Constitution); err != nil {
		return p, err
	}
	if p.requirements, err = marshal(ws.Requirements); err != nil {
		return p, err
	}
	if p.design, err = marshal(ws.Design); err != nil {
		return p, err
	}
	if p.tasks, err = json.Marshal(ws.Tasks); err != nil {
		return p, err
	}
	if p.artifacts, err = json.Marshal(ws.Artifacts); err != nil {
		return p, err
	}
	if p.finalization, err = marshal(ws.Finalization); err != nil {
		return p, err
	}
	if p.summaries, err = json.Marshal(ws.Summaries); err != nil {
		return p, err
	}
	if p.blockingIssues, err = json.Marshal(ws.BlockingIssues); err != nil {
		return p, err
	}
	if p.allSuggestions, err = json.Marshal(ws.AllSuggestions); err != nil {
		return p, err
	}
	if p.personaPromptSnapshot, err = json.Marshal(ws.PersonaPromptSnapshot); err != nil {
		return p, err
	}
	if p.requiredPersonas, err = json.Marshal(ws.RequiredPersonas); err != nil {
		return p, err
	}
	if p.execution, err = json.Marshal(ws.Execution); err != nil {
		return p, err
	}
	return p, nil
}

func nullableUnix(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.Unix()
}

func nullableStringVal(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
