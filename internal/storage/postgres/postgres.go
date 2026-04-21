// Package postgres provides a Postgres-backed implementation of storage.Store
// using pgx/v5.
package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // pgx/v5 driver
	_ "github.com/golang-migrate/migrate/v4/source/file"     // file:// source
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/go-orca/go-orca/internal/events"
	"github.com/go-orca/go-orca/internal/state"
)

// Store implements storage.Store against a Postgres database.
type Store struct {
	pool *pgxpool.Pool
	dsn  string
}

// New opens a connection pool to the given DSN and returns a Store.
func New(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: connect: %w", err)
	}
	return &Store{pool: pool, dsn: dsn}, nil
}

// Migrate runs all pending up-migrations from the given migrations directory.
// migrationsPath should be an absolute or relative path to the directory
// containing *.up.sql / *.down.sql files (e.g. "internal/storage/migrations").
func (s *Store) Migrate(migrationsPath string) error {
	// golang-migrate needs a pgx5:// DSN for the pgx/v5 driver.
	dbURL := "pgx5://" + stripScheme(s.dsn)
	m, err := migrate.New("file://"+migrationsPath, dbURL)
	if err != nil {
		return fmt.Errorf("postgres: migrate init: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("postgres: migrate up: %w", err)
	}
	return nil
}

// stripScheme removes a leading "postgres://" or "postgresql://" from the DSN
// so we can rewrite it as "pgx5://".
func stripScheme(dsn string) string {
	for _, prefix := range []string{"postgresql://", "postgres://"} {
		if len(dsn) > len(prefix) && dsn[:len(prefix)] == prefix {
			return dsn[len(prefix):]
		}
	}
	return dsn
}

// Ping verifies the database connection.
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// Close releases pool resources.
func (s *Store) Close() error {
	s.pool.Close()
	return nil
}

// ─── WorkflowStore ────────────────────────────────────────────────────────────

func (s *Store) CreateWorkflow(ctx context.Context, ws *state.WorkflowState) error {
	payload, err := marshalWorkflow(ws)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO workflows
		  (id, tenant_id, scope_id, status, mode, title, request,
		   provider_name, model_name, constitution, requirements, design,
		   tasks, artifacts, finalization, summaries, blocking_issues,
		   all_suggestions, persona_prompt_snapshot, required_personas, finalizer_action,
		   delivery_action, delivery_config,
		   upload_session_id, input_documents, input_document_corpus_summary, attachment_processing,
		   created_at, updated_at, execution, persona_models, provider_catalogs)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32)`,
		ws.ID, ws.TenantID, ws.ScopeID, ws.Status, ws.Mode, ws.Title, ws.Request,
		ws.ProviderName, ws.ModelName,
		payload.constitution, payload.requirements, payload.design,
		payload.tasks, payload.artifacts, payload.finalization,
		payload.summaries, payload.blockingIssues,
		payload.allSuggestions, payload.personaPromptSnapshot, payload.requiredPersonas, ws.FinalizerAction,
		ws.DeliveryAction, payload.deliveryConfig,
		ws.UploadSessionID, payload.inputDocuments, ws.InputDocumentCorpusSummary, payload.attachmentProcessing,
		ws.CreatedAt, ws.UpdatedAt, payload.execution,
		payload.personaModels, payload.providerCatalogs,
	)
	return err
}

func (s *Store) GetWorkflow(ctx context.Context, id string) (*state.WorkflowState, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, scope_id, status, mode, title, request,
		       provider_name, model_name, error_message,
		       constitution, requirements, design, tasks, artifacts,
		       finalization, summaries, blocking_issues,
		       all_suggestions, persona_prompt_snapshot, required_personas, finalizer_action,
		       delivery_action, delivery_config,
		       created_at, updated_at, started_at, completed_at, execution,
		       persona_models, provider_catalogs,
		       upload_session_id, input_documents, input_document_corpus_summary, attachment_processing
		FROM workflows WHERE id = $1`, id)

	return scanWorkflow(row)
}

