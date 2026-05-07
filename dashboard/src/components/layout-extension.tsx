// Slot pattern for cloud-dashboard (and any other consumer of the OSS
// dashboard) to inject extra nav entries into the sidebar without
// forking layout.tsx. OSS itself leaves the context empty — the
// sidebar renders just the OSS-shipped sections.
//
// Mirrors WorkspaceExtensionProvider in components/workspace/. Cloud's
// main.tsx wraps <RouterProvider> with this provider supplying the
// "Shadow AI" → /governance link, which lives in cloud-dashboard's
// route tree (composed onto the OSS rootRoute via main.tsx).

import { createContext, useContext, type ReactNode } from "react";
import type { LucideIcon } from "lucide-react";

export type LayoutNavItem = {
  // String, not typed against the OSS router's routesByPath, so consumers
  // can register routes that don't exist in OSS (e.g. /governance which
  // cloud-dashboard adds via rootRoute.addChildren). The layout casts
  // through `as never` when handing this to <Link to={…}>.
  to: string;
  label: string;
  icon: LucideIcon;
};

export type LayoutNavSection = {
  label: string;
  items: LayoutNavItem[];
};

export type LayoutNavExtension = {
  sections?: LayoutNavSection[];
};

const Ctx = createContext<LayoutNavExtension>({});

export function LayoutNavExtensionProvider({
  value,
  children,
}: {
  value: LayoutNavExtension;
  children: ReactNode;
}) {
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useLayoutNav(): LayoutNavExtension {
  return useContext(Ctx);
}
