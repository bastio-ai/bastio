package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	bedrocktypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

// BedrockClient implements the Client interface against Amazon Bedrock's
// Converse API. Converse is a unified cross-provider API that accepts
// OpenAI-style messages, so the translation layer is thin.
//
// Authentication uses the standard AWS SDK credential chain (environment,
// shared config, IMDS, etc). The apiKey argument is treated as an opaque
// handle of the form "region:access_key_id:secret_access_key" — set per
// proxy in Bastio — and, if non-empty, overrides the ambient chain for
// that single request. An empty apiKey falls back to the default chain.
type BedrockClient struct{}

// NewBedrockClient creates a Bedrock client. No configuration is resolved
// until a request is made, so constructing the client is free.
func NewBedrockClient() *BedrockClient { return &BedrockClient{} }

func (c *BedrockClient) Name() Provider { return ProviderBedrock }

// Chat sends a non-streaming request via Converse.
func (c *BedrockClient) Chat(ctx context.Context, req *ChatRequest, apiKey string) (*ChatResponse, error) {
	client, err := c.clientFor(ctx, apiKey)
	if err != nil {
		return nil, err
	}

	modelID, msgs, system, err := c.convert(req)
	if err != nil {
		return nil, err
	}

	out, err := client.Converse(ctx, &bedrockruntime.ConverseInput{
		ModelId:  aws.String(modelID),
		Messages: msgs,
		System:   system,
	})
	if err != nil {
		return nil, fmt.Errorf("bedrock converse: %w", err)
	}

	msg, ok := out.Output.(*bedrocktypes.ConverseOutputMemberMessage)
	if !ok || msg == nil {
		return nil, fmt.Errorf("bedrock response missing message")
	}

	var sb strings.Builder
	for _, block := range msg.Value.Content {
		if tb, ok := block.(*bedrocktypes.ContentBlockMemberText); ok {
			sb.WriteString(tb.Value)
		}
	}
	content := sb.String()

	raw, _ := json.Marshal(out)
	resp := &ChatResponse{
		Model:        modelID,
		Content:      content,
		Role:         "assistant",
		FinishReason: string(out.StopReason),
		Raw:          raw,
	}
	if out.Usage != nil {
		if v := out.Usage.InputTokens; v != nil {
			resp.InputTokens = int(*v)
		}
		if v := out.Usage.OutputTokens; v != nil {
			resp.OutputTokens = int(*v)
		}
	}
	return resp, nil
}

// ChatStream sends a streaming request via ConverseStream. Each delta is
// re-emitted as SSE so the gateway's shared streaming pipeline can forward
// it to the client unchanged.
func (c *BedrockClient) ChatStream(ctx context.Context, req *ChatRequest, apiKey string) (<-chan StreamChunk, error) {
	client, err := c.clientFor(ctx, apiKey)
	if err != nil {
		return nil, err
	}

	modelID, msgs, system, err := c.convert(req)
	if err != nil {
		return nil, err
	}

	out, err := client.ConverseStream(ctx, &bedrockruntime.ConverseStreamInput{
		ModelId:  aws.String(modelID),
		Messages: msgs,
		System:   system,
	})
	if err != nil {
		return nil, fmt.Errorf("bedrock converse stream: %w", err)
	}

	ch := make(chan StreamChunk)
	go func() {
		defer close(ch)
		defer out.GetStream().Close()

		for event := range out.GetStream().Events() {
			switch v := event.(type) {
			case *bedrocktypes.ConverseStreamOutputMemberContentBlockDelta:
				if txt, ok := v.Value.Delta.(*bedrocktypes.ContentBlockDeltaMemberText); ok {
					data, _ := json.Marshal(map[string]string{"delta": txt.Value})
					select {
					case ch <- StreamChunk{Data: data}:
					case <-ctx.Done():
						return
					}
				}
			case *bedrocktypes.ConverseStreamOutputMemberMessageStop:
				select {
				case ch <- StreamChunk{Done: true}:
				case <-ctx.Done():
				}
				return
			}
		}
		if err := out.GetStream().Err(); err != nil {
			select {
			case ch <- StreamChunk{Error: err}:
			case <-ctx.Done():
			}
		}
	}()
	return ch, nil
}

// clientFor builds a per-request Bedrock client. When apiKey is provided
// it is interpreted as "region:access_key_id:secret_access_key" and used
// to override the default credential chain for that request. This lets a
// single Bastio instance serve multiple AWS accounts without juggling
// profiles.
func (c *BedrockClient) clientFor(ctx context.Context, apiKey string) (*bedrockruntime.Client, error) {
	var opts []func(*awsconfig.LoadOptions) error

	if apiKey != "" {
		parts := strings.SplitN(apiKey, ":", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("bedrock api key must be region:access_key:secret_key")
		}
		opts = append(opts,
			awsconfig.WithRegion(parts[0]),
			awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(parts[1], parts[2], "")),
		)
	} else if r := os.Getenv("AWS_REGION"); r != "" {
		opts = append(opts, awsconfig.WithRegion(r))
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	return bedrockruntime.NewFromConfig(cfg), nil
}

// convert maps a ChatRequest into Bedrock Converse inputs. System prompts
// (role=system) are extracted out of the message list as Bedrock requires.
func (c *BedrockClient) convert(req *ChatRequest) (string, []bedrocktypes.Message, []bedrocktypes.SystemContentBlock, error) {
	var model string
	var msgs []Message

	if req.Raw != nil {
		var m struct {
			Model    string        `json:"model"`
			Messages []Message `json:"messages"`
		}
		if err := json.Unmarshal(req.Raw, &m); err == nil {
			model = m.Model
			msgs = m.Messages
		}
	}
	if model == "" {
		model = req.Model
	}
	if msgs == nil {
		msgs = req.Messages
	}
	if model == "" {
		return "", nil, nil, fmt.Errorf("bedrock: model id required")
	}

	var system []bedrocktypes.SystemContentBlock
	converted := make([]bedrocktypes.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == "system" {
			system = append(system, &bedrocktypes.SystemContentBlockMemberText{Value: m.Content})
			continue
		}
		role := bedrocktypes.ConversationRoleUser
		if m.Role == "assistant" {
			role = bedrocktypes.ConversationRoleAssistant
		}
		converted = append(converted, bedrocktypes.Message{
			Role:    role,
			Content: []bedrocktypes.ContentBlock{&bedrocktypes.ContentBlockMemberText{Value: m.Content}},
		})
	}
	return model, converted, system, nil
}