func (s *Store) SaveWorkflow(ctx context.Context, ws *state.WorkflowState) error {
	payload, err := marshalWorkflow(ws)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO workflows
		  (id, tenant_id, scope_id, status, mode, title, request,
		   provider_name, model_name, error_message,
		   constitution, requirements, design, tasks, artifacts,
		   finalization, summaries, blocking_issues,
		   all_suggestions, persona_prompt_snapshot, required_personas, finalizer_action,
		   delivery_action, delivery_config,
		   upload_session_id, input_documents, input_document_corpus_summary, attachment_processing,
		   created_at, updated_at, started_at, completed_at, execution,
		   persona_models, provider_catalogs)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35)
		ON CONFLICT (id) DO UPDATE SET
		  status=$4, mode=$5, title=$6,
		  provider_name=$8, model_name=$9, error_message=$10,
		  constitution=$11, requirements=$12, design=$13,
		  tasks=$14, artifacts=$15, finalization=$16,
		  summaries=$17, blocking_issues=$18,
		  all_suggestions=$19, persona_prompt_snapshot=$20,
		  required_personas=$21, finalizer_action=$22,
		  delivery_action=$23, delivery_config=$24,
		  upload_session_id=$25, input_documents=$26,
		  input_document_corpus_summary=$27, attachment_processing=$28,
		  updated_at=$30, started_at=$31, completed_at=$32, execution=$33,
		  persona_models=$34, provider_catalogs=$35`,
		ws.ID, ws.TenantID, ws.ScopeID, ws.Status, ws.Mode, ws.Title, ws.Request,
		ws.ProviderName, ws.ModelName, ws.ErrorMessage,
		payload.constitution, payload.requirements, payload.design,
		payload.tasks, payload.artifacts, payload.finalization,
		payload.summaries, payload.blockingIssues,
		payload.allSuggestions, payload.personaPromptSnapshot, payload.requiredPersonas, ws.FinalizerAction,
		ws.DeliveryAction, payload.deliveryConfig,
		ws.UploadSessionID, payload.inputDocuments, ws.InputDocumentCorpusSummary, payload.attachmentProcessing,
		ws.CreatedAt, ws.UpdatedAt, ws.StartedAt, ws.CompletedAt, payload.execution,
		payload.personaModels, payload.providerCatalogs,
	)
	return err
}

func (s *Store) ListWorkflows(ctx context.Context, tenantID string, limit, offset int) ([]*state.WorkflowState, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, scope_id, status, mode, title, request,
		       provider_name, model_name, error_message,
		       constitution, requirements, design, tasks, artifacts,
		       finalization, summaries, blocking_issues,
		       all_suggestions, persona_prompt_snapshot, required_personas, finalizer_action,
		       delivery_action, delivery_config,
		       created_at, updated_at, started_at, completed_at, execution,
		       persona_models, provider_catalogs,
		       upload_session_id, input_documents, input_document_corpus_summary, attachment_processing
		FROM workflows
		WHERE tenant_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`, tenantID, limit, offset)
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
	_, err := s.pool.Exec(ctx,
		`UPDATE workflows SET status=$2, error_message=$3, updated_at=$4 WHERE id=$1`,
		id, status, errMsg, time.Now().UTC())
	return err
}

