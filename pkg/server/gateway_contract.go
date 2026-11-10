package server

import "github.com/bastio-ai/bastio/internal/gateway"

// ProviderEndpoint resolves a URL path to its native provider API endpoint.
func ProviderEndpoint(path string) string      { return gateway.ProviderEndpoint(path) }
func IsGenerationGatewayPath(path string) bool { return gateway.IsGenerationGatewayPath(path) }

func IsBatchGatewayPath(path string) bool { return gateway.IsBatchGatewayPath(path) }
func GatewayInputText(raw []byte) string  { return gateway.InputText(raw) }
func GatewayOutputText(raw []byte) string { return gateway.OutputText(raw) }

// GatewayMapInputText applies transform to every text field of a gateway
// request body that GatewayInputText reads (OpenAI chat and Responses,
// Claude Messages, Gemini contents, legacy completions, embeddings and
// reranking) and returns the re-encoded body. Keys, ids, tool-call ids,
// signatures, base64 media and system/developer instructions are left
// as they are. changed is false, and raw is returned as is, when the
// body is not a JSON object or transform changed nothing.
func GatewayMapInputText(raw []byte, transform func(string) string) (out []byte, changed bool) {
	return gateway.MapInputText(raw, transform)
}

// GatewayMapOutputText applies transform to every text field of a
// non-streamed gateway response body that GatewayOutputText reads
// (OpenAI chat and Responses, Claude Messages, Gemini candidates),
// including reasoning and tool-call arguments, leaving provider metadata
// as it is. changed is false, and raw is returned as is, when the body is
// not a JSON object or transform changed nothing.
func GatewayMapOutputText(raw []byte, transform func(string) string) (out []byte, changed bool) {
	return gateway.MapOutputText(raw, transform)
}
