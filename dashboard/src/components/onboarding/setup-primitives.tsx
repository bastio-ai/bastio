import { useState, type ReactNode } from "react";
import { Check, Copy, ExternalLink } from "lucide-react";
import { Button } from "@/components/ui/button";

export const setupControl =
  "h-9 w-full rounded-md border border-border-default bg-background px-3 text-[12px] outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50";
export function SetupField({
  label,
  htmlFor,
  hint,
  children,
}: {
  label: string;
  htmlFor?: string;
  hint?: string;
  children: ReactNode;
}) {
  return (
    <div className="min-w-0">
      <label
        htmlFor={htmlFor}
        className="mb-2 block text-[12px] text-foreground"
      >
        {label}
      </label>
      {children}
      {hint ? (
        <p className="mt-2 text-[11px] leading-relaxed text-muted-foreground">
          {hint}
        </p>
      ) : null}
    </div>
  );
}
export function SetupNote({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}) {
  return (
    <div className="border-l-2 border-border-default pl-3">
      <p className="text-[12px] font-medium">{title}</p>
      <div className="mt-1.5 text-[11px] leading-relaxed text-muted-foreground">
        {children}
      </div>
    </div>
  );
}
export function SetupError({
  error,
  retry,
}: {
  error: unknown;
  retry?: () => void;
}) {
  if (!error) return null;
  return (
    <div role="alert" className="text-[12px] text-danger">
      <p>
        {error instanceof Error
          ? error.message
          : "Something went wrong. Please try again."}
      </p>
      {retry ? (
        <button
          type="button"
          onClick={retry}
          className="mt-2 underline underline-offset-4"
        >
          Try again
        </button>
      ) : null}
    </div>
  );
}
export function CopySetup({
  value,
  label = "Copy",
}: {
  value: string;
  label?: string;
}) {
  const [state, setState] = useState<"idle" | "copied" | "error">("idle");
  return (
    <div>
      <Button
        type="button"
        variant="ghost"
        size="sm"
        onClick={async () => {
          try {
            await navigator.clipboard.writeText(value);
            setState("copied");
            window.setTimeout(() => setState("idle"), 2000);
          } catch {
            setState("error");
          }
        }}
      >
        {state === "copied" ? (
          <Check className="size-3" />
        ) : (
          <Copy className="size-3" />
        )}
        {state === "copied" ? "Copied" : label}
      </Button>
      <span role="status" className="sr-only">
        {state === "copied" ? `${label} copied to clipboard` : ""}
      </span>
      {state === "error" ? (
        <p role="alert" className="text-[11px] text-danger">
          Select the text and copy it manually.
        </p>
      ) : null}
    </div>
  );
}
export function SetupCode({
  file,
  code,
  language,
}: {
  file: string;
  code: string;
  language: string;
}) {
  return (
    <div className="min-w-0 overflow-hidden rounded-md border border-border-subtle">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border-subtle px-3 py-1">
        <span className="break-all font-mono text-[10px] text-muted-foreground">
          {file}
        </span>
        <CopySetup value={code} label={`Copy ${language}`} />
      </div>
      <pre
        tabIndex={0}
        aria-label={`${language} configuration`}
        className="max-h-[380px] overflow-auto bg-surface-1 p-4 font-mono text-[11px] leading-6 text-foreground"
      >
        <code>{code}</code>
      </pre>
    </div>
  );
}
export function SetupDocs({
  href,
  children,
}: {
  href: string;
  children: ReactNode;
}) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer"
      className="inline-flex items-center gap-1.5 text-[11px] text-muted-foreground hover:text-foreground"
    >
      {children}
      <ExternalLink className="size-3" aria-hidden />
    </a>
  );
}
