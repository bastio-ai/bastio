package server

import "github.com/bastio-ai/bastio/internal/gateway"

// ProviderEndpoint resolves a URL path to its native provider API endpoint.
func ProviderEndpoint(path string) string      { return gateway.ProviderEndpoint(path) }
func IsGenerationGatewayPath(path string) bool { return gateway.IsGenerationGatewayPath(path) }
func GatewayInputText(raw []byte) string       { return gateway.InputText(raw) }
func GatewayOutputText(raw []byte) string      { return gateway.OutputText(raw) }
