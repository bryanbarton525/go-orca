export type WorkflowStatus =
  | "pending"
  | "running"
  | "paused"
  | "completed"
  | "failed"
  | "cancelled"
  | (string & {});

export type WorkflowMode =
  | "software"
  | "content"
  | "docs"
  | "research"
  | "ops"
  | "mixed"
  | (string & {});

export type ScopeKind = "global" | "org" | "team" | (string & {});

export interface Constitution {
  vision?: string;
  goals?: string[];
  constraints?: string[];
  audience?: string;
  output_medium?: string;
  acceptance_criteria?: string[];
  out_of_scope?: string[];
}

export interface Requirement {
  id?: string;
  title?: string;
  description?: string;
  priority?: string;
  source?: string;
}

export interface Requirements {
  functional?: Requirement[];
  non_functional?: Requirement[];
  dependencies?: string[];
}

export interface DesignComponent {
  name?: string;
  description?: string;
  inputs?: string[];
  outputs?: string[];
}

export interface DesignDecision {
  decision?: string;
  rationale?: string;
  tradeoffs?: string;
}

export interface Design {
  overview?: string;
  components?: DesignComponent[];
  decisions?: DesignDecision[];
  diagrams?: string[];
  tech_stack?: string[];
  delivery_target?: string;
}

export interface Task {
  id: string;
  workflow_id?: string;
  title?: string;
  description?: string;
  status?: string;
  depends_on?: string[];
  assigned_to?: string;
  output?: string;
  created_at?: string;
  completed_at?: string | null;
}

export interface Artifact {
  id: string;
  workflow_id?: string;
  task_id?: string | null;
  kind?: string;
  name?: string;
  description?: string;
  path?: string;
  content?: string;
  created_by?: string;
  created_at?: string;
}

export interface FinalizationResult {
  action?: string;
  summary?: string;
  links?: string[];
  metadata?: Record<string, unknown>;
  suggestions?: string[];
  completed_at?: string;
}

export interface WorkflowExecution {
  current_persona?: string;
  active_task_id?: string;
  active_task_title?: string;
  qa_cycle?: number;
  remediation_attempt?: number;
  workflow_kind?: string;
  parent_workflow_id?: string;
  improvement_depth?: number;
}

export interface WorkflowState {
  id: string;
  tenant_id?: string;
  scope_id?: string;
  status?: WorkflowStatus;
  mode?: WorkflowMode;
  title?: string;
  request?: string;
  constitution?: Constitution | null;
  requirements?: Requirements | null;
  design?: Design | null;
  tasks?: Task[];
  artifacts?: Artifact[];
  finalization?: FinalizationResult | null;
  summaries?: Record<string, string>;
  provider_name?: string;
  model_name?: string;
  blocking_issues?: string[];
  all_suggestions?: string[];
  error_message?: string;
  created_at?: string;
  updated_at?: string;
  started_at?: string | null;
  completed_at?: string | null;
  execution?: WorkflowExecution;
}

export interface EventRecord {
  id: string;
  workflow_id?: string;
  tenant_id?: string;
  scope_id?: string;
  type?: string;
  persona?: string;
  payload?: unknown;
  created_at?: string;
  occurred_at?: string;
}

export interface ProviderInfo {
  name: string;
  default_model?: string;
  enabled?: boolean;
  capabilities?: string[];
}

export interface ModelInfo {
  id: string;
  name: string;
  description?: string;
  capabilities?: string[];
  metadata?: Record<string, string>;
}

export interface ProviderTestResult {
  name?: string;
  ok?: boolean;
  healthy?: boolean;
  latency_ms?: number;
  error?: string;
}

export interface Tenant {
  id: string;
  slug: string;
  name: string;
  created_at?: string;
  updated_at?: string;
}

export interface Scope {
  id: string;
  tenant_id?: string;
  kind?: ScopeKind;
  name?: string;
  slug?: string;
  parent_scope_id?: string | null;
  created_at?: string;
  updated_at?: string;
}

export interface CustomizationItem {
  name: string;
  source?: string;
  sourceName?: string;
  precedence?: number;
  path?: string;
}

export interface CustomizationsResolveResponse {
  scope_id?: string;
  skills: CustomizationItem[];
  agents: CustomizationItem[];
  prompts: CustomizationItem[];
  note?: string;
}

export interface EffectiveConfigResponse {
  scope_id?: string;
  scope_kind?: string;
  resolved_chain?: string[];
  resolution_chain?: string[];
  effective?: Record<string, unknown>;
  depth?: number;
}

export interface OrcaContext {
  tenantId?: string;
  scopeId?: string;
}

export interface ListResult<T> {
  items: T[];
  limit?: number;
  offset?: number;
}

export interface CreateWorkflowRequest {
  request: string;
  title?: string;
  mode?: WorkflowMode | "";
  provider?: string;
  model?: string;
  delivery?: {
    action?: string;
    config?: Record<string, unknown>;
  };
}

export interface CreateTenantRequest {
  slug: string;
  name: string;
}

export interface UpdateTenantRequest {
  slug?: string;
  name?: string;
}

export interface CreateScopeRequest {
  kind: ScopeKind;
  name: string;
  slug: string;
  parent_scope_id?: string;
}

export interface UpdateScopeRequest {
  name?: string;
  slug?: string;
}

export interface OrcaHealthResponse {
  status?: string;
  error?: string;
  provider?: string;
}