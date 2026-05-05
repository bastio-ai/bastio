import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Globe, Trash2, CheckCircle2, AlertCircle, Plus } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { SkeletonRows } from "@/components/skeleton";

import { workspaceApi, type Settings, type Domain } from "./types";
import { MODEL_CATALOG, type AllowedModelEntry } from "./model-picker";

export function SettingsTab() {
  const qc = useQueryClient();
  const settings = useQuery({
    queryKey: ["workspace", "settings"],
    queryFn: workspaceApi.getSettings,
  });

  const [seatLimit, setSeatLimit] = useState(0);
  const [retentionDays, setRetentionDays] = useState(0);
  const [billingMode, setBillingMode] = useState<Settings["billing_mode"]>("platform_keys");
  const [brandingJSON, setBrandingJSON] = useState("{}");
  const [allowedModels, setAllowedModels] = useState<AllowedModelEntry[]>([]);
  // AI persona — workspace-level overlay applied to every assistant's
  // system prompt at chat time. Empty string = "leave the assistant's
  // raw prompt alone". Three independent fields so admins can tune
  // identity, personality, and tone separately.
  const [personaName, setPersonaName] = useState("");
  const [personaPersonality, setPersonaPersonality] = useState("");
  const [personaTone, setPersonaTone] = useState("");

  useEffect(() => {
    if (settings.data) {
      setSeatLimit(settings.data.seat_limit);
      setRetentionDays(settings.data.retention_days);
      setBillingMode(settings.data.billing_mode);
      setBrandingJSON(JSON.stringify(settings.data.branding, null, 2));
      setAllowedModels(settings.data.allowed_models ?? []);
      setPersonaName(settings.data.ai_persona_name ?? "");
      setPersonaPersonality(settings.data.ai_persona_personality ?? "");
      setPersonaTone(settings.data.ai_persona_tone ?? "");
    }
  }, [settings.data]);

  const save = useMutation({
    mutationFn: () => {
      let branding: Record<string, unknown> = {};
      try {
        branding = JSON.parse(brandingJSON || "{}");
      } catch (e) {
        throw new Error("Branding must be valid JSON");
      }
      return workspaceApi.patchSettings({
        seat_limit: seatLimit,
        retention_days: retentionDays,
        billing_mode: billingMode,
        branding,
        allowed_models: allowedModels,
        // Empty string → null clears the field on the server side
        // (server reads nullable TEXT). Non-empty → set verbatim.
        ai_persona_name: personaName.trim() || null,
        ai_persona_personality: personaPersonality.trim() || null,
        ai_persona_tone: personaTone.trim() || null,
      });
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["workspace", "settings"] }),
  });

  if (settings.isLoading) return <SkeletonRows count={4} />;

  return (
    <div className="space-y-6">
      <BrandedChatSection settings={settings.data ?? null} />

      <h3 className="text-sm font-semibold">AI persona</h3>
      <p className="text-xs text-muted-foreground -mt-4">
        Give your workspace's AI a name and personality. Applies to every assistant — employees see
        the same identity ("Have you asked Bob?") regardless of which assistant they pick. Leave
        blank to use each assistant's raw system prompt.
      </p>
      <Card>
        <CardContent className="space-y-3 p-4">
          <Field label="Name">
            <input
              type="text"
              value={personaName}
              onChange={(e) => setPersonaName(e.target.value)}
              placeholder="e.g. Bob"
              maxLength={120}
              className="w-full rounded-md border border-border bg-background px-2 py-1 text-sm"
            />
          </Field>
          <Field label="Personality">
            <input
              type="text"
              value={personaPersonality}
              onChange={(e) => setPersonaPersonality(e.target.value)}
              placeholder="e.g. friendly, helpful, concise"
              maxLength={500}
              className="w-full rounded-md border border-border bg-background px-2 py-1 text-sm"
            />
          </Field>
          <Field label="Tone of voice">
            <input
              type="text"
              value={personaTone}
              onChange={(e) => setPersonaTone(e.target.value)}
              placeholder="e.g. casual, professional, witty"
              maxLength={200}
              className="w-full rounded-md border border-border bg-background px-2 py-1 text-sm"
            />
          </Field>
          <p className="text-xs text-muted-foreground">
            Saved together with the other settings via the button at the bottom of this page.
          </p>
        </CardContent>
      </Card>

      <h3 className="text-sm font-semibold">Workspace settings</h3>

      <Card>
        <CardContent className="space-y-3 p-4">
          <div className="grid grid-cols-2 gap-3">
            <Field label="Seat limit">
              <input
                type="number"
                min={1}
                value={seatLimit}
                onChange={(e) => setSeatLimit(Number(e.target.value))}
                className="w-full rounded-md border border-border bg-background px-2 py-1 text-sm"
              />
            </Field>
            <Field label="Retention (days)">
              <input
                type="number"
                min={1}
                value={retentionDays}
                onChange={(e) => setRetentionDays(Number(e.target.value))}
                className="w-full rounded-md border border-border bg-background px-2 py-1 text-sm"
              />
            </Field>
          </div>
          <Field label="Billing mode">
            <select
              value={billingMode}
              onChange={(e) => setBillingMode(e.target.value as Settings["billing_mode"])}
              className="w-full rounded-md border border-border bg-background px-2 py-1 text-sm"
            >
              <option value="platform_keys">Platform keys (Bastio bills you)</option>
              <option value="byo_keys">BYO keys (your provider keys)</option>
            </select>
          </Field>
          <Field label="Branding (JSON)">
            <textarea
              value={brandingJSON}
              onChange={(e) => setBrandingJSON(e.target.value)}
              rows={6}
              className="w-full resize-y rounded-md border border-border bg-background px-2 py-1 text-sm font-mono"
            />
            <p className="text-xs text-muted-foreground">
              Keys: <code>logo_url</code>, <code>primary_color</code>, <code>welcome_message</code>.
            </p>
          </Field>

          <ModelWhitelistEditor value={allowedModels} onChange={setAllowedModels} />
          {save.error && (
            <p className="text-sm text-destructive">{(save.error as Error).message}</p>
          )}
          <div className="flex justify-end">
            <Button size="sm" onClick={() => save.mutate()} disabled={save.isPending}>
              {save.isPending ? "Saving…" : "Save settings"}
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="flex flex-col gap-1">
      <span className="text-xs font-medium text-muted-foreground">{label}</span>
      {children}
    </label>
  );
}

