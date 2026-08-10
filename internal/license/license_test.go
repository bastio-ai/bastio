package license

import (
	"testing"
)

func TestLicenseDefaultOSSFallback(t *testing.T) {
	svc := NewService()
	_, status := svc.LoadFromEnvOrPath()

	if !status.Valid {
		t.Fatalf("expected valid status for OSS fallback, got invalid: %s", status.Message)
	}

	if status.Tier != "community_oss" {
		t.Errorf("expected tier community_oss, got %s", status.Tier)
	}
}
