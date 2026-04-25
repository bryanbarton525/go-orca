import type {
  Attachment,
  CreateScopeRequest,
  CreateTenantRequest,
  CreateWorkflowRequest,
  CustomizationItem,
  CustomizationsResolveResponse,
  EffectiveConfigResponse,
  EventRecord,
  ListResult,
  MCPRegistrySnapshot,
  ModelInfo,
  OrcaContext,
  OrcaHealthResponse,
  ProviderInfo,
  ProviderTestResult,
  Scope,
  Tenant,
  UpdateScopeRequest,
  UpdateTenantRequest,
  UploadSession,
  WorkflowState,
} from "../../types/orca";

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function appendContext(params: URLSearchParams, context?: OrcaContext) {
  if (!context) {
    return;
  }

  if (context.tenantId) {
    params.set("tenantId", context.tenantId);
  }

  if (context.scopeId) {
    params.set("scopeId", context.scopeId);
  }
}

async function parsePayload(response: Response): Promise<unknown> {
  const text = await response.text();
  if (!text) {
    return null;
  }

  try {
    return JSON.parse(text) as unknown;
  } catch {
    return text;
  }
}

function getErrorMessage(payload: unknown, fallback: string) {
  if (typeof payload === "string") {
    return payload;
  }

  if (isRecord(payload) && typeof payload.error === "string") {
    return payload.error;
  }

  return fallback;
}

async function orcaRequest<T>(
  path: string,
  options: {
    method?: "GET" | "POST" | "PATCH" | "DELETE";
    body?: unknown;
    query?: Record<string, string | number | boolean | undefined>;
    context?: OrcaContext;
  } = {}
): Promise<T> {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(options.query ?? {})) {
    if (value !== undefined && value !== "") {
      params.set(key, String(value));
    }
  }
  appendContext(params, options.context);

  const url = `/api/orca/${path}${params.toString() ? `?${params.toString()}` : ""}`;
  const response = await fetch(url, {
    method: options.method ?? "GET",
    cache: "no-store",
    headers: options.body ? { "Content-Type": "application/json" } : undefined,
    body: options.body ? JSON.stringify(options.body) : undefined,
  });

  const payload = await parsePayload(response);
  if (!response.ok) {
    throw new Error(getErrorMessage(payload, `go-orca request failed with status ${response.status}`));
  }

  return payload as T;
}

function normalizeList<T>(payload: unknown, key: string): ListResult<T> {
  if (Array.isArray(payload)) {
    return { items: payload as T[] };
  }

  if (isRecord(payload)) {
    const maybeItems = payload[key];
    return {
      items: Array.isArray(maybeItems) ? (maybeItems as T[]) : [],
      limit: typeof payload.limit === "number" ? payload.limit : undefined,
      offset: typeof payload.offset === "number" ? payload.offset : undefined,
    };
  }

  return { items: [] };
}

function normalizeProviders(payload: unknown): ProviderInfo[] {
  const base = normalizeList<ProviderInfo>(payload, "providers");
  return base.items.map((provider) => ({
    ...provider,
    capabilities: Array.isArray(provider.capabilities)
      ? provider.capabilities.map((item) => String(item))
      : [],
  }));
}

function normalizeCustomizationItems(items: unknown): CustomizationItem[] {
  if (!Array.isArray(items)) {
    return [];
  }

  return items
    .filter(isRecord)
    .map((item) => ({
      name: typeof item.name === "string" ? item.name : "Unnamed",
      source: typeof item.source === "string" ? item.source : undefined,
      sourceName: typeof item.sourceName === "string" ? item.sourceName : undefined,
      precedence: typeof item.precedence === "number" ? item.precedence : undefined,
      path: typeof item.path === "string" ? item.path : undefined,
    }));
}

export function buildWorkflowStreamUrl(workflowId: string, context?: OrcaContext, timeout = 300) {
  const params = new URLSearchParams({ timeout: String(timeout) });
  appendContext(params, context);
  return `/api/orca/workflows/${workflowId}/stream?${params.toString()}`;
}

export function getHealthz() {
  return orcaRequest<OrcaHealthResponse>("healthz");
}

export function getReadyz() {
  return orcaRequest<OrcaHealthResponse>("readyz");
}

export function getMCPRegistry() {
  return orcaRequest<MCPRegistrySnapshot>("mcp/registry");
}

export async function listWorkflows(context?: OrcaContext, limit = 20, offset = 0) {
  return normalizeList<WorkflowState>(await orcaRequest<unknown>("workflows", { query: { limit, offset }, context }), "workflows");
}

export function createWorkflow(payload: CreateWorkflowRequest, context?: OrcaContext) {
  return orcaRequest<WorkflowState>("workflows", { method: "POST", body: payload, context });
}

export function getWorkflow(id: string, context?: OrcaContext) {
  return orcaRequest<WorkflowState>(`workflows/${id}`, { context });
}

