import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Trash2, Globe } from "lucide-react";

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

export function DomainsTab() {
  const qc = useQueryClient();
  const q = useQuery({
    queryKey: ["governance", "domains"] as const,
    queryFn: () => api.governance.domains.list(),
  });

  const [domain, setDomain] = useState("");
  const [label, setLabel] = useState("");

  const add = useMutation({
    mutationFn: () => api.governance.domains.add({ domain, label }),
    onSuccess: () => {
      setDomain("");
      setLabel("");
      qc.invalidateQueries({ queryKey: ["governance", "domains"] });
    },
  });

  const remove = useMutation({
    mutationFn: (id: string) => api.governance.domains.delete(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["governance", "domains"] }),
  });

  const rows = q.data?.overrides ?? [];

  return (
    <div className="space-y-4">
      <Card>
        <CardContent className="space-y-4 p-6">
          <div>
            <h3 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">
              Track an internal AI tool
            </h3>
            <p className="mt-1 text-xs text-muted-foreground">
              Add domains where employees use AI beyond the bundled public list. The extension picks
              these up at the next /policy refresh (every 30 min).
            </p>
          </div>
          <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
            <div className="space-y-2">
              <label className="text-xs font-medium text-muted-foreground">Domain</label>
              <Input
                value={domain}
                onChange={(e) => setDomain(e.target.value)}
                placeholder="ai.acme.internal"
              />
            </div>
            <div className="space-y-2">
              <label className="text-xs font-medium text-muted-foreground">Label</label>
              <Input
                value={label}
                onChange={(e) => setLabel(e.target.value)}
                placeholder="Acme Internal AI"
              />
            </div>
            <div className="flex items-end justify-end">
              <Button
                onClick={() => add.mutate()}
                disabled={add.isPending || !domain.trim()}
                className="gap-2"
              >
                <Plus className="h-4 w-4" />
                Add
              </Button>
            </div>
          </div>
          {add.isError && (
            <p className="text-xs text-destructive">{(add.error as Error).message}</p>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardContent className="p-0">
          <div className="border-b border-border p-4">
            <h3 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">
              Tracked custom domains
            </h3>
          </div>
          {q.isLoading ? (
            <div className="p-6 text-sm text-muted-foreground">Loading…</div>
          ) : rows.length === 0 ? (
            <EmptyState
              icon={<Globe className="h-6 w-6" />}
              title="Only the public list is tracked"
              description="The extension watches 19 public AI tools by default. Add internal AI tools above to extend coverage."
            />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Domain</TableHead>
                  <TableHead>Label</TableHead>
                  <TableHead className="w-[100px]" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((d) => (
                  <TableRow key={d.ID}>
                    <TableCell className="font-mono text-xs">{d.Domain}</TableCell>
                    <TableCell className="text-xs">{d.Label || "—"}</TableCell>
                    <TableCell className="text-right">
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() => remove.mutate(d.ID)}
                        disabled={remove.isPending}
                        className="text-destructive"
                      >
                        <Trash2 className="h-3 w-3" />
                      </Button>
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
