import { useId, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { FieldLabel } from "@/components/admin/admin-primitives";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { api, type Environment } from "@/api/client";
import { controlClass, ErrorMessage, QueryError } from "./primitives";
export function EnvironmentPicker({
  value,
  onChange,
}: {
  value: string;
  onChange: (value: string) => void;
}) {
  const environmentQuery = useQuery({
    queryKey: ["environments"],
    queryFn: api.environments.list,
  });
  const environments = environmentQuery.data ?? [];
  const pending = environmentQuery.isPending;
  const error = environmentQuery.error;
  const retry = () => environmentQuery.refetch();
  const id = useId();
  const qc = useQueryClient();
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [kind, setKind] = useState<Environment["kind"]>("development");
  const create = useMutation({
    mutationFn: api.environments.create,
    onSuccess: async (environment) => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: ["environments"] }),
        qc.invalidateQueries({
          queryKey: ["dashboard-controls", "managed-environments"],
        }),
      ]);
      if (environment) onChange(environment.name);
      setCreating(false);
      setName("");
    },
  });
  return (
    <div className="space-y-2">
      <FieldLabel htmlFor={`${id}-select`}>Environment</FieldLabel>
      <select
        id={`${id}-select`}
        required
        className={controlClass}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      >
        <option value="" disabled>
          Select an environment
        </option>
        {environments.map((env) => (
          <option key={env.id} value={env.name}>
            {env.name} — {env.kind}
          </option>
        ))}
      </select>
      <p className="text-xs text-muted-foreground">
        Requests are tracked in this environment automatically.
      </p>
      {error ? (
        <QueryError
          title="Environments could not be loaded"
          error={error}
          retry={retry}
        />
      ) : pending ? (
        <p className="text-xs text-muted-foreground">Loading environments…</p>
      ) : creating ? (
        <div className="space-y-3 rounded-lg border border-border bg-muted/30 p-3">
          <div>
            <FieldLabel htmlFor={`${id}-name`}>New environment name</FieldLabel>
            <Input
              id={`${id}-name`}
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. production"
            />
          </div>
          <div>
            <FieldLabel htmlFor={`${id}-kind`}>Environment type</FieldLabel>
            <select
              id={`${id}-kind`}
              className={controlClass}
              value={kind}
              onChange={(e) => setKind(e.target.value as Environment["kind"])}
            >
              {["production", "staging", "development", "custom"].map((k) => (
                <option key={k} value={k}>
                  {k}
                </option>
              ))}
            </select>
          </div>
          <ErrorMessage error={create.error} />
          <div className="flex gap-2">
            <Button
              type="button"
              disabled={!name.trim() || create.isPending}
              onClick={() =>
                create.mutate({ name: name.trim(), kind, description: "" })
              }
            >
              {create.isPending ? "Creating…" : "Create environment"}
            </Button>
            <Button
              type="button"
              variant="ghost"
              onClick={() => setCreating(false)}
            >
              Cancel environment
            </Button>
          </div>
        </div>
      ) : (
        <Button
          type="button"
          variant="link"
          className="h-auto cursor-pointer px-0 text-xs"
          onClick={() => {
            create.reset();
            setCreating(true);
          }}
        >
          <Plus />
          {environments.length
            ? "Create another environment"
            : "Create your first environment"}
        </Button>
      )}
    </div>
  );
}
