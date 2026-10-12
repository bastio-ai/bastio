export type EnforcementAction =
  | "block"
  | "mask"
  | "tokenize"
  | "warn"
  | "log_only";

export const ACTION_HELP: Record<
  EnforcementAction,
  { label: string; outcome: string; description: string }
> = {
  mask: {
    label: "Mask",
    outcome: "Hide detected values",
    description:
      "Replace detected personal data or secrets with masked text before forwarding it. Some masks keep parts of a value, such as an email domain or a card’s last four digits. The full value cannot be restored from the mask.",
  },
  tokenize: {
    label: "Tokenize",
    outcome: "Use placeholders",
    description:
      "Replace detected personal data with placeholders. The model sees placeholders; Bastio keeps a temporary mapping to the original values. Supported gateway responses can restore those values when response restoration is enabled.",
  },
  block: {
    label: "Block",
    outcome: "Stop matching content",
    description:
      "Reject matching requests before sending them to the model provider. Response protection depends on the route’s response inspection and delivery settings. Knowledge files are quarantined instead of indexed.",
  },
  warn: {
    label: "Warn",
    outcome: "Allow and flag",
    description:
      "Allow the content unchanged and record a warning for review. Personal data and secrets can still reach the model provider. This does not guarantee an employee sees a popup.",
  },
  log_only: {
    label: "Log only",
    outcome: "Allow and record",
    description:
      "Allow the content unchanged and record the detection for auditing. This monitors activity; it does not block or remove personal data or secrets.",
  },
};

export function ActionHelp({
  action,
  pii = false,
}: {
  action: EnforcementAction;
  pii?: boolean;
}) {
  const help = ACTION_HELP[action];
  return (
    <div className="mt-2 space-y-2 text-[11px] leading-relaxed text-muted-foreground">
      <p>{help.description}</p>
      {pii && (action === "mask" || action === "tokenize") && (
        <div className="rounded-md border border-border/60 bg-muted/20 p-2">
          <p className="text-[10px] font-medium text-foreground">
            Example email
          </p>
          <p className="mt-1 break-words font-mono text-[10px]">
            alice@example.test →{" "}
            {action === "mask" ? "a***@example.test" : "<PII_EMAIL_1>"}
          </p>
          <p className="mt-1">
            {action === "mask"
              ? "Masking is not a guarantee of anonymity."
              : "Knowledge sources keep processed placeholders without a restoration mapping. Original uploaded files are retained separately."}
          </p>
        </div>
      )}
    </div>
  );
}

export function EnforcementGuide() {
  return (
    <details className="rounded-lg border border-border/60 bg-card p-4">
      <summary className="cursor-pointer text-xs font-medium">
        What do the enforcement actions mean?
      </summary>
      <p className="mt-2 text-[11px] leading-relaxed text-muted-foreground">
        Choose what happens when an enabled detector finds matching content.
        Mask and Tokenize change what the model sees; Warn and Log only allow
        the original content through.
      </p>
      <dl className="mt-3 space-y-3">
        {Object.entries(ACTION_HELP).map(([value, help]) => (
          <div
            key={value}
            className="grid gap-1 sm:grid-cols-[120px_1fr] sm:gap-3"
          >
            <dt className="text-[11px] font-medium text-foreground">
              {help.label} · {help.outcome}
            </dt>
            <dd className="text-[11px] leading-relaxed text-muted-foreground">
              {help.description}
            </dd>
          </div>
        ))}
      </dl>
      <p className="mt-3 border-t border-border/60 pt-3 text-[11px] leading-relaxed text-muted-foreground">
        These actions control processing and forwarding. They do not delete
        previously stored conversations, original knowledge files or existing
        audit records. “Fail open” and “Fail closed” separately control what
        happens when security evaluation fails.
      </p>
    </details>
  );
}
