package gateway

import (
	"context"
	"net/http"
	"testing"

	"github.com/bastio-ai/bastio/pkg/gatewaypolicy"
)

func TestCapturePolicySnapshotCannotBeSpoofedByCaller(t *testing.T) {
	for _, omit := range []bool{false, true} {
		ctx := gatewaypolicy.With(context.Background(), gatewaypolicy.Policy{OmitContent: omit})
		headers := make(http.Header)
		headers.Set("X-Bastio-Tag-Bastio.Content_Capture", "caller-supplied-lie")
		headers.Set("X-Bastio-Tag-Task", "product-refresh")
		tags := gatewayTraceTags(ctx, headers)
		want := "enabled"
		if omit {
			want = "metadata_only"
		}
		if tags["bastio.content_capture"] != want || tags["task"] != "product-refresh" {
			t.Fatalf("incorrect trusted snapshot: %v", tags)
		}
	}
}
