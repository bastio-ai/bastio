import { useEffect, useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Bot,
  Check,
  ChevronDown,
  FileOutput,
  Gauge,
  GitBranch,
  KeyRound,
  ListChecks,
  Lock,
  Plus,
  ShieldCheck,
  Trash2,
  Unlock,
  Wand2,
} from "lucide-react";
import {
  AdminPageHeader,
  AdminSummaryStrip,
  AdminUnderlineNav,
  SecurityNotice,
} from "@/components/admin/admin-primitives";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useSecurityExtension } from "@/components/security-extension";
import { api } from "@/api/client";
import type {
  SecurityProfile,
  UpdateSecurityProfileRequest,
} from "@/api/client";
import { cn } from "@/lib/utils";
import {
  ACTION_HELP,
  ActionHelp,
  EnforcementGuide,
} from "@/components/security-action-help";

type PIIAction = SecurityProfile["pii_action"];
type PIITokenStyle = SecurityProfile["pii_token_style"];
type Strategy = "block" | "mask" | "tokenize" | "warn" | "log_only";

const PII_ACTION_OPTIONS: { value: PIIAction; label: string }[] = [
  { value: "mask", label: "Mask" },
  { value: "tokenize", label: "Tokenize" },
  { value: "block", label: "Block" },
  { value: "warn", label: "Warn" },
  { value: "log_only", label: "Log only" },
];

const PII_TOKEN_STYLE_OPTIONS: { value: PIITokenStyle; label: string }[] = [
  { value: "angle", label: "<PII_SSN_1>" },
  { value: "curly", label: "{{PII_SSN_1}}" },
];

const STRATEGY_OPTIONS: Record<
  "text" | "secrets",
  { value: Strategy; label: string }[]
> = {
  text: [
    { value: "block", label: "Block" },
    { value: "warn", label: "Warn" },
    { value: "log_only", label: "Log only" },
  ],
  secrets: [
    { value: "block", label: "Block" },
    { value: "mask", label: "Mask" },
    { value: "warn", label: "Warn" },
    { value: "log_only", label: "Log only" },
  ],
};

const detectorMeta = [
  {
    key: "injection",
    label: "Prompt Injection",
    enabledField: "injection_enabled" as const,
    strategyField: "injection_strategy" as const,
    thresholdField: "injection_threshold" as const,
    strategyOptions: STRATEGY_OPTIONS.text,
    patterns: 24,
    description:
      "System instruction override, prompt extraction, and multilingual injection variants.",
    icon: ShieldCheck,
  },
  {
    key: "pii",
    label: "PII Detection",
    enabledField: "pii_enabled" as const,
    strategyField: null,
    thresholdField: null,
    strategyOptions: null,
    patterns: 12,
    description:
      "Emails, government identifiers (including Danish CPR), payment data, phone numbers, IPs, and banking identifiers.",
    icon: ShieldCheck,
  },
  {
    key: "jailbreak",
    label: "Jailbreak Detection",
    enabledField: "jailbreak_enabled" as const,
    strategyField: "jailbreak_strategy" as const,
    thresholdField: "jailbreak_threshold" as const,
    strategyOptions: STRATEGY_OPTIONS.text,
    patterns: 20,
    description:
      "DAN, developer-mode impersonation, fiction framing, and hypothetical sandbagging.",
    icon: ShieldCheck,
  },
  {
    key: "secrets",
    label: "Secrets Detection",
    enabledField: "secrets_enabled" as const,
    strategyField: "secrets_strategy" as const,
    thresholdField: null,
    strategyOptions: STRATEGY_OPTIONS.secrets,
    patterns: 22,
    description:
      "Cloud, source control, payment, LLM, SaaS and communication credentials, connection strings, JWTs, and private keys.",
    icon: KeyRound,
  },
  {
    key: "indirect_injection",
    label: "Indirect Injection",
    enabledField: "indirect_injection_enabled" as const,
    strategyField: "indirect_injection_strategy" as const,
    thresholdField: null,
    strategyOptions: STRATEGY_OPTIONS.text,
    patterns: 10,
    description:
      "Embedded prompts in Workspace knowledge-base documents, retrieval, memory, and workspace tool results.",
    icon: GitBranch,
  },
  {
    key: "tool_injection",
    label: "Agent Tool Injection",
    enabledField: "indirect_injection_enabled" as const,
    strategyField: "tool_injection_strategy" as const,
    thresholdField: null,
    strategyOptions: STRATEGY_OPTIONS.text,
    patterns: 10,
    description:
      "Indirect injection in tool results and replayed assistant turns on gateway traffic. Default warn: flagged and forwarded, so a false positive never breaks an agent.",
    icon: GitBranch,
  },
  {
    key: "output_exfil",
    label: "Output Exfiltration",
    enabledField: "output_exfil_enabled" as const,
    strategyField: "output_exfil_strategy" as const,
    thresholdField: null,
    strategyOptions: STRATEGY_OPTIONS.text,
    patterns: 8,
    description:
      "Model responses that expose system prompts, embedded secrets, or memorized data.",
    icon: FileOutput,
  },
  {
    key: "topic_policy",
    label: "Topic Policy",
    enabledField: "topic_policy_enabled" as const,
    strategyField: null,
    thresholdField: null,
    strategyOptions: null,
    patterns: 0,
    description:
      "Customer-defined rules for regulated topics, competitors, and internal terminology.",
    icon: ListChecks,
  },
  {
    key: "rate_anomaly",
    label: "Rate anomaly",
    enabledField: "rate_anomaly_enabled" as const,
    strategyField: null,
    thresholdField: null,
    strategyOptions: null,
    patterns: 0,
    description:
      "Flags request-rate bursts within a session. Requires X-Bastio-Session-Id. Default off.",
    icon: Gauge,
  },
  {
    key: "bot",
    label: "Bot Detection",
    enabledField: "bot_detection_enabled" as const,
    strategyField: null,
    thresholdField: null,
    strategyOptions: null,
    patterns: 0,
    description:
      "Behavioral fingerprinting and automated request detection.",
    icon: Bot,
  },
] as const;

