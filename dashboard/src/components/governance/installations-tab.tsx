import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Download, Trash2, Key, Copy, Check } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { EmptyState } from "@/components/card";
import { api, type GovernanceCreatedInstallation } from "@/api/client";

export function InstallationsTab() {
  const qc = useQueryClient();
  const q = useQuery({
    queryKey: ["governance", "installations"] as const,
    queryFn: () => api.governance.installations.list(),
  });

  const [label, setLabel] = useState("");
  const [created, setCreated] = useState<GovernanceCreatedInstallation | null>(null);
  const [copied, setCopied] = useState<"token" | "secret" | null>(null);

  const create = useMutation({
    mutationFn: () => api.governance.installations.create(label),
    onSuccess: (data) => {
      setCreated(data);
      setLabel("");
      qc.invalidateQueries({ queryKey: ["governance", "installations"] });
    },
  });

  const revoke = useMutation({
    mutationFn: (id: string) => api.governance.installations.revoke(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["governance", "installations"] }),
  });

  const installs = q.data?.installations ?? [];

  const copy = async (kind: "token" | "secret", value: string) => {
    await navigator.clipboard.writeText(value);
    setCopied(kind);
    setTimeout(() => setCopied(null), 1200);
  };

  return (
    <div className="space-y-4">
      <Card>
        <CardContent className="space-y-4 p-6">
          <div>
            <h3 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">
              Generate MDM bundle
            </h3>
            <p className="mt-1 text-xs text-muted-foreground">
              Creates a fresh org_id + installation_token + installation_secret. The secret is shown
              ONCE — store it in your MDM tooling immediately.
            </p>
          </div>
          <div className="flex gap-3">
            <Input
              value={label}
              onChange={(e) => setLabel(e.target.value)}
              placeholder="e.g., Production rollout, EU pilot, Finance team"
              className="max-w-md"
            />
            <Button onClick={() => create.mutate()} disabled={create.isPending} className="gap-2">
              <Plus className="h-4 w-4" />
              {create.isPending ? "Generating…" : "Generate"}
            </Button>
          </div>
          {create.isError && (
            <p className="text-xs text-destructive">{(create.error as Error).message}</p>
          )}
        </CardContent>
      </Card>

      {created && (
        <Card className="border-cyan-500/40 bg-cyan-500/5">
          <CardContent className="space-y-4 p-6">
            <div className="flex items-center gap-2">
              <Key className="h-4 w-4 text-cyan-500" />
              <h3 className="text-sm font-semibold text-cyan-500">
                New installation created — save the secret now
              </h3>
            </div>
            <p className="text-xs text-muted-foreground">{created.warning}</p>
            <div className="space-y-3 rounded-md bg-background p-4">
              <CredentialRow label="Org ID" value={created.org_id} mono />
              <CredentialRow
                label="Installation token"
                value={created.installation_token}
                mono
                onCopy={() => copy("token", created.installation_token)}
                copied={copied === "token"}
              />
              <CredentialRow
                label="Installation secret"
                value={created.installation_secret}
                mono
                sensitive
                onCopy={() => copy("secret", created.installation_secret)}
                copied={copied === "secret"}
              />
            </div>
            <div className="flex gap-2">
              <a
                href={`${api.governance.installations.bundleURL(created.id ?? "")}`}
                download
                className="inline-flex items-center gap-2 rounded-md border border-border bg-transparent px-4 py-2 text-sm font-medium hover:bg-muted"
              >
                <Download className="h-4 w-4" />
                Download MDM bundle (.zip)
              </a>
              <Button variant="ghost" onClick={() => setCreated(null)}>
                Dismiss
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardContent className="p-0">
          <div className="border-b border-border p-4">
            <h3 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">
              Active installations
            </h3>
          </div>
          {q.isLoading ? (
            <div className="p-6 text-sm text-muted-foreground">Loading…</div>
          ) : installs.length === 0 ? (
            <EmptyState
              icon={<Key className="h-6 w-6" />}
              title="No installations yet"
              description="Generate an MDM bundle above to create your first installation, then push it to managed browsers via your MDM tooling."
            />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Label</TableHead>
                  <TableHead>Org ID</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {installs.map((i) => (
                  <TableRow key={i.ID}>
                    <TableCell className="text-sm">{i.Label || "—"}</TableCell>
                    <TableCell className="font-mono text-xs">{i.OrgID}</TableCell>
                    <TableCell className="font-mono text-xs">
                      {new Date(i.CreatedAt).toLocaleString()}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex justify-end gap-1">
                        <a
                          href={`${api.governance.installations.bundleURL(i.ID)}`}
                          download
                          className="inline-flex h-8 items-center gap-1 rounded-md border border-border px-3 text-xs font-medium hover:bg-muted"
                        >
                          <Download className="h-3 w-3" />
                          Bundle
                        </a>
                        <Button
                          size="sm"
                          variant="ghost"
                          onClick={() => {
                            if (confirm("Revoke this installation? Extensions using it will stop authenticating.")) {
                              revoke.mutate(i.ID);
                            }
                          }}
                          disabled={revoke.isPending}
                          className="text-destructive"
                        >
                          <Trash2 className="h-3 w-3" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

interface CredentialRowProps {
  label: string;
  value: string;
  mono?: boolean;
  sensitive?: boolean;
  onCopy?: () => void;
  copied?: boolean;
}

function CredentialRow({ label, value, mono, sensitive, onCopy, copied }: CredentialRowProps) {
  const [revealed, setRevealed] = useState(!sensitive);
  return (
    <div className="flex items-center gap-3">
      <div className="w-40 text-xs uppercase tracking-wide text-muted-foreground">{label}</div>
      <code
        className={`flex-1 break-all rounded bg-muted px-2 py-1 text-xs ${mono ? "font-mono" : ""}`}
      >
        {revealed ? value : "•".repeat(Math.min(value.length, 40))}
      </code>
      {sensitive && (
        <Button size="sm" variant="ghost" onClick={() => setRevealed((r) => !r)}>
          {revealed ? "Hide" : "Reveal"}
        </Button>
      )}
      {onCopy && (
        <Button size="sm" variant="outline" onClick={onCopy} className="gap-1">
          {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
          {copied ? "Copied" : "Copy"}
        </Button>
      )}
    </div>
  );
}
