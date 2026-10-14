import { MODEL_CATALOG, type ModelChoice } from "../workspace/model-catalog.ts";

// Suggestions reuse the workspace catalogue, filtered to this gateway's provider.
// Availability and account permissions are still checked by the provider.
export function gatewayModelChoices(
  provider: string,
  configuredModel = "",
): ModelChoice[] {
  const choices = MODEL_CATALOG.filter(
    (choice) => choice.provider === provider && !choice.legacy,
  );
  const configured = configuredModel.trim();
  if (configured && !choices.some((choice) => choice.model === configured)) {
    const known = MODEL_CATALOG.find(
      (choice) => choice.provider === provider && choice.model === configured,
    );
    choices.unshift(
      known ?? {
        provider: provider as ModelChoice["provider"],
        model: configured,
        label: configured,
        description: "Your gateway's configured default model",
      },
    );
  }
  return choices;
}

export function defaultGatewayTestModel(
  provider: string,
  configuredModel = "",
) {
  const configured = configuredModel.trim();
  if (configured) return configured;
  const choices = gatewayModelChoices(provider);
  return (
    (
      choices.find((choice) =>
        /haiku|luna|flash|instant|20b/i.test(choice.label),
      ) ?? choices[0]
    )?.model ?? ""
  );
}

export function matchesGatewayModel(choice: ModelChoice, query: string) {
  return `${choice.label} ${choice.model} ${choice.description}`
    .toLowerCase()
    .includes(query.trim().toLowerCase());
}

export type GatewayModelListing = {
  provider: string;
  source: "provider" | "catalog";
  models: { id: string; name?: string }[];
};

// Include older IDs when reported by this account. A public catalogue must not
// invent provider account access.
export function listedGatewayModelChoices(
  listing: GatewayModelListing,
): ModelChoice[] {
  if (listing.source === "catalog")
    return gatewayModelChoices(listing.provider);
  return listing.models.map(({ id, name }) => {
    const known = MODEL_CATALOG.find(
      (item) => item.provider === listing.provider && item.model === id,
    );
    return (
      known ?? {
        provider: listing.provider as ModelChoice["provider"],
        model: id,
        label: name || id,
        description: "Reported by your provider key",
      }
    );
  });
}

export function defaultListedGatewayModel(
  choices: ModelChoice[],
  configured = "",
): string {
  if (choices.some((choice) => choice.model === configured.trim()))
    return configured.trim();
  // A tiny hello does not need a large reasoning model. Only select a reported ID.
  return (
    (
      choices.find((choice) =>
        /mini|nano|haiku|flash|instant|20b/i.test(choice.model),
      ) ?? choices[0]
    )?.model ?? ""
  );
}
