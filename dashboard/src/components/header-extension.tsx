import { createContext, useContext, type ReactNode } from "react";

export type HeaderWorkspace = {
  id: string;
  name: string;
  detail?: string;
  role?: "owner" | "admin" | "member" | "viewer" | string;
  isHome?: boolean;
};

export type HeaderExtension = {
  workspaces?: HeaderWorkspace[];
  activeWorkspaceID?: string;
  switchingWorkspace?: boolean;
  onWorkspaceChange?: (workspaceID: string) => void | Promise<void>;
  onCreateWorkspace?: (name: string) => Promise<void>;
  onRenameWorkspace?: (workspaceID: string, name: string) => Promise<void>;
  statusMode?: "dependencies" | "platform";
  statusPageURL?: string;
};

const HeaderExtensionContext = createContext<HeaderExtension>({});

export function HeaderExtensionProvider({ value, children }: { value: HeaderExtension; children: ReactNode }) {
  return <HeaderExtensionContext.Provider value={value}>{children}</HeaderExtensionContext.Provider>;
}

export function useHeaderExtension(): HeaderExtension {
  return useContext(HeaderExtensionContext);
}
