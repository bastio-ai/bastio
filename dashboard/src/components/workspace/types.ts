// Workspace types + API helpers. Types come from the generated OpenAPI
// schema (npm run generate:api) so backend changes fail typecheck instead
// of silently drifting. The handful of methods that don't fit
// openapi-fetch's request model — multipart upload + SSE — keep raw
// fetch with hand-typed bodies; everything else flows through the
// typed client.

import { http, unwrap } from "@/api/typed";
import type { components } from "@/api/schema";

// Base types come from the generated schema. Intersection augmentations
// below carry fields the OpenAPI spec hasn't been updated for yet —
// the runtime payload already includes them (see migration 026 + the
// matching Go struct fields). Bring them into the YAML next time the
// schema gets a polish pass.
export type Settings = components["schemas"]["WorkspaceSettings"] & {
  ai_persona_name?: string | null;
  ai_persona_personality?: string | null;
  ai_persona_tone?: string | null;
};
export type SettingsPatch = components["schemas"]["WorkspaceSettingsPatch"] & {
  ai_persona_name?: string | null;
  ai_persona_personality?: string | null;
  ai_persona_tone?: string | null;
};
export type Assistant = Omit<components["schemas"]["WorkspaceAssistant"], "language"> & {
  language?: string | null; // null/undefined = auto-detect
};
export type AssistantPatch = Omit<components["schemas"]["WorkspaceAssistantPatch"], "language"> & {
  language?: string | null;
};
export type KnowledgeSource = components["schemas"]["WorkspaceKnowledgeSource"] & {
  character_count?: number;
  last_synced_at?: string | null;
};
export type Conversation = components["schemas"]["WorkspaceConversation"];
export type ConversationListItem = components["schemas"]["WorkspaceConversationListItem"];
export type Message = components["schemas"]["WorkspaceMessage"];
export type Member = components["schemas"]["WorkspaceMember"] & {
  monthly_token_limit?: number | null;
  daily_rate_limit?: number | null;
};
export type Invitation = components["schemas"]["WorkspaceInvitation"];
export type Domain = components["schemas"]["WorkspaceDomain"];
export type AnalyticsSummary = components["schemas"]["WorkspaceAnalyticsSummary"];
export type DailyUsagePoint = components["schemas"]["WorkspaceDailyUsagePoint"];
export type ByModelCount = components["schemas"]["WorkspaceByModelCount"];

// Per-user analytics (new in 026 cut). Hand-typed since the openapi
// stub uses `type: object`; revisit when the YAML gets full schemas.
export type TopUserUsage = {
  user_id: string;
  email?: string;
  messages: number;
  tokens: number;
  cost_cents: number;
};

export type ForecastResult = {
  current_cents: number;
  days_elapsed: number;
  days_in_month: number;
  daily_average_cents: number;
  projected_cents: number;
  last_month_cents: number;
  delta_pct_vs_last_month: number;
};

export type PeriodStats = {
  messages: number;
  tokens: number;
  cost_cents: number;
  active_users: number;
  conversations: number;
};

export type CompareResult = {
  this_week: PeriodStats;
  last_week: PeriodStats;
};

export type UserAssistantUsage = {
  assistant_id: string;
  assistant_name: string;
  messages: number;
  tokens: number;
};

export type UserAnalyticsDetail = {
  user_id: string;
  email?: string;
  total_messages: number;
  total_tokens: number;
  total_cost_cents: number;
  daily: DailyUsagePoint[];
  top_assistants: UserAssistantUsage[];
};

// Audit log row. Snapshot fields are captured at write time —
// see migration 028.
export type AuditEntry = {
  id: string;
  customer_id: string;
  actor_user_id: string;
  actor_email: string;
  actor_role: string;
  action: string;
  target_type: string;
  target_id: string;
  target_label: string;
  metadata: Record<string, unknown>;
  ip_address: string;
  user_agent: string;
  created_at: string;
};

