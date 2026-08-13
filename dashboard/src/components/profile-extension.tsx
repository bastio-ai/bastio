import { createContext, useContext, type ReactNode } from "react";

export type ProfileExtension = {
  profileContent?: ReactNode;
  userWidget?: {
    name: string;
    email: string;
    planName?: string;
    usageCount?: number;
    usageLimit?: number;
    onLogout?: () => void;
  };
};

const ProfileExtensionContext = createContext<ProfileExtension>({});

export function ProfileExtensionProvider({
  value,
  children,
}: {
  value: ProfileExtension;
  children: ReactNode;
}) {
  return <ProfileExtensionContext.Provider value={value}>{children}</ProfileExtensionContext.Provider>;
}

export function useProfileExtension(): ProfileExtension {
  return useContext(ProfileExtensionContext);
}