// =============================================================================
// LLM model whitelist
// =============================================================================

// ModelWhitelistEditor lets the admin curate which (provider, model)
// pairs employees can pick from in the workspace chat. Empty list =
// all curated defaults available; non-empty = strict subset.
//
// Grouped by provider for ergonomics — admins typically think
// "OpenAI yes, Anthropic no" first, then per-model fine-tuning.
function ModelWhitelistEditor({
  value,
  onChange,
}: {
  value: AllowedModelEntry[];
  onChange: (next: AllowedModelEntry[]) => void;
}) {
  // Index value into a Set for O(1) membership checks while rendering.
  const allowedKey = (e: AllowedModelEntry) => `${e.provider}/${e.model}`;
  const allowSet = new Set(value.map(allowedKey));

  // Group catalog by provider for stacked sections.
  const byProvider = MODEL_CATALOG.reduce<Record<string, typeof MODEL_CATALOG>>(
    (acc, m) => {
      const list = acc[m.provider] ?? [];
      list.push(m);
      acc[m.provider] = list;
      return acc;
    },
    {},
  );

  const toggleModel = (provider: string, model: string) => {
    const key = `${provider}/${model}`;
    if (allowSet.has(key)) {
      onChange(value.filter((e) => allowedKey(e) !== key));
    } else {
      onChange([...value, { provider, model }]);
    }
  };

  const toggleProvider = (provider: string, models: typeof MODEL_CATALOG) => {
    const allOn = models.every((m) => allowSet.has(`${provider}/${m.model}`));
    if (allOn) {
      onChange(value.filter((e) => e.provider !== provider));
    } else {
      const others = value.filter((e) => e.provider !== provider);
      onChange([...others, ...models.map((m) => ({ provider, model: m.model }))]);
    }
  };

  const empty = value.length === 0;

  return (
    <div className="space-y-3">
      <div className="flex items-baseline justify-between gap-3">
        <span className="text-xs font-medium text-muted-foreground">
          LLM models employees can pick
        </span>
        <span className="text-[10px] uppercase tracking-wide text-muted-foreground">
          {empty ? "All curated defaults available" : `${value.length} model${value.length === 1 ? "" : "s"} allowed`}
        </span>
      </div>
      <p className="text-xs text-muted-foreground">
        Leave all unchecked to expose every curated default. Tick specific
        models to enforce a strict whitelist — both in the chat picker UI
        and as defense-in-depth on the chat send path.
      </p>
      <div className="space-y-3 rounded-md border border-border p-3">
        {Object.entries(byProvider).map(([provider, models]) => {
          const allOn = models.every((m) =>
            allowSet.has(`${provider}/${m.model}`),
          );
          const someOn = models.some((m) =>
            allowSet.has(`${provider}/${m.model}`),
          );
          return (
            <div key={provider} className="space-y-1.5">
              <label className="flex items-center gap-2">
                <input
                  type="checkbox"
                  checked={allOn}
                  ref={(el) => {
                    if (el) el.indeterminate = !allOn && someOn;
                  }}
                  onChange={() => toggleProvider(provider, models)}
                />
                <span className="text-sm font-medium capitalize">{provider}</span>
                <span className="text-[10px] uppercase tracking-wide text-muted-foreground">
                  {someOn ? `${models.filter((m) => allowSet.has(`${provider}/${m.model}`)).length} / ${models.length}` : "off"}
                </span>
              </label>
              <div className="ml-6 grid grid-cols-1 gap-1 sm:grid-cols-2">
                {models.map((m) => {
                  const on = allowSet.has(`${provider}/${m.model}`);
                  return (
                    <label
                      key={`${provider}/${m.model}`}
                      className="flex items-center gap-2 text-xs"
                    >
                      <input
                        type="checkbox"
                        checked={on}
                        onChange={() => toggleModel(provider, m.model)}
                      />
                      <span className={on ? "text-foreground" : "text-muted-foreground"}>
                        {m.label}
                      </span>
                      <span className="ml-auto font-mono text-[10px] text-muted-foreground">
                        {m.model}
                      </span>
                    </label>
                  );
                })}
              </div>
            </div>
          );
        })}
        {!empty && (
          <button
            type="button"
            onClick={() => onChange([])}
            className="text-xs text-cyan-500 hover:underline"
          >
            Clear whitelist (revert to all-defaults)
          </button>
        )}
      </div>
    </div>
  );
}