export const workspaceApi = {
  status: () => unwrap(http.GET("/v1/workspace/status")),

  // settings
  getSettings: async (): Promise<Settings> =>
    (await unwrap(http.GET("/v1/workspace/settings"))) as Settings,
  patchSettings: (patch: SettingsPatch) =>
    unwrap(http.PATCH("/v1/workspace/settings", { body: patch as components["schemas"]["WorkspaceSettingsPatch"] })),

  // assistants
  listAssistants: () => unwrap(http.GET("/v1/workspace/assistants")),
  getAssistant: (id: string) =>
    unwrap(http.GET("/v1/workspace/assistants/{id}", { params: { path: { id } } })),
  createAssistant: (a: Partial<Assistant>) =>
    unwrap(
      http.POST("/v1/workspace/assistants", {
        // Schema cast: language is nullable at runtime (auto-detect)
        // but the generated openapi-fetch type still has it as
        // string. Updating the YAML schema in a follow-up.
        body: a as unknown as components["schemas"]["WorkspaceAssistant"],
      }),
    ),
  updateAssistant: (id: string, p: AssistantPatch) =>
    unwrap(
      http.PATCH("/v1/workspace/assistants/{id}", {
        params: { path: { id } },
        body: p as unknown as components["schemas"]["WorkspaceAssistantPatch"],
      }),
    ),
  archiveAssistant: (id: string) =>
    unwrap(http.DELETE("/v1/workspace/assistants/{id}", { params: { path: { id } } })),

  // knowledge
  listKnowledge: async (): Promise<{ sources: KnowledgeSource[] }> =>
    (await unwrap(http.GET("/v1/workspace/knowledge"))) as { sources: KnowledgeSource[] },
  getKnowledge: (id: string) =>
    unwrap(http.GET("/v1/workspace/knowledge/{id}", { params: { path: { id } } })),
  createKnowledge: (k: Partial<KnowledgeSource>) =>
    unwrap(http.POST("/v1/workspace/knowledge", { body: k as KnowledgeSource })),
  // Multipart upload — openapi-fetch supports `multipart/form-data` only
  // for shapes that JSON-stringify cleanly; for streaming a binary blob
  // we stick with raw fetch and hand the typed schema to the response.
  uploadKnowledge: async (file: File, name?: string): Promise<KnowledgeSource> => {
    const form = new FormData();
    form.append("file", file);
    if (name) form.append("name", name);
    const r = await fetch("/v1/workspace/knowledge/upload", {
      method: "POST",
      body: form,
    });
    if (!r.ok) {
      throw new Error(`upload ${r.status} ${await r.text()}`);
    }
    return (await r.json()) as KnowledgeSource;
  },

  // Chat-attachments are one-shot: server runs ExtractText (same path
  // the Knowledge Base ingest uses) and returns the plain text.
  // Nothing is persisted; the text rides along inline in the next
  // user message. Image types come back with extracted=false; the
  // frontend renders a placeholder for those.
  uploadChatAttachment: async (file: File): Promise<{
    name: string;
    mime_type: string;
    size: number;
    text: string;
    extracted: boolean;
    extract_error?: string;
    is_image: boolean;
    data_url?: string;
  }> => {
    const form = new FormData();
    form.append("file", file);
    const r = await fetch("/v1/workspace/chat-attachments", {
      method: "POST",
      body: form,
    });
    if (!r.ok) {
      throw new Error(`upload ${r.status} ${await r.text()}`);
    }
    return r.json();
  },
  archiveKnowledge: (id: string) =>
    unwrap(http.DELETE("/v1/workspace/knowledge/{id}", { params: { path: { id } } })),
  releaseKnowledge: (id: string) =>
    unwrap(http.POST("/v1/workspace/knowledge/{id}/release", { params: { path: { id } } })),

  // conversations
  listConversations: (limit = 50) =>
    unwrap(
      http.GET("/v1/workspace/conversations", { params: { query: { limit } } }),
    ),
  getConversation: (id: string) =>
    unwrap(
      http.GET("/v1/workspace/conversations/{id}", { params: { path: { id } } }),
    ),
  createConversation: (body: { title?: string; assistant_id?: string }) =>
    unwrap(http.POST("/v1/workspace/conversations", { body })),
  renameConversation: (id: string, title: string) =>
    unwrap(
      http.PATCH("/v1/workspace/conversations/{id}", {
        params: { path: { id } },
        body: { title },
      }),
    ),
  // updateConversation is the modern PATCH — accepts any combo of
  // title / pinned / archived. Use this for the hover-menu pin and
  // archive/unarchive actions. renameConversation stays as a thin
  // wrapper for callers that only know about renaming.
  //
  // The openapi.yaml currently types this body as `{ title: string }`;
  // the runtime accepts the wider shape. Schema update is a follow-up.
  updateConversation: (
    id: string,
    body: { title?: string; pinned?: boolean; archived?: boolean },
  ) =>
    unwrap(
      http.PATCH("/v1/workspace/conversations/{id}", {
        params: { path: { id } },
        body: body as unknown as { title: string },
      }),
    ),
  archiveConversation: (id: string) =>
    unwrap(
      http.DELETE("/v1/workspace/conversations/{id}", { params: { path: { id } } }),
    ),
  // deleteFromMessage wipes the target message and every later
  // message in the same conversation. Used by regenerate + edit
  // flows: client deletes from the divergence point, then re-sends
  // a (possibly edited) user message via streamSendMessage.
  deleteFromMessage: async (conversationID: string, messageID: string) => {
    const r = await fetch(
      `/v1/workspace/conversations/${encodeURIComponent(conversationID)}/messages/${encodeURIComponent(messageID)}`,
      { method: "DELETE" },
    );
    if (!r.ok) {
      throw new Error(`delete from message ${r.status} ${await r.text()}`);
    }
  },
  listMessages: (id: string) =>
    unwrap(
      http.GET("/v1/workspace/conversations/{id}/messages", {
        params: { path: { id } },
      }),
    ),
  sendMessage: (
    id: string,
    body: { content: string; provider?: string; model?: string },
  ) =>
    unwrap(
      http.POST("/v1/workspace/conversations/{id}/messages", {
        params: { path: { id } },
        body,
      }),
    ),
  // SSE — openapi-fetch consumes JSON responses, but our stream is
  // event-stream. Use the existing fetch-based reader.
  streamSendMessage: (
    id: string,
    body: { content: string; provider?: string; model?: string },
    onToken: (delta: string) => void,
    signal?: AbortSignal,
  ): Promise<Message> => streamSSE(id, body, onToken, signal),

  // RBAC: who am I + what can I do?
  whoami: async (): Promise<{
    user_id: string;
    email: string;
    role: "owner" | "admin" | "member" | "viewer";
    can_admin: boolean;
    can_send: boolean;
    is_owner: boolean;
  }> => rawJSON("/v1/workspace/whoami"),
  // Promote / demote between admin / member / viewer. Owner promotion
  // requires the transfer flow.
  changeMemberRole: async (userID: string, role: "admin" | "member" | "viewer") => {
    const r = await fetch(
      `/v1/workspace/members/${encodeURIComponent(userID)}/role`,
      {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ role }),
      },
    );
    if (!r.ok) throw new Error(`change role ${r.status} ${await r.text()}`);
  },
  // Transfer workspace ownership. Owner-only — server enforces.
  transferOwnership: async (newOwnerUserID: string) => {
    const r = await fetch("/v1/workspace/owner/transfer", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ new_owner_user_id: newOwnerUserID }),
    });
    if (!r.ok) throw new Error(`transfer ownership ${r.status} ${await r.text()}`);
  },

  // members + invitations — cloud-only, paths stripped from OSS
  // openapi.yaml. rawJSON keeps these decoupled from generated path
  // types so re-running `npm run generate:api` against the OSS spec
  // doesn't break cloud-dashboard's typed client.
  listMembers: (): Promise<{ members: Member[] }> =>
    rawJSON("/v1/workspace/members"),
  removeMember: (userID: string) =>
    rawJSON<void>(`/v1/workspace/members/${encodeURIComponent(userID)}`, {
      method: "DELETE",
    }),
  listInvitations: (): Promise<{ invitations: Invitation[] }> =>
    rawJSON("/v1/workspace/invitations"),
  createInvitation: (body: {
    email: string;
    role?: "owner" | "admin" | "member" | "viewer";
  }): Promise<Invitation & { token: string }> => rawJSON("/v1/workspace/invitations", { body }),
  revokeInvitation: (id: string) =>
    rawJSON<void>(`/v1/workspace/invitations/${encodeURIComponent(id)}`, {
      method: "DELETE",
    }),

  // branded chat: slug + custom domains — cloud-only.
  setSlug: (slug: string) =>
    rawJSON<void>("/v1/workspace/settings/slug", {
      method: "PUT",
      body: { slug },
    }),
  listDomains: (): Promise<{ domains: Domain[] }> =>
    rawJSON("/v1/workspace/domains"),
  createDomain: (domain: string): Promise<Domain> =>
    rawJSON("/v1/workspace/domains", { body: { domain } }),
  verifyDomain: (id: string): Promise<Domain> =>
    rawJSON(`/v1/workspace/domains/${encodeURIComponent(id)}/verify`, {
      method: "POST",
    }),
  deleteDomain: (id: string) =>
    rawJSON<void>(`/v1/workspace/domains/${encodeURIComponent(id)}`, {
      method: "DELETE",
    }),

  // analytics
  analyticsSummary: () => unwrap(http.GET("/v1/workspace/analytics/summary")),
  analyticsDaily: (days = 14) =>
    unwrap(http.GET("/v1/workspace/analytics/daily", { params: { query: { days } } })),
  analyticsByModel: () => unwrap(http.GET("/v1/workspace/analytics/by-model")),
  // Per-user drill-down (Favio-parity 026 cut). Raw fetch because
  // the openapi.yaml stubs use `type: object` for these endpoints —
  // revisit once the YAML gets full response schemas.
  analyticsTopUsers: async (limit = 10): Promise<{ users: TopUserUsage[] }> =>
    rawJSON(`/v1/workspace/analytics/users?limit=${limit}`),
  analyticsForecast: async (): Promise<ForecastResult> =>
    rawJSON("/v1/workspace/analytics/forecast"),
  analyticsCompare: async (): Promise<CompareResult> =>
    rawJSON("/v1/workspace/analytics/compare"),
  analyticsUserDetail: async (userID: string): Promise<UserAnalyticsDetail> =>
    rawJSON(`/v1/workspace/analytics/users/${encodeURIComponent(userID)}`),

  // Audit log — admin-only on the server. Reverse-chronological;
  // pass `before` (an audit id) to paginate.
  listAudit: async (params: {
    limit?: number;
    action?: string;
    before?: string;
  } = {}): Promise<{ entries: AuditEntry[] }> => {
    const q = new URLSearchParams();
    if (params.limit) q.set("limit", String(params.limit));
    if (params.action) q.set("action", params.action);
    if (params.before) q.set("before", params.before);
    const qs = q.toString();
    return rawJSON(`/v1/workspace/audit${qs ? "?" + qs : ""}`);
  },

  // Per-member budgets. Pass null to clear, integer to set, omit to
  // leave alone. Server responds 204 on success.
  setMemberBudgets: async (
    userID: string,
    body: { monthly_token_limit?: number | null; daily_rate_limit?: number | null },
  ) => {
    const r = await fetch(`/v1/workspace/members/${encodeURIComponent(userID)}/budgets`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    if (!r.ok) throw new Error(`set budgets ${r.status} ${await r.text()}`);
  },
};

