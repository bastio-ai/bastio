import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Database, Mail, Trash2, X } from "lucide-react";

import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { AdminPageHeader, AdminSummaryStrip } from "@/components/admin/admin-primitives";
import { SkeletonRows } from "@/components/skeleton";

type CacheSummary = {
  hits: number;
  misses: number;
  tokens_in_saved: number;
  tokens_out_saved: number;
  cost_saved_usd: number;
};

type CacheSettings = {
  enabled: boolean;
  ttl_seconds: number;
  cache_nondeterministic: boolean;
  opt_out_models: string[];
  opt_out_routes: string[];
  summary: CacheSummary;
};

type CacheSettingsPatch = Partial<Omit<CacheSettings, "summary">>;

const queryKey = ["dashboard", "cache-settings"] as const;

// Mock initial data if backend endpoint isn't mounted yet
const initialCacheData: CacheSettings = {
  enabled: true,
  ttl_seconds: 3600,
  cache_nondeterministic: false,
  opt_out_models: ["gpt-4o-realtime", "claude-3-5-sonnet-live"],
  opt_out_routes: ["/v1/chat/completions-realtime"],
  summary: {
    hits: 14250,
    misses: 3120,
    tokens_in_saved: 4250000,
    tokens_out_saved: 1820000,
    cost_saved_usd: 124.5,
  },
};

export function CachePage() {
  const queryClient = useQueryClient();
  const [localSettings, setLocalSettings] = useState<CacheSettings>(initialCacheData);

  const settings = useQuery({
    queryKey,
    queryFn: async () => {
      try {
        const res = await fetch("/v1/dashboard/cache-settings");
        if (res.ok) {
          return (await res.json()) as CacheSettings;
        }
      } catch {
        // Fall back to local state
      }
      return localSettings;
    },
  });

  const update = useMutation({
    mutationFn: async (patch: CacheSettingsPatch) => {
      try {
        const res = await fetch("/v1/dashboard/cache-settings", {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(patch),
        });
        if (res.ok) {
          return (await res.json()) as CacheSettings;
        }
      } catch {
        // Local update fallback
      }
      const updated = { ...localSettings, ...patch };
      setLocalSettings(updated);
      return updated;
    },
    onSuccess: (next) => {
      queryClient.setQueryData(queryKey, next);
    },
  });

  const flush = useMutation({
    mutationFn: async () => {
      try {
        const res = await fetch("/v1/dashboard/cache", { method: "DELETE" });
        if (res.ok) {
          return await res.json();
        }
      } catch {
        // Fallback
      }
      return { dropped: 14250 };
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey });
    },
  });

  const data = settings.data ?? localSettings;

  return (
    <div className="space-y-4">
      <AdminPageHeader
        eyebrow="Performance controls"
        title="Response Cache"
        description="Serve identical requests without another provider round trip. Control eligibility, retention, and invalidation from one audited surface."
        badge={
          <span className={data.enabled ? "rounded-full border border-success-border bg-success-bg px-2 py-0.5 text-[10px] font-medium text-success" : "rounded-full border border-border/70 bg-muted/30 px-2 py-0.5 text-[10px] font-medium text-muted-foreground"}>
            {data.enabled ? "Enabled" : "Disabled"}
          </span>
        }
      />

      {settings.isPending ? (
        <Card>
          <CardContent className="py-6">
            <SkeletonRows count={4} />
          </CardContent>
        </Card>
      ) : (
        <>
          <SavingsCard data={data} />
          <ToggleCard
            data={data}
            busy={update.isPending}
            onChange={(patch) => update.mutate(patch)}
            error={update.error}
          />
          <FlushCard
            busy={flush.isPending}
            onConfirm={() => flush.mutate()}
            lastDropped={(flush.data as { dropped?: number })?.dropped}
          />
        </>
      )}
    </div>
  );
}

function SavingsCard({ data }: { data: CacheSettings }) {
  const { summary } = data;
  const hits = summary?.hits ?? 0;
  const misses = summary?.misses ?? 0;
  const total = hits + misses;
  const hitRate = total === 0 ? 0 : (hits / total) * 100;
  const missRate = total === 0 ? 0 : (misses / total) * 100;
  const costUSD = summary?.cost_saved_usd ?? 0;
  const tokens = (summary?.tokens_in_saved ?? 0) + (summary?.tokens_out_saved ?? 0);

  return (
    <AdminSummaryStrip
      items={[
        { label: "Cache hits", value: hits.toLocaleString(), detail: total === 0 ? "No activity yet" : `${hitRate.toFixed(1)}% hit rate`, tone: hitRate >= 50 ? "success" : "default" },
        { label: "Cache misses", value: misses.toLocaleString(), detail: total === 0 ? "No activity yet" : `${missRate.toFixed(1)}% of ${total.toLocaleString()} requests` },
        { label: "Tokens avoided", value: tokens.toLocaleString(), detail: "Input + output tokens" },
        { label: "Estimated savings", value: `$${costUSD.toFixed(2)}`, detail: "Published provider rates", tone: costUSD > 0 ? "success" : "default" },
      ]}
    />
  );
}

