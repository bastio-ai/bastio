// Slot pattern for cloud-dashboard (or any other consumer of the OSS
// dashboard) to inject account-level sections into the Settings page
// without forking routes/settings.tsx. OSS itself leaves the context
// empty — the page renders just the OSS-shipped "Quick start" section.
//
// Mirrors SecurityExtensionProvider in components/security-extension.tsx
// and WorkspaceExtensionProvider in components/workspace/. Cloud's
// main.tsx wraps <RouterProvider> with this provider and supplies the
// Account section (subscription summary, billing/usage/team links).
//
// Why this lives in OSS even though only Cloud uses it today: the
// Settings IA is the same in both editions — extension points live in
// OSS so Cloud doesn't fork the page and can't drift out of sync. OSS
// standalone never renders the cloud sections because it never wraps
// with a provider.

import { createContext, useContext, type ReactNode } from "react";

export type SettingsExtension = {
  // Sections rendered between the page header and the OSS "Quick
  // start" section. Account-level concerns (plan, billing, team) sit
  // above integration helpers — they're what a customer opening
  // "Settings" is most likely looking for.
  accountSections?: ReactNode;
};

const Ctx = createContext<SettingsExtension>({});

export function SettingsExtensionProvider({
  value,
  children,
}: {
  value: SettingsExtension;
  children: ReactNode;
}) {
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useSettingsExtension(): SettingsExtension {
  return useContext(Ctx);
}