// rawJSON is the escape hatch for endpoints that openapi.yaml does
// not document — primarily the cloud-only workspace API surface
// (members, invitations, audit, domains, per-user analytics). Those
// paths were stripped from the public OSS spec so /docs only renders
// the single-tenant surface; their handlers still register at runtime
// when cloud-server enables them via WorkspaceCustomizer.EnableCloudOnlyRoutes.
//
// Threads through the same session cookie auth as the typed client
// because both go through the browser's fetch — no headers needed
// beyond Accept. Pass `body` to send JSON; method defaults to POST
// when a body is provided, otherwise GET.
async function rawJSON<T>(
  url: string,
  init: { method?: string; body?: unknown } = {},
): Promise<T> {
  const headers: Record<string, string> = { Accept: "application/json" };
  let bodyStr: string | undefined;
  if (init.body !== undefined) {
    headers["Content-Type"] = "application/json";
    bodyStr = JSON.stringify(init.body);
  }
  const r = await fetch(url, {
    method: init.method ?? (init.body !== undefined ? "POST" : "GET"),
    headers,
    body: bodyStr,
  });
  if (!r.ok) throw new Error(`fetch ${url} ${r.status} ${await r.text()}`);
  if (r.status === 204) return undefined as T;
  return (await r.json()) as T;
}

