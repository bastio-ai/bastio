package server

import "github.com/bastio-ai/bastio/internal/workspace"

// WorkspaceEmbeddingFromEnv is shared by the API and standalone ingestion
// worker, preventing documents and queries from using different vector spaces.
func WorkspaceEmbeddingFromEnv(getenv func(string) string) (EmbeddingClient, error) {
	if endpoint := getenv("WORKSPACE_EMBEDDING_URL"); endpoint != "" {
		return workspace.NewBGEEmbeddingClient(endpoint, getenv("WORKSPACE_EMBEDDING_MODEL"), nil)
	}
	if key := getenv("OPENAI_API_KEY"); key != "" {
		return workspace.NewOpenAIEmbeddingClient(key), nil
	}
	return nil, nil
}
