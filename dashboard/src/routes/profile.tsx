import { KeyRound, ServerCog, ShieldCheck } from "lucide-react";

import { SecurityNotice } from "@/components/admin/admin-primitives";
import { PageHeader, SectionHeader } from "@/components/card";
import { useProfileExtension } from "@/components/profile-extension";
import { Card, CardContent } from "@/components/ui/card";

export function ProfilePage() {
  const extension = useProfileExtension();
  if (extension.profileContent) return <>{extension.profileContent}</>;

  return (
    <div className="space-y-5">
      <PageHeader
        title="Account & Security"
        description="Identity for this dashboard is managed by the deployment's configured authentication provider."
      />

      <SecurityNotice title="Local deployment identity" tone="info">
        This open-source dashboard does not store a separate editable profile. Change identity, password, and multi-factor settings in the authentication provider configured by your operator.
      </SecurityNotice>

      <section className="max-w-3xl">
        <SectionHeader
          title="Authentication posture"
          description="Controls available for this deployment."
        />
        <Card className="border-border/70">
          <CardContent className="divide-y divide-border/60 p-0">
            <PostureRow
              icon={ServerCog}
              title="Identity source"
              description="Deployment-managed authentication"
              value="External"
            />
            <PostureRow
              icon={KeyRound}
              title="Credentials"
              description="Passwords and recovery are not stored in this dashboard"
              value="Provider managed"
            />
            <PostureRow
              icon={ShieldCheck}
              title="Multi-factor authentication"
              description="Availability depends on the configured identity provider"
              value="Provider managed"
            />
          </CardContent>
        </Card>
      </section>
    </div>
  );
}

function PostureRow({
  icon: Icon,
  title,
  description,
  value,
}: {
  icon: typeof ShieldCheck;
  title: string;
  description: string;
  value: string;
}) {
  return (
    <div className="flex items-center gap-3 p-4">
      <span className="flex size-8 shrink-0 items-center justify-center rounded-lg border border-border/60 bg-muted/30 text-muted-foreground">
        <Icon className="size-3.5" aria-hidden />
      </span>
      <div className="min-w-0 flex-1">
        <p className="text-[12px] font-medium text-foreground">{title}</p>
        <p className="mt-0.5 text-[11px] text-muted-foreground">{description}</p>
      </div>
      <span className="font-mono text-[11px] text-muted-foreground">{value}</span>
    </div>
  );
}
