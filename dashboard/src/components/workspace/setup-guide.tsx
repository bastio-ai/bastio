import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowRight,
  CheckCircle2,
  Circle,
  ShieldCheck,
  Users,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { workspaceApi } from "./types";
import { ModelConnections } from "./model-connections";
import type { SettingsSection } from "./navigation";
import { useWorkspaceExtension } from "./extension-context";

const titles: Record<string, string> = {
  models: "Connect AI",
  privacy: "Privacy & limits",
  team: "Invite employees",
  groups: "Create teams",
  assistant: "Share an assistant",
  preview: "Check employee access",
};

export function WorkspaceSetupGuide({
  onOpenTab,
  onOpenChat,
}: {
  onOpenTab?: (tab: string, section?: SettingsSection) => void;
  onOpenChat: () => void;
}) {
  const ext = useWorkspaceExtension();
  const qc = useQueryClient();
  const [active, setActive] = useState<string | null>(null);
  const status = useQuery({
    queryKey: ["workspace", "setup"],
    queryFn: workspaceApi.setupStatus,
    refetchInterval: 30_000,
  });
  const settings = useQuery({
    queryKey: ["workspace", "settings"],
    queryFn: workspaceApi.getSettings,
  });
  const review = useMutation({
    mutationFn: () => workspaceApi.reviewPrivacy(settings.data!),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["workspace", "setup"] }),
  });

  useEffect(() => {
    if (!active && status.data)
      setActive(
        status.data.steps.find((step) => !step.complete)?.id ?? "preview",
      );
  }, [status.data, active]);

  if (status.isPending) {
    return (
      <p
        role="status"
        className="mb-6 border-y border-border-subtle py-4 text-xs text-muted-foreground"
      >
        Checking workspace setup…
      </p>
    );
  }

  if (status.isError) {
    return (
      <div
        role="alert"
        className="mb-6 border-y border-border-subtle py-4 text-xs"
      >
        <p className="text-destructive">Setup status could not be loaded.</p>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="mt-2 h-8 text-xs"
          onClick={() => void status.refetch()}
        >
          Retry
        </Button>
      </div>
    );
  }

  const data = status.data;
  const index = data.steps.findIndex((step) => step.id === active);
  const step = data.steps[index];
  const next = data.steps[index + 1];

  return (
    <section
      className="mb-8 space-y-4 border-y border-border-subtle py-5"
      aria-label="Workspace setup"
    >
      <div className="flex flex-wrap items-baseline justify-between gap-3">
        <div>
          <h2 className="text-[13px] font-medium text-foreground">
            {data.ready ? "Your team is up and running" : "Get your team ready"}
          </h2>
          <p className="mt-0.5 text-xs text-muted-foreground">
            Connect an approved model, set your policies, then help an employee
            get their first useful answer.
          </p>
        </div>
        <span className="font-mono text-[11px] tabular-nums text-muted-foreground">
          {data.completed} of {data.total} steps complete
        </span>
      </div>

      <div
        role="progressbar"
        aria-label="Workspace setup progress"
        aria-valuemin={0}
        aria-valuemax={data.total}
        aria-valuenow={data.completed}
        className="flex gap-1"
      >
        {data.steps.map((item) => (
          <span
            key={item.id}
            className={cn(
              "h-1 flex-1 rounded-full",
              item.complete ? "bg-success" : "bg-surface-2",
            )}
          />
        ))}
      </div>

      <div className="flex gap-4 overflow-x-auto border-b border-border-subtle">
        {data.steps.map((item, i) => (
          <button
            key={item.id}
            type="button"
            aria-current={active === item.id ? "step" : undefined}
            onClick={() => setActive(item.id)}
            className={cn(
              "inline-flex shrink-0 cursor-pointer items-center gap-1.5 border-b-2 px-0.5 pb-2.5 text-xs transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
              active === item.id
                ? "border-foreground/70 text-foreground"
                : "border-transparent text-muted-foreground hover:text-foreground",
            )}
          >
            {item.complete ? (
              <CheckCircle2
                className="size-3.5 shrink-0 text-success"
                aria-hidden="true"
              />
            ) : (
              <span className="font-mono text-[11px] tabular-nums text-muted-foreground">
                {i + 1}.
              </span>
            )}
            <span>{titles[item.id]}</span>
          </button>
        ))}
      </div>

      {step ? (
        <div className="space-y-4 pt-1">
          <div>
            <h3 className="text-[13px] font-medium text-foreground">
              {titles[step.id]}
            </h3>
            <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
              {step.detail}
            </p>
          </div>

          {active === "models" ? <ModelConnections /> : null}

          {active === "privacy" ? (
            <div className="space-y-3">
              <div className="border-l-2 border-border-subtle pl-3.5 text-xs leading-relaxed text-muted-foreground">
                <p>
                  Conversation content:{" "}
                  <strong className="font-medium text-foreground">
                    {settings.data?.store_conversation_content
                      ? "Saved history"
                      : "Not stored"}
                  </strong>
                </p>
                <p className="mt-1">
                  Retention: {settings.data?.retention_days} days · Monthly
                  spending limit:{" "}
                  {settings.data?.spend_cap_cents
                    ? `$${(settings.data.spend_cap_cents / 100).toFixed(2)}`
                    : "No workspace limit"}
                </p>
                <p className="mt-1.5 text-[11px]">
                  Knowledge documents are stored separately. Connected providers
                  have their own data policies.
                </p>
              </div>
              <div className="flex flex-wrap gap-2">
                <Button
                  type="button"
                  size="sm"
                  className="h-8 text-xs"
                  variant="outline"
                  onClick={() => onOpenTab?.("settings", "privacy")}
                >
                  Edit privacy & retention
                </Button>
                <Button
                  type="button"
                  size="sm"
                  className="h-8 text-xs"
                  variant="outline"
                  onClick={() => onOpenTab?.("settings", "usage")}
                >
                  Edit spending limit
                </Button>
                <Button
                  type="button"
                  size="sm"
                  className="h-8 text-xs"
                  disabled={!settings.data || review.isPending}
                  onClick={() => review.mutate()}
                >
                  <ShieldCheck className="size-3.5" aria-hidden="true" />
                  I’ve reviewed these settings
                </Button>
              </div>
              {review.error ? (
                <p role="alert" className="text-xs text-destructive">
                  {review.error.message}
                </p>
              ) : null}
            </div>
          ) : null}

          {active === "team" ? (
            <div className="space-y-3">
              <p className="font-mono text-xs text-foreground">
                {data.members} joined employees · {data.pending_invitations}{" "}
                pending invitations
              </p>
              <p className="text-xs text-muted-foreground">
                Invite employees first. Once they join, assign them to a team
                group and preview their access.
              </p>
              <Button
                type="button"
                size="sm"
                className="h-8 text-xs"
                onClick={() =>
                  onOpenTab?.(
                    ext.extraTabs.some((tab) => tab.id === "team")
                      ? "team"
                      : "groups",
                  )
                }
              >
                <Users className="size-3.5" aria-hidden="true" />
                Manage members & invitations
              </Button>
            </div>
          ) : null}

          {active === "groups" ? (
            <div className="space-y-3">
              <p className="text-xs text-muted-foreground">
                Create Legal or Marketing, add joined employees, and use these
                groups when sharing knowledge, assistants and integrations.
              </p>
              <Button
                type="button"
                size="sm"
                className="h-8 text-xs"
                onClick={() => onOpenTab?.("groups")}
              >
                Create groups & review sharing
                <ArrowRight className="size-3.5" aria-hidden="true" />
              </Button>
            </div>
          ) : null}

          {active === "assistant" ? (
            <div className="space-y-3">
              <p className="text-xs text-muted-foreground">
                Upload the team’s documents, create an assistant, and select its
                audience. Employees need permission for both the assistant and
                each knowledge source.
              </p>
              <p className="font-mono text-xs text-foreground">
                {data.ready_knowledge} knowledge sources ready
              </p>
              <div className="flex flex-wrap gap-2">
                <Button
                  type="button"
                  size="sm"
                  className="h-8 text-xs"
                  variant="outline"
                  onClick={() => onOpenTab?.("knowledge")}
                >
                  Add team knowledge
                </Button>
                <Button
                  type="button"
                  size="sm"
                  className="h-8 text-xs"
                  onClick={() => onOpenTab?.("assistants")}
                >
                  Create or share an assistant
                  <ArrowRight className="size-3.5" aria-hidden="true" />
                </Button>
              </div>
            </div>
          ) : null}

          {active === "preview" ? (
            <div className="space-y-3">
              {data.multi_user ? <EmployeeAccessPreview /> : null}
              <Button
                type="button"
                size="sm"
                className="h-8 text-xs"
                variant="outline"
                onClick={onOpenChat}
              >
                Open employee workspace
                <ArrowRight className="size-3.5" aria-hidden="true" />
              </Button>
              <p className="text-[11px] text-muted-foreground">
                The final step completes when a joined employee receives a
                successful model response.
              </p>
            </div>
          ) : null}

          <div className="flex items-center justify-between border-t border-border-subtle pt-3">
            <span className="flex items-center gap-2 text-xs text-muted-foreground">
              {step.complete ? (
                <CheckCircle2
                  className="size-3.5 text-success"
                  aria-hidden="true"
                />
              ) : (
                <Circle className="size-3.5" aria-hidden="true" />
              )}
              {step.complete ? "Complete" : "Still needs attention"}
            </span>
            {next ? (
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="h-7 text-xs"
                onClick={() => setActive(next.id)}
              >
                Next: {titles[next.id]}
                <ArrowRight className="size-3" aria-hidden="true" />
              </Button>
            ) : null}
          </div>
        </div>
      ) : null}
    </section>
  );
}