export function SecurityPage() {
  const { tab } = useSearch({ from: "/security-settings" });
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { data: profiles, isLoading } = useQuery({
    queryKey: ["security-profiles"],
    queryFn: api.security.profiles,
  });
  const { data: appConfig } = useQuery({
    queryKey: ["config"],
    queryFn: api.config,
  });
  const [expanded, setExpanded] = useState<string | null>("injection");
  const profile = profiles?.[0] as SecurityProfile | undefined;
  const securityMode = appConfig?.security_mode ?? "fail-open";
  const { extraTabs = [] } = useSecurityExtension();

  const updateProfile = useMutation({
    mutationFn: (data: UpdateSecurityProfileRequest) =>
      profile
        ? api.security.updateProfile(profile.id, data)
        : Promise.resolve({ status: "no profile" }),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["security-profiles"] }),
  });

  const enabledCount = profile
    ? detectorMeta.filter((detector) => Boolean(profile[detector.enabledField]))
        .length
    : 0;
  const configuredStrategies = profile
    ? (detectorMeta
        .map((detector) => {
          if (detector.key === "pii") return profile.pii_action as Strategy;
          return detector.strategyField
            ? (profile[detector.strategyField] as Strategy)
            : null;
        })
        .filter(Boolean) as Strategy[])
    : [];
  const blockCount = configuredStrategies.filter(
    (strategy) => strategy === "block",
  ).length;
  const transformCount = configuredStrategies.filter(
    (strategy) => strategy === "mask" || strategy === "tokenize",
  ).length;

  const update = (data: UpdateSecurityProfileRequest) =>
    updateProfile.mutate(data);

  const validTabIds = [
    "detectors",
    "runtime",
    ...extraTabs.map((item) => item.id),
  ];
  const activeTab = validTabIds.includes(tab || "") ? (tab as string) : "detectors";

  return (
    <div className="mx-auto max-w-[1180px] pb-10 pt-2 md:pt-4">
      <AdminPageHeader
        eyebrow="Security & guardrails"
        title="Security Center"
        description="Configure preprocessing, detection, and enforcement across every gateway route."
        bordered={false}
        className="mb-4"
        badge={
          updateProfile.isPending ? (
            <span role="status" className="text-[11px] text-muted-foreground">
              Saving…
            </span>
          ) : updateProfile.isSuccess ? (
            <span
              role="status"
              className="inline-flex items-center gap-1 text-[11px] text-success"
            >
              <Check className="size-3" aria-hidden="true" /> Saved
            </span>
          ) : undefined
        }
      />

      <AdminSummaryStrip
        items={[
          {
            label: "Detectors enabled",
            value: isLoading ? "—" : `${enabledCount}/${detectorMeta.length}`,
            detail: "Across the active profile",
            tone: "success",
          },
          {
            label: "Blocking policies",
            value: isLoading ? "—" : blockCount,
            detail: "Reject before provider",
          },
          {
            label: "Transform policies",
            value: isLoading ? "—" : transformCount,
            detail: "Mask or tokenize",
          },
          {
            label: "Failure mode",
            value: securityMode === "fail-closed" ? "Closed" : "Open",
            detail:
              securityMode === "fail-closed"
                ? "Enforcement first"
                : "Availability first",
            tone: securityMode === "fail-closed" ? "success" : "warning",
          },
        ]}
      />

      <p
        className={cn(
          "mb-6 border-l-2 pl-3 text-[12px] leading-relaxed",
          securityMode === "fail-closed"
            ? "border-success/50 text-muted-foreground"
            : "border-warn/50 text-muted-foreground",
        )}
      >
        <span className="font-medium text-foreground">
          {securityMode === "fail-closed"
            ? "Gateway fails closed."
            : "Gateway fails open."}
        </span>{" "}
        {securityMode === "fail-closed"
          ? "Security evaluation must complete before traffic reaches the model provider."
          : "If the security engine or policy lookup fails, traffic continues to the model provider. Use fail-closed for regulated workloads."}
      </p>

      <AdminUnderlineNav
        ariaLabel="Security sections"
        value={activeTab}
        onChange={(next) =>
          void navigate({
            to: "/security-settings",
            search: { tab: next },
            replace: true,
          })
        }
        items={[
          { id: "detectors", label: "Detection profile" },
          { id: "runtime", label: "Runtime enforcement" },
          ...extraTabs.map((item) => ({ id: item.id, label: item.label })),
        ]}
      />

      {activeTab === "detectors" ? (
        <div className="space-y-6">
          <EnforcementGuide />

          <section aria-label="Input preprocessing" className="border-b border-border-subtle pb-5">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2.5">
                  <Wand2 className="size-4 text-muted-foreground" aria-hidden="true" />
                  <h2 className="text-[13px] font-medium text-foreground">
                    Input canonicalization
                  </h2>
                  <span className="inline-flex items-center gap-1.5 text-[11px] text-muted-foreground">
                    <span
                      aria-hidden="true"
                      className={cn(
                        "size-1.5 rounded-full",
                        profile?.canonicalize_enabled
                          ? "bg-success"
                          : "bg-muted-foreground/40",
                      )}
                    />
                    {profile?.canonicalize_enabled ? "Enabled" : "Disabled"}
                  </span>
                </div>
                <p className="mt-1.5 max-w-3xl text-[12px] leading-relaxed text-muted-foreground">
                  Normalizes Unicode, removes zero-width characters, folds
                  homoglyphs, and decodes embedded payloads before detection to
                  close encoding-based bypass paths.
                </p>
              </div>
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={!profile || updateProfile.isPending}
                onClick={() =>
                  profile &&
                  update({
                    canonicalize_enabled: !profile.canonicalize_enabled,
                  })
                }
              >
                {profile?.canonicalize_enabled ? "Disable" : "Enable"}
              </Button>
            </div>
          </section>

          <section aria-label="Detector policies">
            <div className="mb-3 flex items-baseline justify-between gap-4">
              <div>
                <h2 className="text-[13px] font-medium text-foreground">
                  Detector policies
                </h2>
                <p className="mt-0.5 text-[11px] text-muted-foreground">
                  Select a detector row to inspect coverage and tune enforcement.
                </p>
              </div>
              <span className="font-mono text-[11px] tabular-nums text-muted-foreground">
                {enabledCount} enabled
              </span>
            </div>

            <div className="divide-y divide-border-subtle border-y border-border-subtle">
              {detectorMeta.map((detector) => {
                const enabled = profile
                  ? Boolean(profile[detector.enabledField])
                  : detector.key !== "bot";
                const isExpanded = expanded === detector.key;
                const strategy =
                  detector.key === "pii"
                    ? profile?.pii_action
                    : detector.strategyField && profile
                      ? profile[detector.strategyField]
                      : null;
                const Icon = detector.icon;
                const panelId = `detector-panel-${detector.key}`;
                return (
                  <article key={detector.key}>
                    <div className="flex items-center gap-3 py-3.5 transition-colors hover:bg-surface-2/40 sm:px-2">
                      <button
                        type="button"
                        className="flex min-w-0 flex-1 cursor-pointer items-center gap-3 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                        onClick={() =>
                          setExpanded(isExpanded ? null : detector.key)
                        }
                        aria-expanded={isExpanded}
                        aria-controls={panelId}
                      >
                        <Icon
                          aria-hidden="true"
                          className={cn(
                            "size-4 shrink-0",
                            enabled
                              ? "text-foreground/80"
                              : "text-muted-foreground/60",
                          )}
                        />
                        <div className="min-w-0 flex-1">
                          <div className="flex flex-wrap items-center gap-2">
                            <h3 className="text-[13px] font-medium text-foreground">
                              {detector.label}
                            </h3>
                            <span
                              aria-hidden="true"
                              className={cn(
                                "size-1.5 rounded-full",
                                enabled
                                  ? "bg-success"
                                  : "bg-muted-foreground/40",
                              )}
                            />
                            {detector.key === "bot" ? (
                              <Badge
                                variant="secondary"
                                className="h-4 px-1.5 text-[9px]"
                              >
                                coming soon
                              </Badge>
                            ) : null}
                          </div>
                          <p className="mt-0.5 truncate text-[12px] text-muted-foreground">
                            {detector.description}
                          </p>
                        </div>
                        <div className="hidden items-center gap-6 sm:flex">
                          <div className="w-24">
                            <p className="text-[10px] text-muted-foreground">
                              Action
                            </p>
                            <p className="mt-0.5 font-mono text-[11px] text-foreground">
                              {detector.key === "jailbreak" &&
                              strategy === "block"
                                ? "warn+block"
                                : detector.key === "rate_anomaly"
                                  ? "warn"
                                  : strategy
                                    ? String(strategy).replace("_", " ")
                                    : "custom"}
                            </p>
                          </div>
                          <div className="w-20">
                            <p className="text-[10px] text-muted-foreground">
                              Coverage
                            </p>
                            <p className="mt-0.5 font-mono text-[11px] tabular-nums text-muted-foreground">
                              {detector.patterns
                                ? `${detector.patterns} rules`
                                : detector.key === "rate_anomaly"
                                  ? "session"
                                  : "policy"}
                            </p>
                          </div>
                        </div>
                        <ChevronDown
                          aria-hidden="true"
                          className={cn(
                            "size-4 shrink-0 text-muted-foreground transition-transform",
                            isExpanded && "rotate-180",
                          )}
                        />
                      </button>
                      <Button
                        type="button"
                        variant={enabled ? "outline" : "ghost"}
                        size="sm"
                        className={cn("w-20", enabled && "text-foreground")}
                        aria-label={`${enabled ? "Disable" : "Enable"} ${detector.label}`}
                        disabled={
                          !profile ||
                          detector.key === "bot" ||
                          updateProfile.isPending
                        }
                        onClick={() =>
                          profile &&
                          update({
                            [detector.enabledField]: !enabled,
                          } as UpdateSecurityProfileRequest)
                        }
                      >
                        {enabled ? "Enabled" : "Disabled"}
                      </Button>
                    </div>

                    {isExpanded ? (
                      <div
                        id={panelId}
                        className="border-t border-border-subtle bg-surface-1/40 px-3 py-5 sm:pl-9 sm:pr-4"
                      >
                        <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_minmax(300px,0.85fr)] lg:divide-x lg:divide-border-subtle">
                          <div className="min-w-0">
                            <p className="text-[11px] font-medium text-foreground">
                              Protection coverage
                            </p>
                            <p className="mt-1.5 max-w-2xl text-[12px] leading-relaxed text-muted-foreground">
                              {detector.description}
                            </p>
                            <div className="mt-3 flex flex-wrap gap-2 text-[11px] text-muted-foreground">
                              <span className="font-mono">
                                {detector.patterns
                                  ? `${detector.patterns} built-in patterns`
                                  : "Customer-defined policy"}
                              </span>
                              <span aria-hidden="true">·</span>
                              <span
                                className={
                                  enabled
                                    ? "text-success"
                                    : "text-muted-foreground"
                                }
                              >
                                {enabled
                                  ? "Evaluating traffic"
                                  : "Not evaluating traffic"}
                              </span>
                            </div>
                          </div>

                          <div
                            className={cn(
                              "min-w-0 space-y-4 lg:pl-6",
                              !enabled && "opacity-60",
                            )}
                          >
                            <p className="text-[11px] font-medium text-foreground">
                              Enforcement
                            </p>
                            {detector.key === "pii" && profile ? (
                              <PiiControls
                                profile={profile}
                                disabled={!enabled || updateProfile.isPending}
                                onUpdate={update}
                              />
                            ) : detector.key === "topic_policy" && profile ? (
                              <TopicPatternList
                                profileId={profile.id}
                                disabled={updateProfile.isPending}
                              />
                            ) : detector.key === "rate_anomaly" ? (
                              <p className="text-[12px] leading-relaxed text-muted-foreground">
                                Warns when a session’s request rate jumps well
                                above its own trailing baseline. Clients must
                                send{" "}
                                <code className="rounded-sm border border-border-subtle bg-surface-2 px-1.5 py-0.5 font-mono text-[11px] text-foreground">
                                  X-Bastio-Session-Id
                                </code>
                                . The playground sends one automatically.
                              </p>
                            ) : detector.strategyField &&
                              detector.strategyOptions &&
                              profile ? (
                              <div className="space-y-4">
                                <StrategyField
                                  id={`strategy-${detector.key}`}
                                  value={
                                    (profile[detector.strategyField] ??
                                      detector.strategyOptions[0]
                                        ?.value) as Strategy
                                  }
                                  options={detector.strategyOptions}
                                  onChange={(next) =>
                                    update({
                                      [detector.strategyField as string]: next,
                                    } as UpdateSecurityProfileRequest)
                                  }
                                  disabled={
                                    !enabled || updateProfile.isPending
                                  }
                                />
                                {detector.key === "jailbreak" &&
                                strategy === "block" ? (
                                  <p className="text-[11px] leading-relaxed text-muted-foreground">
                                    Scores ≥0.8 block the request; 0.6–0.8 warn
                                    and continue.
                                  </p>
                                ) : null}
                                {detector.thresholdField ? (
                                  <ThresholdField
                                    id={`threshold-${detector.key}`}
                                    value={
                                      (profile[
                                        detector.thresholdField
                                      ] as number) ?? 0
                                    }
                                    onChange={(next) =>
                                      update({
                                        [detector.thresholdField as string]:
                                          next,
                                      } as UpdateSecurityProfileRequest)
                                    }
                                    disabled={
                                      !enabled || updateProfile.isPending
                                    }
                                  />
                                ) : null}
                              </div>
                            ) : (
                              <p className="text-[12px] leading-relaxed text-muted-foreground">
                                This detector is configured through policy
                                rules. Add or edit rules in the corresponding
                                policy workspace.
                              </p>
                            )}
                            {profile && detector.key !== "bot" ? (
                              <AllowedPatternList
                                profileId={profile.id}
                                detector={detector.key}
                                disabled={updateProfile.isPending}
                              />
                            ) : null}
                          </div>
                        </div>
                      </div>
                    ) : null}
                  </article>
                );
              })}
            </div>
          </section>

          {updateProfile.isError ? (
            <SecurityNotice title="Policy update failed" tone="warning">
              <span role="alert">
                {(updateProfile.error as Error)?.message ??
                  "The server rejected the update."}
              </span>
            </SecurityNotice>
          ) : null}
        </div>
      ) : activeTab === "runtime" ? (
        <section aria-label="Failure behavior">
          <div className="mb-4">
            <h2 className="text-[13px] font-medium text-foreground">
              Failure behavior
            </h2>
            <p className="mt-1 text-[12px] text-muted-foreground">
              Determines gateway behavior if security evaluation cannot
              complete. Read-only in the console.
            </p>
          </div>
          <div className="grid divide-y divide-border-subtle border-y border-border-subtle sm:grid-cols-2 sm:divide-x sm:divide-y-0">
            <RuntimeModeCard
              active={securityMode === "fail-open"}
              icon={Unlock}
              title="Fail open"
              description="Pass traffic when evaluation cannot complete. Availability-first and higher risk."
            />
            <RuntimeModeCard
              active={securityMode === "fail-closed"}
              icon={Lock}
              title="Fail closed"
              description="Return 503 when evaluation cannot complete. Enforcement-first."
            />
          </div>
          <p className="mt-4 text-[12px] text-muted-foreground">
            Set{" "}
            <code className="rounded-sm border border-border-subtle bg-surface-2 px-1.5 py-0.5 font-mono text-[11px] text-foreground">
              BASTIO_SECURITY_MODE
            </code>{" "}
            and restart the service to change this behavior.
          </p>
        </section>
      ) : (
        extraTabs.find((item) => item.id === activeTab)?.component ?? null
      )}
    </div>
  );
}

