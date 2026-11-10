import { createContext, useContext, type ReactNode } from "react";

export type GatewayExtension = {
  providerKeyManagement: "allowed" | "restricted" | "loading" | "unavailable";
  managedProviders: string[];
  accountNotice?: ReactNode;
  /**
   * Public origin applications call the gateway on, used for connection
   * examples. Hosts whose dashboard and gateway live on different origins
   * (cloud: cloud.bastio.com vs api.bastio.com) set this. Falls back to
   * VITE_API_URL, then the dashboard's own origin.
   */
  gatewayOrigin?: string;
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

/** Origin (no trailing slash) used in copy-paste gateway connection examples. */
export function useGatewayOrigin() {
  const { gatewayOrigin } = useGatewayExtension();
  return new URL(
    gatewayOrigin || import.meta.env.VITE_API_URL || window.location.origin,
    window.location.origin,
  ).href.replace(/\/$/, "");
}