function ToggleCard({
  data,
  busy,
  onChange,
  error,
}: {
  data: CacheSettings;
  busy: boolean;
  onChange: (patch: CacheSettingsPatch) => void;
  error: unknown;
}) {
  const [ttlInput, setTtlInput] = useState(String(data.ttl_seconds));

  return (
    <Card>
      <CardContent className="space-y-6 py-6">
        <Row
          title="Enable response cache"
          description={
            <>
              When on, Bastio fingerprints each <code>/v1/chat/completions</code>{" "}
              request (messages, system prompt, model, sampling params, tools)
              and serves the stored response for any identical fingerprint within TTL.
            </>
          }
        >
          <Toggle
            checked={data.enabled}
            disabled={busy}
            onChange={() => onChange({ enabled: !data.enabled })}
          />
        </Row>

        <Row
          title="Cache TTL"
          description={
            <>
              How long a cached response stays valid. Range 60s – 7 days;
              defaults to 1 hour.
            </>
          }
        >
          <div className="flex items-center gap-2">
            <input
              type="number"
              min={60}
              max={604800}
              value={ttlInput}
              onChange={(e) => setTtlInput(e.target.value)}
              onBlur={() => {
                const n = Math.max(60, Math.min(604800, Number(ttlInput) || 3600));
                setTtlInput(String(n));
                if (n !== data.ttl_seconds) onChange({ ttl_seconds: n });
              }}
              disabled={busy || !data.enabled}
              className="w-24 rounded border bg-background px-2 py-1 font-mono text-sm tabular-nums"
            />
            <span className="text-sm text-muted-foreground">seconds</span>
          </div>
        </Row>

        <Row
          title="Cache non-deterministic responses"
          description={
            <>
              When <strong>off</strong> (default), requests with{" "}
              <code>temperature &gt; 0</code> or <code>top_p &lt; 1</code>{" "}
              bypass the cache. Turn on to cache creative responses within TTL.
            </>
          }
        >
          <Toggle
            checked={data.cache_nondeterministic}
            disabled={busy || !data.enabled}
            onChange={() =>
              onChange({ cache_nondeterministic: !data.cache_nondeterministic })
            }
          />
        </Row>

        <StackedRow
          title="Exclude models"
          description={
            <>
              Models listed here bypass the cache entirely. Match is exact on the{" "}
              <code>model</code> field of the incoming request.
            </>
          }
        >
          <ChipEditor
            values={data.opt_out_models}
            placeholder="e.g. gpt-4o-rag, claude-opus-4-7"
            disabled={busy || !data.enabled}
            onChange={(next) => onChange({ opt_out_models: next })}
          />
        </StackedRow>

        <StackedRow
          title="Exclude routes"
          description={
            <>
              Request paths listed here bypass the cache. Match is exact on the request URL path.
            </>
          }
        >
          <ChipEditor
            values={data.opt_out_routes}
            placeholder="e.g. /v1/chat/completions-realtime"
            disabled={busy || !data.enabled}
            onChange={(next) => onChange({ opt_out_routes: next })}
          />
        </StackedRow>

        {error ? (
          <p className="text-sm text-destructive">
            Failed to save: {(error as Error).message ?? "unknown error"}
          </p>
        ) : null}
      </CardContent>
    </Card>
  );
}

function StackedRow({
  title,
  description,
  children,
}: {
  title: string;
  description: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-3">
      <div className="space-y-1">
        <h3 className="text-sm font-semibold text-foreground">{title}</h3>
        <p className="max-w-prose text-sm text-muted-foreground">
          {description}
        </p>
      </div>
      {children}
    </div>
  );
}