function AllowedPatternList({
  profileId,
  detector,
  disabled,
}: {
  profileId: string;
  detector: string;
  disabled: boolean;
}) {
  const queryClient = useQueryClient();
  const listed = useQuery({
    queryKey: ["security-suppressions", profileId],
    queryFn: () => api.security.listSuppressions(profileId),
  });
  const rows = (listed.data ?? []).filter(
    (row) => row.detector.toLowerCase() === detector.toLowerCase(),
  );
  const remove = useMutation({
    mutationFn: (id: string) => api.security.deleteSuppression(profileId, id),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: ["security-suppressions"],
      });
    },
  });
  return (
    <div className="space-y-2 border-t border-border-subtle pt-3">
      <p className="text-[11px] font-medium text-muted-foreground">
        Allowed patterns
      </p>
      {rows.length ? (
        <ul className="divide-y divide-border-subtle border-y border-border-subtle">
          {rows.map((row) => (
            <li
              key={row.id}
              className="flex items-center gap-2 py-1.5"
            >
              <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-foreground">
                {row.pattern}
              </span>
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                disabled={disabled || remove.isPending}
                onClick={() => remove.mutate(row.id)}
                aria-label={`Stop allowing ${row.pattern}`}
              >
                <Trash2 className="size-3" />
              </Button>
            </li>
          ))}
        </ul>
      ) : (
        <p className="text-[11px] text-muted-foreground">
          None. Allow a false positive from a threat row.
        </p>
      )}
    </div>
  );
}

