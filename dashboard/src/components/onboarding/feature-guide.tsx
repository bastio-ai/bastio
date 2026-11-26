import { useEffect, useRef } from "react";
import { Link } from "@tanstack/react-router";
import { ArrowLeft, ArrowRight, Check, ExternalLink } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useWorkspaceExtension } from "@/components/workspace/extension-context";
import { SetupNote } from "./setup-primitives";
import type { SetupDraft } from "./setup-model";

type TourStep = {
  title: string;
  description: string;
  points: string[];
  to:
    | "/security-settings"
    | "/playground"
    | "/threats"
    | "/traces"
    | "/sessions"
    | "/workspace"
    | "/chat"
    | "/api-keys";
  link: string;
};
const tours: Record<string, TourStep[]> = {
  documents: [
    {
      title: "Choose how personal information is handled",
      description:
        "Bastio Workspace is a place to chat with AI and attach documents. Its security policy determines what is masked or blocked before the model receives text.",
      points: [
        "Review personal-information detection and its action in Security Center. Recording a finding alone does not remove the information.",
        "Use masking or tokenization to replace detected personal information, or blocking to stop the message. Ask your workspace administrator if these settings are managed for you.",
        "This applies to conversations sent in Bastio Workspace. Your other Claude and Codex apps are separate connections.",
      ],
      to: "/security-settings",
      link: "Review personal-information protection",
    },
    {
      title: "Try a document in Workspace",
      description:
        "Start with a harmless sample so you can check the result before using a real document.",
      points: [
        "Open Workspace and choose an available model. If none is available, your administrator needs to finish Workspace setup.",
        "Attach a text file, DOCX, or PDF with selectable text containing made-up personal information, then ask the model to summarize it.",
        "Text extracted from the attachment is included in the message scan. Images and scanned PDFs do not currently receive automatic personal-information protection; do not use them for sensitive content.",
      ],
      to: "/chat",
      link: "Open Bastio Workspace",
    },
    {
      title: "Check what Bastio protected",
      description:
        "A successful upload or an AI reply does not by itself prove that personal information was removed.",
      points: [
        "Find the test conversation in Traces and inspect its security decision.",
        "Confirm that the intended information was masked, tokenized, or blocked. If the action only records a finding, review the policy before sharing sensitive documents.",
        "This tour records what you have reviewed. It does not turn on protection for applications outside Workspace.",
      ],
      to: "/traces",
      link: "Check the test interaction",
    },
  ],
  security: [
    {
      title: "Choose how findings are handled",
      description:
        "Start by reviewing the controls that apply to your workspace.",
      points: [
        "Open Security Center and review enabled detectors, thresholds, and actions.",
        "Check whether findings are recorded, warned, masked, or blocked. Detection alone does not mean a request was prevented.",
        "Choose actions appropriate to the traffic you route through Bastio.",
      ],
      to: "/security-settings",
      link: "Open Security Center",
    },
    {
      title: "Test before relying on a policy",
      description:
        "Use the playground to understand a detector's behavior with harmless synthetic content.",
      points: [
        "Use a made-up example rather than real secrets or customer information.",
        "Inspect the finding, confidence, and selected action.",
        "A playground result tests policy behavior. It does not prove your local application is connected; use a connection guide for that.",
      ],
      to: "/playground",
      link: "Open Security Playground",
    },
    {
      title: "Review real security decisions",
      description:
        "Once your app sends traffic, review what Bastio actually did.",
      points: [
        "Filter threats by severity, detector, and action.",
        "Open the associated trace to see the request outcome and security decision.",
        "Tune noisy detections and confirm intended enforcement on representative traffic.",
      ],
      to: "/threats",
      link: "Open Threats",
    },
  ],
  traces: [
    {
      title: "Find the interaction",
      description:
        "Traces are the starting point for understanding traffic through Bastio.",
      points: [
        "Select the environment and time window in the header.",
        "Filter by request type, model, provider, user, or security signal.",
        "An empty view may mean the time window is too narrow or your app is not connected yet.",
      ],
      to: "/traces",
      link: "Open Traces",
    },
    {
      title: "Inspect the security decision",
      description:
        "A request outcome and a security decision describe different things.",
      points: [
        "Select a trace and switch between request details and security information.",
        "Review whether content was blocked, masked, warned, or recorded only.",
        "Provider errors can occur without a security finding; an allowed request can still contain a recorded finding.",
      ],
      to: "/traces",
      link: "Inspect a trace",
    },
    {
      title: "Follow related activity",
      description: "Use sessions to understand a sequence of interactions.",
      points: [
        "Open a session to follow related requests and tool calls.",
        "Compare latency, token usage, cost, and security findings.",
        "Session grouping depends on the identifiers your integration sends.",
      ],
      to: "/sessions",
      link: "Open Sessions",
    },
  ],
  workspace: [
    {
      title: "Prepare your shared workspace",
      description: "Set up the space your colleagues will use.",
      points: [
        "Choose workspace models and connect the provider credentials they need.",
        "Review privacy, retention, and spending settings.",
        "Use the workspace's setup status to identify missing configuration.",
      ],
      to: "/workspace",
      link: "Open Workspace",
    },
    {
      title: "Give people appropriate access",
      description:
        "Review the team and resource permissions available in your deployment.",
      points: [
        "In Cloud, invite colleagues and choose roles appropriate to their responsibilities.",
        "Use groups and sharing settings to control access to assistants and resources.",
        "Self-hosted deployments may manage membership through their configured identity provider.",
      ],
      to: "/workspace",
      link: "Review workspace access",
    },
    {
      title: "Keep app access separate",
      description:
        "People and application integrations use different credentials.",
      points: [
        "Use scoped API keys for gateway integrations instead of sharing a person's login.",
        "Assign an environment and review which gateway each key can access.",
        "Guide colleagues through their own app connection, then verify actual traffic.",
      ],
      to: "/api-keys",
      link: "Review API Keys",
    },
  ],
};
export function FeatureGuide({
  draft,
  update,
  onExit,
}: {
  draft: SetupDraft;
  update: (patch: Partial<SetupDraft>) => void;
  onExit: () => void;
}) {
  const { openWorkspaceURL } = useWorkspaceExtension();
  const steps = tours[draft.guide] ?? [];
  const finished = draft.step >= steps.length;
  const current = steps[Math.min(draft.step, steps.length - 1)];
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    heading.current?.focus();
  }, [draft.step]);
  return (
    <div className="grid gap-8 lg:grid-cols-[210px_minmax(0,1fr)] lg:gap-10">
      <ol aria-label="Feature tour steps" className="space-y-3">
        {steps.map((step, index) => (
          <li key={step.title} className="flex items-center gap-3 text-[12px]">
            <span className="flex size-5 shrink-0 items-center justify-center rounded-full border border-border-default text-[10px]">
              {index < draft.step ? <Check className="size-3" /> : index + 1}
            </span>
            <span
              className={
                index === draft.step
                  ? "text-foreground"
                  : "text-muted-foreground"
              }
            >
              {step.title}
            </span>
          </li>
        ))}
      </ol>
      <div className="max-w-[700px] space-y-6">
        <p className="text-[10px] text-muted-foreground">
          Guided tour ·{" "}
          {finished ? "Reviewed" : `Step ${draft.step + 1} of ${steps.length}`}
        </p>
        <h2
          tabIndex={-1}
          ref={heading}
          className="text-[21px] font-medium tracking-tight outline-none"
        >
          {finished ? "You're ready to explore" : current?.title}
        </h2>
        {finished ? (
          <SetupNote title="Tour reviewed">
            This records your progress through the guide. Settings and policies
            change only when you save them in their respective feature pages.
          </SetupNote>
        ) : (
          <>
            <p className="text-[12px] leading-relaxed text-muted-foreground">
              {current?.description}
            </p>
            <ol className="list-decimal space-y-4 pl-4 text-[13px] leading-relaxed">
              {current?.points.map((point) => (
                <li key={point}>{point}</li>
              ))}
            </ol>
            {current?.to === "/chat" && openWorkspaceURL ? (
              <a
                href={openWorkspaceURL}
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex items-center gap-2 border-b border-foreground/30 pb-1 text-[12px]"
              >
                {current.link}
                <ExternalLink className="size-3" />
              </a>
            ) : current ? (
              <Link
                to={current.to}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-2 border-b border-foreground/30 pb-1 text-[12px]"
              >
                {current.link}
                <ExternalLink className="size-3" />
              </Link>
            ) : null}
            <p className="text-[11px] text-muted-foreground">
              The feature opens in a new tab so you can keep this guide beside
              it. Return here when you've reviewed this step.
            </p>
          </>
        )}
        <footer className="flex flex-wrap items-center justify-between gap-3 border-t border-border-subtle pt-5">
          <Button
            size="sm"
            variant="ghost"
            onClick={
              draft.step === 0 ? onExit : () => update({ step: draft.step - 1 })
            }
          >
            <ArrowLeft className="size-3" />
            {draft.step === 0 ? "All guides" : "Back"}
          </Button>
          <div className="flex gap-2">
            {!finished ? (
              <Button variant="ghost" size="sm" onClick={onExit}>
                Finish later
              </Button>
            ) : null}
            <Button
              size="sm"
              onClick={() =>
                finished ? onExit() : update({ step: draft.step + 1 })
              }
            >
              {finished ? "Back to setup hub" : "I've reviewed this step"}
              <ArrowRight className="size-3" />
            </Button>
          </div>
        </footer>
      </div>
    </div>
  );
}
