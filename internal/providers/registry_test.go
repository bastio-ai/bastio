package providers

import (
	"context"
	"testing"
)

// fakeClient is a minimal Client used to exercise Registry behaviour
// without standing up a real HTTP backend.
type fakeClient struct {
	name Provider
}

func (f *fakeClient) Name() Provider                                                              { return f.name }
func (f *fakeClient) Chat(_ context.Context, _ *ChatRequest, _ string) (*ChatResponse, error)     { return &ChatResponse{}, nil }
func (f *fakeClient) ChatStream(_ context.Context, _ *ChatRequest, _ string) (<-chan StreamChunk, error) {
	return nil, nil
}

// decoratingClient wraps another Client and tags Chat responses so a
// test can prove the wrapper actually ran.
type decoratingClient struct {
	Client
	tag string
}

func (d *decoratingClient) Chat(ctx context.Context, req *ChatRequest, apiKey string) (*ChatResponse, error) {
	resp, err := d.Client.Chat(ctx, req, apiKey)
	if err != nil {
		return nil, err
	}
	resp.ID = d.tag
	return resp, nil
}

func TestRegistry_DecorateAppliesToGet(t *testing.T) {
	r := NewRegistry()
	r.Register(&fakeClient{name: ProviderOpenAI})

	// No decorator: Get returns the raw client untouched.
	c, err := r.Get(ProviderOpenAI)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if _, ok := c.(*decoratingClient); ok {
		t.Fatal("got a decorating client without a decorator installed")
	}

	// Install a decorator. Subsequent Get returns wrapped clients.
	r.Decorate(func(p Provider, inner Client) Client {
		return &decoratingClient{Client: inner, tag: "wrapped:" + string(p)}
	})
	c2, err := r.Get(ProviderOpenAI)
	if err != nil {
		t.Fatalf("get post-decorate: %v", err)
	}
	dc, ok := c2.(*decoratingClient)
	if !ok {
		t.Fatal("decorator did not run on Get")
	}
	if dc.tag != "wrapped:openai" {
		t.Fatalf("decorator saw wrong provider: %s", dc.tag)
	}

	// The decorated client's Chat must use the wrapper's behaviour.
	resp, err := c2.Chat(context.Background(), &ChatRequest{}, "")
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if resp.ID != "wrapped:openai" {
		t.Fatalf("decorator's Chat override didn't apply: id=%s", resp.ID)
	}
}

func TestRegistry_DecorateNilClears(t *testing.T) {
	r := NewRegistry()
	r.Register(&fakeClient{name: ProviderOpenAI})
	r.Decorate(func(_ Provider, inner Client) Client {
		return &decoratingClient{Client: inner, tag: "wrap"}
	})

	// Clearing the decorator: subsequent Get returns the raw client.
	r.Decorate(nil)
	c, _ := r.Get(ProviderOpenAI)
	if _, ok := c.(*decoratingClient); ok {
		t.Fatal("Decorate(nil) should clear the decorator")
	}
}

func TestRegistry_DecorateSelectiveByProvider(t *testing.T) {
	// A decorator that wraps only one provider must leave others
	// untouched. Verifies the (Provider, Client) signature is used,
	// not just (Client).
	r := NewRegistry()
	r.Register(&fakeClient{name: ProviderOpenAI})
	r.Register(&fakeClient{name: ProviderAnthropic})
	r.Decorate(func(p Provider, inner Client) Client {
		if p == ProviderOpenAI {
			return &decoratingClient{Client: inner, tag: "wrap"}
		}
		return inner
	})

	if c, _ := r.Get(ProviderOpenAI); func() bool { _, ok := c.(*decoratingClient); return ok }() == false {
		t.Fatal("openai should be wrapped")
	}
	if c, _ := r.Get(ProviderAnthropic); func() bool { _, ok := c.(*decoratingClient); return ok }() == true {
		t.Fatal("anthropic should NOT be wrapped")
	}
}
