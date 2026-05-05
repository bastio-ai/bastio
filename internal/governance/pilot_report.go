package governance

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"html/template"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"
)

// PilotReport is the structured output for the Shadow AI Audit deliverable.
// Generated 14 days after the first event arrives for an org. Same data
// powers both the interactive HTML page and the downloadable PDF — sales
// uses the HTML for live demos, prospects share the PDF internally.
type PilotReport struct {
	GeneratedAt    time.Time         `json:"generated_at"`
	WindowDays     int               `json:"window_days"`
	OrgLabel       string            `json:"org_label"`
	TotalEvents    int64             `json:"total_events"`
	UniqueUsers    int64             `json:"unique_users"`
	UniqueDomains  int64             `json:"unique_domains"`
	HighSeverity   int64             `json:"high_severity"`
	MediumSeverity int64             `json:"medium_severity"`
	BlockedCount   int64             `json:"blocked_count"`
	OverriddenCount int64            `json:"overridden_count"`
	RedirectedCount int64            `json:"redirected_count"`
	TopDomains     []DomainCount     `json:"top_domains"`
	TopRules       []RuleCount       `json:"top_rules"`
	ByDepartment   []DepartmentCount `json:"by_department,omitempty"`
	SampleEvents   []EventRow        `json:"sample_events"`
	ComplianceMap  []ArticleMap      `json:"compliance_map"`
}

// DepartmentCount aggregates events per SCIM group ("department"). Empty
// when SCIM hasn't been wired up by the customer's IdP.
type DepartmentCount struct {
	Department string `json:"department"`
	Count      int64  `json:"count"`
}

// ArticleMap shows how Bastio addresses specific EU AI Act / GDPR articles.
// Sales asset — the prospect can show this to their DPO or legal team.
type ArticleMap struct {
	Article  string `json:"article"`
	Title    string `json:"title"`
	Coverage string `json:"coverage"`
}

func defaultComplianceMap() []ArticleMap {
	return []ArticleMap{
		{
			Article:  "EU AI Act Art. 14",
			Title:    "Human oversight",
			Coverage: "Bastio Workspace logs every model interaction with full reviewer access. Governance enforces block + IT-controlled override.",
		},
		{
			Article:  "EU AI Act Art. 26",
			Title:    "Deployer obligations",
			Coverage: "Per-employee logs across Workspace and Governance. Compliance-grade audit log surface.",
		},
		{
			Article:  "EU AI Act Art. 50",
			Title:    "Transparency to users",
			Coverage: "Governance pop-ups disclose policy reasoning to employees in plain language.",
		},
		{
			Article:  "GDPR Art. 30",
			Title:    "Records of processing",
			Coverage: "Article 30-compatible records on every Workspace, Governance, and Gateway interaction. Exportable as DPO-ready report.",
		},
		{
			Article:  "GDPR Art. 32",
			Title:    "Security of processing",
			Coverage: "Pseudonymization toggle, EU residency by default, FSL-1.1-ALv2 open-source detection rules — auditable end-to-end.",
		},
	}
}

func (h *Handler) buildPilotReport(ctx context.Context, customerID uuid.UUID) (*PilotReport, error) {
	overview, err := h.events.Overview(ctx, customerID, 14)
	if err != nil {
		return nil, err
	}
	samples, err := h.events.RecentEvents(ctx, customerID, 25, "", "")
	if err != nil {
		return nil, err
	}

	// Pseudonymize sample events server-side when the policy says so. The
	// pilot report is the artifact that leaves our system and lands in a
	// prospect's DPO inbox — defense in depth: never trust the client.
	pseudonymize := false
	if pol, err := h.policies.Get(ctx, customerID); err == nil && pol != nil {
		pseudonymize = pol.PseudonymizePII
	}
	if pseudonymize {
		for i := range samples {
			samples[i].UserID = pseudonymizeID(samples[i].UserID)
		}
	}

	// SCIM-driven department breakdown. Empty when the customer hasn't
	// wired their IdP yet — the dashboard renders the report fine without it.
	var byDept []DepartmentCount
	if h.scim != nil {
		userToDept, _ := h.scim.UserGroupBreakdown(ctx, customerID)
		if len(userToDept) > 0 {
			byDept = h.events.aggregateByDepartment(ctx, customerID, 14, userToDept)
		}
	}

	report := &PilotReport{
		GeneratedAt:    time.Now().UTC(),
		WindowDays:     14,
		OrgLabel:       fmt.Sprintf("Customer %s", strings.Split(customerID.String(), "-")[0]),
		TotalEvents:    overview.TotalEvents,
		UniqueUsers:    overview.UniqueUsers,
		UniqueDomains:  overview.UniqueDomains,
		HighSeverity:   overview.BySeverity["high"],
		MediumSeverity: overview.BySeverity["medium"],
		BlockedCount:   overview.ByAction["blocked"],
		OverriddenCount: overview.ByAction["overridden"],
		RedirectedCount: overview.ByAction["redirected"],
		TopDomains:     overview.TopDomains,
		TopRules:       overview.TopRules,
		ByDepartment:   byDept,
		SampleEvents:   samples,
		ComplianceMap:  defaultComplianceMap(),
	}
	return report, nil
}

