import {
  gatewayModelChoices,
  listedGatewayModelChoices,
} from "../gateways/gateway-models.ts";
import type { ModelChoice } from "./model-catalog.ts";
export type ProviderModels = {
  provider: string;
  configured: boolean;
  source: "provider" | "catalog";
  error_code?: string;
  discovered_at?: string;
  models: {
    id: string;
    name?: string;
    checked_at?: string;
    check_status?: "untested" | "passed" | "failed";
    error_code?: string;
  }[];
};
export type ModelInventory = {
  providers: ProviderModels[];
  restricted: boolean;
};
export function workspaceModelChoices(
  inventory?: ModelInventory,
): ModelChoice[] {
  if (!inventory) return [];
  return inventory.providers
    .flatMap((provider) => {
      if (!provider.configured) return [];
      const reported = listedGatewayModelChoices({
        ...provider,
        source: "provider",
      });
      const choices =
        provider.source === "catalog" && !inventory.restricted
          ? [
              ...gatewayModelChoices(provider.provider),
              ...reported.filter(
                (item) =>
                  !gatewayModelChoices(provider.provider).some(
                    (known) => known.model === item.model,
                  ),
              ),
            ]
          : reported;
      return choices
        .filter(
          (choice) =>
            !provider.models.some(
              (model) =>
                model.id === choice.model &&
                model.check_status === "failed" &&
                [
                  "model_access_denied",
                  "provider_authentication_failed",
                ].includes(model.error_code ?? ""),
            ),
        )
        .map((choice) => {
          const evidence = provider.models.find(
            (model) => model.id === choice.model,
          );
          return {
            ...choice,
            badge:
              evidence?.check_status === "passed"
                ? "Test passed"
                : "Not tested",
            description:
              evidence?.check_status === "passed" && evidence.checked_at
                ? `Last successful test: ${new Date(evidence.checked_at).toLocaleDateString()}`
                : provider.source === "provider"
                  ? "Reported by your workspace provider. Access is checked when you send."
                  : "Suggested model; provider discovery is unavailable.",
          };
        });
    })
    .sort(
      (a, b) =>
        Number(b.badge === "Test passed") - Number(a.badge === "Test passed"),
    );
}
export function workspaceModelError(code?: string): string {
  switch (code) {
    case "model_access_denied":
      return "The provider cannot use this model with the current workspace key. Choose another model or check the provider project’s access.";
    case "provider_authentication_failed":
      return "The provider rejected this credential. Ask an admin to update the provider key.";
    case "provider_access_denied":
      return "The provider denied access. Check the key’s project and permissions.";
    case "provider_usage_limit":
      return "The provider stopped the request because of a balance or usage limit.";
    case "workspace_usage_limit":
      return "A workspace spending or member limit stopped this request.";
    case "workspace_security_policy":
    case "workspace_policy_or_limit":
      return "A workspace policy or usage limit stopped the test. Review your security and spending settings.";
    case "provider_timeout":
      return "The provider took too long. Try again when the connection is available.";
    case "no_text_reply":
      return "The provider returned no text. Try a different chat model.";
    default:
      return "Could not verify this model. Refresh discovery, check the provider connection, or try again.";
  }
}
