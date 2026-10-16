// Package gatewaypolicy carries operator-resolved gateway policy across the
// Cloud classifiers, core gateway and recorder. Never populate it from client
// headers: the embedding application must authenticate and resolve the tenant.
package gatewaypolicy

import "context"

type Policy struct {
	MonitorOnly bool
	// OmitContent also removes matched values from retained security findings.
	OmitContent bool
}

type key struct{}

func With(ctx context.Context, policy Policy) context.Context {
	return context.WithValue(ctx, key{}, policy)
}

func From(ctx context.Context) Policy {
	p, _ := ctx.Value(key{}).(Policy)
	return p
}
