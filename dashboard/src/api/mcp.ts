import { http, unwrap } from "./typed";
import type { components } from "./schema";

export type McpServer = components["schemas"]["MCPServer"];
export type McpEvent = components["schemas"]["MCPEvent"];
export type McpPolicy = components["schemas"]["MCPPolicy"];
export const mcpKeys = ["mcp"] as const;
export const mcpApi = {
  servers: () => unwrap(http.GET("/v1/mcp/servers", {})).then((r) => r.servers),
  events: (server_id?: string) =>
    unwrap(
      http.GET("/v1/mcp/events", {
        params: { query: { server_id, limit: 100 } },
      }),
    ).then((r) => r.events),
  create: (body: components["schemas"]["MCPCreateServer"]) =>
    unwrap(http.POST("/v1/mcp/servers", { body })),
  policy: (id: string, body: McpPolicy) =>
    unwrap(
      http.PUT("/v1/mcp/servers/{id}/policy", {
        params: { path: { id } },
        body,
      }),
    ),
  rotate: (id: string) =>
    unwrap(
      http.POST("/v1/mcp/servers/{id}/rotate-token", {
        params: { path: { id } },
      }),
    ),
  revoke: (id: string) =>
    unwrap(http.DELETE("/v1/mcp/servers/{id}", { params: { path: { id } } })),
  inspect: (body: components["schemas"]["MCPInspectRequest"]) =>
    unwrap(http.POST("/v1/mcp/inspect", { body })),
};
