import { useEffect, useState } from "react";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowLeft,
  ArrowRight,
  Bot,
  Code2,
  Compass,
  FileText,
  Orbit,
  Search,
  Shield,
  Terminal,
  Users,
  X,
} from "lucide-react";
import { api } from "@/api/client";
import { mcpApi, mcpKeys } from "@/api/mcp";
import { useHeaderExtension } from "@/components/header-extension";
import { useProfileExtension } from "@/components/profile-extension";
import { useGatewayExtension } from "@/components/gateways/extension-context";
import { Button } from "@/components/ui/button";
import { ConnectionGuide } from "@/components/onboarding/connection-guide";
import { FeatureGuide } from "@/components/onboarding/feature-guide";
import { SetupError } from "@/components/onboarding/setup-primitives";
import {
  freshDraft,
  guideLabels,
  isFeatureGuide,
  progressKey,
  readProgress,
  saveProgress,
  type GuideId,
  type SetupDraft,
} from "@/components/onboarding/setup-model";

const apps = [
  {
    id: "claude",
    icon: Terminal,
    detail:
      "Connect Claude Code's tools or route model traffic through Bastio.",
  },
  {
    id: "codex",
    icon: Code2,
    detail: "Set up protected MCP tools or a local model gateway for Codex.",
  },
  {
    id: "antigravity",
    icon: Orbit,
    detail: "Connect Antigravity to a Bastio-protected MCP tool server.",
  },
] as const;
const features = [
  {
    id: "mcp",
    icon: Bot,
    detail:
      "Connect a remote or local tool server, set rules, and verify a tool call.",
    tag: "Connection guide",
  },
  {
    id: "gateway",
    icon: Code2,
    detail:
      "Connect a provider, create a scoped key, and send your first request.",
    tag: "Connection guide",
  },
  {
    id: "security",
    icon: Shield,
    detail:
      "Review detectors and actions, test a policy, and inspect findings.",
    tag: "Feature tour",
  },
  {
    id: "traces",
    icon: Search,
    detail:
      "Find a request, understand its security decision, and follow a session.",
    tag: "Feature tour",
  },
  {
    id: "workspace",
    icon: Users,
    detail: "Prepare models, review team access, and separate app credentials.",
    tag: "Feature tour",
  },
] as const;
export function ConnectPage() {
  const header = useHeaderExtension();
  const profile = useProfileExtension();
  const scope = progressKey(
    header.activeWorkspaceID ?? "local",
    profile.userWidget?.email || "local",
  );
  return <ConnectWorkspace key={scope} scope={scope} />;
}
function ConnectWorkspace({ scope }: { scope: string }) {
  const navigate = useNavigate();
  const search = useSearch({ from: "/connect" });
  const extension = useGatewayExtension();
  const [progress, setProgress] = useState<
    Partial<Record<GuideId, SetupDraft>>
  >(() => {
    try {
      return readProgress(window.localStorage, scope);
    } catch {
      return {};
    }
  });
  const [saved, setSaved] = useState(true);
  useEffect(() => {
    try {
      setSaved(saveProgress(window.localStorage, scope, progress));
    } catch {
      setSaved(false);
    }
  }, [progress, scope]);
  const selected = search.guide;
  const draft = selected ? progress[selected] : undefined;
  const gateways = useQuery({
    queryKey: ["proxies"],
    queryFn: api.proxies.list,
  });
  const servers = useQuery({
    queryKey: [...mcpKeys, "servers"],
    queryFn: mcpApi.servers,
  });
  const start = (guide: GuideId, reset = false) => {
    setProgress((previous) => ({
      ...previous,
      [guide]:
        !reset && previous[guide]
          ? previous[guide]
          : freshDraft(
              guide,
              /Win/.test(navigator.userAgent)
                ? "windows"
                : /Linux/.test(navigator.userAgent)
                  ? "linux"
                  : "mac",
            ),
    }));
    void navigate({ to: "/connect", search: { guide } });
  };
  useEffect(() => {
    if (selected)
      setProgress((previous) =>
        previous[selected]
          ? previous
          : {
              ...previous,
              [selected]: freshDraft(
                selected,
                /Win/.test(navigator.userAgent)
                  ? "windows"
                  : /Linux/.test(navigator.userAgent)
                    ? "linux"
                    : "mac",
              ),
            },
      );
  }, [selected]);
  const update = (patch: Partial<SetupDraft>) => {
    if (selected)
      setProgress((previous) => ({
        ...previous,
        [selected]: {
          ...(previous[selected] ?? freshDraft(selected)),
          ...patch,
        },
      }));
  };
  const exit = () => {
    void navigate({ to: "/connect", search: {} });
  };
  const activeGateways =
    gateways.data?.filter((gateway) => gateway.is_active) ?? [];
  const activeServers =
    servers.data?.filter((server) => !server.revoked_at) ?? [];

  return (
    <div className="mx-auto max-w-[1180px] pb-12 pt-2 md:pt-4">
      <header className="mb-7 flex flex-wrap items-start justify-between gap-4">
        <div>
          <p className="mb-2 text-[11px] text-muted-foreground">
            {selected ? "Guided setup" : "Get started"}
          </p>
          <h1 className="text-[25px] font-medium tracking-tight">
            {selected
              ? guideLabels[selected]
              : "What would you like to protect?"}
          </h1>
          <p className="mt-2 max-w-2xl text-[12px] leading-relaxed text-muted-foreground">
            {selected
              ? isFeatureGuide(selected)
                ? "Learn this workflow at your own pace. Your progress is saved."
                : draft?.step === 0 && draft.intent === "overview"
                  ? "Understand the available protection before choosing a connection."
                : "A guided path from configuration to a verified connection."
              : "Start with what you want to protect. We'll explain where protection applies and help you check it."}
          </p>
        </div>
        {selected ? (
          <Button size="sm" variant="ghost" onClick={exit}>
            <ArrowLeft className="size-3" />
            All guides
          </Button>
        ) : null}
      </header>
      {!saved ? (
        <p role="status" className="mb-5 text-[11px] text-warn">
          Browser storage is unavailable. Progress lasts for this visit; save
          connection credentials before leaving.
        </p>
      ) : null}
      {draft ? (
        <div className="border-t border-border-subtle pt-5">
          {isFeatureGuide(draft.guide) ? (
            <FeatureGuide
              key={draft.verificationId}
              draft={draft}
              update={update}
              onExit={exit}
            />
          ) : (
            <ConnectionGuide
              key={draft.verificationId}
              draft={draft}
              update={update}
              onExit={exit}
              onRestart={() => start(draft.guide, true)}
            />
          )}
        </div>
      ) : !selected ? (
        <div className="space-y-9">
          <section aria-label="Choose your protection path">
            <div className="grid border-y border-border-subtle md:grid-cols-2">
              <button
                type="button"
                onClick={() => start("documents")}
                className="group flex flex-col items-start gap-3 p-5 text-left transition-colors hover:bg-surface-2/50"
              >
                <FileText className="size-5 text-muted-foreground" />
                <span className="text-[15px] font-medium tracking-tight">
                  Protect conversations & documents
                </span>
                <span className="max-w-md text-[12px] leading-relaxed text-muted-foreground">
                  Chat and upload documents in Bastio Workspace. Learn how to
                  remove detected personal information before text reaches the AI.
                </span>
                <span className="text-[11px] text-muted-foreground">
                  No MCP server or command-line setup
                </span>
                <span className="mt-auto inline-flex items-center gap-2 pt-2 text-[12px]">
                  {progress.documents
                    ? "Continue Workspace guide"
                    : "Start with Workspace"}
                  <ArrowRight className="size-3 transition-transform group-hover:translate-x-0.5" />
                </span>
              </button>
              <a
                href="#app-connections"
                className="group flex flex-col items-start gap-3 border-t border-border-subtle p-5 transition-colors hover:bg-surface-2/50 md:border-l md:border-t-0"
              >
                <Terminal className="size-5 text-muted-foreground" />
                <span className="text-[15px] font-medium tracking-tight">
                  Connect an app I already use
                </span>
                <span className="max-w-md text-[12px] leading-relaxed text-muted-foreground">
                  Configure Claude Code, Codex, or Antigravity to send selected
                  activity through Bastio.
                </span>
                <span className="text-[11px] leading-relaxed text-muted-foreground">
                  Coverage depends on the connection. This does not automatically
                  protect everything in the app or every uploaded document.
                </span>
                <span className="mt-auto inline-flex items-center gap-2 pt-2 text-[12px]">
                  See available connections
                  <ArrowRight className="size-3 transition-transform group-hover:translate-x-0.5" />
                </span>
              </a>
            </div>
          </section>
          <section
            id="app-connections"
            aria-label="Connect your AI app"
            className="scroll-mt-5"
          >
            <div className="mb-4 flex items-center gap-2">
              <Terminal className="size-3.5 text-muted-foreground" />
              <h2 className="text-[13px] font-medium">
                Existing app connections
              </h2>
              <span className="ml-auto text-[10px] text-muted-foreground">
                Advanced setup · save and resume
              </span>
            </div>
            <p className="mb-4 max-w-3xl text-[12px] leading-relaxed text-muted-foreground">
              Tool connections inspect only the tools you connect. Model gateways
              inspect the requests routed through them and require separate
              provider API credentials and billing. Neither option is a
              device-wide shield for a desktop app.
            </p>
            <div className="grid border-y border-border-subtle md:grid-cols-3">
              {apps.map(({ id, icon: Icon, detail }, index) => (
                <button
                  key={id}
                  type="button"
                  onClick={() => start(id)}
                  className={`group flex min-w-0 flex-col items-start gap-3 p-5 text-left transition-colors hover:bg-surface-2/50 ${index ? "border-t border-border-subtle md:border-l md:border-t-0" : ""}`}
                >
                  <Icon className="size-5 text-muted-foreground" />
                  <h3 className="text-[15px] font-medium tracking-tight">
                    {guideLabels[id]}
                  </h3>
                  <p className="min-h-10 text-[11px] leading-relaxed text-muted-foreground">
                    {detail}
                  </p>
                  <span className="mt-1 inline-flex items-center gap-2 text-[12px]">
                    {progress[id]?.step === 4
                      ? "Review setup"
                      : progress[id]
                        ? "Resume setup"
                        : "Set up"}
                    <ArrowRight className="size-3 transition-transform group-hover:translate-x-0.5" />
                  </span>
                  {progress[id] ? (
                    <span className="text-[10px] text-muted-foreground">
                      {progress[id]?.verifiedAt
                        ? "Setup verified"
                        : `Step ${(progress[id]?.step ?? 0) + 1} of 5`}
                    </span>
                  ) : null}
                </button>
              ))}
            </div>
          </section>
          <section aria-label="Feature guides">
            <h2 className="mb-4 text-[13px] font-medium">
              Explore and set up Bastio
            </h2>
            <div className="divide-y divide-border-subtle border-y border-border-subtle">
              {features.map(({ id, icon: Icon, detail, tag }) => (
                <button
                  key={id}
                  type="button"
                  onClick={() => start(id)}
                  className="group flex w-full items-center gap-4 py-4 text-left hover:bg-surface-2/30"
                >
                  <Icon className="ml-1 size-4 shrink-0 text-muted-foreground" />
                  <span className="min-w-0 flex-1">
                    <span className="flex flex-wrap items-center gap-3">
                      <span className="text-[13px]">{guideLabels[id]}</span>
                      <span className="text-[10px] text-muted-foreground">
                        {tag}
                      </span>
                    </span>
                    <span className="mt-1 block text-[11px] leading-relaxed text-muted-foreground">
                      {detail}
                    </span>
                  </span>
                  <span className="hidden text-[11px] text-muted-foreground sm:block">
                    {progress[id]
                      ? isFeatureGuide(id)
                        ? progress[id]!.step >= 3
                          ? "Reviewed"
                          : "Resume tour"
                        : progress[id]!.verifiedAt
                          ? "Setup verified"
                          : "Resume setup"
                      : "Start"}
                  </span>
                  <ArrowRight className="mr-1 size-3.5 shrink-0 text-muted-foreground" />
                </button>
              ))}
            </div>
          </section>
          <section aria-label="Configured connections">
            <h2 className="mb-2 text-[13px] font-medium">
              Configured connections
            </h2>
            <p className="mb-4 text-[11px] leading-relaxed text-muted-foreground">
              Created gateways and MCP servers in this workspace. Configuration
              alone does not verify traffic or enforcement.
            </p>
            {gateways.isPending || servers.isPending ? (
              <p className="text-[12px] text-muted-foreground">
                Checking connections…
              </p>
            ) : null}
            <SetupError
              error={gateways.error ?? servers.error}
              retry={() => {
                void gateways.refetch();
                void servers.refetch();
              }}
            />
            {activeGateways.length || activeServers.length ? (
              <div className="divide-y divide-border-subtle border-y border-border-subtle">
                {activeGateways.map((gateway) => (
                  <Link
                    key={gateway.id}
                    to="/proxies/$id"
                    params={{ id: gateway.id }}
                    className="flex items-center gap-3 py-3 text-[12px]"
                  >
                    <Code2 className="size-3.5 text-muted-foreground" />
                    <span className="min-w-0 flex-1 truncate">
                      {gateway.name}
                    </span>
                    <span className="text-[10px] text-muted-foreground">
                      {gateway.target_provider} · configured
                    </span>
                    <ArrowRight className="size-3 text-muted-foreground" />
                  </Link>
                ))}
                {activeServers.map((server) => (
                  <Link
                    key={server.id}
                    to="/mcp"
                    className="flex items-center gap-3 py-3 text-[12px]"
                  >
                    <Bot className="size-3.5 text-muted-foreground" />
                    <span className="min-w-0 flex-1 truncate">
                      {server.name}
                    </span>
                    <span className="text-[10px] text-muted-foreground">
                      {server.last_seen_at
                        ? `Last seen ${new Date(server.last_seen_at).toLocaleDateString()}`
                        : "Awaiting connection"}
                    </span>
                    <ArrowRight className="size-3 text-muted-foreground" />
                  </Link>
                ))}
              </div>
            ) : gateways.isSuccess && servers.isSuccess ? (
              <p className="border-y border-border-subtle py-5 text-[12px] text-muted-foreground">
                No connections yet. Choose an app or follow the MCP firewall
                guide above.
              </p>
            ) : null}
          </section>
          {extension.providerKeyManagement === "restricted" ? (
            <p className="text-[11px] text-muted-foreground">
              Some provider configuration is managed by your workspace owner.
              MCP guides are available independently of model-provider access.
            </p>
          ) : null}
        </div>
      ) : (
        <p className="text-xs text-muted-foreground">Opening guide…</p>
      )}
    </div>
  );
}

