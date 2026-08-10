import { http, unwrap } from "./typed";
import type { components } from "./schema";

// Types re-exported from the generated OpenAPI schema so route code can
// import them without knowing the codegen layout. When the backend spec
// changes, `npm run generate:api` updates these at source.
export type HealthResponse = components["schemas"]["HealthResponse"];
export type Proxy = components["schemas"]["Proxy"];
export type Trace = components["schemas"]["Trace"];
export type Observation = components["schemas"]["Observation"];
export type TraceDetail = components["schemas"]["TraceDetail"];
export type TraceScore = components["schemas"]["TraceScore"];
export type CreateTraceScoreRequest = components["schemas"]["CreateTraceScoreRequest"];
export type TraceThreatDetection = components["schemas"]["TraceThreatDetection"];
export type Session = components["schemas"]["Session"];
export type SessionDetail = components["schemas"]["SessionDetail"];
export type User = components["schemas"]["User"];
export type UserDetail = components["schemas"]["UserDetail"];
export type UserThreatBreakdown = components["schemas"]["UserThreatBreakdown"];
export type Prompt = components["schemas"]["Prompt"];
export type PromptVersion = components["schemas"]["PromptVersion"];
export type PromptDetail = components["schemas"]["PromptDetail"];
export type CreatePromptRequest = components["schemas"]["CreatePromptRequest"];
export type CreatePromptVersionRequest = components["schemas"]["CreatePromptVersionRequest"];
export type SetPromptLabelsRequest = components["schemas"]["SetPromptLabelsRequest"];
export type PromptUsage = components["schemas"]["PromptUsage"];
export type UserAnalytics = components["schemas"]["UserAnalytics"];
export type ThreatEvent = components["schemas"]["ThreatEvent"];
export type AnalyticsOverview = components["schemas"]["AnalyticsOverview"];
export type APIKey = components["schemas"]["APIKey"];
export type SecurityProfile = components["schemas"]["SecurityProfile"];
export type CreateProxyRequest = components["schemas"]["CreateProxyRequest"];
export type UpdateProxyRequest = components["schemas"]["UpdateProxyRequest"];
export type CreateAPIKeyRequest = components["schemas"]["CreateAPIKeyRequest"];
export type ProviderKey = components["schemas"]["ProviderKey"];
export type CreateProviderKeyRequest = components["schemas"]["CreateProviderKeyRequest"];
export type AppConfig = components["schemas"]["AppConfig"];
export type UpdateSecurityProfileRequest = components["schemas"]["UpdateSecurityProfileRequest"];

// Governance — Bastio Governance browser extension API
export type GovernanceSeverity = components["schemas"]["GovernanceSeverity"];
export type GovernanceAction = components["schemas"]["GovernanceAction"];
export type GovernanceOverviewSummary = components["schemas"]["GovernanceOverviewSummary"];
export type GovernanceEventRow = components["schemas"]["GovernanceEventRow"];
export type GovernanceDeploymentRow = components["schemas"]["GovernanceDeploymentRow"];
export type GovernanceInstallation = components["schemas"]["GovernanceInstallation"];
export type GovernanceCreatedInstallation = components["schemas"]["GovernanceCreatedInstallation"];
export type GovernanceCustomerPolicy = components["schemas"]["GovernanceCustomerPolicy"];
export type GovernanceWebhook = components["schemas"]["GovernanceWebhook"];
export type GovernanceCreateWebhookRequest = components["schemas"]["GovernanceCreateWebhookRequest"];
export type GovernanceDomainOverride = components["schemas"]["GovernanceDomainOverride"];
export type GovernanceAddDomainRequest = components["schemas"]["GovernanceAddDomainRequest"];
export type GovernancePilotReport = components["schemas"]["GovernancePilotReport"];
export type GovernanceRedirectTarget = components["schemas"]["GovernanceRedirectTarget"];
export type GovernanceRegexPack = components["schemas"]["GovernanceRegexPack"];