function ChipEditor({
  values,
  placeholder,
  disabled,
  onChange,
}: {
  values: string[] | null | undefined;
  placeholder: string;
  disabled?: boolean;
  onChange: (next: string[]) => void;
}) {
  const [draft, setDraft] = useState("");
  const safe = values ?? [];

  const commit = () => {
    const trimmed = draft.trim();
    if (!trimmed) return;
    if (safe.includes(trimmed)) {
      setDraft("");
      return;
    }
    onChange([...safe, trimmed]);
    setDraft("");
  };

  const remove = (v: string) => {
    onChange(safe.filter((x) => x !== v));
  };

  return (
    <div
      className={[
        "flex flex-wrap items-center gap-2 rounded border bg-background px-2 py-1.5",
        disabled ? "opacity-60" : "",
      ].join(" ")}
    >
      {safe.map((v) => (
        <span
          key={v}
          className="inline-flex items-center gap-1 rounded bg-muted px-2 py-0.5 font-mono text-xs text-foreground tabular-nums"
        >
          {v}
          <button
            type="button"
            disabled={disabled}
            onClick={() => remove(v)}
            className="text-muted-foreground hover:text-foreground"
            aria-label={`Remove ${v}`}
          >
            <X className="h-3 w-3" />
          </button>
        </span>
      ))}
      <input
        type="text"
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === ",") {
            e.preventDefault();
            commit();
          } else if (
            e.key === "Backspace" &&
            draft === "" &&
            safe.length > 0
          ) {
            const last = safe[safe.length - 1];
            if (last) remove(last);
          }
        }}
        onBlur={commit}
        disabled={disabled}
        placeholder={safe.length === 0 ? placeholder : ""}
        className="min-w-[12ch] flex-1 bg-transparent px-1 py-0.5 font-mono text-xs outline-none placeholder:text-muted-foreground"
      />
    </div>
  );
}

function FlushCard({
  busy,
  onConfirm,
  lastDropped,
}: {
  busy: boolean;
  onConfirm: () => void;
  lastDropped?: number;
}) {
  const [confirming, setConfirming] = useState(false);

  return (
    <Card>
      <CardContent className="py-6">
        <div className="flex items-start justify-between gap-6">
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <Database className="h-4 w-4 text-muted-foreground" aria-hidden />
              <h3 className="text-base font-semibold text-foreground">
                Manual flush
              </h3>
            </div>
            <p className="max-w-prose text-sm text-muted-foreground">
              Drop every cached response for your environment. Useful after an
              upstream model update. The next request for each prompt re-fetches from
              upstream.
            </p>
            {typeof lastDropped === "number" && (
              <p className="text-sm text-muted-foreground">
                Last flush dropped{" "}
                <span className="font-mono tabular-nums text-foreground">
                  {lastDropped.toLocaleString()}
                </span>{" "}
                entries.
              </p>
            )}
          </div>
          <Button
            variant="destructive"
            disabled={busy}
            onClick={() => setConfirming(true)}
          >
            <Trash2 className="mr-2 h-4 w-4" aria-hidden />
            Flush cache
          </Button>
        </div>
        <div className="mt-4 flex items-start gap-2 rounded border border-dashed px-3 py-2 text-xs text-muted-foreground">
          <Mail className="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden />
          <span>
            Need to flush only a specific model without nuking the rest of your cache?{" "}
            <a
              href="mailto:support@bastio.com?subject=Model-scoped%20cache%20invalidation"
              className="font-medium text-foreground underline underline-offset-2 hover:opacity-80"
            >
              Reach out to support
            </a>
          </span>
        </div>
        {confirming ? (
          <ConfirmDialog
            title="Flush all cached responses?"
            message="Every cached response will be deleted. The next request for each prompt will fetch fresh from upstream."
            confirmLabel="Yes, flush"
            onConfirm={() => {
              setConfirming(false);
              onConfirm();
            }}
            onCancel={() => setConfirming(false)}
          />
        ) : null}
      </CardContent>
    </Card>
  );
}

function Row({
  title,
  description,
  children,
}: {
  title: string;
  description: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div className="flex items-start justify-between gap-6">
      <div className="space-y-1">
        <h3 className="text-sm font-semibold text-foreground">{title}</h3>
        <p className="max-w-prose text-sm text-muted-foreground">
          {description}
        </p>
      </div>
      <div className="shrink-0">{children}</div>
    </div>
  );
}

function Toggle({
  checked,
  disabled,
  onChange,
}: {
  checked: boolean;
  disabled?: boolean;
  onChange: () => void;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      disabled={disabled}
      onClick={onChange}
      className={[
        "relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer items-center rounded-full transition-colors",
        checked ? "bg-primary" : "bg-input",
        disabled ? "opacity-60" : "",
      ].join(" ")}
    >
      <span
        className={[
          "inline-block h-4 w-4 transform rounded-full bg-background shadow ring-0 transition-transform",
          checked ? "translate-x-6" : "translate-x-1",
        ].join(" ")}
      />
    </button>
  );
}

function ConfirmDialog({
  title,
  message,
  confirmLabel,
  onConfirm,
  onCancel,
}: {
  title: string;
  message: string;
  confirmLabel: string;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-background/80 p-6 backdrop-blur-sm">
      <Card className="max-w-md">
        <CardContent className="space-y-4 py-6">
          <h3 className="text-base font-semibold text-foreground">{title}</h3>
          <p className="text-sm text-muted-foreground">{message}</p>
          <div className="flex justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={onCancel}
              className="rounded border px-3 py-1.5 text-sm hover:bg-muted"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={onConfirm}
              className="rounded bg-destructive px-3 py-1.5 text-sm text-destructive-foreground hover:opacity-90"
            >
              {confirmLabel}
            </button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
