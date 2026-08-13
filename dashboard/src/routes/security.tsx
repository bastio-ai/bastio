import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Bot,
  Check,
  ChevronDown,
  FileOutput,
  GitBranch,
  KeyRound,
  ListChecks,
  Lock,
  ShieldCheck,
  Unlock,
  Wand2,
} from "lucide-react";
import { AdminSummaryStrip, SecurityNotice } from "@/components/admin/admin-primitives";
import { PageHeader } from "@/components/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useSecurityExtension } from "@/components/security-extension";
import { api } from "@/api/client";
import type { SecurityProfile, UpdateSecurityProfileRequest } from "@/api/client";
import { cn } from "@/lib/utils";

type PIIAction = SecurityProfile["pii_action"];
type PIITokenStyle = SecurityProfile["pii_token_style"];
type Strategy = "block" | "mask" | "tokenize" | "warn" | "log_only";

const PII_ACTION_OPTIONS: { value: PIIAction; label: string; hint: string }[] = [
  { value: "mask", label: "Mask", hint: "Lossy one-way redaction" },
  { value: "tokenize", label: "Tokenize", hint: "Reversible placeholders" },
  { value: "block", label: "Block", hint: "Reject requests containing PII" },
  { value: "warn", label: "Warn", hint: "Allow with a security finding" },
  { value: "log_only", label: "Log only", hint: "Observe without enforcement" },
];

const PII_TOKEN_STYLE_OPTIONS: { value: PIITokenStyle; label: string }[] = [
  { value: "angle", label: "<PII_SSN_1>" },
  { value: "curly", label: "{{PII_SSN_1}}" },
];

const STRATEGY_HINT: Record<Strategy, string> = {
  block: "Reject the request or response before it reaches the next system.",
  mask: "Replace the detected secret with an irreversible mask.",
  tokenize: "Replace PII with a reversible placeholder.",
  warn: "Allow the request and create a security finding.",
  log_only: "Record the detection without changing traffic.",
};

const STRATEGY_OPTIONS: Record<"text" | "secrets", { value: Strategy; label: string }[]> = {
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
  { key: "injection", label: "Prompt Injection", enabledField: "injection_enabled" as const, strategyField: "injection_strategy" as const, thresholdField: "injection_threshold" as const, strategyOptions: STRATEGY_OPTIONS.text, patterns: 24, description: "System instruction override, prompt extraction, and multilingual injection variants.", icon: ShieldCheck },
  { key: "pii", label: "PII Detection", enabledField: "pii_enabled" as const, strategyField: null, thresholdField: null, strategyOptions: null, patterns: 11, description: "Emails, government identifiers, payment data, phone numbers, IPs, and banking identifiers.", icon: ShieldCheck },
  { key: "jailbreak", label: "Jailbreak Detection", enabledField: "jailbreak_enabled" as const, strategyField: "jailbreak_strategy" as const, thresholdField: "jailbreak_threshold" as const, strategyOptions: STRATEGY_OPTIONS.text, patterns: 20, description: "DAN, developer-mode impersonation, fiction framing, and hypothetical sandbagging.", icon: ShieldCheck },
  { key: "secrets", label: "Secrets Detection", enabledField: "secrets_enabled" as const, strategyField: "secrets_strategy" as const, thresholdField: null, strategyOptions: STRATEGY_OPTIONS.secrets, patterns: 12, description: "Cloud, source control, payment, LLM and communication credentials, JWTs, and private keys.", icon: KeyRound },
  { key: "indirect_injection", label: "Indirect Injection", enabledField: "indirect_injection_enabled" as const, strategyField: "indirect_injection_strategy" as const, thresholdField: null, strategyOptions: STRATEGY_OPTIONS.text, patterns: 10, description: "Embedded prompts retrieved from tools, memory, documents, and RAG content.", icon: GitBranch },
  { key: "output_exfil", label: "Output Exfiltration", enabledField: "output_exfil_enabled" as const, strategyField: "output_exfil_strategy" as const, thresholdField: null, strategyOptions: STRATEGY_OPTIONS.text, patterns: 8, description: "Model responses that expose system prompts, embedded secrets, or memorized data.", icon: FileOutput },
  { key: "topic_policy", label: "Topic Policy", enabledField: "topic_policy_enabled" as const, strategyField: null, thresholdField: null, strategyOptions: null, patterns: 0, description: "Customer-defined rules for regulated topics, competitors, and internal terminology.", icon: ListChecks },
  { key: "bot", label: "Bot Detection", enabledField: "bot_detection_enabled" as const, strategyField: null, thresholdField: null, strategyOptions: null, patterns: 0, description: "Behavioral fingerprinting and automated request detection.", icon: Bot },
] as const;

