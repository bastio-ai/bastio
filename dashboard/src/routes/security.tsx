import { useEffect, useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  ShieldCheck,
  Lock,
  Unlock,
  AlertCircle,
  Wand2,
  KeyRound,
  GitBranch,
  FileOutput,
  ListChecks,
} from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { PageHeader, SectionHeader } from "@/components/card";
import { useSecurityExtension } from "@/components/security-extension";
import { cn } from "@/lib/utils";
import { api } from "@/api/client";
import type { SecurityProfile, UpdateSecurityProfileRequest } from "@/api/client";

type PIIAction = SecurityProfile["pii_action"];
type PIITokenStyle = SecurityProfile["pii_token_style"];

const PII_ACTION_OPTIONS: { value: PIIAction; label: string; hint: string }[] = [
  { value: "mask", label: "Mask", hint: "Lossy one-way (***-**-6789)" },
  { value: "tokenize", label: "Tokenize", hint: "Reversible <PII_SSN_1>" },
  { value: "block", label: "Block", hint: "Reject any request with PII" },
  { value: "warn", label: "Warn", hint: "Pass through with warning" },
  { value: "log_only", label: "Log only", hint: "Detect silently, no rewrite" },
];

const PII_TOKEN_STYLE_OPTIONS: { value: PIITokenStyle; label: string }[] = [
  { value: "angle", label: "<PII_SSN_1>" },
  { value: "curly", label: "{{PII_SSN_1}}" },
];

type Strategy = "block" | "mask" | "tokenize" | "warn" | "log_only";

const STRATEGY_HINT: Record<Strategy, string> = {
  block: "Reject the request / response outright.",
  mask: "Rewrite with a one-way mask (e.g. ***-**-6789). Irreversible.",
  tokenize: "Replace with a reversible placeholder. Restored on response.",
  warn: "Allow through but attach a threat finding for the security team.",
  log_only: "Record the detection silently. No user-visible effect.",
};

const STRATEGY_OPTIONS: Record<"text" | "secrets" | "pii", { value: Strategy; label: string }[]> = {
  // Text-threat detectors — nothing meaningful to sanitize.
  text: [
    { value: "block", label: "Block" },
    { value: "warn", label: "Warn" },
    { value: "log_only", label: "Log only" },
  ],
  // Secrets — mask is a valid and common default.
  secrets: [
    { value: "block", label: "Block" },
    { value: "mask", label: "Mask" },
    { value: "warn", label: "Warn" },
    { value: "log_only", label: "Log only" },
  ],
  // PII — full menu including tokenize (reversible).
  pii: [
    { value: "mask", label: "Mask" },
    { value: "tokenize", label: "Tokenize" },
    { value: "block", label: "Block" },
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
    description: "Detects attempts to override system instructions, extract prompts, and multi-language variants (EN/FR/DE/ES/PT/IT)",
    icon: ShieldCheck,
  },
  {
    key: "pii",
    label: "PII Detection",
    enabledField: "pii_enabled" as const,
    strategyField: null,
    thresholdField: null,
    strategyOptions: STRATEGY_OPTIONS.pii,
    patterns: 11,
    description: "Emails, SSN, Luhn-validated credit cards, phones, IPs, IBAN, UK NI, Canadian SIN, EU VAT, Aadhaar",
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
    description: "DAN, evil mode, fiction framing, hypothetical sandbagging, developer-mode impersonation",
    icon: ShieldCheck,
  },
  {
    key: "secrets",
    label: "Secrets Detection",
    enabledField: "secrets_enabled" as const,
    strategyField: "secrets_strategy" as const,
    thresholdField: null,
    strategyOptions: STRATEGY_OPTIONS.secrets,
    patterns: 12,
    description: "AWS / GCP / GitHub / Stripe / OpenAI / Anthropic / Slack keys, JWTs, private keys, high-entropy assignments",
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
    description: "Scans tool / retrieval / memory content (not user input) for embedded prompts — the attack surface in RAG and agent tools",
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
    description: "Catches model responses that leak the system prompt, echo embedded secrets, or regurgitate training data",
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
    description: "Per-customer regex/keyword rules from the security_patterns table — competitor lists, regulated topics, internal terminology",
    icon: ListChecks,
  },
  {
    key: "bot",
    label: "Bot Detection",
    enabledField: "bot_detection_enabled" as const,
    strategyField: null,
    thresholdField: null,
    strategyOptions: null,
    patterns: 0,
    description: "Behavioral fingerprinting and automated request detection (coming soon)",
    icon: ShieldCheck,
  },
] as const;