// pseudonymizeID maps an arbitrary identifier to a stable "Employee #NNNN"
// label. Same input always returns the same output (so leaderboards still
// group correctly), but the actual identifier never reaches the rendered
// PDF / HTML / dashboard view.
//
// Hash is a non-crypto FNV-1a → mod 10000. Strength isn't the goal; we just
// want a stable surrogate. Four digits is enough for most enterprise pilots
// (20-2000 employees); collisions only matter for rendering, not auth.
func pseudonymizeID(raw string) string {
	if raw == "" {
		return "Employee #0000"
	}
	const offset uint32 = 2166136261
	const prime uint32 = 16777619
	h := offset
	for i := 0; i < len(raw); i++ {
		h ^= uint32(raw[i])
		h *= prime
	}
	return fmt.Sprintf("Employee #%04d", h%10000)
}

// renderPilotReportHTML produces a self-contained HTML doc suitable for
// printing to PDF via a headless Chromium pipeline. No external assets —
// fonts inlined as system fallback, all CSS embedded.
func renderPilotReportHTML(r *PilotReport) ([]byte, error) {
	tmpl, err := template.New("pilot").Parse(pilotReportTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, r); err != nil {
		return nil, fmt.Errorf("execute template: %w", err)
	}
	return buf.Bytes(), nil
}

// renderPilotReportPDF prints the HTML to PDF. Tries headless Chromium via
// system binary first (production); falls back to a minimal PDF wrapper
// containing the HTML as an attachment for environments without Chromium.
//
// In production cloud, this should run in a sidecar container with chromium
// already installed. Since we can't guarantee that in OSS, the fallback
// gives operators a downloadable artifact even if PDF rendering isn't
// available.
func renderPilotReportPDF(r *PilotReport) ([]byte, error) {
	htmlBytes, err := renderPilotReportHTML(r)
	if err != nil {
		return nil, err
	}

	if pdf, err := chromiumPrintToPDF(htmlBytes); err == nil {
		return pdf, nil
	}

	// Fallback: minimal valid PDF that says "see HTML version" + base64
	// encodes the HTML. Prospects can save the HTML version from the
	// dashboard if their server doesn't have chromium.
	return fallbackPDF(htmlBytes), nil
}

// chromiumPrintToPDF spawns a headless chromium process to print the HTML
// to PDF. Looks for `chromium`, `chromium-browser`, `google-chrome`, and
// `google-chrome-stable` on PATH. Times out after 30s.
func chromiumPrintToPDF(html []byte) ([]byte, error) {
	chromium := findChromium()
	if chromium == "" {
		return nil, fmt.Errorf("chromium binary not found on PATH")
	}

	tmp, err := writeTempHTML(html)
	if err != nil {
		return nil, err
	}
	defer cleanupTemp(tmp)

	outFile := tmp + ".pdf"
	defer cleanupTemp(outFile)

	cmd := exec.Command(chromium,
		"--headless=new",
		"--no-sandbox",
		"--disable-gpu",
		"--no-pdf-header-footer",
		"--print-to-pdf=" + outFile,
		"--print-to-pdf-no-header",
		"file://"+tmp,
	)
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("chromium print-to-pdf: %w", err)
	}
	return readFile(outFile)
}

