import { useEffect } from "react";
import { useNavigate } from "@tanstack/react-router";

/** Compatibility entry point for existing Connect buttons. All callers now
 * open the app-specific guided setup instead of disconnected sample snippets. */
export function ConnectCliDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const navigate = useNavigate();
  useEffect(() => {
    if (!open) return;
    onOpenChange(false);
    void navigate({ to: "/connect", search: {} });
  }, [open, onOpenChange, navigate]);
  return null;
}
