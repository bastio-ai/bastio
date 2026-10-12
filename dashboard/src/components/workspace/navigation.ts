export const SETTINGS_SECTIONS = [
  "general",
  "models",
  "privacy",
  "usage",
] as const;
export type SettingsSection = (typeof SETTINGS_SECTIONS)[number];
export type WorkspaceSearch = {
  view?: string;
  section?: SettingsSection;
  subscribed?: string;
};

export function validateWorkspaceSearch(
  search: Record<string, unknown>,
): WorkspaceSearch {
  return {
    view: typeof search.view === "string" ? search.view : undefined,
    section: SETTINGS_SECTIONS.includes(search.section as SettingsSection)
      ? (search.section as SettingsSection)
      : undefined,
    subscribed:
      search.subscribed === "1" || search.subscribed === 1 ? "1" : undefined,
  };
}
