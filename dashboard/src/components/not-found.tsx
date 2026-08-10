import { Link } from "@tanstack/react-router";
import { buttonVariants } from "@/components/ui/button";

const DESTINATIONS = [
  { to: "/", label: "Overview", description: "Traffic, threats and spend at a glance" },
  { to: "/threats", label: "Threats", description: "Blocked and flagged requests" },
  { to: "/proxies", label: "LLM Gateways", description: "Gateway keys and model routing" },
  { to: "/security-settings", label: "Security Center", description: "Profiles, detectors and thresholds" },
] as const;

export function NotFound() {
  return (
    <div className="mx-auto max-w-3xl px-6 py-24">
      <span className="font-mono text-[11px] uppercase tracking-[0.08em] text-muted-foreground">
        Error 404
      </span>

      <h1 className="mt-4 text-3xl font-semibold tracking-tight">
        This page doesn't exist
      </h1>

      <p className="mt-3 max-w-lg text-sm leading-relaxed text-muted-foreground">
        The route isn't registered on this gateway. It may have been renamed, or
        the link that sent you here may be out of date.
      </p>

      <div className="mt-10 grid gap-px overflow-hidden rounded-lg border border-border/60 bg-border/60 sm:grid-cols-2">
        {DESTINATIONS.map((d) => (
          <Link key={d.to} to={d.to} className="block bg-background p-5 transition-colors hover:bg-muted/40">
            <span className="font-mono text-[11px] uppercase tracking-[0.06em]">
              {d.label}
            </span>
            <p className="mt-2 text-xs leading-relaxed text-muted-foreground">
              {d.description}
            </p>
          </Link>
        ))}
      </div>

      <div className="mt-10 flex flex-wrap gap-3">
        <Link to="/" className={buttonVariants({ size: "sm" })}>
          Back to overview
        </Link>
        <Link to="/playground" className={buttonVariants({ size: "sm", variant: "outline" })}>
          Open API sandbox
        </Link>
      </div>
    </div>
  );
}
