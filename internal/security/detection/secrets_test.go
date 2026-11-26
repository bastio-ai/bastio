package detection

import (
	"context"
	"strings"
	"testing"
)

// Test fixtures below split well-known token prefixes (e.g. "gh"+"p_")
// across string concatenations so source-level secret scanners (GitHub
// Push Protection, gitleaks) don't false-positive on this file. Runtime
// values are unchanged and the detector still matches them.

func TestSecretsDetector_AWSKey(t *testing.T) {
	d := NewSecretsDetector()
	findings, _ := d.Detect(context.Background(), "my key is AKIAIOSFODNN7EXAMPLE please help")
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	if findings[0].MatchedPattern != "aws_access_key" {
		t.Errorf("want aws_access_key, got %s", findings[0].MatchedPattern)
	}
}

func TestSecretsDetector_GitHubPAT(t *testing.T) {
	d := NewSecretsDetector()
	findings, _ := d.Detect(context.Background(), "token: gh"+"p_1234567890abcdefghijklmnopqrstuvwxyz")
	if len(findings) == 0 {
		t.Fatal("expected github_pat finding")
	}
}

func TestSecretsDetector_OpenAIKey(t *testing.T) {
	d := NewSecretsDetector()
	findings, _ := d.Detect(context.Background(), "sk-abcdef1234567890ABCDEF please use this key")
	if len(findings) == 0 {
		t.Fatal("expected openai_api_key finding")
	}
}

func TestSecretsDetector_MaskReplacesSecrets(t *testing.T) {
	d := NewSecretsDetector()
	masked := d.Mask("key=AKIAIOSFODNN7EXAMPLE and another gh" + "p_1234567890abcdefghijklmnopqrstuvwxyz")
	if strings.Contains(masked, "AKIA") {
		t.Errorf("AWS key leaked after mask: %q", masked)
	}
	if strings.Contains(masked, "gh"+"p_1234567890") {
		t.Errorf("GitHub PAT leaked after mask: %q", masked)
	}
}

func TestSecretsDetector_LowEntropyAssignmentIgnored(t *testing.T) {
	d := NewSecretsDetector()
	// "hello world example text" is low entropy; shouldn't count as
	// a secret even though the shape matches `SECRET=...`.
	findings, _ := d.Detect(context.Background(), "SECRET=helloworldhelloworldhelloworld")
	for _, f := range findings {
		if f.MatchedPattern == "generic_secret_assignment" {
			t.Errorf("low-entropy value shouldn't fire generic secret rule: %+v", f)
		}
	}
}

func TestSecretsDetector_HighEntropyAssignmentFires(t *testing.T) {
	d := NewSecretsDetector()
	// Random-looking value.
	findings, _ := d.Detect(context.Background(), "SECRET=Xk7pQ2vL9mN4aT6bZ1cYwRf8d")
	found := false
	for _, f := range findings {
		if f.MatchedPattern == "generic_secret_assignment" {
			found = true
		}
	}
	if !found {
		t.Error("expected generic secret assignment to fire on high-entropy value")
	}
}

func TestSecretsDetector_PrivateKeyHeader(t *testing.T) {
	d := NewSecretsDetector()
	findings, _ := d.Detect(context.Background(), "here is my key\n-----BEGIN RSA PRIVATE KEY-----\nMIIE...\n")
	if len(findings) == 0 {
		t.Fatal("expected private_key finding")
	}
}