export const api = {
  health: () => unwrap(http.GET("/health", {})),

  config: () => unwrap(http.GET("/v1/config", {})),

  traces: {
    list: (params?: {
      limit?: number;
      offset?: number;
      status?: string;
      provider?: string;
      model?: string;
      end_user_id?: string;
      proxy_id?: string;
      search?: string;
      from?: string;
      to?: string;
      environment?: string;
      release?: string;
      trace_name?: string;
      tag?: string[];
    }) =>
      unwrap(
        http.GET("/v1/traces", {
          params: { query: params ?? {} },
        }),
      ),
    get: (id: string) =>
      unwrap(http.GET("/v1/traces/{id}", { params: { path: { id } } })),
    scores: (id: string) =>
      unwrap(http.GET("/v1/traces/{id}/scores", { params: { path: { id } } })),
    createScore: (id: string, data: CreateTraceScoreRequest) =>
      unwrap(
        http.POST("/v1/traces/{id}/scores", {
          params: { path: { id } },
          body: data,
        }),
      ),
    threats: (id: string) =>
      unwrap(
        http.GET("/v1/traces/{id}/threats", { params: { path: { id } } }),
      ),
  },

  prompts: {
    list: () => unwrap(http.GET("/v1/prompts", {})),
    get: (name: string, params?: { version?: number; label?: string }) =>
      unwrap(
        http.GET("/v1/prompts/{name}", {
          params: { path: { name }, query: params ?? {} },
        }),
      ),
    create: (data: CreatePromptRequest) =>
      unwrap(http.POST("/v1/prompts", { body: data })),
    remove: (name: string) =>
      unwrap(http.DELETE("/v1/prompts/{name}", { params: { path: { name } } })),
    versions: (name: string) =>
      unwrap(http.GET("/v1/prompts/{name}/versions", { params: { path: { name } } })),
    createVersion: (name: string, data: CreatePromptVersionRequest) =>
      unwrap(
        http.POST("/v1/prompts/{name}/versions", {
          params: { path: { name } },
          body: data,
        }),
      ),
    setLabels: (name: string, version: number, data: SetPromptLabelsRequest) =>
      unwrap(
        http.PUT("/v1/prompts/{name}/versions/{version}/labels", {
          params: { path: { name, version } },
          body: data,
        }),
      ),
    usage: (name: string) =>
      unwrap(http.GET("/v1/prompts/{name}/usage", { params: { path: { name } } })),
  },

  users: {
    list: (params?: {
      limit?: number;
      order_by?: "cost" | "requests" | "threats" | "latency";
    }) =>
      unwrap(
        http.GET("/v1/users", {
          params: { query: params ?? {} },
        }),
      ),
    get: (id: string) =>
      unwrap(http.GET("/v1/users/{id}", { params: { path: { id } } })),
  },

  sessions: {
    list: (params?: {
      limit?: number;
      offset?: number;
      end_user_id?: string;
      search?: string;
      from?: string;
      to?: string;
      environment?: string;
    }) =>
      unwrap(
        http.GET("/v1/sessions", {
          params: { query: params ?? {} },
        }),
      ),
    get: (id: string) =>
      unwrap(http.GET("/v1/sessions/{id}", { params: { path: { id } } })),
  },

  threats: {
    list: (params?: {
      limit?: number;
      offset?: number;
      severity?: string;
      threat_type?: string;
      detector_name?: string;
      action_taken?: string;
      end_user_id?: string;
      ip_address?: string;
      from?: string;
      to?: string;
      search?: string;
      sort?: "detected_at" | "severity" | "score" | "confidence";
      order?: "asc" | "desc";
    }) =>
      unwrap(
        http.GET("/v1/threats", {
          params: { query: params ?? {} },
        }),
      ),
    get: (id: string) =>
      unwrap(http.GET("/v1/threats/{id}", { params: { path: { id } } })),
  },

  analytics: {
    overview: (params?: { from?: string; to?: string }) =>
      unwrap(
        http.GET("/v1/analytics/overview", {
          params: { query: params ?? {} },
        }),
      ),
    users: (params?: { limit?: number; order_by?: "cost" | "requests" | "threats" | "latency" }) =>
      unwrap(
        http.GET("/v1/analytics/users", {
          params: { query: params ?? {} },
        }),
      ),
  },

  proxies: {
    list: () => unwrap(http.GET("/v1/proxies", {})),
    get: (id: string) =>
      unwrap(http.GET("/v1/proxies/{id}", { params: { path: { id } } })),
    create: (data: CreateProxyRequest) =>
      unwrap(http.POST("/v1/proxies", { body: data })),
    update: (id: string, data: UpdateProxyRequest) =>
      unwrap(
        http.PUT("/v1/proxies/{id}", {
          params: { path: { id } },
          body: data,
        }),
      ),
    delete: (id: string) =>
      unwrap(http.DELETE("/v1/proxies/{id}", { params: { path: { id } } })),
    providerKeys: (id: string) =>
      unwrap(
        http.GET("/v1/proxies/{id}/provider-keys", {
          params: { path: { id } },
        }),
      ),
  },

  providerKeys: {
    list: () => unwrap(http.GET("/v1/provider-keys", {})),
    create: (data: CreateProviderKeyRequest) =>
      unwrap(http.POST("/v1/provider-keys", { body: data })),
    delete: (id: string) =>
      unwrap(http.DELETE("/v1/provider-keys/{id}", { params: { path: { id } } })),
  },

  apiKeys: {
    list: () => unwrap(http.GET("/v1/api-keys", {})),
    create: (data: CreateAPIKeyRequest & { scopes?: string[]; proxy_id?: string }) =>
      unwrap(http.POST("/v1/api-keys", { body: data })),
    update: (id: string, data: { name?: string; rate_limit_rpm?: number; scopes?: string[]; proxy_id?: string }) =>
      unwrap(http.PUT("/v1/api-keys/{id}" as any, { params: { path: { id } }, body: data })),
    revoke: (id: string) =>
      unwrap(http.DELETE("/v1/api-keys/{id}", { params: { path: { id } } })),
  },

  security: {
    profiles: () => unwrap(http.GET("/v1/security/profiles", {})),
    updateProfile: (id: string, data: UpdateSecurityProfileRequest) =>
      unwrap(
        http.PUT("/v1/security/profiles/{id}", {
          params: { path: { id } },
          body: data,
        }),
      ),
  },

};