function EmployeeAccessPreview() {
  const [member, setMember] = useState("");
  const members = useQuery({
    queryKey: ["workspace", "members"],
    queryFn: workspaceApi.listMembers,
    retry: false,
  });
  const preview = useQuery({
    queryKey: ["workspace", "setup", "preview", member],
    queryFn: () => workspaceApi.previewMember(member),
    enabled: !!member,
    staleTime: 0,
  });

  return (
    <div className="space-y-3 border-y border-border-subtle py-4">
      <p className="text-xs font-medium text-foreground">
        Preview a member’s shared resources
      </p>
      <p className="text-xs text-muted-foreground">
        This checks their role and current sharing rules. It does not sign in as
        them or access their personal credentials.
      </p>
      <select
        aria-label="Employee to preview"
        className="h-8 w-full max-w-sm cursor-pointer rounded-md border border-border-subtle bg-surface-1 px-2.5 text-xs text-foreground"
        value={member}
        onChange={(event) => setMember(event.target.value)}
      >
        <option value="">Choose a joined member…</option>
        {members.data?.members.map((item) => (
          <option key={item.user_id} value={item.user_id}>
            {item.email} · {item.role}
          </option>
        ))}
      </select>
      {members.error ? (
        <p role="alert" className="text-xs text-muted-foreground">
          Joined member information is unavailable on this deployment.
        </p>
      ) : null}
      {preview.isFetching ? (
        <p role="status" className="text-xs text-muted-foreground">
          Checking access…
        </p>
      ) : null}
      {preview.error ? (
        <p role="alert" className="text-xs text-destructive">
          {preview.error.message}
        </p>
      ) : null}
      {preview.data ? (
        <div className="space-y-3">
          <p className="text-xs">
            {preview.data.email} · {preview.data.role}
            {["owner", "admin"].includes(preview.data.role)
              ? " · Admins can manage restricted resources; choose an employee to check employee access."
              : ""}
          </p>
          <div className="grid divide-y divide-border-subtle border-y border-border-subtle sm:grid-cols-3 sm:divide-x sm:divide-y-0">
            {[
              ["Assistants", preview.data.assistants],
              ["Knowledge", preview.data.knowledge],
              ["Shared tools", preview.data.integrations],
            ].map(([title, items]) => (
              <div key={title as string} className="p-3">
                <p className="text-xs font-medium text-foreground">
                  {title as string}
                </p>
                <ul className="mt-1.5 space-y-1 text-xs text-muted-foreground">
                  {(items as { id: string; name: string }[]).map((item) => (
                    <li key={item.id}>{item.name}</li>
                  ))}
                  {!(items as unknown[]).length ? (
                    <li>No shared resources available</li>
                  ) : null}
                </ul>
              </div>
            ))}
          </div>
          <p className="text-[11px] text-muted-foreground">
            Chat content{" "}
            {preview.data.store_conversation_content
              ? "is saved"
              : "is not stored"}
            .{" "}
            {preview.data.allowed_models.length
              ? `${preview.data.allowed_models.length} approved model pairs.`
              : "No model allowlist; connected provider models apply."}
          </p>
        </div>
      ) : null}
    </div>
  );
}
