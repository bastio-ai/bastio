import { Sparkles, BookOpen, Bot, Send } from "lucide-react";

// OnboardingCard is the first-impression surface a brand-new
// workspace employee sees. The criteria for showing this (vs the
// returning-user empty state) is conversations.length === 0 — i.e.
// no chat has ever happened on this account.
//
// Employee-shaped: the user is the operator's team member, not the
// operator themselves. The cards explain what they have access to
// (assistants their admin curated, knowledge their admin uploaded)
// without exposing admin surfaces — admins manage that on
// bastio.com/workspace, not here.
export function OnboardingCard({
  onSendFirstMessage,
}: {
  onSendFirstMessage: () => void;
}) {
  return (
    <div className="mx-auto flex h-full max-w-2xl flex-col items-center justify-center gap-8 px-6 text-center">
      <div className="space-y-3">
        <div className="inline-flex items-center gap-2 rounded-full border border-cyan-500/30 bg-cyan-500/5 px-3 py-1 text-xs text-cyan-500">
          <Sparkles className="h-3 w-3" />
          Workspace is ready
        </div>
        <h1 className="text-3xl font-semibold tracking-tight">
          Welcome to your AI workspace.
        </h1>
        <p className="mx-auto max-w-md text-sm text-muted-foreground">
          Multi-model chat with your company's knowledge inline. Zero
          retention by default. Audited and policy-enforced, with the same speed
          as the public chatbots, but the data stays inside your
          tenant's perimeter.
        </p>
      </div>

      <div className="grid w-full grid-cols-1 gap-3 sm:grid-cols-3">
        <ActionCard
          icon={<Send className="h-4 w-4" />}
          title="Send your first message"
          body="Pick a model up top, type below, hit Cmd+Enter."
          onClick={onSendFirstMessage}
        />
        <InfoCard
          icon={<Bot className="h-4 w-4" />}
          title="Assistants are pre-configured"
          body="Your admin curated team-specific personas with system prompts and default models."
        />
        <InfoCard
          icon={<BookOpen className="h-4 w-4" />}
          title="Knowledge runs automatically"
          body="Your company's docs are linked to assistants — relevant chunks cite themselves in answers."
        />
      </div>

      <p className="text-[11px] uppercase tracking-wide text-muted-foreground">
        EU-resident · zero retention default · audit log on by default
      </p>
    </div>
  );
}

function ActionCard({
  icon,
  title,
  body,
  onClick,
}: {
  icon: React.ReactNode;
  title: string;
  body: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="group flex flex-col gap-2 rounded-lg border border-cyan-500/40 bg-cyan-500/5 p-4 text-left transition hover:border-cyan-500/70 hover:bg-cyan-500/10"
    >
      <div className="flex items-center gap-2 text-cyan-500">
        {icon}
        <span className="text-sm font-medium">{title}</span>
      </div>
      <p className="text-xs leading-relaxed text-muted-foreground">{body}</p>
    </button>
  );
}

// InfoCard is the non-actionable variant — explains a feature without
// a click target. Used to set expectations about admin-curated
// surfaces (assistants, knowledge bases) the employee doesn't manage.
function InfoCard({
  icon,
  title,
  body,
}: {
  icon: React.ReactNode;
  title: string;
  body: string;
}) {
  return (
    <div className="flex flex-col gap-2 rounded-lg border border-border bg-background p-4 text-left">
      <div className="flex items-center gap-2 text-foreground">
        {icon}
        <span className="text-sm font-medium">{title}</span>
      </div>
      <p className="text-xs leading-relaxed text-muted-foreground">{body}</p>
    </div>
  );
}