func TestSecretsDetector_ExtensionCatalog(t *testing.T) {
	d := NewSecretsDetector()
	azureKey := "AccountKey=" + strings.Repeat("A", 86) + "=="
	npmTok := "npm_" + strings.Repeat("a", 36)
	sendgrid := "SG." + strings.Repeat("x", 22) + "." + strings.Repeat("y", 43)
	pypi := "pypi-AgEIcHlwaS5vcmcC" + strings.Repeat("z", 40)
	tailscale := "tskey-auth-" + strings.Repeat("a", 16) + "-" + strings.Repeat("b", 32)
	twilio := "AC" + strings.Repeat("a", 32)
	discord := "https://discord.com/api/webhooks/123456789012345678/" + strings.Repeat("W", 60)
	heroku := "heroku_api_key=aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	cases := []struct {
		content string
		kind    string
	}{
		{"sts key ASIAIOSFODNN7EXAMPLE in logs", "aws_access_key"},
		{azureKey + " in the connection string", "azure_storage_key"},
		{"uri mongodb+srv://user:pass@cluster0.mongodb.net/app", "mongodb_uri"},
		{"sid " + twilio, "twilio_sid"},
		{"key " + sendgrid, "sendgrid_key"},
		{"hook " + discord, "discord_webhook"},
		{"auth " + npmTok, "npm_token"},
		{"auth " + tailscale, "tailscale_key"},
		{"auth " + pypi, "pypi_token"},
		{heroku, "heroku_api_key"},
	}
	for _, tt := range cases {
		t.Run(tt.kind, func(t *testing.T) {
			findings, err := d.Detect(context.Background(), tt.content)
			if err != nil {
				t.Fatalf("detect: %v", err)
			}
			found := false
			for _, f := range findings {
				if f.MatchedPattern == tt.kind {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("want %s in %+v", tt.kind, findings)
			}
			masked := d.Mask(tt.content)
			if masked == tt.content {
				t.Errorf("mask left content unchanged for %s", tt.kind)
			}
		})
	}
}

func TestSecretsDetector_AnthropicNotClassifiedAsOpenAI(t *testing.T) {
	d := NewSecretsDetector()
	key := "sk-ant-" + strings.Repeat("a", 40)
	findings, _ := d.Detect(context.Background(), "key "+key)
	for _, f := range findings {
		if f.MatchedPattern == "openai_api_key" {
			t.Fatalf("sk-ant key classified as openai: %+v", findings)
		}
	}
	found := false
	for _, f := range findings {
		if f.MatchedPattern == "anthropic_api_key" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected anthropic_api_key, got %+v", findings)
	}
}

// Passwords are split from their URLs so source-level secret scanners do not
// flag this file; runtime values are whole.
const urlTestPassword = "S3cr3t" + "Passw0rd!"

