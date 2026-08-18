import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ShieldOff } from "lucide-react";

import { api } from "@/api/client";
import type { ThreatEvent } from "@/api/client";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

function patternFromThreat(threat: ThreatEvent): string {
  return (threat.matched_pattern || threat.threat_subtype || threat.matched_content || "")
    .trim()
    .slice(0, 256);
}

export function AllowPatternAction({
  threat,
  className,
}: {
  threat: ThreatEvent;
  className?: string;
}) {
  const queryClient = useQueryClient();
  const detector = (threat.detector_name || "").trim();
  const pattern = patternFromThreat(threat);

  const profiles = useQuery({
    queryKey: ["security-profiles"],
    queryFn: api.security.profiles,
  });
  const profileId = profiles.data?.[0]?.id;

  const listed = useQuery({
    queryKey: ["security-suppressions", profileId],
    queryFn: () => api.security.listSuppressions(profileId ?? ""),
    enabled: Boolean(profileId),
  });

  const match = listed.data?.find(
    (row) =>
      row.detector.toLowerCase() === detector.toLowerCase() &&
      row.pattern.toLowerCase() === pattern.toLowerCase(),
  );

  const allow = useMutation({
    mutationFn: () => {
      if (!profileId) {
        return Promise.reject(new Error("no security profile"));
      }
      return api.security.createSuppression(profileId, { detector, pattern });
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["security-suppressions"] });
      void queryClient.invalidateQueries({ queryKey: ["security-patterns"] });
    },
  });

  const revoke = useMutation({
    mutationFn: () => {
      if (!profileId || !match?.id) {
        return Promise.reject(new Error("no suppression to remove"));
      }
      return api.security.deleteSuppression(profileId, match.id);
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["security-suppressions"] });
    },
  });

  const busy = allow.isPending || revoke.isPending;
  const disabled = !profileId || !detector || !pattern || busy;
  const label = match ? "Allowed" : "Allow this pattern";
  const hint = !pattern
    ? "This event has no pattern to allow"
    : match
      ? "Click to start flagging this pattern again"
      : "Skip this detector pattern on future requests";

  return (
    <Button
      variant="outline"
      size="sm"
      className={cn("h-8 text-xs", className)}
      disabled={disabled}
      title={hint}
      onClick={() => (match ? revoke.mutate() : allow.mutate())}
    >
      <ShieldOff className="h-3 w-3" /> {label}
    </Button>
  );
}
