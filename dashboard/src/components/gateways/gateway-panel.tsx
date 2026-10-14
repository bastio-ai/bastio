import { useState } from "react";
import { Link, useBlocker } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ArrowUpRight, Plus, ShieldCheck, Trash2 } from "lucide-react";
import {
  FieldLabel,
  SecurityNotice,
} from "@/components/admin/admin-primitives";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { api } from "@/api/client";
import type {
  APIKey,
  CreateProviderKeyRequest,
  ProviderKey,
  Proxy,
} from "@/api/client";
import {
  availableProviderKeys,
  gatewayAppKeys,
  gatewayConnection,
  gatewaySetupStatus,
  providerLabels,
} from "@/components/gateway-setup";
import { cn } from "@/lib/utils";
import { useGatewayExtension } from "./extension-context";
import { GatewayTestDialog } from "./gateway-test-dialog";
import {
  CopyButton,
  ErrorMessage,
  SetupCard,
  QueryError,
  ProviderKeyList,
  navigationClass,
} from "./primitives";
export function GatewayPanel({
  proxy,
  providerKeys,
  appKeys,
  providerError,
  appError,
  retryProvider,
  retryApp,
  onAddProvider,
  onCreateKey,
  onDeleteProvider,
  onDelete,
}: {
  proxy: Proxy;
  providerKeys?: ProviderKey[];
  appKeys?: APIKey[];
  providerError: Error | null;
  appError: Error | null;
  retryProvider: () => void;
  retryApp: () => void;
  onAddProvider: () => void;
  onCreateKey: () => void;
  onDeleteProvider: (key: ProviderKey) => void;
  onDelete: () => void;
}) {
  const extension = useGatewayExtension();
  const qc = useQueryClient();
  const [section, setSection] = useState<"setup" | "settings">("setup");
  const [name, setName] = useState(proxy.name);
  const [model, setModel] = useState(proxy.target_model);
  const [example, setExample] = useState<"curl" | "python">("curl");
  const update = useMutation({
    mutationFn: (active: boolean) =>
      api.proxies.update(proxy.id, {
        name: name.trim(),
        target_provider:
          proxy.target_provider as CreateProviderKeyRequest["provider"],
        target_model: model.trim(),
        is_active: active,
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["proxies"] }),
  });
  const toggle = useMutation({
    mutationFn: () =>
      api.proxies.update(proxy.id, {
        name: proxy.name,
        target_provider:
          proxy.target_provider as CreateProviderKeyRequest["provider"],
        target_model: proxy.target_model,
        is_active: !proxy.is_active,
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["proxies"] }),
  });
  const keys = availableProviderKeys(proxy, providerKeys ?? []);
  const scopedKeys = gatewayAppKeys(proxy, appKeys ?? []);
  const globalKeys = (appKeys ?? []).filter(
    (key) =>
      key.is_active &&
      !(key.scopes ?? []).some((scope) => scope.startsWith("proxy:")),
  );
  const serverAuth = ["ollama", "bedrock"].includes(proxy.target_provider);
  const managedAuth = extension.managedProviders.includes(
    proxy.target_provider,
  );
  const configured = keys.length > 0 || serverAuth || managedAuth;
  const origin = new URL(
    import.meta.env.VITE_API_URL || window.location.origin,
    window.location.origin,
  ).href.replace(/\/$/, "");
  const connection = gatewayConnection(proxy, origin);
  const status = gatewaySetupStatus(
    proxy,
    providerKeys,
    appKeys,
    extension.managedProviders,
  );
  const snippet = example === "curl" ? connection.curl : connection.python;
  const dirty =
    name.trim() !== proxy.name || model.trim() !== proxy.target_model;
  const blocker = useBlocker({
    shouldBlockFn: ({ next }) =>
      dirty &&
      (next.pathname !== "/proxies" ||
        (next.search as { tab?: string }).tab === "providers" ||
        (next.search as { gateway?: string }).gateway !== proxy.id),
    enableBeforeUnload: dirty,
    withResolver: true,
  });

  return (
    <section className="min-w-0 space-y-5">
      <Dialog
        open={blocker.status === "blocked"}
        onOpenChange={(open) => {
          if (!open && blocker.status === "blocked") blocker.reset();
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Leave without saving?</DialogTitle>
            <DialogDescription>
              Your changes to this gateway’s name or default model have not been
              saved.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => blocker.status === "blocked" && blocker.reset()}
            >
              Keep editing
            </Button>
            <Button
              variant="destructive"
              onClick={() => blocker.status === "blocked" && blocker.proceed()}
            >
              Discard changes
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="break-words text-xl font-bold tracking-tight">
              {proxy.name}
            </h2>
            <Badge variant={proxy.is_active ? "outline" : "secondary"}>
              {proxy.is_active ? "Enabled" : "Disabled"}
            </Badge>
          </div>
          <p className="mt-1 text-sm text-muted-foreground">
            {providerLabels[proxy.target_provider] ?? proxy.target_provider} ·{" "}
            {proxy.target_model || "App supplies the model"}
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <GatewayTestDialog proxy={proxy} />
          <Button
            nativeButton={false}
            variant="outline"
            className="cursor-pointer"
            render={<Link to="/proxies/$id" params={{ id: proxy.id }} />}
          >
            <ArrowUpRight /> View traffic
          </Button>
        </div>
      </header>
      <nav
        aria-label={`${proxy.name} configuration`}
        className="flex gap-1 border-b border-border pb-2"
      >
        <button
          type="button"
          aria-current={section === "setup" ? "page" : undefined}
          onClick={() => setSection("setup")}
          className={cn(navigationClass, section === "setup" && "bg-muted")}
        >
          Setup & connection
        </button>
        <button
          type="button"
          aria-current={section === "settings" ? "page" : undefined}
          onClick={() => setSection("settings")}
          className={cn(navigationClass, section === "settings" && "bg-muted")}
        >
          Gateway settings
        </button>
      </nav>
      {section === "settings" ? (
        <div className="space-y-5 rounded-xl border border-border bg-card p-5">
          <form
            className="space-y-4"
            onSubmit={(e) => {
              e.preventDefault();
              update.mutate(proxy.is_active);
            }}
          >
            <div>
              <FieldLabel htmlFor="edit-gateway-name">Gateway name</FieldLabel>
              <Input
                id="edit-gateway-name"
                required
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </div>
            <div>
              <FieldLabel htmlFor="edit-gateway-model" optional>
                Default model
              </FieldLabel>
              <Input
                id="edit-gateway-model"
                value={model}
                onChange={(e) => setModel(e.target.value)}
                placeholder="App supplies the model"
              />
              <p className="mt-1.5 text-xs text-muted-foreground">
                Used when the request does not include a model.
              </p>
            </div>
            <ErrorMessage error={update.error} />
            <Button
              type="submit"
              disabled={!dirty || !name.trim() || update.isPending}
            >
              {update.isPending ? "Saving…" : "Save gateway settings"}
            </Button>
            {update.isSuccess && !dirty ? (
              <span role="status" className="ml-3 text-sm text-success">
                Saved
              </span>
            ) : null}
          </form>
          <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border pt-4">
            <div>
              <h3 className="text-sm font-bold tracking-tight">
                {proxy.is_active ? "Disable gateway" : "Enable gateway"}
              </h3>
              <p className="mt-1 text-xs text-muted-foreground">
                {proxy.is_active
                  ? "Stops new requests. You can enable it again later."
                  : "Allows applications to send requests through this gateway."}
              </p>
            </div>
            <Button
              variant="outline"
              disabled={toggle.isPending}
              onClick={() => toggle.mutate()}
            >
              {toggle.isPending
                ? "Updating…"
                : proxy.is_active
                  ? "Disable gateway"
                  : "Enable gateway"}
            </Button>
          </div>
          <ErrorMessage error={toggle.error} />
          <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border pt-4">
            <div>
              <h3 className="text-sm font-bold tracking-tight">
                Delete gateway
              </h3>
              <p className="mt-1 text-xs text-muted-foreground">
                Permanently removes this route. Shared provider keys remain.
              </p>
            </div>
            <Button variant="destructive" onClick={onDelete}>
              <Trash2 /> Delete gateway
            </Button>
          </div>
        </div>
      ) : (
        <>
          {status === "Setup complete" && !providerError && !appError ? (
            <p className="flex items-center gap-2 text-sm text-success">
              <ShieldCheck className="size-4" /> Setup complete{" "}
              <span className="text-xs text-muted-foreground">
                · Send a request below to verify the connection.
              </span>
            </p>
          ) : !proxy.is_active ? (
            <SecurityNotice title="This gateway is disabled" tone="warning">
              Enable it in Gateway settings before sending requests.
            </SecurityNotice>
          ) : null}
          <div className="grid gap-4 xl:grid-cols-2">
            <SetupCard
              step={2}
              title="Provider key"
              description={`Bastio → ${providerLabels[proxy.target_provider] ?? proxy.target_provider}. This secret comes from your provider’s account.`}
              badge={
                !providerError && providerKeys ? (
                  <Badge variant={keys.length ? "success" : "outline"}>
                    {keys.length
                      ? "Saved"
                      : managedAuth
                        ? "Bastio-managed"
                        : serverAuth
                          ? "Server auth"
                          : "Needed"}
                  </Badge>
                ) : null
              }
            >
              {providerError ? (
                <QueryError
                  title="Provider keys unavailable"
                  error={providerError}
                  retry={retryProvider}
                />
              ) : !providerKeys ? (
                <p className="text-sm text-muted-foreground">
                  Checking provider keys…
                </p>
              ) : (
                <>
                  {keys.length ? (
                    <ProviderKeyList keys={keys} onDelete={onDeleteProvider} />
                  ) : (
                    <p className="text-sm leading-relaxed text-muted-foreground">
                      {managedAuth
                        ? "Uses a provider credential configured by Bastio on this server."
                        : serverAuth
                          ? "Uses credentials or a local endpoint configured on the Bastio server. Confirm that server configuration before sending requests."
                          : extension.providerKeyManagement === "restricted"
                            ? "This provider has no stored credential, and your plan cannot add one. Review provider access and billing above."
                            : "Add a provider key to let Bastio call this provider. The key is stored as a shared default."}
                    </p>
                  )}
                  <Button
                    disabled={extension.providerKeyManagement !== "allowed"}
                    variant="outline"
                    className="w-full cursor-pointer"
                    onClick={onAddProvider}
                  >
                    <Plus />{" "}
                    {keys.length
                      ? "Add another provider key"
                      : "Add provider key"}
                  </Button>
                </>
              )}
            </SetupCard>
            <SetupCard
              step={3}
              title="Bastio API key"
              description="Your app → Bastio. Give each application its own key, restricted to this gateway."
              badge={
                !appError && appKeys ? (
                  <Badge variant={scopedKeys.length ? "success" : "outline"}>
                    {scopedKeys.length
                      ? `${scopedKeys.length} active`
                      : "Needed"}
                  </Badge>
                ) : null
              }
            >
              {appError ? (
                <QueryError
                  title="Bastio API keys unavailable"
                  error={appError}
                  retry={retryApp}
                />
              ) : !appKeys ? (
                <p className="text-sm text-muted-foreground">
                  Checking application keys…
                </p>
              ) : (
                <>
                  {scopedKeys.length ? (
                    <ul className="divide-y divide-border rounded-lg border border-border">
                      {scopedKeys.map((key) => (
                        <li key={key.id} className="p-3">
                          <p className="break-words text-sm font-medium">
                            {key.name}
                          </p>
                          <p className="mt-1 break-all font-mono text-xs text-muted-foreground">
                            {key.key_prefix}… · {key.environment}
                            {key.rate_limit_rpm
                              ? ` · ${key.rate_limit_rpm} rpm`
                              : ""}
                          </p>
                        </li>
                      ))}
                    </ul>
                  ) : (
                    <p className="text-sm leading-relaxed text-muted-foreground">
                      No application key restricted to this gateway yet.
                      {globalKeys.length
                        ? " Existing global keys can also access it, but a dedicated key limits access."
                        : " Create one, then store the secret in your application."}
                    </p>
                  )}
                  <Button
                    variant={scopedKeys.length ? "outline" : "default"}
                    className="w-full cursor-pointer"
                    onClick={onCreateKey}
                  >
                    <Plus /> Create Bastio API key
                  </Button>
                  <Link
                    to="/api-keys"
                    className="block text-center text-xs text-muted-foreground underline underline-offset-4 hover:text-foreground"
                  >
                    Manage access, limits & revocation
                  </Link>
                </>
              )}
            </SetupCard>
          </div>
          <section className="min-w-0 overflow-hidden rounded-xl border border-border bg-card">
            <div className="border-b border-border p-5">
              <h3 className="text-base font-bold tracking-tight">
                Connect your application
              </h3>
              <p className="mt-1 text-sm leading-relaxed text-muted-foreground">
                Set BASTIO_API_KEY to your Bastio key in your app’s environment.
                Use this URL instead of your provider’s URL.
              </p>
            </div>
            <div className="space-y-4 p-5">
              <div>
                <p className="mb-2 text-xs font-medium">
                  {proxy.target_provider === "anthropic"
                    ? "Anthropic SDK base URL"
                    : "SDK base URL"}
                </p>
                <div className="flex items-start gap-2 rounded-lg border border-border bg-muted/40 p-3">
                  <code className="min-w-0 flex-1 break-all font-mono text-xs leading-relaxed">
                    {connection.sdkBaseURL}
                  </code>
                  <CopyButton
                    value={connection.sdkBaseURL}
                    label="Copy SDK base URL"
                  />
                </div>
              </div>
              {!configured && !providerError ? (
                <p className="text-xs text-muted-foreground">
                  Save a provider key before trying the example.
                </p>
              ) : null}
              {!proxy.target_model ? (
                <p className="text-xs text-muted-foreground">
                  Replace YOUR_MODEL_ID with a model ID from{" "}
                  {providerLabels[proxy.target_provider] ??
                    proxy.target_provider}
                  , or set a default in Gateway settings.
                </p>
              ) : null}
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div
                  className="flex gap-1"
                  aria-label="Connection example format"
                >
                  {(["curl", "python"] as const).map((format) => (
                    <button
                      type="button"
                      key={format}
                      aria-pressed={example === format}
                      className={cn(
                        navigationClass,
                        "min-h-9 px-3 text-xs",
                        example === format && "bg-muted",
                      )}
                      onClick={() => setExample(format)}
                    >
                      {format === "curl" ? "cURL" : "Python"}
                    </button>
                  ))}
                </div>
                <CopyButton value={snippet} label="Copy connection example" />
              </div>
              <pre className="max-h-96 overflow-auto rounded-lg border border-border bg-muted/40 p-4 text-xs leading-relaxed">
                <code>{snippet}</code>
              </pre>
              <p className="text-xs leading-relaxed text-muted-foreground">
                After sending a request, open{" "}
                <Link
                  to="/proxies/$id"
                  params={{ id: proxy.id }}
                  className="underline underline-offset-4"
                >
                  View traffic
                </Link>{" "}
                to inspect its result. Security behavior is configured in the{" "}
                <Link
                  to="/security-settings"
                  className="underline underline-offset-4"
                >
                  Security Center
                </Link>
                .
              </p>
            </div>
          </section>
        </>
      )}
    </section>
  );
}
