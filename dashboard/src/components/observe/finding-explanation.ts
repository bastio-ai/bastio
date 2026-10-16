// Explain detector output without turning a pattern match into a confirmed
// incident, or inferring a historical retention policy from today's settings.
export type ExplainableFinding = {
  threat_type: string;
  detector_name: string;
  matched_pattern?: string;
  matched_content?: string;
  action_taken?: string;
  source?: string;
};

export type FindingTraceContext = {
  trace_name?: string;
  provider?: string;
  model?: string;
  environment?: string;
  tags?: Record<string, string>;
};

const identifiers: Record<string, { label: string; description: string }> = {
  ssn: { label: "Possible US Social Security number", description: "A number matched the format and basic validity rules of a US Social Security number. Ordinary nine-digit product or reference numbers can also match; this does not confirm personal data." },
  canadian_sin: { label: "Possible Canadian Social Insurance Number", description: "A nine-digit number matched the format and checksum of a Canadian Social Insurance Number. Other numeric identifiers can pass the same checks; context is needed to confirm personal data." },
  credit_card: { label: "Possible payment card number", description: "A number matched a payment card pattern and checksum. A checksum alone does not establish that it is a payment card." },
  email: { label: "Possible email address", description: "Text matched an email address pattern. It may be personal, a public business contact or an example address; review its context." },
  phone: { label: "Possible phone number", description: "Text matched a phone number pattern. Other numeric references can resemble phone numbers." },
  iban: { label: "Possible bank account identifier", description: "Text matched an IBAN format and checksum. Review the surrounding context to understand whose account it may identify." },
  danish_cpr: { label: "Possible Danish CPR number", description: "A number matched a Danish CPR format and date rules. This does not establish that it was assigned to a person." },
  cpr: { label: "Possible Danish CPR number", description: "A number matched a Danish CPR format and date rules. This does not establish that it was assigned to a person." },
  uk_nino: { label: "Possible UK National Insurance number", description: "Text matched a UK National Insurance number format. Review the original context before treating it as personal data." },
  eu_vat: { label: "Possible VAT identifier", description: "Text matched a national VAT identifier format. A company identifier is not necessarily personal data." },
  ip_address: { label: "Possible IP address", description: "Text matched an IP address pattern. Its privacy significance depends on whether it identifies a person in this context." },
};

export function findingExplanation(finding: ExplainableFinding, trace?: FindingTraceContext) {
  const pii = finding.threat_type === "pii" || finding.detector_name === "pii" || finding.detector_name === "privacy-filter";
  const identifier = finding.detector_name === "pii" ? identifiers[finding.matched_pattern || ""] : undefined;
  const title = identifier?.label || (pii ? "Possible personal data" : "Security finding to review");
  const description = identifier?.description || (pii
    ? "A detector flagged potentially sensitive information. A finding alone does not confirm personal data or a data leak; review the evidence and its context."
    : "A detector flagged this content for review. The finding alone does not establish a successful attack.");
  const evidence = finding.matched_content?.trim() ? undefined
    : trace?.tags?.["bastio.content_capture"] === "metadata_only"
      ? "Matched text was not retained because this call used metadata-only recording. Enabling recording now only affects future calls; this evidence cannot be recovered."
      : "No matched excerpt is available for this event. It may not have been captured or retained. This event did not record why; today's settings cannot recover missing evidence.";
  const action = finding.action_taken;
  const outcome = action === "log_only" ? "Logged only. This finding did not block or rewrite content."
    : action === "warn" ? "Warning recorded. This finding did not block content."
      : action === "block" ? "Content was blocked or withheld by policy. Review the trace for the affected stage."
        : action === "mask" || action === "tokenize" ? "Policy requested masking or tokenization. Review the trace for the resulting content."
          : "Review the trace for the effective outcome.";
  const stages: Record<string, string> = { input: "AI request", response: "AI response", tool: "Tool content", retrieval: "Retrieved content", memory: "Memory content" };
  const direction = stages[finding.source || ""];
  return { title, description, evidence, outcome, direction };
}
