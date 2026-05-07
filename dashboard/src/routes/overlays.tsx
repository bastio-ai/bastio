import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Layers, Plus, Sparkles } from "lucide-react";

import { overlayApi, overlayKeys, type Overlay } from "@/api/overlay";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { EmptyState, PageHeader } from "@/components/card";
import { SkeletonRows } from "@/components/skeleton";

export function OverlaysPage() {
  const navigate = useNavigate();

  const { data: overlays = [], isLoading } = useQuery({
    queryKey: overlayKeys.list(),
    queryFn: overlayApi.list,
  });

  return (
    <>
      <div className="flex items-end justify-between">
        <PageHeader
          title="Custom Policies"
          description="Add your own rules and detector tweaks on top of the core security profile. Shadow-test before activating, and roll back instantly if something's wrong."
        />
        <div className="flex gap-2">
          <Button
            size="sm"
            variant="outline"
            className="h-8 text-xs"
            onClick={() => navigate({ to: "/overlay-templates" })}
          >
            <Sparkles className="h-3 w-3" /> Browse templates
          </Button>
          <Button
            size="sm"
            className="h-8 text-xs"
            onClick={() =>
              navigate({
                to: "/overlays/new",
                search: { template: undefined, from_threat: undefined },
              })
            }
          >
            <Plus className="h-3 w-3" /> New policy
          </Button>
        </div>
      </div>

      <Card className="border-border/50 overflow-hidden mt-4">
        <CardContent className="p-0">
          {isLoading ? (
            <SkeletonRows count={5} />
          ) : !overlays.length ? (
            <EmptyState
              icon={<Layers className="h-6 w-6" />}
              title="No custom policies yet"
              description="Add extra patterns, access rules, and detector overrides on top of your core security profile — or start from a vertical template."
            />
          ) : (
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent border-border/50">
                  <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Name
                  </TableHead>
                  <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Description
                  </TableHead>
                  <TableHead className="h-10 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Active
                  </TableHead>
                  <TableHead className="h-10 text-right text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                    Updated
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {overlays.map((o: Overlay) => (
                  <TableRow
                    key={o.id}
                    className="cursor-pointer border-border/30 hover:bg-muted/30"
                    onClick={() =>
                      navigate({ to: "/overlays/$id", params: { id: o.id } })
                    }
                  >
                    <TableCell className="font-mono text-xs">{o.name}</TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {o.description || "—"}
                    </TableCell>
                    <TableCell>
                      {o.active_version_id ? (
                        <Badge variant="default" className="text-[10px] px-1.5 py-0">
                          active
                        </Badge>
                      ) : (
                        <Badge variant="outline" className="text-[10px] px-1.5 py-0">
                          draft only
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell className="text-right text-xs text-muted-foreground">
                      {new Date(o.updated_at).toLocaleString()}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </>
  );
}