export async function getWorkflowEvents(id: string, context?: OrcaContext) {
  return normalizeList<EventRecord>(await orcaRequest<unknown>(`workflows/${id}/events`, { context }), "events");
}

export function cancelWorkflow(id: string, context?: OrcaContext) {
  return orcaRequest<{ status: string }>(`workflows/${id}/cancel`, { method: "POST", context });
}

export function resumeWorkflow(id: string, context?: OrcaContext) {
  return orcaRequest<{ status: string }>(`workflows/${id}/resume`, { method: "POST", context });
}

export async function listProviders() {
  return normalizeProviders(await orcaRequest<unknown>("providers"));
}

export async function testProvider(name: string) {
  const payload = await orcaRequest<ProviderTestResult>(`providers/${name}/test`, { method: "POST" });
  return {
    ...payload,
    ok: payload.ok ?? payload.healthy,
  } satisfies ProviderTestResult;
}

export async function listProviderModels(name: string) {
  return normalizeList<ModelInfo>(await orcaRequest<unknown>(`providers/${name}/models`), "models");
}

export function listTenants() {
  return normalizeList<Tenant>(orcaRequest<unknown>("tenants"), "tenants");
}

export function createTenant(payload: CreateTenantRequest) {
  return orcaRequest<Tenant>("tenants", { method: "POST", body: payload });
}

export function getTenant(id: string) {
  return orcaRequest<Tenant>(`tenants/${id}`);
}

export function updateTenant(id: string, payload: UpdateTenantRequest) {
  return orcaRequest<Tenant>(`tenants/${id}`, { method: "PATCH", body: payload });
}

export function deleteTenant(id: string) {
  return orcaRequest<void>(`tenants/${id}`, { method: "DELETE" });
}

export async function listScopesForTenant(tenantId: string) {
  return normalizeList<Scope>(await orcaRequest<unknown>(`tenants/${tenantId}/scopes`), "scopes");
}

export function createScope(tenantId: string, payload: CreateScopeRequest) {
  return orcaRequest<Scope>(`tenants/${tenantId}/scopes`, { method: "POST", body: payload });
}

export function updateScope(tenantId: string, scopeId: string, payload: UpdateScopeRequest) {
  return orcaRequest<Scope>(`tenants/${tenantId}/scopes/${scopeId}`, { method: "PATCH", body: payload });
}

export function deleteScope(tenantId: string, scopeId: string) {
  return orcaRequest<void>(`tenants/${tenantId}/scopes/${scopeId}`, { method: "DELETE" });
}

export async function getEffectiveConfig(scopeId: string, context?: OrcaContext) {
  const payload = await orcaRequest<EffectiveConfigResponse>(`scopes/${scopeId}/effective-config`, { context });
  return {
    ...payload,
    resolved_chain: payload.resolved_chain ?? payload.resolution_chain,
  } satisfies EffectiveConfigResponse;
}

export async function resolveCustomizations(context?: OrcaContext) {
  const payload = await orcaRequest<unknown>("customizations/resolve", { context });
  if (!isRecord(payload)) {
    return { skills: [], agents: [], prompts: [] } satisfies CustomizationsResolveResponse;
  }

  return {
    scope_id: typeof payload.scope_id === "string" ? payload.scope_id : undefined,
    skills: normalizeCustomizationItems(payload.skills),
    agents: normalizeCustomizationItems(payload.agents),
    prompts: normalizeCustomizationItems(payload.prompts),
    note: typeof payload.note === "string" ? payload.note : undefined,
  } satisfies CustomizationsResolveResponse;
}

// ─── Upload sessions ──────────────────────────────────────────────────────────

export function createUploadSession() {
  return orcaRequest<UploadSession>("upload-sessions", { method: "POST" });
}

export async function uploadFile(sessionId: string, file: File): Promise<Attachment> {
  const formData = new FormData();
  formData.append("file", file);
  
  // Include relative path if available (from folder uploads with webkitdirectory).
  // webkitRelativePath is a non-standard File property available when using webkitdirectory.
  const relativePath = (file as File & { webkitRelativePath?: string }).webkitRelativePath;
  if (relativePath) {
    formData.append("relative_path", relativePath);
  }

  const response = await fetch(`/api/orca/upload-sessions/${sessionId}/files`, {
    method: "POST",
    body: formData,
  });

  const payload = await parsePayload(response);
  if (!response.ok) {
    throw new Error(getErrorMessage(payload, `upload failed with status ${response.status}`));
  }
  return payload as Attachment;
}

export async function listSessionFiles(sessionId: string) {
  return normalizeList<Attachment>(
    await orcaRequest<unknown>(`upload-sessions/${sessionId}/files`),
    "items"
  );
}

export function abortUploadSession(sessionId: string) {
  return orcaRequest<{ status: string }>(`upload-sessions/${sessionId}/abort`, { method: "POST" });
}

export async function listWorkflowAttachments(workflowId: string) {
  return normalizeList<Attachment>(
    await orcaRequest<unknown>(`workflows/${workflowId}/attachments`),
    "items"
  );
}