// =============================================================================
// SSE consumer — kept for the streaming chat path (POST + event-stream
// don't fit openapi-fetch's request/response model).
// =============================================================================

async function streamSSE(
  conversationID: string,
  body: { content: string; provider?: string; model?: string },
  onToken: (delta: string) => void,
  signal?: AbortSignal,
): Promise<Message> {
  const r = await fetch(
    `/v1/workspace/conversations/${conversationID}/messages/stream`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json", Accept: "text/event-stream" },
      body: JSON.stringify(body),
      signal,
    },
  );
  if (!r.ok || !r.body) {
    const text = await r.text().catch(() => "");
    throw new Error(`stream ${r.status} ${text}`);
  }
  const reader = r.body.getReader();
  const decoder = new TextDecoder();
  let buf = "";
  let final: Message | null = null;
  let serverError: string | null = null;

  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    buf += decoder.decode(value, { stream: true });
    let idx: number;
    while ((idx = buf.indexOf("\n\n")) !== -1) {
      const frame = buf.slice(0, idx);
      buf = buf.slice(idx + 2);
      const evt = parseSSEFrame(frame);
      if (!evt) continue;
      if (evt.event === "token" && typeof evt.data?.delta === "string") {
        onToken(evt.data.delta);
      } else if (evt.event === "done" && evt.data?.message) {
        final = evt.data.message as Message;
      } else if (evt.event === "error" && typeof evt.data?.error === "string") {
        serverError = evt.data.error;
      }
    }
  }
  if (serverError && !final) throw new Error(serverError);
  if (!final) throw new Error("stream ended without final message");
  return final;
}

function parseSSEFrame(frame: string): { event: string; data: any } | null {
  let event = "message";
  let dataStr = "";
  for (const line of frame.split("\n")) {
    if (line.startsWith("event:")) event = line.slice(6).trim();
    else if (line.startsWith("data:")) dataStr += line.slice(5).trim();
  }
  if (!dataStr) return null;
  try {
    return { event, data: JSON.parse(dataStr) };
  } catch {
    return null;
  }
}

// =============================================================================
// Format helpers
// =============================================================================

export function formatCents(cents: number): string {
  return `$${(cents / 100).toFixed(2)}`;
}

export function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}

export function relativeTime(iso: string): string {
  const t = new Date(iso).getTime();
  const diff = Date.now() - t;
  const s = Math.floor(diff / 1000);
  if (s < 60) return `${s}s ago`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  const d = Math.floor(h / 24);
  return `${d}d ago`;
}
