package governance

// defaultDomainList is the bundled public AI-tool allowlist served by
// /v1/governance/domain-list. The browser extension's manifest already
// bundles its own copy; this endpoint exists so customer deployments can
// extend the list without releasing a new extension version.
//
// Keep this in sync with bastio-extension/manifest.json's content_scripts.matches.
func defaultDomainList() []string {
	return []string{
		"chatgpt.com",
		"chat.openai.com",
		"claude.ai",
		"gemini.google.com",
		"bard.google.com",
		"copilot.microsoft.com",
		"www.bing.com/chat",
		"perplexity.ai",
		"www.perplexity.ai",
		"character.ai",
		"poe.com",
		"chat.mistral.ai",
		"mistral.ai/chat",
		"you.com",
		"huggingface.co/chat",
		"meta.ai",
		"chat.deepseek.com",
		"www.deepseek.com",
		"www.phind.com",
	}
}