func TestSecretsDetector_URLCredentials(t *testing.T) {
	d := NewSecretsDetector()
	positives := map[string]string{
		"postgres":             "Connect with postgres://svc:" + urlTestPassword + "@db.internal:5432/prod today.",
		"postgresql":           "DATABASE_URL=postgresql://svc:" + urlTestPassword + "@db.internal/prod?sslmode=require",
		"mysql":                "mysql://app:" + urlTestPassword + "@10.0.0.5:3306/shop",
		"mongodb":              "mongodb://app:" + urlTestPassword + "@mongo.internal:27017/shop",
		"mongodb+srv":          "mongodb+srv://app:" + urlTestPassword + "@cluster0.example.net/shop",
		"redis empty user":     "redis://:" + urlTestPassword + "@cache.internal:6379/0",
		"rediss":               "rediss://default:" + urlTestPassword + "@cache.internal:6380",
		"amqp":                 "amqp://worker:" + urlTestPassword + "@queue.internal:5672/vhost",
		"amqps":                "amqps://worker:" + urlTestPassword + "@queue.internal",
		"https basic auth":     "git clone https://deploy:" + urlTestPassword + "@git.internal/team/repo.git",
		"http basic auth":      "curl http://admin:" + urlTestPassword + "@10.0.0.9:8080/metrics",
		"upper-case scheme":    "POSTGRES://svc:" + urlTestPassword + "@db.internal/prod",
		"percent-encoded pass": "postgres://svc:p%40ss%21w0rd%2Fx9@db.internal:5432/prod",
		"quoted in config":     `url = "postgres://svc:` + urlTestPassword + `@db.internal/prod"`,
		// Weak, default and documented passwords are still credentials.
		"literal password": "postgres://user:password@localhost:5432/app",
		"default postgres": "postgres://postgres:postgres@localhost:5432/app",
		"default guest":    "amqp://guest:guest@localhost:5672",
		"same as user":     "mysql://shop:shop@localhost/shop",
		"root":             "mysql://root:root@10.0.0.5:3306/shop",
		"admin":            "http://admin:admin@10.0.0.9:8080/metrics",
		"changeme":         "redis://:changeme@cache.internal:6379",
		"short word":       "postgres://svc:foo@db.internal/prod",
		"all x":            "postgres://svc:xxxx@db.internal/prod",
		"dollar inside":    "postgres://svc:Pa$$w0rd9@db.internal/prod",
		"braces inside":    "postgres://svc:ab{c}d9@db.internal/prod",
		"percent inside":   "postgres://svc:100%sure9@db.internal/prod",
	}
	for name, text := range positives {
		t.Run("positive/"+name, func(t *testing.T) {
			findings, _ := d.Detect(context.Background(), text)
			found := false
			for _, f := range findings {
				if f.MatchedPattern == "url_credentials" {
					found = true
					if strings.Contains(f.MatchedContent, "Passw0rd") || strings.Contains(f.MatchedContent, "w0rd") {
						t.Errorf("finding carries the password: %q", f.MatchedContent)
					}
				}
			}
			if !found {
				t.Fatalf("no url_credentials finding")
			}
			masked := d.Mask(text)
			if strings.Contains(masked, "Passw0rd") || strings.Contains(masked, "p%40ss") || strings.Contains(masked, "@") {
				t.Errorf("mask kept the password: %q", masked)
			}
			for _, span := range d.RewriteSpans(context.Background(), text) {
				if span.Start < 0 || span.End > len(text) || span.Start >= span.End {
					t.Errorf("invalid rewrite span %+v", span)
				}
			}
		})
	}
	negatives := map[string]string{
		"no userinfo":           "postgres://db.internal:5432/prod",
		"host and port only":    "http://localhost:3000/health and https://example.com:8443/path",
		"user name only":        "postgres://svc@db.internal:5432/prod and https://deploy@git.internal/repo.git",
		"empty password":        "postgres://svc:@db.internal/prod",
		"shell variable":        "postgres://svc:${DB_PASSWORD}@db.internal/prod and redis://:$REDIS_PASSWORD@cache:6379",
		"brace template":        "postgres://svc:{password}@db.internal/prod",
		"double-brace template": "postgres://svc:{{password}}@db.internal/prod and postgres://svc:{{.Values.db.password}}@db/prod",
		"redaction marker":      "postgres://svc:[REDACTED]@db.internal/prod",
		"unicode ellipsis":      "postgres://svc:…@db.internal/prod",
		"format verb":           "postgres://%s:%s@%s:%d/%s",
		"angle placeholder":     "postgres://<user>:<password>@<host>/<db>",
		"starred":               "postgres://svc:****@db.internal/prod",
		"elided":                "postgres://svc:...@db.internal/prod",
		"at sign in path":       "https://example.com/users/@alice and https://example.com/a:b@c",
		"at sign in query":      "https://example.com/search?q=a:b@c",
		"ipv6 literal":          "http://[::1]:8080/metrics",
		"email and time":        "Mail alice at 10:30 about https://example.com",
		"unsupported scheme":    "ftp://svc:" + urlTestPassword + "@files.internal/pub",
		"scp-style git":         "git@github.com:org/repo.git",
		"malformed":             "postgres:/svc:secretvalue@host and postgres//svc:x@host and ://a:b@c",
		"prose with colon":      "Use the format scheme://user:password@host for connection strings.",
	}
	for name, text := range negatives {
		t.Run("negative/"+name, func(t *testing.T) {
			findings, _ := d.Detect(context.Background(), text)
			for _, f := range findings {
				if f.MatchedPattern == "url_credentials" {
					t.Fatalf("false positive on %q", text)
				}
			}
		})
	}
	// The path and the surrounding text survive a mask; only scheme through host is replaced.
	masked := d.Mask("see postgres://svc:" + urlTestPassword + "@db.internal:5432/prod?sslmode=require for details")
	if masked != "see [REDACTED_URL_CREDENTIALS]/prod?sslmode=require for details" {
		t.Errorf("unexpected mask result %q", masked)
	}
}
