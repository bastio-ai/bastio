import { createContext, useContext, type ReactNode } from "react";

export type OverviewExtension = {
  insights?: ReactNode;
};

const Ctx = createContext<OverviewExtension>({});

export function OverviewExtensionProvider({
  value,
  children,
}: {
  value: OverviewExtension;
  children: ReactNode;
}) {
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useOverviewExtension(): OverviewExtension {
  return useContext(Ctx);
}