func (s *Store) AppendEvents(ctx context.Context, evts ...*events.Event) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	for _, e := range evts {
		_, err := tx.Exec(ctx, `
			INSERT INTO workflow_events (id, workflow_id, tenant_id, scope_id, type, persona, payload, occurred_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			e.ID, e.WorkflowID, e.TenantID, e.ScopeID, e.Type, e.Persona, e.Payload, e.OccurredAt,
		)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ─── EventStore ───────────────────────────────────────────────────────────────

func (s *Store) ListEvents(ctx context.Context, workflowID string) ([]*events.Event, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, workflow_id, tenant_id, scope_id, type, persona, payload, occurred_at
		FROM workflow_events WHERE workflow_id=$1 ORDER BY occurred_at`, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

func (s *Store) ListEventsByType(ctx context.Context, workflowID string, evtType events.EventType) ([]*events.Event, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, workflow_id, tenant_id, scope_id, type, persona, payload, occurred_at
		FROM workflow_events WHERE workflow_id=$1 AND type=$2 ORDER BY occurred_at`,
		workflowID, evtType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

func (s *Store) EventsSince(ctx context.Context, tenantID string, after time.Time) ([]*events.Event, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, workflow_id, tenant_id, scope_id, type, persona, payload, occurred_at
		FROM workflow_events WHERE tenant_id=$1 AND occurred_at > $2 ORDER BY occurred_at`,
		tenantID, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

// ─── TenantStore ──────────────────────────────────────────────────────────────

func (s *Store) CreateTenant(ctx context.Context, t *state.Tenant) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO tenants (id, slug, name, created_at, updated_at) VALUES ($1,$2,$3,$4,$5)`,
		t.ID, t.Slug, t.Name, t.CreatedAt, t.UpdatedAt)
	return err
}

func (s *Store) GetTenant(ctx context.Context, id string) (*state.Tenant, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, slug, name, created_at, updated_at FROM tenants WHERE id=$1`, id)
	t := &state.Tenant{}
	if err := row.Scan(&t.ID, &t.Slug, &t.Name, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Store) GetTenantBySlug(ctx context.Context, slug string) (*state.Tenant, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, slug, name, created_at, updated_at FROM tenants WHERE slug=$1`, slug)
	t := &state.Tenant{}
	if err := row.Scan(&t.ID, &t.Slug, &t.Name, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Store) ListTenants(ctx context.Context) ([]*state.Tenant, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, slug, name, created_at, updated_at FROM tenants ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*state.Tenant
	for rows.Next() {
		t := &state.Tenant{}
		if err := rows.Scan(&t.ID, &t.Slug, &t.Name, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) UpdateTenant(ctx context.Context, t *state.Tenant) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE tenants SET slug=$2, name=$3, updated_at=$4 WHERE id=$1`,
		t.ID, t.Slug, t.Name, t.UpdatedAt)
	return err
}

func (s *Store) DeleteTenant(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM tenants WHERE id=$1`, id)
	return err
}

// ─── ScopeStore ───────────────────────────────────────────────────────────────

func (s *Store) CreateScope(ctx context.Context, sc *state.Scope) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO scopes (id, tenant_id, kind, name, slug, parent_scope_id, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		sc.ID, sc.TenantID, sc.Kind, sc.Name, sc.Slug,
		nullableString(sc.ParentScopeID), sc.CreatedAt, sc.UpdatedAt)
	return err
}

func (s *Store) GetScope(ctx context.Context, id string) (*state.Scope, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, kind, name, slug, COALESCE(parent_scope_id,''), created_at, updated_at
		 FROM scopes WHERE id=$1`, id)
	sc := &state.Scope{}
	if err := row.Scan(&sc.ID, &sc.TenantID, &sc.Kind, &sc.Name, &sc.Slug,
		&sc.ParentScopeID, &sc.CreatedAt, &sc.UpdatedAt); err != nil {
		return nil, err
	}
	return sc, nil
}

func (s *Store) ListScopes(ctx context.Context, tenantID string) ([]*state.Scope, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, kind, name, slug, COALESCE(parent_scope_id,''), created_at, updated_at
		 FROM scopes WHERE tenant_id=$1 ORDER BY created_at`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*state.Scope
	for rows.Next() {
		sc := &state.Scope{}
		if err := rows.Scan(&sc.ID, &sc.TenantID, &sc.Kind, &sc.Name, &sc.Slug,
			&sc.ParentScopeID, &sc.CreatedAt, &sc.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

func (s *Store) UpdateScope(ctx context.Context, sc *state.Scope) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE scopes SET name=$2, slug=$3, updated_at=$4 WHERE id=$1`,
		sc.ID, sc.Name, sc.Slug, sc.UpdatedAt)
	return err
}

