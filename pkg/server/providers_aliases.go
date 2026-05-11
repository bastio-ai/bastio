package server

// Type aliases re-exporting the OSS internal/providers package so
// downstream callers (bastio-cloud) can describe LLM provider clients
// without touching an `internal/...` import path. Mirrors the same
// pattern used for `KeyResolver`, `EmbeddingClient`, `CustomerIDKey`
// — keep the internal package internal, surface only what extension
// code legitimately needs.
//
// What's exported here is exactly what's needed to write a Client
// decorator (the typical bastio-cloud use case for
// WithProvidersDecorator): the interface, the request/response/chunk
// types it operates on, and the Provider name constants.

import (
	"github.com/bastio-ai/bastio/internal/providers"
)

// Provider identifies an LLM provider.
type Provider = providers.Provider

// Client is the interface every provider implementation satisfies —
// Chat (non-streaming) + ChatStream (SSE) + Name. Downstream code
// implementing decorators must satisfy this interface.
type Client = providers.Client

// ChatRequest is the normalized request shape passed to every Client.
type ChatRequest = providers.ChatRequest

// ChatResponse is the normalized non-streaming response.
type ChatResponse = providers.ChatResponse

// Message is one chat turn in a request's Messages slice.
type Message = providers.Message

// Image is an optional inline attachment on a Message.
type Image = providers.Image

// StreamChunk is one chunk in a ChatStream response channel.
type StreamChunk = providers.StreamChunk

// Provider name constants. Re-exported so callers can switch on the
// provider returned by Client.Name() or ChatRequest.Provider.
const (
	ProviderOpenAI    = providers.ProviderOpenAI
	ProviderAnthropic = providers.ProviderAnthropic
	ProviderBedrock   = providers.ProviderBedrock
	ProviderVertex    = providers.ProviderVertex
	ProviderAzure     = providers.ProviderAzure
	ProviderOllama    = providers.ProviderOllama
)
