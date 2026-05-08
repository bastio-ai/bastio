// Slot pattern that lets a consumer of the OSS dashboard inject extra
// rows into the /traces feed. Cloud-dashboard uses it to render
// browser-extension governance events alongside gateway-API traces in
// a single timeline. OSS standalone leaves the context empty — the
// page renders only the rows api.traces.list() returns.
//
// Mirrors LayoutNavExtensionProvider (components/layout-extension.tsx)
// and WorkspaceExtensionProvider — same shape: cloud's main.tsx wraps
// <RouterProvider> with this provider and supplies a fetcher; OSS
// standalone does nothing.

import { createContext, useContext, type ReactNode } from "react";

import type { Trace } from "@/api/client";
import type { ObserveFilters } from "@/components/observe/filter-bar";

export type TracesExtension = {
  // Async fetcher run alongside the primary api.traces.list() call.
  // Should return rows shaped as Trace — typically by projecting from
  // some other event store (e.g. governance_events) into the Trace
  // shape. The OSS Traces page merges these into the main result and
  // sorts by started_at desc, so the timeline is still in the order
  // a user expects.
  //
  // The fetcher receives the current ObserveFilters so it can apply
  // matching server-side filters where possible (time-window, severity,
  // etc). Filters that don't apply to extension data are fine to ignore.
  fetchExtra?: (filters: ObserveFilters) => Promise<Trace[]>;

  // Detail-fetcher used by the /traces/:id route. The OSS detail page
  // first tries api.traces.get(id) and on 404 calls this fallback.
  // Returning null means "not mine — let the 404 stand." A non-null
  // Trace renders the extension's own detail panel via renderDetail
  // (or a default panel if renderDetail is missing).
  fetchExtraDetail?: (id: string) => Promise<Trace | null>;

  // Optional custom renderer for the detail page when the row came
  // from fetchExtraDetail. The OSS page passes the trace; the consumer
  // returns a JSX element. When omitted, the page renders the standard
  // Trace detail layout — usable when projection alone is enough.
  renderDetail?: (trace: Trace) => React.ReactNode;
};

const Ctx = createContext<TracesExtension>({});

export function TracesExtensionProvider({
  value,
  children,
}: {
  value: TracesExtension;
  children: ReactNode;
}) {
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useTracesExtension(): TracesExtension {
  return useContext(Ctx);
}
