import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Trash2, Zap, Send } from "lucide-react";

import { Badge } from "@/components/ui/badge";
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
import { api } from "@/api/client";

type WebhookFormat = "slack" | "teams" | "raw_json";
type WebhookTrigger = "severity:high" | "action:overridden" | "any";

const FORMATS: WebhookFormat[] = ["slack", "teams", "raw_json"];
const TRIGGERS: WebhookTrigger[] = ["severity:high", "action:overridden", "any"];

export function WebhooksTab() {
  const qc = useQueryClient();
  const q = useQuery({
    queryKey: ["governance", "webhooks"] as const,
    queryFn: () => api.governance.webhooks.list(),
  });

  const [draft, setDraft] = useState({
    name: "",
    url: "",
    format: "slack" as WebhookFormat,
    trigger: "severity:high" as WebhookTrigger,
  });

  const create = useMutation({
    mutationFn: () => api.governance.webhooks.create(draft),
    onSuccess: () => {
      setDraft({ name: "", url: "", format: "slack", trigger: "severity:high" });
      qc.invalidateQueries({ queryKey: ["governance", "webhooks"] });
    },
  });

  const remove = useMutation({
    mutationFn: (id: string) => api.governance.webhooks.delete(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["governance", "webhooks"] }),
  });

  const test = useMutation({
    mutationFn: (id: string) => api.governance.webhooks.test(id),
  });

  const hooks = q.data?.webhooks ?? [];

  return (
    <div className="space-y-4">
      <Card>
        <CardContent className="space-y-4 p-6">
          <div>
            <h3 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">
              Add webhook
            </h3>
            <p className="mt-1 text-xs text-muted-foreground">
              Fires asynchronously on matching events. Failures are logged on the webhook row.
            </p>
          </div>
          <div className="grid grid-cols-1 gap-4 md:grid-cols-4">
            <div className="space-y-2">
              <label className="text-xs font-medium text-muted-foreground">Name</label>
              <Input
                value={draft.name}
                onChange={(e) => setDraft((d) => ({ ...d, name: e.target.value }))}
                placeholder="soc-team"
              />
            </div>
            <div className="space-y-2 md:col-span-2">
              <label className="text-xs font-medium text-muted-foreground">URL (HTTPS only)</label>
              <Input
                value={draft.url}
                onChange={(e) => setDraft((d) => ({ ...d, url: e.target.value }))}
                placeholder="https://hooks.slack.com/services/…"
              />
            </div>
            <div className="space-y-2">
              <label className="text-xs font-medium text-muted-foreground">Format</label>
              <select
                value={draft.format}
                onChange={(e) => setDraft((d) => ({ ...d, format: e.target.value as WebhookFormat }))}
                className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
              >
                {FORMATS.map((f) => (
                  <option key={f} value={f}>
                    {f}
                  </option>
                ))}
              </select>
            </div>
          </div>
          <div className="grid grid-cols-1 gap-4 md:grid-cols-4">
            <div className="space-y-2">
              <label className="text-xs font-medium text-muted-foreground">Trigger</label>
              <select
                value={draft.trigger}
                onChange={(e) => setDraft((d) => ({ ...d, trigger: e.target.value as WebhookTrigger }))}
                className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
              >
                {TRIGGERS.map((t) => (
                  <option key={t} value={t}>
                    {t}
                  </option>
                ))}
              </select>
            </div>
            <div className="md:col-span-3 flex items-end justify-end">
              <Button
                onClick={() => create.mutate()}
                disabled={create.isPending || !draft.url || !draft.url.startsWith("https://")}
                className="gap-2"
              >
                <Plus className="h-4 w-4" />
                {create.isPending ? "Creating…" : "Add webhook"}
              </Button>
            </div>
          </div>
          {create.isError && (
            <p className="text-xs text-destructive">{(create.error as Error).message}</p>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardContent className="p-0">
          <div className="border-b border-border p-4">
            <h3 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">
              Configured webhooks
            </h3>
          </div>
          {q.isLoading ? (
            <div className="p-6 text-sm text-muted-foreground">Loading…</div>
          ) : hooks.length === 0 ? (
            <EmptyState
              icon={<Zap className="h-6 w-6" />}
              title="No webhooks configured"
              description="Add a Slack, Teams, or generic HTTPS endpoint above to receive real-time notifications when high-severity events fire."
            />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Format</TableHead>
                  <TableHead>Trigger</TableHead>
                  <TableHead>URL</TableHead>
                  <TableHead>Last fired</TableHead>
                  <TableHead>Last error</TableHead>
                  <TableHead className="w-[180px] text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {hooks.map((h) => (
                  <TableRow key={h.ID}>
                    <TableCell className="font-mono text-xs">{h.Name}</TableCell>
                    <TableCell>
                      <Badge variant="outline" className="font-mono text-xs">
                        {h.Format}
                      </Badge>
                    </TableCell>
                    <TableCell className="font-mono text-xs">{h.Trigger}</TableCell>
                    <TableCell className="max-w-xs truncate font-mono text-xs">
                      {h.URL}
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      {h.LastFiredAt ? new Date(h.LastFiredAt).toLocaleString() : "—"}
                    </TableCell>
                    <TableCell className="max-w-xs truncate text-xs text-destructive">
                      {h.LastError || ""}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex justify-end gap-1">
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => test.mutate(h.ID)}
                          disabled={test.isPending}
                          className="gap-1"
                        >
                          <Send className="h-3 w-3" />
                          Test
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          onClick={() => remove.mutate(h.ID)}
                          disabled={remove.isPending}
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