// =============================================================================
// Branded chat: slug + custom domains
// =============================================================================

function BrandedChatSection({ settings }: { settings: Settings | null }) {
  const qc = useQueryClient();
  const domains = useQuery({
    queryKey: ["workspace", "domains"],
    queryFn: workspaceApi.listDomains,
  });
  const [slugDraft, setSlugDraft] = useState("");
  const [slugError, setSlugError] = useState<string | null>(null);

  useEffect(() => {
    if (settings?.slug) setSlugDraft(settings.slug);
  }, [settings?.slug]);

  const saveSlug = useMutation({
    mutationFn: () => workspaceApi.setSlug(slugDraft.trim()),
    onSuccess: () => {
      setSlugError(null);
      qc.invalidateQueries({ queryKey: ["workspace", "settings"] });
    },
    onError: (err) => setSlugError((err as Error).message),
  });

  const slugSet = !!settings?.slug;
  const hostedURL = settings?.slug
    ? `${window.location.origin.replace("//bastio.", "//workspace.bastio.").replace("//bastio.com", "//workspace.bastio.com")}/c/${settings.slug}`
    : null;

  return (
    <div className="space-y-4">
      <h3 className="text-sm font-semibold">Branded chat</h3>
      <p className="text-xs text-muted-foreground">
        Give your team or clients a hosted chat URL. Use a custom domain
        (e.g. <code>ai.acme.com</code>) to put your own brand on it — visitors
        never see <code>bastio.com</code>.
      </p>

      <Card>
        <CardContent className="space-y-3 p-4">
          <Field label="Workspace slug">
            <div className="flex gap-2">
              <input
                value={slugDraft}
                onChange={(e) => setSlugDraft(e.target.value.toLowerCase())}
                placeholder="acme-team"
                className="flex-1 rounded-md border border-border bg-background px-2 py-1 text-sm"
              />
              <Button
                size="sm"
                onClick={() => saveSlug.mutate()}
                disabled={!slugDraft.trim() || saveSlug.isPending || slugDraft === settings?.slug}
              >
                {saveSlug.isPending ? "Saving…" : slugSet ? "Update" : "Claim"}
              </Button>
            </div>
          </Field>
          {slugError && (
            <p className="text-sm text-destructive">{slugError}</p>
          )}
          {hostedURL && (
            <p className="text-xs text-muted-foreground">
              Hosted at:{" "}
              <a
                href={hostedURL}
                target="_blank"
                rel="noopener"
                className="text-cyan-500 hover:underline"
              >
                {hostedURL}
              </a>
            </p>
          )}
          <p className="text-xs text-muted-foreground">
            Lowercase letters, digits, and hyphens. 3–40 chars.
          </p>
        </CardContent>
      </Card>

      <DomainsCard
        domains={domains.data?.domains ?? []}
        loading={domains.isLoading}
      />
    </div>
  );
}

