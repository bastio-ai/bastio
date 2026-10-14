import { createContext, useContext, type ReactNode } from "react";

export type GatewayExtension = {
  providerKeyManagement: "allowed" | "restricted" | "loading" | "unavailable";
  managedProviders: string[];
  accountNotice?: ReactNode;
};
const Context = createContext<GatewayExtension>({
  providerKeyManagement: "allowed",
  managedProviders: [],
});
export function GatewayExtensionProvider({
  value,
  children,
}: {
  value: GatewayExtension;
  children: ReactNode;
}) {
  return <Context.Provider value={value}>{children}</Context.Provider>;
}
export function useGatewayExtension() {
  return useContext(Context);
}