export function SecurityPage() {
  const queryClient = useQueryClient();
  const { data: profiles, isLoading } = useQuery({ queryKey: ["security-profiles"], queryFn: api.security.profiles });
  const { data: appConfig } = useQuery({ queryKey: ["config"], queryFn: api.config });
  const [expanded, setExpanded] = useState<string | null>("injection");
  const profile = profiles?.[0] as SecurityProfile | undefined;
  const securityMode = appConfig?.security_mode ?? "fail-open";
  const { extraTabs = [] } = useSecurityExtension();

  const updateProfile = useMutation({
    mutationFn: (data: UpdateSecurityProfileRequest) => profile ? api.security.updateProfile(profile.id, data) : Promise.resolve({ status: "no profile" }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["security-profiles"] }),
  });

  const enabledCount = profile ? detectorMeta.filter((detector) => Boolean(profile[detector.enabledField])).length : 0;
  const configuredStrategies = profile ? detectorMeta.map((detector) => {
    if (detector.key === "pii") return profile.pii_action as Strategy;
    return detector.strategyField ? (profile[detector.strategyField] as Strategy) : null;
  }).filter(Boolean) as Strategy[] : [];
  const blockCount = configuredStrategies.filter((strategy) => strategy === "block").length;
  const transformCount = configuredStrategies.filter((strategy) => strategy === "mask" || strategy === "tokenize").length;

  const update = (data: UpdateSecurityProfileRequest) => updateProfile.mutate(data);

  return (
    <>
      <PageHeader
        title="Security Center"
        description="Configure preprocessing, detection, and enforcement across every gateway route."
        badge={updateProfile.isPending ? <Badge variant="outline" className="text-[9px]">Saving changes…</Badge> : updateProfile.isSuccess ? <Badge variant="success" className="text-[9px]"><Check /> Saved</Badge> : undefined}
      />

      <AdminSummaryStrip items={[
        { label: "Detectors enabled", value: isLoading ? "—" : `${enabledCount}/${detectorMeta.length}`, detail: "Across the active profile", tone: "success" },
        { label: "Blocking policies", value: blockCount, detail: "Reject before provider" },
        { label: "Transform policies", value: transformCount, detail: "Mask or tokenize" },
        { label: "Failure mode", value: securityMode === "fail-closed" ? "Closed" : "Open", detail: securityMode === "fail-closed" ? "Enforcement first" : "Availability first", tone: securityMode === "fail-closed" ? "success" : "warning" },
      ]} />

      {securityMode === "fail-open" ? (
        <SecurityNotice title="Gateway is configured to fail open" tone="warning" className="mb-5">
          If the security engine or policy lookup fails, traffic continues to the model provider. Use fail-closed for regulated or high-risk workloads.
        </SecurityNotice>
      ) : (
        <SecurityNotice title="Gateway is configured to fail closed" tone="success" className="mb-5">
          Security evaluation must complete before traffic can reach the model provider.
        </SecurityNotice>
      )}

      <Tabs defaultValue="detectors">
        <TabsList variant="line" className="mb-4">
          <TabsTrigger value="detectors">Detection profile</TabsTrigger>
          <TabsTrigger value="runtime">Runtime enforcement</TabsTrigger>
          {extraTabs.map((tab) => <TabsTrigger key={tab.id} value={tab.id}>{tab.label}</TabsTrigger>)}
        </TabsList>

        <TabsContent value="detectors" className="space-y-5">
          <section className="overflow-hidden rounded-xl border border-border/70 bg-card">
            <div className="flex flex-col gap-3 p-4 sm:flex-row sm:items-center">
              <div className={cn("flex size-9 shrink-0 items-center justify-center rounded-lg border", profile?.canonicalize_enabled ? "border-success-border bg-success-bg text-success" : "border-border bg-muted/40 text-muted-foreground")}><Wand2 className="size-4" /></div>
              <div className="min-w-0 flex-1"><div className="flex flex-wrap items-center gap-2"><h2 className="text-[12px] font-medium">Input canonicalization</h2><Badge variant={profile?.canonicalize_enabled ? "success" : "secondary"} className="h-4 px-1.5 text-[9px]">{profile?.canonicalize_enabled ? "enabled" : "disabled"}</Badge></div><p className="mt-1 max-w-4xl text-[10px] leading-relaxed text-muted-foreground">Normalizes Unicode, removes zero-width characters, folds homoglyphs, and decodes embedded payloads before detection. This closes common encoding-based bypass paths.</p></div>
              <Button variant="outline" size="sm" disabled={!profile || updateProfile.isPending} onClick={() => profile && update({ canonicalize_enabled: !profile.canonicalize_enabled })}>{profile?.canonicalize_enabled ? "Disable" : "Enable"}</Button>
            </div>
          </section>

          <section className="overflow-hidden rounded-xl border border-border/70 bg-card">
            <div className="flex items-center justify-between border-b border-border/60 px-4 py-3"><div><h2 className="text-[12px] font-medium">Detector policies</h2><p className="mt-0.5 text-[10px] text-muted-foreground">Select a row to inspect and tune its enforcement behavior.</p></div><Badge variant="outline" className="font-mono text-[9px]">{enabledCount} enabled</Badge></div>
            {detectorMeta.map((detector) => {
              const enabled = profile ? Boolean(profile[detector.enabledField]) : detector.key !== "bot";
              const isExpanded = expanded === detector.key;
              const strategy = detector.key === "pii" ? profile?.pii_action : detector.strategyField && profile ? profile[detector.strategyField] : null;
              const Icon = detector.icon;
              return (
                <article key={detector.key} className="border-b border-border/50 last:border-b-0">
                  <div className="flex items-center gap-3 px-4 py-3 transition-colors hover:bg-muted/20">
                    <button type="button" className="flex min-w-0 flex-1 items-center gap-3 text-left" onClick={() => setExpanded(isExpanded ? null : detector.key)} aria-expanded={isExpanded}>
                      <div className={cn("flex size-8 shrink-0 items-center justify-center rounded-lg border", enabled ? "border-success-border bg-success-bg text-success" : "border-border bg-muted/40 text-muted-foreground")}><Icon className="size-3.5" /></div>
                      <div className="min-w-0 flex-1"><div className="flex flex-wrap items-center gap-2"><h3 className="text-[12px] font-medium">{detector.label}</h3>{detector.key === "bot" ? <Badge variant="secondary" className="h-4 px-1.5 text-[8px]">coming soon</Badge> : null}</div><p className="mt-0.5 truncate text-[10px] text-muted-foreground">{detector.description}</p></div>
                      <div className="hidden items-center gap-5 sm:flex"><div className="w-20"><p className="text-[9px] uppercase tracking-wide text-muted-foreground">Action</p><p className="mt-0.5 font-mono text-[10px] text-foreground">{strategy ? String(strategy).replace("_", " ") : "Custom"}</p></div><div className="w-16"><p className="text-[9px] uppercase tracking-wide text-muted-foreground">Coverage</p><p className="mt-0.5 font-mono text-[10px] text-foreground">{detector.patterns ? `${detector.patterns} rules` : "Policy"}</p></div></div>
                      <ChevronDown className={cn("size-4 shrink-0 text-muted-foreground transition-transform", isExpanded && "rotate-180")} />
                    </button>
                    <Button variant={enabled ? "outline" : "ghost"} size="sm" className={cn("w-20", enabled && "text-success")} disabled={!profile || detector.key === "bot" || updateProfile.isPending} onClick={() => profile && update({ [detector.enabledField]: !enabled } as UpdateSecurityProfileRequest)}>{enabled ? "Enabled" : "Disabled"}</Button>
                  </div>

                  {isExpanded ? (
                    <div className="border-t border-border/50 bg-muted/10 px-4 py-4 sm:pl-15">
                      <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_minmax(320px,0.75fr)]">
                        <div><p className="text-[10px] font-medium uppercase tracking-[0.1em] text-muted-foreground">Protection coverage</p><p className="mt-2 max-w-2xl text-[11px] leading-relaxed text-foreground/80">{detector.description}</p><div className="mt-3 flex flex-wrap gap-2"><Badge variant="outline" className="font-mono text-[9px]">{detector.patterns ? `${detector.patterns} built-in patterns` : "customer-defined policy"}</Badge><Badge variant={enabled ? "success" : "secondary"} className="text-[9px]">{enabled ? "evaluating traffic" : "not evaluating traffic"}</Badge></div></div>
                        <div className={cn("rounded-lg border border-border/70 bg-background p-3", !enabled && "opacity-60")}>
                          <p className="mb-3 text-[10px] font-medium uppercase tracking-[0.1em] text-muted-foreground">Enforcement</p>
                          {detector.key === "pii" && profile ? <PiiControls profile={profile} disabled={!enabled || updateProfile.isPending} onUpdate={update} /> : detector.strategyField && detector.strategyOptions && profile ? <div className="space-y-4"><StrategyField value={(profile[detector.strategyField] ?? detector.strategyOptions[0]?.value) as Strategy} options={detector.strategyOptions} onChange={(next) => update({ [detector.strategyField as string]: next } as UpdateSecurityProfileRequest)} disabled={!enabled || updateProfile.isPending} />{detector.thresholdField ? <ThresholdField value={(profile[detector.thresholdField] as number) ?? 0} onChange={(next) => update({ [detector.thresholdField as string]: next } as UpdateSecurityProfileRequest)} disabled={!enabled || updateProfile.isPending} /> : null}</div> : <p className="text-[10px] leading-relaxed text-muted-foreground">This detector is configured through policy rules. Add or edit rules in the corresponding policy workspace.</p>}
                        </div>
                      </div>
                    </div>
                  ) : null}
                </article>
              );
            })}
          </section>
          {updateProfile.isError ? <SecurityNotice title="Policy update failed" tone="warning"><span>{(updateProfile.error as Error)?.message ?? "The server rejected the update."}</span></SecurityNotice> : null}
        </TabsContent>

        <TabsContent value="runtime">
          <section className="overflow-hidden rounded-xl border border-border/70 bg-card">
            <div className="border-b border-border/60 px-4 py-3"><h2 className="text-[12px] font-medium">Failure behavior</h2><p className="mt-0.5 text-[10px] text-muted-foreground">This runtime setting is read-only in the dashboard.</p></div>
            <div className="grid gap-px bg-border/60 sm:grid-cols-2">
              <RuntimeModeCard active={securityMode === "fail-open"} icon={Unlock} title="Fail open" description="Pass traffic when evaluation cannot complete. Availability-first and higher risk." />
              <RuntimeModeCard active={securityMode === "fail-closed"} icon={Lock} title="Fail closed" description="Return 503 when evaluation cannot complete. Enforcement-first." />
            </div>
            <div className="border-t border-border/60 px-4 py-3 text-[10px] text-muted-foreground">Set <code className="rounded border border-border bg-muted/40 px-1.5 py-0.5 font-mono text-foreground">BASTIO_SECURITY_MODE</code> and restart the service to change this behavior.</div>
          </section>
        </TabsContent>

        {extraTabs.map((tab) => <TabsContent key={tab.id} value={tab.id}>{tab.component}</TabsContent>)}
      </Tabs>
    </>
  );
}