func (s *Store) DeleteScope(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM scopes WHERE id=$1`, id)
	return err
}

// ─── Scan / marshal helpers ───────────────────────────────────────────────────

// workflowPayload holds marshaled JSONB fields.
type workflowPayload struct {
	constitution          []byte
	requirements          []byte
	design                []byte
	tasks                 []byte
	artifacts             []byte
	finalization          []byte
	summaries             []byte
	blockingIssues        []byte
	allSuggestions        []byte
	personaPromptSnapshot []byte
	requiredPersonas      []byte
	deliveryConfig        []byte
	execution             []byte
	personaModels         []byte
	providerCatalogs      []byte
	inputDocuments        []byte
	attachmentProcessing  []byte
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
	if len(ws.DeliveryConfig) > 0 {
		p.deliveryConfig = ws.DeliveryConfig
	} else {
		p.deliveryConfig = []byte("null")
	}
	if p.execution, err = json.Marshal(ws.Execution); err != nil {
		return p, err
	}
	if p.personaModels, err = json.Marshal(ws.PersonaModels); err != nil {
		return p, err
	}
	if p.providerCatalogs, err = json.Marshal(ws.ProviderCatalogs); err != nil {
		return p, err
	}
	if p.inputDocuments, err = json.Marshal(ws.InputDocuments); err != nil {
		return p, err
	}
	if p.attachmentProcessing, err = marshal(ws.AttachmentProcessing); err != nil {
		return p, err
	}
	return p, nil
}

// scanner abstracts pgx Row and Rows so we can reuse scanWorkflow.
type scanner interface {
	Scan(dest ...any) error
}

func scanWorkflow(row scanner) (*state.WorkflowState, error) {
	ws := &state.WorkflowState{}
	var (
		providerName          *string
		modelName             *string
		errorMessage          *string
		constitution          []byte
		requirements          []byte
		design                []byte
		tasks                 []byte
		artifacts             []byte
		finalization          []byte
		summaries             []byte
		blockingIssues        []byte
		allSuggestions        []byte
		personaPromptSnapshot []byte
		requiredPersonas      []byte
		deliveryConfig        []byte
		execution             []byte
		personaModels         []byte
		providerCatalogs      []byte
		inputDocuments        []byte
		inputDocCorpusSummary *string
		attachmentProcessing  []byte
	)

	err := row.Scan(
		&ws.ID, &ws.TenantID, &ws.ScopeID, &ws.Status, &ws.Mode, &ws.Title, &ws.Request,
		&providerName, &modelName, &errorMessage,
		&constitution, &requirements, &design, &tasks, &artifacts,
		&finalization, &summaries, &blockingIssues,
		&allSuggestions, &personaPromptSnapshot, &requiredPersonas, &ws.FinalizerAction,
		&ws.DeliveryAction, &deliveryConfig,
		&ws.CreatedAt, &ws.UpdatedAt, &ws.StartedAt, &ws.CompletedAt, &execution,
		&personaModels, &providerCatalogs,
		&ws.UploadSessionID, &inputDocuments, &inputDocCorpusSummary, &attachmentProcessing,
	)
	if err != nil {
		return nil, err
	}

	if providerName != nil {
		ws.ProviderName = *providerName
	}
	if modelName != nil {
		ws.ModelName = *modelName
	}
	if errorMessage != nil {
		ws.ErrorMessage = *errorMessage
	}

	unmarshal := func(data []byte, v interface{}) error {
		if len(data) == 0 || string(data) == "null" {
			return nil
		}
		return json.Unmarshal(data, v)
	}

	_ = unmarshal(constitution, &ws.Constitution)
	_ = unmarshal(requirements, &ws.Requirements)
	_ = unmarshal(design, &ws.Design)
	_ = unmarshal(tasks, &ws.Tasks)
	_ = unmarshal(artifacts, &ws.Artifacts)
	_ = unmarshal(finalization, &ws.Finalization)
	_ = unmarshal(summaries, &ws.Summaries)
	_ = unmarshal(blockingIssues, &ws.BlockingIssues)
	_ = unmarshal(allSuggestions, &ws.AllSuggestions)
	_ = unmarshal(personaPromptSnapshot, &ws.PersonaPromptSnapshot)
	_ = unmarshal(requiredPersonas, &ws.RequiredPersonas)
	if len(deliveryConfig) > 0 && string(deliveryConfig) != "null" {
		ws.DeliveryConfig = json.RawMessage(deliveryConfig)
	}
	_ = unmarshal(execution, &ws.Execution)
	_ = unmarshal(personaModels, &ws.PersonaModels)
	_ = unmarshal(providerCatalogs, &ws.ProviderCatalogs)
	_ = unmarshal(inputDocuments, &ws.InputDocuments)
	_ = unmarshal(attachmentProcessing, &ws.AttachmentProcessing)
	if inputDocCorpusSummary != nil {
		ws.InputDocumentCorpusSummary = *inputDocCorpusSummary
	}

	return ws, nil
}

func scanEvents(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]*events.Event, error) {
	var out []*events.Event
	for rows.Next() {
		e := &events.Event{}
		if err := rows.Scan(
			&e.ID, &e.WorkflowID, &e.TenantID, &e.ScopeID,
			&e.Type, &e.Persona, &e.Payload, &e.OccurredAt,
		); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// ─── AttachmentStore ──────────────────────────────────────────────────────────

func (s *Store) CreateUploadSession(ctx context.Context, sess *state.UploadSession) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO upload_sessions (id, tenant_id, scope_id, status, workflow_id, expires_at, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		sess.ID, sess.TenantID, sess.ScopeID, sess.Status,
		nullableString(sess.WorkflowID), sess.ExpiresAt, sess.CreatedAt, sess.UpdatedAt)
	return err
}

func (s *Store) GetUploadSession(ctx context.Context, id string) (*state.UploadSession, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, scope_id, status, COALESCE(workflow_id,''), expires_at, created_at, updated_at
		FROM upload_sessions WHERE id=$1`, id)
	sess := &state.UploadSession{}
	if err := row.Scan(&sess.ID, &sess.TenantID, &sess.ScopeID, &sess.Status,
		&sess.WorkflowID, &sess.ExpiresAt, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
		return nil, err
	}
	return sess, nil
}