func findChromium() string {
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "chrome"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

func fallbackPDF(html []byte) []byte {
	// Minimum valid PDF with a single page noting fallback + HTML attached
	// as an embedded base64 string in metadata. Customers without chromium
	// can save the /pilot-report HTML view directly.
	encoded := base64.StdEncoding.EncodeToString(html)
	return []byte(fmt.Sprintf(`%%PDF-1.4
1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj
2 0 obj << /Type /Pages /Kids [3 0 R] /Count 1 >> endobj
3 0 obj << /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >> endobj
4 0 obj << /Length 220 >> stream
BT /F1 14 Tf 60 780 Td (Bastio Shadow AI Audit) Tj 0 -28 Td /F1 10 Tf
(PDF rendering requires headless Chromium on the bastio host.) Tj 0 -16 Td
(Save the HTML report from the dashboard /governance/pilot-report) Tj 0 -16 Td
(or install chromium on the host running bastio.) Tj ET
endstream endobj
5 0 obj << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> endobj
6 0 obj << /HTMLReport <%s> >> endobj
xref
0 7
0000000000 65535 f
0000000010 00000 n
0000000060 00000 n
0000000115 00000 n
0000000220 00000 n
0000000490 00000 n
0000000560 00000 n
trailer << /Size 7 /Root 1 0 R >>
startxref
700
%%%%EOF
`, encoded))
}

const pilotReportTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>Bastio Shadow AI Audit — {{.OrgLabel}}</title>
<style>
  * { box-sizing: border-box; }
  body { font-family: -apple-system, BlinkMacSystemFont, "Inter", "Segoe UI", sans-serif; color: #0d1117; margin: 0; padding: 56px 64px; background: #fff; }
  h1 { font-size: 32px; letter-spacing: -0.025em; margin: 0 0 4px; }
  h2 { font-size: 20px; letter-spacing: -0.02em; margin: 36px 0 16px; padding-bottom: 6px; border-bottom: 1px solid #d0d7de; }
  h3 { font-size: 14px; text-transform: uppercase; letter-spacing: 0.04em; color: #57606a; margin: 24px 0 8px; }
  .header { display: flex; justify-content: space-between; align-items: flex-end; border-bottom: 2px solid #0d1117; padding-bottom: 16px; }
  .brand { font-family: ui-monospace, "JetBrains Mono", "SF Mono", monospace; font-weight: 600; color: #2ee5d8; letter-spacing: -0.01em; }
  .meta { color: #57606a; font-size: 12px; text-align: right; }
  .kpi-row { display: grid; grid-template-columns: repeat(4, 1fr); gap: 16px; margin: 24px 0; }
  .kpi { border: 1px solid #d0d7de; border-radius: 8px; padding: 16px; }
  .kpi .label { font-size: 11px; text-transform: uppercase; letter-spacing: 0.05em; color: #57606a; }
  .kpi .value { font-size: 28px; font-weight: 600; margin-top: 4px; letter-spacing: -0.02em; }
  .kpi.danger .value { color: #cf222e; }
  .kpi.warn .value { color: #9a6700; }
  table { width: 100%; border-collapse: collapse; font-size: 13px; }
  th, td { text-align: left; padding: 8px 10px; border-bottom: 1px solid #eaeef2; }
  th { font-size: 11px; text-transform: uppercase; letter-spacing: 0.05em; color: #57606a; font-weight: 600; }
  td.mono, .mono { font-family: ui-monospace, "JetBrains Mono", "SF Mono", monospace; font-size: 12px; }
  .badge { display: inline-block; padding: 2px 8px; border-radius: 999px; font-size: 11px; font-weight: 500; }
  .badge.high { background: #ffebe9; color: #cf222e; }
  .badge.medium { background: #fff8c5; color: #9a6700; }
  .badge.low { background: #ddf4ff; color: #0969da; }
  .footer { margin-top: 60px; padding-top: 16px; border-top: 1px solid #d0d7de; color: #57606a; font-size: 11px; display: flex; justify-content: space-between; }
  ul { margin: 0; padding-left: 18px; line-height: 22px; }
  .compliance { display: grid; gap: 8px; }
  .compliance .row { border: 1px solid #eaeef2; border-radius: 6px; padding: 12px 14px; }
  .compliance .article { font-family: ui-monospace, "JetBrains Mono", "SF Mono", monospace; font-size: 11px; color: #2ee5d8; font-weight: 600; }
  .compliance .title { font-size: 14px; font-weight: 600; margin-top: 2px; }
  .compliance .coverage { font-size: 12px; color: #57606a; margin-top: 6px; line-height: 18px; }
  @page { size: A4; margin: 18mm 14mm; }
</style>
</head>
<body>
  <div class="header">
    <div>
      <h1>Shadow AI Audit Report</h1>
      <div class="mono">{{.OrgLabel}}</div>
    </div>
    <div class="meta">
      <div><span class="brand">bastio</span> · governance v1</div>
      <div>14-day window through {{.GeneratedAt.Format "Jan 2, 2006"}}</div>
    </div>
  </div>

  <h2>Executive summary</h2>
  <p>Across the past 14 days, your employees attempted to share data with public AI tools <strong>{{.TotalEvents}}</strong> times. <strong>{{.HighSeverity}}</strong> of those events involved high-severity content (PII, secrets, or code). <strong>{{.UniqueUsers}}</strong> distinct users touched <strong>{{.UniqueDomains}}</strong> different AI tools.</p>

  <div class="kpi-row">
    <div class="kpi"><div class="label">Total events</div><div class="value">{{.TotalEvents}}</div></div>
    <div class="kpi danger"><div class="label">High severity</div><div class="value">{{.HighSeverity}}</div></div>
    <div class="kpi"><div class="label">Blocked</div><div class="value">{{.BlockedCount}}</div></div>
    <div class="kpi warn"><div class="label">Overridden</div><div class="value">{{.OverriddenCount}}</div></div>
  </div>

  <h2>Top tools used</h2>
  <table>
    <thead><tr><th>Tool</th><th class="mono">Events</th></tr></thead>
    <tbody>
      {{range .TopDomains}}<tr><td class="mono">{{.Domain}}</td><td class="mono">{{.Count}}</td></tr>{{end}}
    </tbody>
  </table>

  <h2>Top rules fired</h2>
  <table>
    <thead><tr><th>Rule</th><th class="mono">Hits</th></tr></thead>
    <tbody>
      {{range .TopRules}}<tr><td class="mono">{{.RuleID}}</td><td class="mono">{{.Count}}</td></tr>{{end}}
    </tbody>
  </table>

  <h2>Sample anonymized incidents</h2>
  <table>
    <thead><tr><th>When</th><th>User</th><th>Tool</th><th>Severity</th><th>Action</th></tr></thead>
    <tbody>
      {{range .SampleEvents}}
      <tr>
        <td class="mono">{{.OccurredAt.Format "Jan 2 15:04"}}</td>
        <td class="mono">{{.UserID}}</td>
        <td class="mono">{{.SourceDomain}}</td>
        <td><span class="badge {{.Severity}}">{{.Severity}}</span></td>
        <td class="mono">{{.Action}}</td>
      </tr>
      {{end}}
    </tbody>
  </table>

  <h2>Compliance coverage</h2>
  <p>How Bastio Governance maps to your AI Act and GDPR obligations:</p>
  <div class="compliance">
    {{range .ComplianceMap}}
    <div class="row">
      <div class="article">{{.Article}}</div>
      <div class="title">{{.Title}}</div>
      <div class="coverage">{{.Coverage}}</div>
    </div>
    {{end}}
  </div>

  <h2>Recommended next step</h2>
  <p>Deploy <strong>Bastio Workspace</strong> as the secure-redirect destination so the {{.RedirectedCount}} redirects (and rising) carry through to a policy-enforced workspace with zero data retention. Talk to your account team for a Workspace seat estimate.</p>

  <div class="footer">
    <span>Generated by Bastio Governance · {{.GeneratedAt.Format "2006-01-02 15:04 UTC"}}</span>
    <span>bastio.com · FSL-1.1-ALv2 OSS · proprietary cloud</span>
  </div>
</body>
</html>
`
