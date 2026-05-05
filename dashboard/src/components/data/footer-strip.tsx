import { StatusDot } from "@/components/data/status-dot";

/**
 * Trust-signal footer. Shows only values we can actually verify:
 * - build SHA (from VITE_BUILD_SHA at build time)
 * - live connection pulse (from the /healthz query state in the caller)
 *
 * Additional fields (region, p95, SOC 2 badge, uptime) will be added when the
 * backend exposes them. Until then: don't lie.
 */
export function FooterStrip({
  buildSha,
  connected,
}: {
  buildSha?: string;
  connected: boolean;
}) {
  return (
    <footer className="flex items-center gap-5 px-6 py-2.5 border-t border-border-subtle bg-background font-mono text-[11px] tabular-nums text-text-muted">
      {buildSha && (
        <span className="inline-flex items-center gap-1.5">
          build
          <span className="text-text-secondary">{buildSha}</span>
        </span>
      )}
      <span className="ml-auto inline-flex items-center gap-2">
        <StatusDot tone={connected ? "success" : "danger"} pulse={connected} size={5} />
        {connected ? "connected" : "disconnected"}
      </span>
    </footer>
  );
}
