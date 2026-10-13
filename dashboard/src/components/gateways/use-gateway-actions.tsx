import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ArrowRight } from "lucide-react";
import {
  FieldLabel,
  SecurityNotice,
} from "@/components/admin/admin-primitives";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { api } from "@/api/client";
import type {
  CreateProviderKeyRequest,
  ProviderKey,
  Proxy,
} from "@/api/client";
import { providerLabels } from "@/components/gateway-setup";
import { controlClass, CopyButton, ErrorMessage } from "./primitives";
import { EnvironmentPicker } from "./environment-picker";

export function useGatewayActions(onCreated: (id: string) => void) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [createOpen, setCreateOpen] = useState(false);
  const [name, setName] = useState("");
  const [provider, setProvider] = useState("openai");
  const [model, setModel] = useState("");
  const [providerDialog, setProviderDialog] = useState<string | null>(null);
  const [providerSecret, setProviderSecret] = useState("");
  const [deleteKey, setDeleteKey] = useState<ProviderKey | null>(null);
  const [deleteProxy, setDeleteProxy] = useState<Proxy | null>(null);
  const [appKeyProxy, setAppKeyProxy] = useState<Proxy | null>(null);
  const [keyName, setKeyName] = useState("");
  const [keyEnvironment, setKeyEnvironment] = useState("");
  const [rateLimit, setRateLimit] = useState("");
  const [newSecret, setNewSecret] = useState<string | null>(null);

  const create = useMutation({
    mutationFn: api.proxies.create,
    onSuccess: async (p) => {
      await qc.invalidateQueries({ queryKey: ["proxies"] });
      setCreateOpen(false);
      setName("");
      setModel("");
      onCreated(p.id);
    },
  });
  const storeProvider = useMutation({
    mutationFn: api.providerKeys.create,
    onSuccess: async () => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: ["provider-keys"] }),
        qc.invalidateQueries({ queryKey: ["proxy-provider-keys"] }),
        qc.invalidateQueries({ queryKey: ["workspace", "provider-readiness"] }),
      ]);
      setProviderDialog(null);
      setProviderSecret("");
    },
  });
  const removeProvider = useMutation({
    mutationFn: api.providerKeys.delete,
    onSuccess: async () => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: ["provider-keys"] }),
        qc.invalidateQueries({ queryKey: ["proxy-provider-keys"] }),
        qc.invalidateQueries({ queryKey: ["workspace", "provider-readiness"] }),
      ]);
      setDeleteKey(null);
    },
  });
  const removeGateway = useMutation({
    mutationFn: api.proxies.delete,
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["proxies"] });
      setDeleteProxy(null);
      void navigate({ to: "/proxies", search: {} });
    },
  });
  const createAppKey = useMutation({
    mutationFn: api.apiKeys.create,
    onSuccess: async (data) => {
      setNewSecret(data.key);
      setAppKeyProxy(null);
      await qc.invalidateQueries({ queryKey: ["api-keys"] });
    },
  });
  const openProviderDialog = (value: string) => {
    storeProvider.reset();
    setProviderSecret("");
    setProviderDialog(value);
  };
  const openAppKey = (p: Proxy) => {
    createAppKey.reset();
    setKeyName(`${p.name} app`);
    setKeyEnvironment("");
    setRateLimit("");
    setAppKeyProxy(p);
  };

  return {
    openCreate: () => {
      create.reset();
      setCreateOpen(true);
    },
    openProviderDialog,
    openAppKey,
    deleteProvider: (key: ProviderKey) => {
      removeProvider.reset();
      setDeleteKey(key);
    },
    deleteGateway: (proxy: Proxy) => {
      removeGateway.reset();
      setDeleteProxy(proxy);
    },
    dialogs: (
      <>
        <Dialog open={createOpen} onOpenChange={setCreateOpen}>
          <DialogContent className="sm:max-w-lg">
            <DialogHeader>
              <DialogTitle>Create a gateway</DialogTitle>
              <DialogDescription>
                Step 1 of 3 · Choose where your application’s AI requests go.
              </DialogDescription>
            </DialogHeader>
            <form
              className="space-y-4"
              onSubmit={(e) => {
                e.preventDefault();
                create.mutate({
                  name: name.trim(),
                  target_provider:
                    provider as CreateProviderKeyRequest["provider"],
                  target_model: model.trim(),
                });
              }}
            >
              <div>
                <FieldLabel htmlFor="gateway-name">Gateway name</FieldLabel>
                <Input
                  id="gateway-name"
                  required
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="e.g. Customer support"
                />
                <p className="mt-1.5 text-xs text-muted-foreground">
                  A name that helps your team identify this application.
                </p>
              </div>
              <div>
                <FieldLabel htmlFor="gateway-provider">AI provider</FieldLabel>
                <select
                  id="gateway-provider"
                  className={controlClass}
                  value={provider}
                  onChange={(e) => setProvider(e.target.value)}
                >
                  {Object.entries(providerLabels).map(([id, label]) => (
                    <option key={id} value={id}>
                      {label}
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <FieldLabel htmlFor="gateway-model" optional>
                  Default model
                </FieldLabel>
                <Input
                  id="gateway-model"
                  value={model}
                  onChange={(e) => setModel(e.target.value)}
                  placeholder="Model ID from your provider"
                />
                <p className="mt-1.5 text-xs leading-relaxed text-muted-foreground">
                  Leave empty if your app supplies a model with each request. A
                  request’s model takes precedence over this default.
                </p>
              </div>
              <p className="rounded-lg bg-muted p-3 text-xs leading-relaxed text-muted-foreground">
                Next, we’ll help you add the provider key and connect your
                application.
              </p>
              <ErrorMessage error={create.error} />
              <DialogFooter>
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => setCreateOpen(false)}
                >
                  Cancel
                </Button>
                <Button
                  type="submit"
                  disabled={!name.trim() || create.isPending}
                >
                  {create.isPending ? "Creating…" : "Create & continue"}
                  <ArrowRight />
                </Button>
              </DialogFooter>
            </form>
          </DialogContent>
        </Dialog>

        <Dialog
          open={providerDialog !== null}
          onOpenChange={(open) => {
            if (!open) {
              setProviderDialog(null);
              setProviderSecret("");
              storeProvider.reset();
            }
          }}
        >
          <DialogContent className="sm:max-w-lg">
            <DialogHeader>
              <DialogTitle>Add a provider key</DialogTitle>
              <DialogDescription>
                Paste a secret from your AI provider’s account. Bastio uses it
                to make requests to that provider.
              </DialogDescription>
            </DialogHeader>
            <form
              className="space-y-4"
              onSubmit={(e) => {
                e.preventDefault();
                if (providerDialog)
                  storeProvider.mutate({
                    provider:
                      providerDialog as CreateProviderKeyRequest["provider"],
                    key: providerSecret.trim(),
                  });
              }}
            >
              <div>
                <FieldLabel htmlFor="credential-provider">
                  AI provider
                </FieldLabel>
                <select
                  id="credential-provider"
                  className={controlClass}
                  value={providerDialog ?? "openai"}
                  onChange={(e) => {
                    setProviderDialog(e.target.value);
                    storeProvider.reset();
                  }}
                >
                  {Object.entries(providerLabels).map(([id, label]) => (
                    <option key={id} value={id}>
                      {label}
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <FieldLabel htmlFor="provider-secret">
                  Provider secret key
                </FieldLabel>
                <Input
                  id="provider-secret"
                  type="password"
                  autoComplete="off"
                  required
                  value={providerSecret}
                  onChange={(e) => setProviderSecret(e.target.value)}
                  placeholder="Paste the key from your provider"
                />
                {providerDialog === "bedrock" ? (
                  <p className="mt-1.5 text-xs text-muted-foreground">
                    Format: region:access_key_id:secret_access_key. Bedrock can
                    also use credentials configured on the Bastio server.
                  </p>
                ) : null}
              </div>
              <SecurityNotice title="Saved as a shared provider default">
                Other gateways for this provider and workspace AI using your own
                keys can use this secret. Only a masked version is shown after
                saving. Existing keys remain until you remove them.
              </SecurityNotice>
              <ErrorMessage error={storeProvider.error} />
              <DialogFooter>
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => {
                    setProviderDialog(null);
                    setProviderSecret("");
                  }}
                >
                  Cancel
                </Button>
                <Button
                  type="submit"
                  disabled={!providerSecret.trim() || storeProvider.isPending}
                >
                  {storeProvider.isPending ? "Saving…" : "Save provider key"}
                </Button>
              </DialogFooter>
            </form>
          </DialogContent>
        </Dialog>

        <Dialog
          open={appKeyProxy !== null}
          onOpenChange={(open) => {
            if (!open) setAppKeyProxy(null);
          }}
        >
          <DialogContent className="sm:max-w-lg">
            <DialogHeader>
              <DialogTitle>Create a Bastio API key</DialogTitle>
              <DialogDescription>
                Your application uses this key to call {appKeyProxy?.name}. It
                can access only this gateway.
              </DialogDescription>
            </DialogHeader>
            <form
              className="space-y-4"
              onSubmit={(e) => {
                e.preventDefault();
                if (appKeyProxy)
                  createAppKey.mutate({
                    name: keyName.trim(),
                    proxy_id: appKeyProxy.id,
                    environment: keyEnvironment,
                    allow_environment_override: false,
                    ...(rateLimit ? { rate_limit_rpm: Number(rateLimit) } : {}),
                  });
              }}
            >
              <div>
                <FieldLabel htmlFor="app-key-name">
                  Application key name
                </FieldLabel>
                <Input
                  id="app-key-name"
                  required
                  value={keyName}
                  onChange={(e) => setKeyName(e.target.value)}
                  placeholder="e.g. Support app production"
                />
              </div>
              <EnvironmentPicker
                value={keyEnvironment}
                onChange={setKeyEnvironment}
              />
              <div>
                <FieldLabel htmlFor="app-key-limit" optional>
                  Requests per minute
                </FieldLabel>
                <Input
                  id="app-key-limit"
                  type="number"
                  min="1"
                  step="1"
                  value={rateLimit}
                  onChange={(e) => setRateLimit(e.target.value)}
                  placeholder="No limit"
                />
              </div>
              <ErrorMessage error={createAppKey.error} />
              <DialogFooter>
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => setAppKeyProxy(null)}
                >
                  Cancel
                </Button>
                <Button
                  type="submit"
                  disabled={
                    !keyName.trim() || !keyEnvironment || createAppKey.isPending
                  }
                >
                  {createAppKey.isPending ? "Creating…" : "Create API key"}
                </Button>
              </DialogFooter>
            </form>
          </DialogContent>
        </Dialog>

        <Dialog
          open={newSecret !== null}
          onOpenChange={(open) => {
            if (!open) {
              setNewSecret(null);
              createAppKey.reset();
            }
          }}
        >
          <DialogContent className="sm:max-w-xl">
            <DialogHeader>
              <DialogTitle>Save your Bastio API key</DialogTitle>
              <DialogDescription>
                This is the only time the full key is shown. Store it in your
                app’s secrets manager as BASTIO_API_KEY, then use the connection
                example.
              </DialogDescription>
            </DialogHeader>
            <div className="flex items-start gap-2 rounded-lg border border-border bg-muted p-3">
              <code className="min-w-0 flex-1 break-all font-mono text-sm">
                {newSecret}
              </code>
              <CopyButton
                value={newSecret ?? ""}
                label="Copy new Bastio API key"
              />
            </div>
            <DialogFooter>
              <Button
                onClick={() => {
                  setNewSecret(null);
                  createAppKey.reset();
                }}
              >
                I’ve saved the key
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        <Dialog
          open={deleteKey !== null}
          onOpenChange={(open) => !open && setDeleteKey(null)}
        >
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Remove provider key?</DialogTitle>
              <DialogDescription>
                {deleteKey?.key_masked} will be removed.{" "}
                {deleteKey?.proxy_id
                  ? "Requests through its gateway may stop working."
                  : "This is a shared provider key. Other gateways and workspace AI using this credential may stop working."}{" "}
                This cannot be undone.
              </DialogDescription>
            </DialogHeader>
            <ErrorMessage error={removeProvider.error} />
            <DialogFooter>
              <Button variant="outline" onClick={() => setDeleteKey(null)}>
                Cancel
              </Button>
              <Button
                variant="destructive"
                disabled={removeProvider.isPending}
                onClick={() => deleteKey && removeProvider.mutate(deleteKey.id)}
              >
                {removeProvider.isPending ? "Removing…" : "Remove provider key"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
        <Dialog
          open={deleteProxy !== null}
          onOpenChange={(open) => !open && setDeleteProxy(null)}
        >
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Delete {deleteProxy?.name}?</DialogTitle>
              <DialogDescription>
                Applications using this gateway will stop working. Shared
                provider keys are kept. This cannot be undone.
              </DialogDescription>
            </DialogHeader>
            <ErrorMessage error={removeGateway.error} />
            <DialogFooter>
              <Button variant="outline" onClick={() => setDeleteProxy(null)}>
                Cancel
              </Button>
              <Button
                variant="destructive"
                disabled={removeGateway.isPending}
                onClick={() =>
                  deleteProxy && removeGateway.mutate(deleteProxy.id)
                }
              >
                {removeGateway.isPending ? "Deleting…" : "Delete gateway"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </>
    ),
  };
}
