package detection

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTopicPolicyDetector_Invalidate(t *testing.T) {
	id := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	d := NewTopicPolicyDetector(nil, func(context.Context) uuid.UUID { return id }, time.Hour)
	d.compiled[id] = &compiledPolicy{}
	d.Invalidate(id)
	if _, ok := d.compiled[id]; ok {
		t.Fatal("invalidate must drop the compiled cache for the tenant")
	}
}