function TopicPatternList({
  profileId,
  disabled,
}: {
  profileId: string;
  disabled: boolean;
}) {
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState("");
  const [action, setAction] = useState<"warn" | "block">("warn");
  const patterns = useQuery({
    queryKey: ["security-patterns", profileId],
    queryFn: () => api.security.listPatterns(profileId),
  });
  const create = useMutation({
    mutationFn: () =>
      api.security.createPattern(profileId, {
        pattern: draft.trim(),
        action,
        pattern_type: "keyword",
      }),
    onSuccess: () => {
      setDraft("");
      void queryClient.invalidateQueries({
        queryKey: ["security-patterns", profileId],
      });
      void queryClient.invalidateQueries({ queryKey: ["security-profiles"] });
    },
  });
  const remove = useMutation({
    mutationFn: (patternId: string) =>
      api.security.deletePattern(profileId, patternId),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: ["security-patterns", profileId],
      });
    },
  });
  return (
    <div className="space-y-3">
      <p className="text-[11px] leading-relaxed text-muted-foreground">
        Keywords matched in prompts. Adding the first keyword enables Topic
        Policy.
      </p>
      <form
        className="flex items-center gap-2"
        onSubmit={(event) => {
          event.preventDefault();
          if (!draft.trim() || create.isPending) return;
          create.mutate();
        }}
      >
        <label htmlFor="topic-keyword-input" className="sr-only">
          Topic keyword
        </label>
        <Input
          id="topic-keyword-input"
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          placeholder="e.g. competitor name"
          autoCorrect="off"
          autoCapitalize="none"
          spellCheck={false}
          disabled={disabled || create.isPending}
          className="h-8 text-[12px]"
        />
        <Select
          value={action}
          onValueChange={(next) => next && setAction(next as "warn" | "block")}
        >
          <SelectTrigger
            aria-label="Keyword action"
            className="h-8 w-[88px]"
            disabled={disabled || create.isPending}
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="warn">Warn</SelectItem>
            <SelectItem value="block">Block</SelectItem>
          </SelectContent>
        </Select>
        <Button
          type="submit"
          size="sm"
          variant="outline"
          disabled={disabled || create.isPending || !draft.trim()}
        >
          <Plus className="size-3.5" /> Add
        </Button>
      </form>
      {patterns.data?.length ? (
        <ul className="divide-y divide-border-subtle border-y border-border-subtle">
          {patterns.data.map((pattern) => (
            <li
              key={pattern.id}
              className="flex items-center gap-2 py-1.5"
            >
              <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-foreground">
                {pattern.pattern}
              </span>
              <Badge variant="outline" className="h-4 px-1.5 text-[9px]">
                {pattern.action}
              </Badge>
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                disabled={disabled || remove.isPending}
                onClick={() => remove.mutate(pattern.id)}
                aria-label={`Remove ${pattern.pattern}`}
              >
                <Trash2 className="size-3" />
              </Button>
            </li>
          ))}
        </ul>
      ) : (
        <p className="text-[11px] text-muted-foreground">No keywords yet.</p>
      )}
      {create.isError ? (
        <p role="alert" className="text-[11px] text-destructive">
          {(create.error as Error).message}
        </p>
      ) : null}
    </div>
  );
}

