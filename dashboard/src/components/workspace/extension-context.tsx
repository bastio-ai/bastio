// WorkspaceExtensionContext — slot for cloud-dashboard to inject
// cloud-only tabs and toggles into the OSS Workspace admin page
// without forking routes. OSS supplies the default value (empty
// `extraTabs`, no `openWorkspaceURL`, upsell card visible). Cloud-
// dashboard wraps the RouterProvider with `<WorkspaceExtensionProvider>`
// in main.tsx and supplies its own value.
//
// The split is architectural, not a runtime flag: OSS source ships
// without any cloud-only tab code (team, audit, analytics, custom
// domains live in bastio-cloud only). This context is the seam that
// lets cloud-dashboard add them back.
//
// Gating is physical, not runtime: if cloud-dashboard didn't inject
// a tab, OSS doesn't have it to render.

import { createContext, useContext, type ReactNode } from "react";

export type WorkspaceExtraTab = {
  id: string;
  label: string;
  component: ReactNode;
};

export type WorkspaceExtension = {
  // Tabs to append after the OSS-shipped tabs in the admin tab strip.
  // Cloud-dashboard supplies team/audit/analytics/domains here.
  extraTabs: WorkspaceExtraTab[];

  // External URL for the "Open Workspace" button. Cloud points this at
  // workspace.bastio.com (or the customer's branded host); OSS leaves
  // it null, which makes the workspace page link to the in-app
  // /workspace/chat route instead — there's no separate OSS SPA.
  openWorkspaceURL: string | null;

  // Suppress the in-page "Run on Bastio Cloud" upsell card. Cloud
  // sets true (no need to advertise to existing customers); OSS
  // leaves it false so self-hosters see the funnel.
  hideUpsell: boolean;
};

const defaultValue: WorkspaceExtension = {
  extraTabs: [],
  openWorkspaceURL: null,
  hideUpsell: false,
};

const Ctx = createContext<WorkspaceExtension>(defaultValue);

export function WorkspaceExtensionProvider({
  children,
  value,
}: {
  children: ReactNode;
  value: Partial<WorkspaceExtension>;
}) {
  const merged: WorkspaceExtension = { ...defaultValue, ...value };
  return <Ctx.Provider value={merged}>{children}</Ctx.Provider>;
}

export function useWorkspaceExtension(): WorkspaceExtension {
  return useContext(Ctx);
}
