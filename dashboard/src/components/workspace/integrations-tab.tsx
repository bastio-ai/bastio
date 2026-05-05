import { Mail, Calendar, Contact, FolderInput } from "lucide-react";

import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";

const PLANNED = [
  { icon: Mail, name: "Gmail / Outlook", desc: "Read inbox, draft replies" },
  { icon: Calendar, name: "Google / Outlook Calendar", desc: "Schedule + meeting context" },
  { icon: Contact, name: "Contacts", desc: "Find and reference people" },
  { icon: FolderInput, name: "Drive / Notion / Confluence", desc: "Pull docs into context" },
];

export function IntegrationsTab() {
  return (
    <div className="space-y-4">
      <div>
        <h3 className="text-sm font-semibold">Integrations</h3>
        <p className="mt-1 text-sm text-muted-foreground">
          Bring your tools into chat. Coming after the MVP — join the waitlist by reaching out.
        </p>
      </div>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        {PLANNED.map(({ icon: Icon, name, desc }) => (
          <Card key={name} className="opacity-70">
            <CardContent className="flex items-center gap-3 p-4">
              <Icon className="h-5 w-5 text-muted-foreground" />
              <div className="flex-1">
                <div className="flex items-center gap-2">
                  <h4 className="text-sm font-medium">{name}</h4>
                  <Badge variant="outline" className="text-[10px]">Coming soon</Badge>
                </div>
                <p className="text-xs text-muted-foreground">{desc}</p>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  );
}