func (s *Store) ConsumeUploadSession(ctx context.Context, sessionID, workflowID, tenantID string) error {
	now := time.Now().UTC()
	res, err := s.pool.Exec(ctx, `
		UPDATE upload_sessions SET status='consumed', workflow_id=$1, updated_at=$2
		WHERE id=$3 AND tenant_id=$4 AND status='open'`,
		workflowID, now, sessionID, tenantID)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return fmt.Errorf("upload session %s not found, not open, or belongs to a different tenant", sessionID)
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE attachments SET workflow_id=$1, updated_at=$2 WHERE upload_session_id=$3`,
		workflowID, now, sessionID)
	return err
}

func (s *Store) AbortUploadSession(ctx context.Context, sessionID, tenantID string) error {
	now := time.Now().UTC()
	res, err := s.pool.Exec(ctx, `
		UPDATE upload_sessions SET status='aborted', updated_at=$1
		WHERE id=$2 AND tenant_id=$3 AND status='open'`,
		now, sessionID, tenantID)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return fmt.Errorf("upload session %s not found, not open, or belongs to a different tenant", sessionID)
	}
	return nil
}

func (s *Store) CreateAttachment(ctx context.Context, att *state.Attachment) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO attachments
		  (id, upload_session_id, workflow_id, tenant_id, scope_id,
		   filename, relative_path, content_type, size_bytes, storage_path,
		   status, summary, chunk_count, error_message, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		att.ID, att.UploadSessionID, nullableString(att.WorkflowID),
		att.TenantID, att.ScopeID,
		att.Filename, att.RelativePath, att.ContentType, att.SizeBytes, att.StoragePath,
		att.Status, att.Summary, att.ChunkCount, att.ErrorMessage,
		att.CreatedAt, att.UpdatedAt)
	return err
}

func (s *Store) GetAttachment(ctx context.Context, id string) (*state.Attachment, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, upload_session_id, COALESCE(workflow_id,''), tenant_id, scope_id,
		       filename, relative_path, content_type, size_bytes, storage_path,
		       status, summary, chunk_count, error_message, created_at, updated_at
		FROM attachments WHERE id=$1`, id)
	att := &state.Attachment{}
	if err := row.Scan(
		&att.ID, &att.UploadSessionID, &att.WorkflowID, &att.TenantID, &att.ScopeID,
		&att.Filename, &att.RelativePath, &att.ContentType, &att.SizeBytes, &att.StoragePath,
		&att.Status, &att.Summary, &att.ChunkCount, &att.ErrorMessage,
		&att.CreatedAt, &att.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return att, nil
}