function PiiControls({ profile, disabled, onUpdate }: { profile: SecurityProfile; disabled: boolean; onUpdate: (data: UpdateSecurityProfileRequest) => void }) {
  return (
    <div className="space-y-3">
      <div><label className="mb-1.5 block text-[10px] text-muted-foreground">Action</label><Select value={profile.pii_action} onValueChange={(value) => value && onUpdate({ pii_action: value as PIIAction })}><SelectTrigger className="w-full" disabled={disabled}><SelectValue /></SelectTrigger><SelectContent>{PII_ACTION_OPTIONS.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}</SelectContent></Select><p className="mt-1 text-[9px] text-muted-foreground">{PII_ACTION_OPTIONS.find((option) => option.value === profile.pii_action)?.hint}</p></div>
      {profile.pii_action === "tokenize" ? <div><label className="mb-1.5 block text-[10px] text-muted-foreground">Token style</label><Select value={profile.pii_token_style} onValueChange={(value) => value && onUpdate({ pii_token_style: value as PIITokenStyle })}><SelectTrigger className="w-full" disabled={disabled}><SelectValue /></SelectTrigger><SelectContent>{PII_TOKEN_STYLE_OPTIONS.map((option) => <SelectItem key={option.value} value={option.value}><code>{option.label}</code></SelectItem>)}</SelectContent></Select></div> : null}
      <label className="flex items-start gap-2 text-[10px] leading-relaxed text-muted-foreground"><input type="checkbox" className="mt-0.5 size-3.5" checked={profile.pii_scan_response} onChange={(event) => onUpdate({ pii_scan_response: event.target.checked })} disabled={disabled} />Scan model responses for hallucinated or retrieved PII</label>
      {profile.pii_action === "tokenize" ? <label className="flex items-start gap-2 text-[10px] leading-relaxed text-muted-foreground"><input type="checkbox" className="mt-0.5 size-3.5" checked={profile.pii_restore_response} onChange={(event) => onUpdate({ pii_restore_response: event.target.checked })} disabled={disabled} />Restore original values in the response</label> : null}
    </div>
  );
}

