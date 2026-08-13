const configuredDocsURL = import.meta.env.VITE_DOCS_URL?.trim();

export const DOCS_URL = configuredDocsURL || "https://bastio.com/docs";
