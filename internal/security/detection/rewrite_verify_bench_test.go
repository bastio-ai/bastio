package detection

import (
	"context"
	"strings"
	"testing"

	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/normalize"
)

// benchEngine mirrors pkg/server.BuildSecurityEngine without the
// PG-backed topic detector (topic_policy is off in the default profile).
func benchEngine() *security.Engine {
	return security.NewEngine(
		NewInjectionDetector(),
		NewPIIDetector(),
		NewJailbreakDetector(),
		NewSecretsDetector(),
		NewIndirectInjectionDetector(),
		NewExfilDetector(),
	)
}

// benchPrompt is ~2 KB of ordinary ASCII prose.
func benchPrompt() string {
	const para = "Please review the attached quarterly planning notes and summarise the main risks for the platform team. " +
		"Focus on delivery dates, staffing assumptions and anything that depends on a third party vendor. "
	var b strings.Builder
	for b.Len() < 2048 {
		b.WriteString(para)
	}
	return b.String()[:2048]
}

// gatewayInput reproduces the gateway request contract: one scan with
// SkipSanitization, then the per-field rewrite when the scan chose one.
func gatewayInput(ctx context.Context, e *security.Engine, p security.Profile, text string) (string, *security.ScanResult, bool) {
	in, _ := security.StepsFromLegacyProfile(p)
	scan := e.Scan(ctx, &security.ScanRequest{
		Content:          text,
		Steps:            in,
		Role:             security.RoleUser,
		Canonicalize:     p.CanonicalizeEnabled,
		SkipSanitization: true,
		Suppressions:     p.Suppressions,
		Normalize:        normalize.Options{Unicode: p.NormalizeUnicode, Decode: p.NormalizeDecode},
	})
	if scan.ShouldBlock || !(scan.Action.IsMaskLike() || scan.Action == security.ActionTokenize) {
		return text, scan, true
	}
	var tm *security.TokenMap
	if scan.Action == security.ActionTokenize {
		tm = security.NewTokenMap(p.PIITokenStyle)
	}
	out, ok := e.SanitizeDecisions(text, in, scan.DetectorActions, tm, false)
	return out, scan, ok
}

func logOnlyProfile() security.Profile {
	p := security.DefaultProfile()
	p.InjectionStrategy = security.ActionLogOnly
	p.JailbreakStrategy = security.ActionLogOnly
	p.SecretsStrategy = security.ActionLogOnly
	p.IndirectInjectionStrategy = security.ActionLogOnly
	p.OutputExfilStrategy = security.ActionLogOnly
	p.PIIAction = security.ActionLogOnly
	return p
}

func BenchmarkGatewayInputRewriteVerify(b *testing.B) {
	e := benchEngine()
	ctx := context.Background()
	prompt := benchPrompt()
	key := syntheticOpenAIKey()
	withSecret := prompt[:1000] + " my key is " + key + " " + prompt[1000:2048-len(key)-12]
	obfuscated := prompt[:1000] + " my key is " + key[:12] + "\u200b" + key[12:] + " " + prompt[1000:2048-len(key)-15]
	cases := []struct {
		name    string
		profile security.Profile
		text    string
	}{
		{"default/clean_2KB", security.DefaultProfile(), prompt},
		{"default/one_secret_2KB", security.DefaultProfile(), withSecret},
		{"log_only/clean_2KB", logOnlyProfile(), prompt},
		{"log_only/obfuscated_secret_2KB", logOnlyProfile(), obfuscated},
		{"default/obfuscated_secret_2KB", security.DefaultProfile(), obfuscated},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				gatewayInput(ctx, e, c.profile, c.text)
			}
		})
	}
}
