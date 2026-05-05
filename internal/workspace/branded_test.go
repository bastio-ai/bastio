package workspace

import (
	"testing"
)

func TestValidSlug(t *testing.T) {
	t.Parallel()
	good := []string{"acme", "acme-team", "acme-team-2026", "abc", "ai-co"}
	bad := []string{
		"",            // empty
		"ab",          // too short
		"-acme",       // leading hyphen
		"acme-",       // trailing hyphen
		"AcMe",        // uppercase
		"acme_team",   // underscore
		"acme.team",   // dot
		"acme team",   // space
	}
	for _, s := range good {
		if !validSlug(s) {
			t.Errorf("expected %q to be valid", s)
		}
	}
	for _, s := range bad {
		if validSlug(s) {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}

func TestNormalizeDomain(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"https://AI.Acme.com/path", "ai.acme.com"},
		{"http://ai.acme.com:8080", "ai.acme.com"},
		{"AI.ACME.COM", "ai.acme.com"},
		{"  ai.acme.com  ", "ai.acme.com"},
	}
	for _, c := range cases {
		if got := normalizeDomain(c.in); got != c.want {
			t.Errorf("normalizeDomain(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestValidDomain(t *testing.T) {
	t.Parallel()
	good := []string{"acme.com", "ai.acme.com", "chat-pro.acme.io", "a.b.c.example"}
	bad := []string{
		"",          // empty
		"localhost", // no dot
		"acme",      // no dot
		"under_score.com",
		"acme.com/", // contains slash
		" .com",
	}
	for _, d := range good {
		if !validDomain(d) {
			t.Errorf("expected %q valid", d)
		}
	}
	for _, d := range bad {
		if validDomain(d) {
			t.Errorf("expected %q invalid", d)
		}
	}
}

func TestPseudonymizeIPStable(t *testing.T) {
	t.Parallel()
	a := pseudonymizeIP("192.168.1.5:54321")
	b := pseudonymizeIP("192.168.1.5:54321")
	if a != b || a == "" {
		t.Fatalf("hash should be stable + non-empty: %q vs %q", a, b)
	}
	if pseudonymizeIP("192.168.1.5:54321") == pseudonymizeIP("10.0.0.1:54321") {
		t.Fatal("different IPs should hash differently")
	}
	// Empty input → empty output (don't fingerprint nothing).
	if pseudonymizeIP("") != "" {
		t.Fatal("empty in → empty out")
	}
}

func TestBrandedShortPath(t *testing.T) {
	t.Parallel()
	yes := []string{"/", "/messages", "/messages/stream", "/session/label", "/messages/foo"}
	no := []string{"/v1/foo", "/healthz", "/scim/v2/Users", "/dashboard"}
	for _, p := range yes {
		if !brandedShortPath(p) {
			t.Errorf("expected %q in scope", p)
		}
	}
	for _, p := range no {
		if brandedShortPath(p) {
			t.Errorf("expected %q out of scope", p)
		}
	}
}
