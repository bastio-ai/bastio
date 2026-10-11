package workspace

// defaultWorkspaceModel is paired with the curated employee model catalogue.
// Use provider-native IDs: a model chosen for one provider cannot be sent to another.
func defaultWorkspaceModel(provider string) string {
	switch provider {
	case "anthropic":
		return "claude-sonnet-5-5"
	case "gemini":
		return "gemini-3.8-flash"
	case "deepseek":
		return "deepseek-flash"
	case "xai":
		return "grok-4.7"
	case "groq":
		return "openai/gpt-oss-120b"
	case "openrouter":
		return "openai/gpt-6.1-sol"
	case "bedrock":
		return "anthropic.claude-sonnet-5-5"
	case "ollama":
		return "gemma4"
	default:
		return "gpt-6.1-sol"
	}
}
