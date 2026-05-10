// Slot pattern for cloud-dashboard (or any other consumer of the OSS
// dashboard) to inject extra tabs into the Security Center page
// without forking routes/security.tsx. OSS itself leaves the context
// empty — the page renders just the OSS-shipped "Detectors" tab.
//
// Mirrors WorkspaceExtensionProvider in components/workspace/ and
// LayoutNavExtensionProvider in components/layout-extension.tsx.
// Cloud's main.tsx wraps <RouterProvider> with this provider and
// supplies tabs for cloud-only security toggles (today: "Privacy
// Filter" — the v4.1.B response-side scan opt-in; future: per-detector
// block policy, custom rule overrides, etc.).
//
// Why this lives in OSS even though only Cloud uses it today: the
// Security Center IA is the same in both editions — extension points
// live in OSS so Cloud doesn't fork the page, doesn't duplicate the
// shell, and can't drift out of sync with OSS. OSS standalone never
// renders the cloud tabs because it never wraps with a provider.

import { createContext, useContext, type ReactNode } from "react";

export type SecurityTab = {
  // Stable id used as the Tabs `value`. Keep it short + URL-safe in
  // case we ever sync the active tab to a query param.
  id: string;
  // Human-readable label shown in the tab strip.
  label: string;
  // What renders inside <TabsContent value={id}>. Self-contained —
  // the Security Center shell hands over the area below the tab strip.
  component: ReactNode;
};

export type SecurityExtension = {
  // Tabs appended after the OSS-shipped tabs. Order is the array
  // order; OSS's "Detectors" tab is always first (it owns the
  // default-active position).
  extraTabs?: SecurityTab[];
};

const Ctx = createContext<SecurityExtension>({});

export function SecurityExtensionProvider({
  value,
  children,
}: {
  value: SecurityExtension;
  children: ReactNode;
}) {
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useSecurityExtension(): SecurityExtension {
  return useContext(Ctx);
}
