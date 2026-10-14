import { useState, type ReactNode } from "react";
import { Check, Copy, KeyRound, Pencil, Trash2 } from "lucide-react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import {
  FieldLabel,
  SecurityNotice,
} from "@/components/admin/admin-primitives";
import { api, type ProviderKey } from "@/api/client";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { useGatewayExtension } from "./extension-context";
import { providerLabels } from "@/components/gateway-setup";
export const controlClass =
  "h-10 w-full rounded-lg border border-input bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring";
export const navigationClass =
  "min-h-11 cursor-pointer rounded-lg px-4 py-2 text-sm font-medium transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring";

export function ErrorMessage({ error }: { error: Error | null }) {
  return error ? (
    <p role="alert" className="text-sm text-destructive">
      {error.message}
    </p>
  ) : null;
}

export function CopyButton({ value, label }: { value: string; label: string }) {
  const [copied, setCopied] = useState(false);
  const [failed, setFailed] = useState(false);
  return (
    <div className="shrink-0">
      <Button
        variant="outline"
        className="cursor-pointer"
        aria-label={label}
        onClick={async () => {
          try {
            await navigator.clipboard.writeText(value);
            setCopied(true);
            setFailed(false);
            window.setTimeout(() => setCopied(false), 1800);
          } catch {
            setFailed(true);
          }
        }}
      >
        {copied ? <Check /> : <Copy />}
        {copied ? "Copied" : "Copy"}
      </Button>
      {failed ? (
        <p role="alert" className="mt-1 text-xs text-destructive">
          Select and copy manually.
        </p>
      ) : null}
      <span role="status" className="sr-only">
        {copied ? "Copied to clipboard" : ""}
      </span>
    </div>
  );
}

export function SetupCard({
  step,
  title,
  description,
  badge,
  children,
}: {
  step: number;
  title: string;
  description: string;
  badge?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="min-w-0 rounded-xl border border-border bg-card p-5">
      <div className="flex items-start gap-3">
        <span className="flex size-7 shrink-0 items-center justify-center rounded-full border border-border bg-muted font-mono text-xs">
          {step}
        </span>
        <div className="min-w-0 flex-1">
          <h3 className="text-sm font-bold tracking-tight">{title}</h3>
          <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
            {description}
          </p>
        </div>
        {badge}
      </div>
      <div className="mt-4 space-y-3">{children}</div>
    </section>
  );
}

export function QueryError({
  title,
  error,
  retry,
}: {
  title: string;
  error: Error;
  retry: () => void;
}) {
  return (
    <SecurityNotice title={title} tone="warning">
      <p role="alert" className="break-words">
        {error.message}
      </p>
      <Button variant="outline" className="mt-3 cursor-pointer" onClick={retry}>
        Retry
      </Button>
    </SecurityNotice>
  );
}

export function ProviderKeyList({
  keys,
  onDelete,
}: {
  keys: ProviderKey[];
  onDelete: (key: ProviderKey) => void;
}) {
  const extension = useGatewayExtension();
  const qc = useQueryClient();
  const [target, setTarget] = useState<ProviderKey | null>(null);
  const [label, setLabel] = useState("");
  const rename = useMutation({
    mutationFn: ({ id, name }: { id: string; name: string }) =>
      api.providerKeys.rename(id, name),
    onSuccess: async () => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: ["provider-keys"] }),
        qc.invalidateQueries({ queryKey: ["proxy-provider-keys"] }),
      ]);
      setTarget(null);
    },
  });
  return (
    <>
      <ul className="divide-y divide-border rounded-lg border border-border">
        {keys.map((key) => (
          <li key={key.id} className="flex flex-wrap items-center gap-3 p-3">
            <KeyRound className="size-4 shrink-0 text-muted-foreground" />
            <div className="min-w-0 flex-1">
              <p className="text-sm font-medium">
                {key.name ||
                  `${providerLabels[key.provider] ?? key.provider} key · ${key.id.slice(0, 8)}`}
              </p>
              <p className="mt-1 text-xs text-muted-foreground">
                {providerLabels[key.provider] ?? key.provider}
              </p>
              <code className="mt-1 block break-all font-mono text-xs text-muted-foreground">
                {key.key_masked}
              </code>
              <p className="mt-1 text-xs text-muted-foreground">
                {key.proxy_id
                  ? "Gateway-specific"
                  : key.is_default
                    ? "Shared default"
                    : "Workspace provider key"}{" "}
                · Added {new Date(key.created_at).toLocaleDateString()}
              </p>
            </div>
            <Button
              variant="ghost"
              size="icon"
              aria-label={`Rename ${key.name || key.key_masked}`}
              onClick={() => {
                rename.reset();
                setLabel(key.name || "");
                setTarget(key);
              }}
            >
              <Pencil />
            </Button>
            <Button
              variant="ghost"
              size="icon"
              aria-label={`Remove ${providerLabels[key.provider] ?? key.provider} key ${key.key_masked}`}
              disabled={extension.providerKeyManagement !== "allowed"}
              className="cursor-pointer text-muted-foreground hover:text-destructive"
              onClick={() => onDelete(key)}
            >
              <Trash2 />
            </Button>
          </li>
        ))}
      </ul>
      <Dialog
        open={target !== null}
        onOpenChange={(open) => !open && setTarget(null)}
      >
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>Rename provider key</DialogTitle>
            <DialogDescription>
              Change the label your team sees. The secret and its access stay
              the same.
            </DialogDescription>
          </DialogHeader>
          <form
            className="space-y-4"
            onSubmit={(event) => {
              event.preventDefault();
              if (target) rename.mutate({ id: target.id, name: label.trim() });
            }}
          >
            <div>
              <FieldLabel htmlFor="rename-provider-key">Key label</FieldLabel>
              <Input
                id="rename-provider-key"
                required
                autoFocus
                maxLength={120}
                value={label}
                onChange={(event) => setLabel(event.target.value)}
                placeholder="e.g. OpenAI — Production EU"
              />
            </div>
            <ErrorMessage error={rename.error} />
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => setTarget(null)}
              >
                Cancel
              </Button>
              <Button
                type="submit"
                disabled={!label.trim() || rename.isPending}
              >
                {rename.isPending ? "Saving…" : "Save label"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </>
  );
}