function DomainsCard({ domains, loading }: { domains: Domain[]; loading: boolean }) {
  const qc = useQueryClient();
  const [draft, setDraft] = useState("");
  const [error, setError] = useState<string | null>(null);

  const create = useMutation({
    mutationFn: () => workspaceApi.createDomain(draft.trim()),
    onSuccess: () => {
      setDraft("");
      setError(null);
      qc.invalidateQueries({ queryKey: ["workspace", "domains"] });
    },
    onError: (err) => setError((err as Error).message),
  });
  const verify = useMutation({
    mutationFn: workspaceApi.verifyDomain,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["workspace", "domains"] }),
  });
  const remove = useMutation({
    mutationFn: workspaceApi.deleteDomain,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["workspace", "domains"] }),
  });

  return (
    <Card>
      <CardContent className="space-y-3 p-4">
        <h4 className="text-sm font-semibold">Custom domains</h4>
        <p className="text-xs text-muted-foreground">
          Point your domain at <code>bastio.com</code> via CNAME, then verify ownership with
          a DNS TXT record. Multiple domains are supported.
        </p>

        <div className="flex gap-2">
          <input
            value={draft}
            onChange={(e) => setDraft(e.target.value.toLowerCase())}
            placeholder="ai.acme.com"
            className="flex-1 rounded-md border border-border bg-background px-2 py-1 text-sm font-mono"
          />
          <Button
            size="sm"
            onClick={() => create.mutate()}
            disabled={!draft.trim() || create.isPending}
          >
            <Plus className="mr-2 h-4 w-4" /> Add domain
          </Button>
        </div>
        {error && <p className="text-sm text-destructive">{error}</p>}

        {loading && <SkeletonRows count={2} />}
        {!loading && domains.length === 0 && (
          <p className="py-4 text-center text-xs text-muted-foreground">
            No custom domains yet.
          </p>
        )}

        <ul className="space-y-3">
          {domains.map((d) => (
            <DomainRow
              key={d.id}
              d={d}
              onVerify={() => verify.mutate(d.id)}
              onDelete={() => {
                if (confirm(`Remove ${d.domain}?`)) remove.mutate(d.id);
              }}
              verifying={verify.isPending}
            />
          ))}
        </ul>
      </CardContent>
    </Card>
  );
}

function DomainRow({
  d,
  onVerify,
  onDelete,
  verifying,
}: {
  d: Domain;
  onVerify: () => void;
  onDelete: () => void;
  verifying: boolean;
}) {
  const verified = !!d.verified_at;
  return (
    <li className="rounded-md border border-border p-3">
      <div className="flex items-center justify-between gap-3">
        <div className="flex min-w-0 flex-1 items-center gap-2">
          <Globe className="h-4 w-4 text-muted-foreground" />
          <span className="truncate font-mono text-sm">{d.domain}</span>
          {verified ? (
            <Badge variant="secondary" className="gap-1">
              <CheckCircle2 className="h-3 w-3" /> Verified
            </Badge>
          ) : (
            <Badge variant="outline" className="gap-1">
              <AlertCircle className="h-3 w-3" /> Pending
            </Badge>
          )}
        </div>
        <div className="flex items-center gap-1">
          {!verified && (
            <Button size="sm" variant="ghost" onClick={onVerify} disabled={verifying}>
              {verifying ? "Checking…" : "Verify"}
            </Button>
          )}
          <Button size="sm" variant="ghost" onClick={onDelete}>
            <Trash2 className="h-3.5 w-3.5" />
          </Button>
        </div>
      </div>
      {!verified && (
        <div className="mt-3 space-y-1 text-xs">
          <p className="text-muted-foreground">
            Add a TXT record at <code className="font-mono">{d.domain}</code> with this
            value:
          </p>
          <code className="block break-all rounded-md bg-muted p-2 font-mono">
            bastio-verify={d.verification_token}
          </code>
          {d.last_check_error && (
            <p className="text-destructive">Last check: {d.last_check_error}</p>
          )}
        </div>
      )}
    </li>
  );
}