function StrategyField({ value, options, onChange, disabled }: { value: Strategy; options: readonly { value: Strategy; label: string }[]; onChange: (next: Strategy) => void; disabled: boolean }) {
  return <div><label className="mb-1.5 block text-[10px] text-muted-foreground">Action</label><Select value={value} onValueChange={(next) => next && onChange(next as Strategy)}><SelectTrigger className="w-full" disabled={disabled}><SelectValue /></SelectTrigger><SelectContent>{options.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}</SelectContent></Select><p className="mt-1 text-[9px] leading-relaxed text-muted-foreground">{STRATEGY_HINT[value]}</p></div>;
}

function ThresholdField({ value, onChange, disabled }: { value: number; onChange: (next: number) => void; disabled: boolean }) {
  const [draft, setDraft] = useState(value.toString());
  useEffect(() => setDraft(value.toString()), [value]);
  const commit = () => {
    const parsed = Number(draft);
    if (Number.isFinite(parsed) && parsed >= 0 && parsed <= 1 && parsed !== value) onChange(parsed);
    else setDraft(value.toString());
  };
  return <div><label className="mb-1.5 block text-[10px] text-muted-foreground">Confidence threshold</label><div className="flex items-center gap-2"><input type="number" min={0} max={1} step={0.05} value={draft} onChange={(event) => setDraft(event.target.value)} onBlur={commit} onKeyDown={(event) => event.key === "Enter" && (event.target as HTMLInputElement).blur()} disabled={disabled} className="h-8 w-24 rounded-lg border border-input bg-background px-2 font-mono text-[11px] outline-none focus-visible:ring-3 focus-visible:ring-ring/50" /><span className="text-[9px] text-muted-foreground">0 sensitive · 1 strict</span></div></div>;
}

function RuntimeModeCard({ active, icon: Icon, title, description }: { active: boolean; icon: typeof Lock; title: string; description: string }) {
  return <div className={cn("bg-card p-5", !active && "opacity-55")}><div className="flex items-center gap-3"><div className={cn("flex size-9 items-center justify-center rounded-lg border", active ? "border-warn-border bg-warn-bg text-warn" : "border-border bg-muted/40 text-muted-foreground")}><Icon className="size-4" /></div><div><div className="flex items-center gap-2"><h3 className="text-[12px] font-medium">{title}</h3>{active ? <Badge variant="outline" className="h-4 px-1.5 text-[8px]">current</Badge> : null}</div><p className="mt-1 text-[10px] leading-relaxed text-muted-foreground">{description}</p></div></div></div>;
}
