// An observed detector signal is distinct from the action a policy enforced.
// Missing actions on older records stay unknown; never infer a block from severity.
export function securityDecision(action?: string) {
  switch (action) {
    case "block": return { label: "Blocked", tone: "danger", explanation: "The security policy blocked this content." } as const;
    case "mask": return { label: "Masked", tone: "success", explanation: "Detected values were replaced before forwarding." } as const;
    case "tokenize": return { label: "Tokenized", tone: "success", explanation: "Detected values were replaced with placeholders before forwarding." } as const;
    case "warn": return { label: "Warning", tone: "warning", explanation: "The policy allowed the content and recorded a warning." } as const;
    case "log":
    case "log_only": return { label: "Recorded only", tone: "neutral", explanation: "The policy recorded this signal without changing or blocking content." } as const;
    case "pass": return { label: "Observed · no enforcement", tone: "neutral", explanation: "This signal did not trigger a policy action. A detector label or score alone does not confirm an attack." } as const;
    default: return { label: "Action not recorded", tone: "neutral", explanation: "This record does not specify the policy action." } as const;
  }
}

export function hasActionableFindings(findings: { action_taken?: string }[]) {
  return findings.some(({ action_taken }) => ["block", "mask", "tokenize", "warn"].includes(action_taken ?? ""));
}

export function onlyPassiveFindings(findings: { action_taken?: string }[]) {
  return findings.length > 0 && findings.every(({ action_taken }) => ["pass", "log", "log_only"].includes(action_taken ?? ""));
}