export function SecurityPage() {
  const queryClient = useQueryClient();
  const { data: profiles } = useQuery({ queryKey: ["security-profiles"], queryFn: api.security.profiles });
  const { data: appConfig } = useQuery({ queryKey: ["config"], queryFn: api.config });

  const profile = profiles?.[0] as SecurityProfile | undefined;
  const securityMode = appConfig?.security_mode ?? "fail-open";

  const updateProfile = useMutation({
    mutationFn: (data: UpdateSecurityProfileRequest) =>
      profile ? api.security.updateProfile(profile.id, data) : Promise.resolve({ status: "no profile" }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["security-profiles"] }),
  });

  const toggleDetector = (field: keyof UpdateSecurityProfileRequest, currentValue: boolean) => {
    updateProfile.mutate({ [field]: !currentValue } as UpdateSecurityProfileRequest);
  };

  // Cloud (or any other consumer) can inject extra tabs into the
  // Security Center via SecurityExtensionProvider. OSS standalone
  // never wraps with the provider, so extraTabs is always empty
  // and the page renders just the "Detectors" tab as before.
  const { extraTabs = [] } = useSecurityExtension();

  return (
    <>
      <PageHeader title="Security Center" description="Threat detection and security policy configuration" />

      <Tabs defaultValue="detectors" className="mt-2">
        <TabsList variant="line">
          <TabsTrigger value="detectors">Detectors</TabsTrigger>
          {extraTabs.map((t) => (
            <TabsTrigger key={t.id} value={t.id}>
              {t.label}
            </TabsTrigger>
          ))}
        </TabsList>

        <TabsContent value="detectors" className="pt-6">
          <SectionHeader title="Preprocessing" description="Normalize input before detection runs" />

      <Card className="border-border/50 mb-6">
        <CardContent className="p-4">
          <div className="flex items-start gap-3">
            <div className={cn(
              "flex h-8 w-8 items-center justify-center rounded-lg flex-shrink-0",
              profile?.canonicalize_enabled
                ? "bg-success-bg text-success"
                : "bg-muted/50 text-muted-foreground/50"
            )}>
              <Wand2 className="h-4 w-4" />
            </div>
            <div className="flex-1 min-w-0">
              <div className="flex items-center justify-between gap-2">
                <p className="text-[13px] font-semibold">Canonicalization</p>
                <Button
                  variant="ghost" size="sm"
                  className={cn("h-7 text-xs", profile?.canonicalize_enabled ? "text-success" : "text-muted-foreground")}
                  onClick={() => profile && updateProfile.mutate({ canonicalize_enabled: !profile.canonicalize_enabled })}
                  disabled={!profile || updateProfile.isPending}
                >
                  {profile?.canonicalize_enabled ? "Enabled" : "Disabled"}
                </Button>
              </div>
              <p className="text-xs text-muted-foreground leading-relaxed mt-1">
                Normalizes unicode, strips zero-width characters, folds Cyrillic / Greek homoglyphs to Latin, and
                decodes embedded base64/hex/URL payloads before detectors see the content. Biggest single defense
                against encoding-based bypasses.
              </p>
            </div>
          </div>
        </CardContent>
      </Card>

      <SectionHeader
        title="Detectors"
        description="Each detector ships with a strategy (block / warn / log-only, plus mask for secrets and the full PII menu) and, where applicable, a confidence threshold. Tune per-detector to match your risk appetite."
      />

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mb-8">
        {detectorMeta.map((d) => {
          const enabled = profile ? (profile[d.enabledField] as boolean) : d.key !== "bot";

          return (
            <Card key={d.key} className={cn("border-border/50 transition-all", enabled ? "hover:border-border/80" : "opacity-60")}>
              <CardHeader className="pb-3">
                <div className="flex items-start justify-between">
                  <div className="flex items-center gap-2.5">
                    <div className={cn(
                      "flex h-8 w-8 items-center justify-center rounded-lg",
                      enabled
                        ? "bg-success-bg text-success"
                        : "bg-muted/50 text-muted-foreground/50"
                    )}>
                      <d.icon className="h-4 w-4" />
                    </div>
                    <div>
                      <CardTitle className="text-[13px] font-semibold">{d.label}</CardTitle>
                    </div>
                  </div>
                  <Button
                    variant="ghost" size="sm"
                    className={cn("h-7 text-xs", enabled ? "text-success" : "text-muted-foreground")}
                    onClick={() => toggleDetector(d.enabledField, enabled)}
                    disabled={d.key === "bot" || updateProfile.isPending}
                  >
                    {enabled ? "Enabled" : "Disabled"}
                  </Button>
                </div>
              </CardHeader>
              <CardContent className="pt-0">
                <p className="text-xs text-muted-foreground leading-relaxed">{d.description}</p>
                <div className="mt-3 flex items-center gap-3">
                  {d.patterns > 0 && (
                    <span className="text-[11px] text-muted-foreground/50">
                      <span className="font-medium text-foreground/60">{d.patterns}</span> patterns
                    </span>
                  )}
                </div>

                {/* Strategy + threshold editor. Always shown so the
                    operator can see and edit policy without first
                    enabling the detector. Greyed when disabled.
                    PII uses its own custom block below. */}
                {d.key !== "pii" && d.strategyField && d.strategyOptions && profile && (
                  <div className={cn(
                    "mt-4 space-y-3 border-t border-border/50 pt-3",
                    !enabled && "opacity-60",
                  )}>
                    <StrategyField
                      value={(profile[d.strategyField] ?? d.strategyOptions[0]?.value) as Strategy}
                      options={d.strategyOptions}
                      onChange={(next) =>
                        updateProfile.mutate({ [d.strategyField as string]: next } as UpdateSecurityProfileRequest)
                      }
                      disabled={updateProfile.isPending || !enabled}
                    />
                    {d.thresholdField && (
                      <ThresholdField
                        value={(profile[d.thresholdField] as number) ?? 0}
                        onChange={(next) =>
                          updateProfile.mutate({ [d.thresholdField as string]: next } as UpdateSecurityProfileRequest)
                        }
                        disabled={updateProfile.isPending || !enabled}
                      />
                    )}
                    {updateProfile.isError && (
                      <div className="flex items-start gap-1.5 text-[11px] text-destructive">
                        <AlertCircle className="h-3 w-3 mt-0.5 shrink-0" />
                        <span>{(updateProfile.error as Error)?.message ?? "Save failed"}</span>
                      </div>
                    )}
                  </div>
                )}

                {/* PII handling controls */}
                {d.key === "pii" && profile && (
                  <div className="mt-4 space-y-3 border-t border-border/50 pt-3">
                    <div className="space-y-1">
                      <div className="flex items-center gap-2">
                        <span className="text-[11px] text-muted-foreground w-20">Action</span>
                        <Select
                          value={profile.pii_action}
                          onValueChange={(v) => v && updateProfile.mutate({ pii_action: v as PIIAction })}
                        >
                          <SelectTrigger className="h-7 text-xs w-36">
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            {PII_ACTION_OPTIONS.map((opt) => (
                              <SelectItem key={opt.value} value={opt.value}>
                                {opt.label}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      </div>
                      <p className="text-[10px] text-muted-foreground/80 pl-[88px]">
                        {PII_ACTION_OPTIONS.find((o) => o.value === profile.pii_action)?.hint}
                      </p>
                    </div>

                    {profile.pii_action === "tokenize" && (
                      <div className="flex items-center gap-2">
                        <span className="text-[11px] text-muted-foreground w-20">Token style</span>
                        <Select
                          value={profile.pii_token_style}
                          onValueChange={(v) => v && updateProfile.mutate({ pii_token_style: v as PIITokenStyle })}
                        >
                          <SelectTrigger className="h-7 text-xs w-36">
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            {PII_TOKEN_STYLE_OPTIONS.map((opt) => (
                              <SelectItem key={opt.value} value={opt.value}>
                                <span className="font-mono text-[11px]">{opt.label}</span>
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      </div>
                    )}

                    <label className="flex items-center gap-2 text-[11px] text-muted-foreground cursor-pointer select-none">
                      <input
                        type="checkbox"
                        className="h-3.5 w-3.5 rounded border-border"
                        checked={profile.pii_scan_response}
                        onChange={(e) => updateProfile.mutate({ pii_scan_response: e.target.checked })}
                      />
                      Scan LLM response for PII (hallucination / RAG leak)
                    </label>

                    {profile.pii_action === "tokenize" && (
                      <label className="flex items-center gap-2 text-[11px] text-muted-foreground cursor-pointer select-none">
                        <input
                          type="checkbox"
                          className="h-3.5 w-3.5 rounded border-border"
                          checked={profile.pii_restore_response}
                          onChange={(e) => updateProfile.mutate({ pii_restore_response: e.target.checked })}
                        />
                        Restore originals in response (swap placeholders back)
                      </label>
                    )}

                    {updateProfile.isError && (
                      <div className="flex items-start gap-1.5 text-[11px] text-destructive">
                        <AlertCircle className="h-3 w-3 mt-0.5 shrink-0" />
                        <span>{(updateProfile.error as Error)?.message ?? "Save failed"}</span>
                      </div>
                    )}
                    {updateProfile.isPending && (
                      <span className="text-[11px] text-muted-foreground">Saving…</span>
                    )}
                  </div>
                )}
              </CardContent>
            </Card>
          );
        })}
      </div>

      <SectionHeader
        title="Security Mode"
        description="How the gateway handles requests when security evaluation can't complete (engine not configured, profile lookup error). Distinct from per-detector strategies above."
      />

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <SecurityModeCard
          mode="fail-open"
          active={securityMode === "fail-open"}
          icon={Unlock}
          title="Fail Open"
          description="Security errors pass the request through to the LLM. Prioritizes availability over strict enforcement."
        />
        <SecurityModeCard
          mode="fail-closed"
          active={securityMode === "fail-closed"}
          icon={Lock}
          title="Fail Closed"
          description="Security errors return 503 before reaching the LLM. Prioritizes strict enforcement over availability."
        />
      </div>

      <p className="mt-3 text-[11px] text-muted-foreground">
        Set via the{" "}
        <code className="font-mono text-[10px] px-1 py-0.5 rounded bg-muted/50 border border-border/40">
          BASTIO_SECURITY_MODE
        </code>
        {" "}environment variable. Restart the server to change.
      </p>
        </TabsContent>

        {extraTabs.map((t) => (
          <TabsContent key={t.id} value={t.id} className="pt-6">
            {t.component}
          </TabsContent>
        ))}
      </Tabs>
    </>
  );
}

interface SecurityModeCardProps {
  mode: "fail-open" | "fail-closed";
  active: boolean;
  icon: typeof Unlock;
  title: string;
  description: string;
}

function SecurityModeCard({ active, icon: Icon, title, description }: SecurityModeCardProps) {
  return (
    <Card
      className={cn(
        "transition-colors",
        active
          ? "border-2 border-foreground/10 bg-foreground/[0.02]"
          : "border-border/50 opacity-60",
      )}
    >
      <CardContent className="p-5">
        <div className="flex items-center gap-3 mb-3">
          <div
            className={cn(
              "flex h-8 w-8 items-center justify-center rounded-lg",
              active ? "bg-warn-bg text-warn" : "bg-muted/50 text-muted-foreground/50",
            )}
          >
            <Icon className="h-4 w-4" />
          </div>
          <div>
            <p className="text-[13px] font-semibold">{title}</p>
            {active ? (
              <Badge variant="outline" className="text-[10px] px-1.5 py-0 mt-0.5">
                Current
              </Badge>
            ) : null}
          </div>
        </div>
        <p className="text-xs text-muted-foreground leading-relaxed">{description}</p>
      </CardContent>
    </Card>
  );
}

interface StrategyFieldProps {
  value: Strategy;
  options: { value: Strategy; label: string }[];
  onChange: (next: Strategy) => void;
  disabled: boolean;
}

// StrategyField renders the per-detector policy selector. Each option
// lists only the strategies that make sense for the detector class —
// text-threat detectors omit mask/tokenize, secrets omits tokenize,
// PII uses the full menu via the existing custom controls.
function StrategyField({ value, options, onChange, disabled }: StrategyFieldProps) {
  return (
    <div className="space-y-1">
      <div className="flex items-center gap-2">
        <span className="text-[11px] text-muted-foreground w-20">Strategy</span>
        <Select
          value={value}
          onValueChange={(v) => v && onChange(v as Strategy)}
        >
          <SelectTrigger className="h-7 text-xs w-36" disabled={disabled}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {options.map((opt) => (
              <SelectItem key={opt.value} value={opt.value}>
                {opt.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <p className="text-[10px] text-muted-foreground/80 pl-[88px]">
        {STRATEGY_HINT[value] ?? ""}
      </p>
    </div>
  );
}

interface ThresholdFieldProps {
  value: number;
  onChange: (next: number) => void;
  disabled: boolean;
}

// ThresholdField is a plain number input, not a slider. Sliders imply
// a precision we don't have — real-world attack scores cluster in
// documented bands, so asking for a number + showing that band is
// more honest. The hint text is the production-data reality.
function ThresholdField({ value, onChange, disabled }: ThresholdFieldProps) {
  const [draft, setDraft] = useState(value.toString());
  useEffect(() => {
    setDraft(value.toString());
  }, [value]);

  const commit = () => {
    const parsed = Number(draft);
    if (Number.isFinite(parsed) && parsed >= 0 && parsed <= 1 && parsed !== value) {
      onChange(parsed);
    } else {
      setDraft(value.toString());
    }
  };

  return (
    <div className="space-y-1">
      <div className="flex items-center gap-2">
        <span className="text-[11px] text-muted-foreground w-20">Threshold</span>
        <input
          type="number"
          min={0}
          max={1}
          step={0.05}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onBlur={commit}
          onKeyDown={(e) => {
            if (e.key === "Enter") (e.target as HTMLInputElement).blur();
          }}
          disabled={disabled}
          className="h-7 w-24 rounded-md border border-border bg-background px-2 text-[12px] font-mono focus:outline-none focus:ring-1 focus:ring-foreground/20"
        />
        <span className="text-[11px] text-muted-foreground/60">
          weighted score 0–1
        </span>
      </div>
      <p className="text-[10px] text-muted-foreground/80 pl-[88px]">
        Real-world attacks typically score 0.6–0.9. Lower = more sensitive (more false positives); higher = fewer false positives but more misses.
      </p>
    </div>
  );
}
