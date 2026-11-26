import { useState } from "react";
import { Link, useBlocker } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ArrowUpRight, Plus, ShieldCheck, Trash2 } from "lucide-react";
import {
  AdminUnderlineNav,
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
import { useGatewayExtension, useGatewayOrigin } from "./extension-context";
import { GatewayTestDialog } from "./gateway-test-dialog";
import { ConnectionExample } from "./connection-example";
import {
  CopyButton,
  ErrorMessage,
  SetupCard,
  QueryError,
  ProviderKeyList,
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
  const origin = useGatewayOrigin();
  const connection = gatewayConnection(proxy, origin);
  const status = gatewaySetupStatus(
    proxy,
    providerKeys,
    appKeys,
    extension.managedProviders,
  );
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
              type="button"
              variant="outline"
              onClick={() => blocker.status === "blocked" && blocker.reset()}
            >
              Keep editing
            </Button>
            <Button
              type="button"
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
          <div className="flex flex-wrap items-center gap-2.5">
            <h2 className="break-words text-[18px] font-medium tracking-tight text-foreground">
              {proxy.name}
            </h2>
            <span className="inline-flex items-center gap-1.5 text-[11px] text-muted-foreground">
              <span
                aria-hidden="true"
                className={cn(
                  "size-1.5 rounded-full",
                  proxy.is_active ? "bg-success" : "bg-muted-foreground/40",
                )}
              />
              {proxy.is_active ? "Enabled" : "Disabled"}
            </span>
          </div>
          <p className="mt-1 text-[12px] text-muted-foreground">
            {providerLabels[proxy.target_provider] ?? proxy.target_provider} ·{" "}
            <span className="font-mono">
              {proxy.target_model || "App supplies model"}
            </span>
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <GatewayTestDialog proxy={proxy} />
          <Button
            nativeButton={false}
            size="sm"
            variant="outline"
            className="cursor-pointer"
            render={<Link to="/proxies/$id" params={{ id: proxy.id }} />}
          >
            <ArrowUpRight /> View traffic
          </Button>
        </div>
      </header>

      <AdminUnderlineNav
        ariaLabel={`${proxy.name} configuration`}
        value={section}
        onChange={setSection}
        className="mb-4"
        items={[
          { id: "setup", label: "Setup & connection" },
          { id: "settings", label: "Gateway settings" },
        ]}
      />

      {section === "settings" ? (
        <div className="divide-y divide-border-subtle">
          <form
            className="space-y-4 pb-6"
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
                autoCorrect="off"
                autoCapitalize="none"
                spellCheck={false}
                aria-describedby="edit-gateway-model-hint"
                onChange={(e) => setModel(e.target.value)}
                placeholder="App supplies the model"
                className="font-mono text-xs"
              />
              <p
                id="edit-gateway-model-hint"
                className="mt-1.5 text-[11px] text-muted-foreground"
              >
                Used when the request does not include a model.
              </p>
            </div>
            <ErrorMessage error={update.error} />
            <div className="flex items-center gap-3">
              <Button
                type="submit"
                size="sm"
                disabled={!dirty || !name.trim() || update.isPending}
              >
                {update.isPending ? "Saving…" : "Save gateway settings"}
              </Button>
              {update.isSuccess && !dirty ? (
                <span role="status" className="text-xs text-success">
                  Saved
                </span>
              ) : null}
            </div>
          </form>

          <div className="flex flex-wrap items-center justify-between gap-3 py-5">
            <div>
              <h3 className="text-[13px] font-medium text-foreground">
                {proxy.is_active ? "Disable gateway" : "Enable gateway"}
              </h3>
              <p className="mt-1 text-[12px] text-muted-foreground">
                {proxy.is_active
                  ? "Stops new requests. You can enable it again later."
                  : "Allows applications to send requests through this gateway."}
              </p>
            </div>
            <Button
              type="button"
              size="sm"
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

          <div className="flex flex-wrap items-center justify-between gap-3 py-5">
            <div>
              <h3 className="text-[13px] font-medium text-foreground">
                Delete gateway
              </h3>
              <p className="mt-1 text-[12px] text-muted-foreground">
                Permanently removes this route. Shared provider keys remain.
              </p>
            </div>
            <Button
              type="button"
              size="sm"
              variant="destructive"
              onClick={onDelete}
            >
              <Trash2 /> Delete gateway
            </Button>
          </div>
        </div>
      ) : (
        <div className="space-y-5">
          {status === "Setup complete" && !providerError && !appError ? (
            <p className="flex items-center gap-2 text-[12px] text-success">
              <ShieldCheck className="size-3.5" aria-hidden="true" /> Setup
              complete
              <span className="text-muted-foreground">
                · Send a request below to verify the connection.
              </span>
            </p>
          ) : !proxy.is_active ? (
            <SecurityNotice title="This gateway is disabled" tone="warning">
              Enable it in Gateway settings before sending requests.
            </SecurityNotice>
          ) : null}

          <div className="grid divide-y divide-border-subtle border-y border-border-subtle xl:grid-cols-2 xl:divide-x xl:divide-y-0">
            <div className="xl:pr-6">
              <SetupCard
                step={2}
                title="Provider key"
                description={`Bastio → ${providerLabels[proxy.target_provider] ?? proxy.target_provider}. Secret from your provider’s account.`}
                badge={
                  !providerError && providerKeys ? (
                    <Badge
                      variant={keys.length ? "success" : "outline"}
                      className="text-[10px]"
                    >
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
                  <p className="text-xs text-muted-foreground">
                    Checking provider keys…
                  </p>
                ) : (
                  <>
                    {keys.length ? (
                      <ProviderKeyList
                        keys={keys}
                        onDelete={onDeleteProvider}
                      />
                    ) : (
                      <p className="text-[12px] leading-relaxed text-muted-foreground">
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
                      type="button"
                      size="sm"
                      disabled={extension.providerKeyManagement !== "allowed"}
                      variant="outline"
                      className="cursor-pointer"
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
            </div>

            <div className="xl:pl-6">
              <SetupCard
                step={3}
                title="Bastio API key"
                description="Your app → Bastio. Give each application its own key, restricted to this gateway."
                badge={
                  !appError && appKeys ? (
                    <Badge
                      variant={scopedKeys.length ? "success" : "outline"}
                      className="text-[10px]"
                    >
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
                  <p className="text-xs text-muted-foreground">
                    Checking application keys…
                  </p>
                ) : (
                  <>
                    {scopedKeys.length ? (
                      <ul className="divide-y divide-border-subtle border-y border-border-subtle">
                        {scopedKeys.map((key) => (
                          <li key={key.id} className="py-2.5">
                            <p className="break-words text-[13px] font-medium text-foreground">
                              {key.name}
                            </p>
                            <p className="mt-0.5 break-all font-mono text-[11px] text-muted-foreground">
                              {key.key_prefix}… · {key.environment}
                              {key.rate_limit_rpm
                                ? ` · ${key.rate_limit_rpm} rpm`
                                : ""}
                            </p>
                          </li>
                        ))}
                      </ul>
                    ) : (
                      <p className="text-[12px] leading-relaxed text-muted-foreground">
                        No application key restricted to this gateway yet.
                        {globalKeys.length
                          ? " Existing global keys can also access it, but a dedicated key limits access."
                          : " Create one, then store the secret in your application."}
                      </p>
                    )}
                    <div className="flex flex-wrap items-center gap-3">
                      <Button
                        type="button"
                        size="sm"
                        variant={scopedKeys.length ? "outline" : "default"}
                        className="cursor-pointer"
                        onClick={onCreateKey}
                      >
                        <Plus /> Create Bastio API key
                      </Button>
                      <Link
                        to="/api-keys"
                        className="text-[11px] text-muted-foreground underline underline-offset-4 hover:text-foreground"
                      >
                        Manage access &amp; limits
                      </Link>
                    </div>
                  </>
                )}
              </SetupCard>
            </div>
          </div>

          <section className="min-w-0 space-y-4 pt-2">
            <div>
              <h3 className="text-[13px] font-medium text-foreground">
                Connect your application
              </h3>
              <p className="mt-1 text-[12px] leading-relaxed text-muted-foreground">
                Set{" "}
                <code className="font-mono text-[11px] text-foreground">
                  BASTIO_API_KEY
                </code>{" "}
                to your Bastio key in your app’s environment. Use this URL
                instead of your provider’s URL.
              </p>
            </div>

            <div>
              <p className="mb-1.5 text-[11px] font-medium text-muted-foreground">
                {proxy.target_provider === "anthropic"
                  ? "Anthropic SDK base URL"
                  : "SDK base URL"}
              </p>
              <div className="flex items-center gap-2 rounded-md border border-border-subtle bg-surface-1/60 px-3 py-2">
                <code className="min-w-0 flex-1 break-all font-mono text-xs leading-relaxed text-foreground">
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

            <ConnectionExample proxy={proxy} />

            <p className="text-[11px] leading-relaxed text-muted-foreground">
              After sending a request, open{" "}
              <Link
                to="/proxies/$id"
                params={{ id: proxy.id }}
                className="text-foreground underline underline-offset-4"
              >
                View traffic
              </Link>{" "}
              to inspect its result. Security behavior is configured in the{" "}
              <Link
                to="/security-settings"
                className="text-foreground underline underline-offset-4"
              >
                Security Center
              </Link>
              .
            </p>
          </section>
        </div>
      )}
    </section>
  );
}