func (s *Store) ListAttachmentsBySession(ctx context.Context, sessionID string) ([]*state.Attachment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, upload_session_id, COALESCE(workflow_id,''), tenant_id, scope_id,
		       filename, relative_path, content_type, size_bytes, storage_path,
		       status, summary, chunk_count, error_message, created_at, updated_at
		FROM attachments WHERE upload_session_id=$1 ORDER BY created_at`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPGAttachments(rows)
}

func (s *Store) ListAttachmentsByWorkflow(ctx context.Context, workflowID string) ([]*state.Attachment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, upload_session_id, COALESCE(workflow_id,''), tenant_id, scope_id,
		       filename, relative_path, content_type, size_bytes, storage_path,
		       status, summary, chunk_count, error_message, created_at, updated_at
		FROM attachments WHERE workflow_id=$1 ORDER BY created_at`, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPGAttachments(rows)
}

func (s *Store) UpdateAttachmentStatus(ctx context.Context, id string, status state.AttachmentStatus, summary string, chunkCount int, errMsg string) error {
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		UPDATE attachments SET status=$2, summary=$3, chunk_count=$4, error_message=$5, updated_at=$6
		WHERE id=$1`,
		id, status, summary, chunkCount, errMsg, now)
	return err
}

func (s *Store) CreateAttachmentChunks(ctx context.Context, chunks []state.AttachmentChunk) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	for _, ch := range chunks {
		_, err := tx.Exec(ctx, `
			INSERT INTO attachment_chunks (id, attachment_id, workflow_id, chunk_index, content)
			VALUES ($1,$2,$3,$4,$5)`,
			ch.ID, ch.AttachmentID, ch.WorkflowID, ch.Index, ch.Content)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) GetAttachmentChunk(ctx context.Context, attachmentID string, index int) (*state.AttachmentChunk, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, attachment_id, workflow_id, chunk_index, content
		FROM attachment_chunks WHERE attachment_id=$1 AND chunk_index=$2`, attachmentID, index)
	ch := &state.AttachmentChunk{}
	if err := row.Scan(&ch.ID, &ch.AttachmentID, &ch.WorkflowID, &ch.Index, &ch.Content); err != nil {
		return nil, err
	}
	return ch, nil
}

func (s *Store) ListAttachmentChunks(ctx context.Context, attachmentID string) ([]state.AttachmentChunk, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, attachment_id, workflow_id, chunk_index, content
		FROM attachment_chunks WHERE attachment_id=$1 ORDER BY chunk_index`, attachmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []state.AttachmentChunk
	for rows.Next() {
		var ch state.AttachmentChunk
		if err := rows.Scan(&ch.ID, &ch.AttachmentID, &ch.WorkflowID, &ch.Index, &ch.Content); err != nil {
			return nil, err
		}
		out = append(out, ch)
	}
	return out, rows.Err()
}

func scanPGAttachments(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]*state.Attachment, error) {
	var out []*state.Attachment
	for rows.Next() {
		att := &state.Attachment{}
		if err := rows.Scan(
			&att.ID, &att.UploadSessionID, &att.WorkflowID, &att.TenantID, &att.ScopeID,
			&att.Filename, &att.RelativePath, &att.ContentType, &att.SizeBytes, &att.StoragePath,
			&att.Status, &att.Summary, &att.ChunkCount, &att.ErrorMessage,
			&att.CreatedAt, &att.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, att)
	}
	return out, rows.Err()
}