export function SetupWelcome() {
  const header = useHeaderExtension();
  const profile = useProfileExtension();
  const scope = progressKey(
    header.activeWorkspaceID ?? "local",
    profile.userWidget?.email || "local",
  );
  return <WorkspaceWelcome key={scope} scope={scope} />;
}
function WorkspaceWelcome({ scope }: { scope: string }) {
  const [dismissed, setDismissed] = useState(() => {
    try {
      return window.localStorage.getItem(`${scope}:welcome-dismissed`) === "1";
    } catch {
      return false;
    }
  });
  if (dismissed) return null;
  return (
    <section
      aria-label="Start using Bastio"
      className="border-y border-border-subtle py-5"
    >
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <p className="mb-2 text-[10px] text-muted-foreground">
            Start using Bastio
          </p>
          <h2 className="text-[17px] font-medium tracking-tight">
            Protect your conversations, documents, and AI tools.
          </h2>
          <p className="mt-2 max-w-xl text-[12px] leading-relaxed text-muted-foreground">
            Start with conversations and documents in Workspace, or explore
            connections for the apps you already use. Each guide explains its
            coverage and how to check the result.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Link
            to="/connect"
            search={{}}
            className="inline-flex items-center gap-2 rounded-md border border-border-default px-3 py-2 text-[12px] hover:bg-surface-2"
          >
            <Compass className="size-3.5" />
            Setup guides
            <ArrowRight className="size-3" />
          </Link>
          <Button
            size="icon-sm"
            variant="ghost"
            aria-label="Dismiss getting started"
            onClick={() => {
              setDismissed(true);
              try {
                window.localStorage.setItem(`${scope}:welcome-dismissed`, "1");
              } catch {
                /* Dismissal lasts for this visit. */
              }
            }}
          >
            <X className="size-3" />
          </Button>
        </div>
      </div>
      <div className="mt-4 flex flex-wrap gap-x-6 gap-y-3">
        <Link
          to="/connect"
          search={{ guide: "documents" }}
          className="inline-flex items-center gap-2 text-[12px] text-muted-foreground hover:text-foreground"
        >
          <FileText className="size-3.5" />
          Conversations & documents
          <ArrowRight className="size-3" />
        </Link>
        {apps.map(({ id, icon: Icon }) => (
          <Link
            key={id}
            to="/connect"
            search={{ guide: id }}
            className="inline-flex items-center gap-2 text-[12px] text-muted-foreground hover:text-foreground"
          >
            <Icon className="size-3.5" />
            {guideLabels[id]}
            <ArrowRight className="size-3" />
          </Link>
        ))}
      </div>
    </section>
  );
}