function PiiControls({
  profile,
  disabled,
  onUpdate,
}: {
  profile: SecurityProfile;
  disabled: boolean;
  onUpdate: (data: UpdateSecurityProfileRequest) => void;
}) {
  return (
    <div className="space-y-3">
      <div>
        <label
          htmlFor="pii-action-select"
          className="mb-1.5 block text-[11px] text-muted-foreground"
        >
          Action
        </label>
        <Select
          value={profile.pii_action}
          onValueChange={(value) =>
            value && onUpdate({ pii_action: value as PIIAction })
          }
        >
          <SelectTrigger
            id="pii-action-select"
            className="w-full"
            aria-label="PII enforcement action"
            disabled={disabled}
          >
            <SelectValue>{ACTION_HELP[profile.pii_action].label}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            {PII_ACTION_OPTIONS.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                <ActionOption action={option.value} />
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <ActionHelp action={profile.pii_action} pii />
      </div>
      {profile.pii_action === "tokenize" ? (
        <div>
          <label
            htmlFor="pii-token-style-select"
            className="mb-1.5 block text-[11px] text-muted-foreground"
          >
            Token style
          </label>
          <Select
            value={profile.pii_token_style}
            onValueChange={(value) =>
              value && onUpdate({ pii_token_style: value as PIITokenStyle })
            }
          >
            <SelectTrigger
              id="pii-token-style-select"
              className="w-full"
              disabled={disabled}
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {PII_TOKEN_STYLE_OPTIONS.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  <code>{option.label}</code>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      ) : null}
      <div className="flex items-start gap-2.5 text-[11px] leading-relaxed text-muted-foreground">
        <input
          id="pii-scan-response"
          type="checkbox"
          className="mt-0.5 size-3.5 accent-foreground"
          checked={profile.pii_scan_response}
          onChange={(event) =>
            onUpdate({ pii_scan_response: event.target.checked })
          }
          disabled={disabled}
        />
        <label htmlFor="pii-scan-response" className="cursor-pointer">
          Scan model responses for hallucinated or retrieved PII
        </label>
      </div>
      {profile.pii_action === "tokenize" ? (
        <div className="flex items-start gap-2.5 text-[11px] leading-relaxed text-muted-foreground">
          <input
            id="pii-restore-response"
            type="checkbox"
            className="mt-0.5 size-3.5 accent-foreground"
            checked={profile.pii_restore_response}
            onChange={(event) =>
              onUpdate({ pii_restore_response: event.target.checked })
            }
            disabled={disabled}
          />
          <label htmlFor="pii-restore-response" className="cursor-pointer">
            Restore original values in the response
          </label>
        </div>
      ) : null}
    </div>
  );
}

function StrategyField({
  id,
  value,
  options,
  onChange,
  disabled,
}: {
  id: string;
  value: Strategy;
  options: readonly { value: Strategy; label: string }[];
  onChange: (next: Strategy) => void;
  disabled: boolean;
}) {
  return (
    <div>
      <label
        htmlFor={id}
        className="mb-1.5 block text-[11px] text-muted-foreground"
      >
        Action
      </label>
      <Select
        value={value}
        onValueChange={(next) => next && onChange(next as Strategy)}
      >
        <SelectTrigger
          id={id}
          className="w-full"
          aria-label="Detector enforcement action"
          disabled={disabled}
        >
          <SelectValue>{ACTION_HELP[value].label}</SelectValue>
        </SelectTrigger>
        <SelectContent>
          {options.map((option) => (
            <SelectItem key={option.value} value={option.value}>
              <ActionOption action={option.value} />
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <ActionHelp action={value} />
    </div>
  );
}

function ActionOption({ action }: { action: Strategy }) {
  const help = ACTION_HELP[action];
  return (
    <span className="block min-w-0 whitespace-normal py-0.5">
      <span className="block text-[11px] font-medium">{help.label}</span>
      <span className="block text-[10px] leading-relaxed text-muted-foreground">
        {help.outcome}
      </span>
    </span>
  );
}

function ThresholdField({
  id,
  value,
  onChange,
  disabled,
}: {
  id: string;
  value: number;
  onChange: (next: number) => void;
  disabled: boolean;
}) {
  const [draft, setDraft] = useState(value.toString());
  useEffect(() => setDraft(value.toString()), [value]);
  const commit = () => {
    const parsed = Number(draft);
    if (
      Number.isFinite(parsed) &&
      parsed >= 0 &&
      parsed <= 1 &&
      parsed !== value
    )
      onChange(parsed);
    else setDraft(value.toString());
  };
  return (
    <div>
      <label
        htmlFor={id}
        className="mb-1.5 block text-[11px] text-muted-foreground"
      >
        Confidence threshold
      </label>
      <div className="flex items-center gap-2.5">
        <input
          id={id}
          type="number"
          min={0}
          max={1}
          step={0.05}
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          onBlur={commit}
          onKeyDown={(event) =>
            event.key === "Enter" && (event.target as HTMLInputElement).blur()
          }
          disabled={disabled}
          aria-describedby={`${id}-hint`}
          className="h-8 w-24 rounded-md border border-input bg-background px-2.5 font-mono text-[12px] tabular-nums outline-none focus-visible:ring-2 focus-visible:ring-ring"
        />
        <span id={`${id}-hint`} className="text-[11px] text-muted-foreground">
          0 sensitive · 1 strict
        </span>
      </div>
    </div>
  );
}

function RuntimeModeCard({
  active,
  icon: Icon,
  title,
  description,
}: {
  active: boolean;
  icon: typeof Lock;
  title: string;
  description: string;
}) {
  return (
    <div className={cn("py-5 sm:px-5 first:sm:pl-0", !active && "opacity-55")}>
      <div className="flex items-start gap-3">
        <Icon
          aria-hidden="true"
          className={cn(
            "mt-0.5 size-4 shrink-0",
            active ? "text-foreground" : "text-muted-foreground",
          )}
        />
        <div>
          <div className="flex items-center gap-2">
            <h3 className="text-[13px] font-medium text-foreground">{title}</h3>
            {active ? (
              <span className="inline-flex items-center gap-1.5 text-[11px] text-muted-foreground">
                <span className="size-1.5 rounded-full bg-success" aria-hidden="true" />
                Current
              </span>
            ) : null}
          </div>
          <p className="mt-1 text-[12px] leading-relaxed text-muted-foreground">
            {description}
          </p>
        </div>
      </div>
    </div>
  );
}
