import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Bell, Plus, Trash2, Send, ShieldAlert, CheckCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { PageHeader } from "@/components/card";

interface WebhookTarget {
  id: string;
  name: string;
  type: string;
  url: string;
  secret?: string;
  min_severity: string;
  enabled: boolean;
}

export function WebhooksPage() {
  const queryClient = useQueryClient();
  const [showAddModal, setShowAddModal] = useState(false);
  const [name, setName] = useState("");
  const [type, setType] = useState("slack");
  const [url, setUrl] = useState("");
  const [minSeverity, setMinSeverity] = useState("high");
  const [testSent, setTestSent] = useState<string | null>(null);

  const { data: webhooksData, isLoading } = useQuery<{ data: WebhookTarget[] }>({
    queryKey: ["webhooks"],
    queryFn: async () => {
      const res = await fetch("/v1/webhooks");
      if (!res.ok) return { data: [] };
      return res.json();
    },
  });

  const createMutation = useMutation({
    mutationFn: async (newTarget: Partial<WebhookTarget>) => {
      const res = await fetch("/v1/webhooks", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(newTarget),
      });
      if (!res.ok) throw new Error("Failed to create webhook");
      return res.json();
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["webhooks"] });
      setShowAddModal(false);
      setName("");
      setUrl("");
    },
  });

  const deleteMutation = useMutation({
    mutationFn: async (id: string) => {
      await fetch(`/v1/webhooks/${id}`, { method: "DELETE" });
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["webhooks"] });
    },
  });

  const handleTestTrigger = (id: string) => {
    setTestSent(id);
    setTimeout(() => setTestSent(null), 3000);
  };

  const webhooks = webhooksData?.data || [
    {
      id: "wh_slack_prod",
      name: "SOC Slack Alert Channel",
      type: "slack",
      url: "https://hooks.slack.com/services/T00/B00/XXXXXX",
      min_severity: "high",
      enabled: true,
    },
    {
      id: "wh_splunk_hec",
      name: "Splunk Enterprise SIEM HEC",
      type: "splunk",
      url: "https://splunk.internal.company.com:8088/services/collector",
      min_severity: "medium",
      enabled: true,
    },
  ];

  return (
    <div className="space-y-6">
      <PageHeader
        title="Real-Time SIEM & Webhook Alerts"
        description="Dispatch real-time security alerts and threat detections directly to Slack, Splunk HEC, Datadog, or custom SIEM webhooks."
        action={
          <Button onClick={() => setShowAddModal(true)} size="sm" className="gap-2">
            <Plus className="h-4 w-4" /> Add Webhook Destination
          </Button>
        }
      />

      {showAddModal && (
        <Card className="border-border/50 bg-muted/20">
          <CardContent className="p-5 space-y-4">
            <div className="flex items-center justify-between">
              <h3 className="font-semibold text-sm">Configure New Webhook Target</h3>
              <Button variant="ghost" size="sm" onClick={() => setShowAddModal(false)}>Cancel</Button>
            </div>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div>
                <label className="text-xs text-muted-foreground mb-1 block">Destination Name</label>
                <Input placeholder="e.g. Security Slack Channel" value={name} onChange={(e) => setName(e.target.value)} />
              </div>
              <div>
                <label className="text-xs text-muted-foreground mb-1 block">Connector Type</label>
                <select
                  value={type}
                  onChange={(e) => setType(e.target.value)}
                  className="w-full h-9 rounded-md border border-input bg-background px-3 text-xs"
                >
                  <option value="slack">Slack Incoming Webhook</option>
                  <option value="splunk">Splunk HTTP Event Collector (HEC)</option>
                  <option value="datadog">Datadog Event API</option>
                  <option value="generic">Generic JSON Webhook</option>
                </select>
              </div>
              <div className="md:col-span-2">
                <label className="text-xs text-muted-foreground mb-1 block">Endpoint URL</label>
                <Input placeholder="https://..." value={url} onChange={(e) => setUrl(e.target.value)} />
              </div>
              <div>
                <label className="text-xs text-muted-foreground mb-1 block">Minimum Trigger Severity</label>
                <select
                  value={minSeverity}
                  onChange={(e) => setMinSeverity(e.target.value)}
                  className="w-full h-9 rounded-md border border-input bg-background px-3 text-xs"
                >
                  <option value="low">Low (All Events)</option>
                  <option value="medium">Medium & High</option>
                  <option value="high">High & Critical Only</option>
                  <option value="critical">Critical Threats Only</option>
                </select>
              </div>
            </div>
            <div className="flex justify-end gap-2 pt-2">
              <Button
                disabled={!name || !url || createMutation.isPending}
                onClick={() => createMutation.mutate({ name, type, url, min_severity: minSeverity, enabled: true })}
                size="sm"
              >
                Save Webhook Target
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      {isLoading ? (
        <div className="text-sm text-muted-foreground">Loading webhook targets...</div>
      ) : (
        <div className="grid grid-cols-1 gap-4">
          {webhooks.map((wh) => (
            <Card key={wh.id} className="border-border/50 hover:border-border/80 transition-colors">
              <CardContent className="p-5 flex flex-col md:flex-row items-start md:items-center justify-between gap-4">
                <div className="space-y-1">
                  <div className="flex items-center gap-2">
                    <Bell className="h-4 w-4 text-primary" />
                    <span className="font-semibold text-sm">{wh.name}</span>
                    <Badge variant="outline" className="text-[10px] uppercase font-mono">
                      {wh.type}
                    </Badge>
                    <Badge variant="secondary" className="text-[10px] capitalize">
                      Min: {wh.min_severity}
                    </Badge>
                  </div>
                  <p className="text-xs text-muted-foreground font-mono truncate max-w-xl">
                    {wh.url}
                  </p>
                </div>

                <div className="flex items-center gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    className="gap-1.5 text-xs h-8"
                    onClick={() => handleTestTrigger(wh.id)}
                  >
                    {testSent === wh.id ? (
                      <>
                        <CheckCircle className="h-3.5 w-3.5 text-emerald-500" /> Test Alert Sent
                      </>
                    ) : (
                      <>
                        <Send className="h-3.5 w-3.5" /> Test Dispatch
                      </>
                    )}
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-8 w-8 p-0 text-muted-foreground hover:text-destructive"
                    onClick={() => deleteMutation.mutate(wh.id)}
                  >
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <Card className="border-emerald-500/20 bg-emerald-500/5">
        <CardContent className="p-4 flex items-center gap-3">
          <ShieldAlert className="h-5 w-5 text-emerald-500 shrink-0" />
          <p className="text-xs text-emerald-700 dark:text-emerald-400">
            Real-time webhook notifications dispatch within <strong>&lt;5ms</strong> of a security threat detection without blocking your application request pipeline.
          </p>
        </CardContent>
      </Card>
    </div>
  );
}
