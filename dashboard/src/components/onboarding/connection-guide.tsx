import { useEffect, useRef, useState } from "react";
import { Link } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, ArrowRight, Check, Loader2, RefreshCw } from "lucide-react";
import { api, type Proxy } from "@/api/client";
import { mcpApi, mcpKeys } from "@/api/mcp";
import { availableProviderKeys } from "@/components/gateway-setup";
import {
  useGatewayExtension,
  useGatewayOrigin,
} from "@/components/gateways/extension-context";
import { EnvironmentPicker } from "@/components/gateways/environment-picker";
import { useGatewayModelListing } from "@/components/gateways/gateway-requests";
import { snippetModelIds } from "@/components/gateways/gateway-models";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
  CopySetup,
  SetupCode,
  SetupDocs,
  SetupError,
  SetupField,
  SetupNote,
  setupControl,
} from "./setup-primitives";
import {
  gatewayEvidence,
  guideLabels,
  integrationDocs,
  mcpConfiguration,
  mcpEvidence,
  modelConfiguration,
  parseArguments,
  parseToolList,
  setupSteps,
  type AppId,
  type SetupDraft,
} from "./setup-model";

export function ConnectionGuide({
  draft,
  update,
  onExit,
  onRestart,
}: {
  draft: SetupDraft;
  update: (patch: Partial<SetupDraft>) => void;
  onExit: () => void;
  onRestart: () => void;
}) {
  const qc = useQueryClient();
  const extension = useGatewayExtension();
  const gatewayOrigin = useGatewayOrigin();
  const [secret, setSecret] = useState("");
  const [providerSecret, setProviderSecret] = useState("");
  const [upstream, setUpstream] = useState("");
  const [credential, setCredential] = useState("");
  const [allowed, setAllowed] = useState("");
  const [blocked, setBlocked] = useState("");
  const [approval, setApproval] = useState("");
  const [executable, setExecutable] = useState("");
  const [args, setArgs] = useState("[]");
  const [bastioPath, setBastioPath] = useState("bastio");
  const [help, setHelp] = useState(false);
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    heading.current?.focus();
  }, [draft.step, draft.intent]);
  const modelMode = draft.coverage === "model";
  const provider =
    draft.guide === "gateway"
      ? draft.provider
      : draft.app === "claude"
        ? "anthropic"
        : "openai";
  const gateways = useQuery({
    queryKey: ["proxies"],
    queryFn: api.proxies.list,
  });
  const keys = useQuery({
    queryKey: ["api-keys"],
    queryFn: api.apiKeys.list,
    enabled: modelMode,
  });
  const providers = useQuery({
    queryKey: ["provider-keys"],
    queryFn: api.providerKeys.list,
    enabled: modelMode && extension.providerKeyManagement === "allowed",
  });
  const servers = useQuery({
    queryKey: [...mcpKeys, "servers"],
    queryFn: mcpApi.servers,
    enabled: !modelMode,
    refetchInterval: draft.step === 3 ? 3000 : false,
  });
  const gateway = gateways.data?.find(
    (item) => item.id === draft.gatewayId && item.is_active,
  );
  const server = servers.data?.find(
    (item) => item.id === draft.serverId && !item.revoked_at,
  );
  const appKey = keys.data?.find(
    (item) => item.id === draft.keyId && item.is_active,
  );
  const providerReady =
    gateway &&
    (extension.managedProviders.includes(gateway.target_provider) ||
      availableProviderKeys(gateway, providers.data ?? []).length > 0);
  const createGateway = useMutation({
    mutationFn: () =>
      api.proxies.create({
        name: draft.name.trim(),
        target_provider: provider,
        target_model: draft.model.trim(),
      }),
    onSuccess: async (data) => {
      update({ gatewayId: data.id });
      await qc.invalidateQueries({ queryKey: ["proxies"] });
    },
  });
  const addProvider = useMutation({
    mutationFn: () =>
      api.providerKeys.create({
        provider,
        key: providerSecret.trim(),
        proxy_id: draft.gatewayId,
        is_default: false,
      }),
    onSuccess: async () => {
      setProviderSecret("");
      await qc.invalidateQueries({ queryKey: ["provider-keys"] });
    },
  });
  const createKey = useMutation({
    mutationFn: () =>
      api.apiKeys.create({
        name: `${draft.name.trim()} app`,
        environment: draft.environment,
        allow_environment_override: false,
        proxy_id: draft.gatewayId,
        scopes: [`proxy:${draft.gatewayId}`],
      }),
    onSuccess: async (data) => {
      setSecret(data.key);
      update({ keyId: data.id });
      await qc.invalidateQueries({ queryKey: ["api-keys"] });
    },
  });
  const createServer = useMutation({
    mutationFn: () =>
      mcpApi.create({
        name: draft.name.trim(),
        client: draft.app === "claude" ? "claude" : "other",
        mode: draft.mode,
        auth_type:
          draft.mode === "remote" && credential.trim() ? "bearer" : "none",
        ...(draft.mode === "remote"
          ? {
              upstream_url: upstream.trim(),
              credential: credential.trim() || undefined,
            }
          : {}),
        policy: {
          allowed_tools: parseToolList(allowed),
          blocked_tools: parseToolList(blocked),
          approval_required_tools: parseToolList(approval),
        },
      }),
    onSuccess: async (data) => {
      setSecret(data.enrollment_token);
      setCredential("");
      update({ serverId: data.server.id });
      await qc.invalidateQueries({ queryKey: mcpKeys });
    },
  });
  const rotate = useMutation({
    mutationFn: () => mcpApi.rotate(draft.serverId),
    onSuccess: (data) => setSecret(data.enrollment_token),
  });
  const traffic = useQuery({
    queryKey: [
      "guided-setup",
      "traffic",
      draft.gatewayId,
      draft.verificationId,
    ],
    queryFn: () =>
      api.traces.list({
        proxy_id: draft.gatewayId,
        from: draft.startedAt,
        limit: 100,
      }),
    enabled: draft.step === 3 && modelMode && Boolean(gateway),
    retry: false,
    refetchInterval: (query) =>
      gatewayEvidence(query.state.data ?? [], draft) ? false : 3000,
  });
  const events = useQuery({
    queryKey: ["guided-setup", "mcp-events", draft.serverId, draft.startedAt],
    queryFn: () => mcpApi.events(draft.serverId),
    enabled: draft.step === 3 && !modelMode && Boolean(server),
    retry: false,
    refetchInterval: (query) =>
      mcpEvidence(query.state.data ?? [], draft) ? false : 3000,
  });
  const trace = gatewayEvidence(traffic.data ?? [], draft);
  const event = mcpEvidence(events.data ?? [], draft);
  const received = Boolean(trace || event);
  const failedRequest = trace?.status === "error";
  const active = modelMode ? Boolean(gateway && appKey) : Boolean(server);
  const connectionUnavailable = modelMode
    ? gateways.isSuccess && keys.isSuccess && !active
    : servers.isSuccess && !server;
  const hasResource = Boolean(draft.gatewayId || draft.serverId);
  const isAppGuide = ["claude", "codex", "antigravity"].includes(draft.guide);
  const parsed = parseArguments(args);
  const docs = integrationDocs[draft.app];
  const canNext =
    draft.step === 0
      ? Boolean(draft.name.trim()) &&
        (modelMode ? draft.auth === "api" : draft.toolLocationChosen)
      : draft.step === 1
        ? active && Boolean(secret.trim())
        : draft.step === 2
          ? active &&
            Boolean(secret.trim()) &&
            (modelMode
              ? Boolean(draft.model.trim() || gateway?.target_model)
              : draft.mode === "remote" ||
                Boolean(executable.trim() && parsed && bastioPath.trim()))
          : received && !failedRequest;
  const mutationError =
    createGateway.error ??
    addProvider.error ??
    createKey.error ??
    createServer.error ??
    rotate.error;

  if (isAppGuide && draft.step === 0 && draft.intent === "overview" && !hasResource) {
    return <AppProtectionIntro draft={draft} update={update} onExit={onExit} />;
  }

  return (
    <div className="grid gap-8 lg:grid-cols-[210px_minmax(0,1fr)] lg:gap-10">
      <aside className="min-w-0">
        <ol
          aria-label="Connection setup steps"
          className="flex gap-4 overflow-x-auto pb-3 lg:flex-col lg:gap-0"
        >
          {setupSteps.map((label, index) => (
            <li key={label} className="shrink-0">
              <button
                type="button"
                disabled={index > draft.step}
                onClick={() => update({ step: index })}
                aria-current={index === draft.step ? "step" : undefined}
                className={cn(
                  "flex items-center gap-3 py-3 text-left text-[12px] disabled:cursor-default",
                  index === draft.step
                    ? "text-foreground"
                    : "text-muted-foreground",
                )}
              >
                <span
                  className={cn(
                    "flex size-5 items-center justify-center rounded-full border font-mono text-[10px]",
                    index === draft.step
                      ? "border-foreground/50"
                      : "border-border-subtle",
                  )}
                >
                  {index < draft.step ? (
                    <Check className="size-3" />
                  ) : (
                    index + 1
                  )}
                </span>
                {label}
              </button>
            </li>
          ))}
        </ol>
        <p className="mt-5 hidden border-t border-border-subtle pt-4 text-[11px] leading-relaxed text-muted-foreground lg:block">
          You can leave and resume this guide. Connection secrets are shown once
          and aren't saved with your progress.
        </p>
      </aside>
      <div className="min-w-0 max-w-[780px]">
        <div className="mb-6">
          <p className="mb-2 text-[10px] text-muted-foreground">
            {guideLabels[draft.guide]} · Step {draft.step + 1} of 5
          </p>
          <h2
            ref={heading}
            tabIndex={-1}
            className="text-[21px] font-medium tracking-tight outline-none"
          >
            {setupSteps[draft.step]}
          </h2>
        </div>
        {draft.step === 0 ? (
          <div className="space-y-6">
            {isAppGuide && !hasResource ? (
              <button
                type="button"
                onClick={() => update({ intent: "overview" })}
                className="inline-flex items-center gap-2 text-[12px] text-muted-foreground hover:text-foreground"
              >
                <ArrowLeft className="size-3" />
                Change what to protect
              </button>
            ) : null}
            <p className="text-[12px] leading-relaxed text-muted-foreground">
              {modelMode
                ? "Set up inspection of model requests routed through Bastio. You'll need a provider API account with separate billing."
                : "This setup is for an extra tool you have already added to your AI app. First, identify how that tool connects."}
            </p>
            <div className="grid gap-5 sm:grid-cols-2">
              <SetupField htmlFor="setup-name" label="Connection name">
                <input
                  id="setup-name"
                  required
                  maxLength={100}
                  className={setupControl}
                  value={draft.name}
                  disabled={hasResource}
                  onChange={(e) => update({ name: e.target.value })}
                />
              </SetupField>
              <SetupField htmlFor="setup-os" label="Your operating system">
                <select
                  id="setup-os"
                  className={setupControl}
                  value={draft.os}
                  onChange={(e) =>
                    update({ os: e.target.value as SetupDraft["os"] })
                  }
                >
                  <option value="mac">macOS</option>
                  <option value="windows">Windows</option>
                  <option value="linux">Linux</option>
                </select>
              </SetupField>
            </div>
            {draft.guide === "gateway" ? (
              <SetupField htmlFor="setup-provider" label="Your AI provider">
                <select
                  id="setup-provider"
                  className={setupControl}
                  disabled={hasResource}
                  value={draft.provider}
                  onChange={(event) =>
                    update({
                      provider: event.target.value as SetupDraft["provider"],
                      model: "",
                    })
                  }
                >
                  <option value="openai">OpenAI</option>
                  <option value="anthropic">Anthropic / Claude</option>
                </select>
              </SetupField>
            ) : null}
            {draft.guide === "mcp" ? (
              <SetupField
                htmlFor="setup-app"
                label="Where will you use this tool connection?"
              >
                <select
                  id="setup-app"
                  className={setupControl}
                  disabled={hasResource}
                  value={draft.app}
                  onChange={(e) => update({ app: e.target.value as AppId })}
                >
                  <option value="claude">Claude Code</option>
                  <option value="codex">Codex app</option>
                  <option value="antigravity">Antigravity</option>
                  <option value="other">Another MCP client</option>
                </select>
              </SetupField>
            ) : null}
            {!modelMode ? (
              <>
                <SetupNote title="Coverage: the tools you connect">
                  This connection inspects MCP tool calls and results. It does
                  not route your app's other prompts, built-in shell commands,
                  or model responses through Bastio.
                  {draft.guide === "antigravity"
                    ? " Antigravity setup uses its documented MCP configuration."
                    : ""}
                </SetupNote>
                <fieldset
                  disabled={hasResource}
                  className="divide-y divide-border-subtle border-y border-border-subtle"
                >
                  <legend className="mb-3 text-[12px]">
                    How does your existing tool connect?
                  </legend>
                  <Choice
                    name="setup-mode"
                    checked={draft.toolLocationChosen && draft.mode === "stdio"}
                    onChange={() => update({ mode: "stdio", toolLocationChosen: true })}
                    label="It starts with a command on my computer"
                    detail="Your tool configuration contains a command such as npx, uvx, or python. Bastio will wrap that existing tool process."
                  />
                  <Choice
                    name="setup-mode"
                    checked={draft.toolLocationChosen && draft.mode === "remote"}
                    onChange={() => update({ mode: "remote", toolLocationChosen: true })}
                    label="It connects to an online address"
                    detail="Your tool configuration contains an https:// server URL. You can use a hosted tool service; you don't have to own the server."
                  />
                </fieldset>
                <p className="text-[12px] leading-relaxed text-muted-foreground">
                  This is about the extra tool, not where {guideLabels[draft.app === "other" ? "mcp" : draft.app]} runs.
                  Installing an AI app on your Mac or Windows PC does not mean
                  you have a local tool server.
                </p>
                {isAppGuide && !hasResource ? (
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => update({ intent: "overview", toolLocationChosen: false })}
                  >
                    I don't know / I haven't added any tools
                    <ArrowRight className="size-3" />
                  </Button>
                ) : (
                  <SetupNote title="Don't have an extra tool configured?">
                    You can skip MCP setup. Open the guide for your AI app to
                    see the other protection options, or ask whoever configured
                    your tools for the command or server address.
                  </SetupNote>
                )}
              </>
            ) : (
              <SetupNote title="Coverage: model traffic routed through this gateway">
                Bastio can inspect requests and responses sent to this gateway.
                Your app subscription does not pay for these provider API calls.
                Built-in terminal actions and MCP tools need their own controls.
                {draft.app === "codex"
                  ? " Managed or cloud-coordinated Codex sessions may ignore custom provider settings; verify from the local app before relying on this route."
                  : ""}
              </SetupNote>
            )}
            {hasResource ? (
              <p className="text-[11px] text-muted-foreground">
                Connection choices are locked after creation.{" "}
                <button
                  type="button"
                  onClick={onRestart}
                  className="underline underline-offset-4"
                >
                  Start another setup
                </button>{" "}
                to change them; existing connections are kept.
              </p>
            ) : null}
          </div>
        ) : null}
        {draft.step === 1 ? (
          <div className="space-y-6">
            {modelMode ? (
              <>
                {!gateway ? (
                  <div className="space-y-4">
                    <SetupField
                      htmlFor="setup-existing-gateway"
                      label={`Connect to ${provider === "anthropic" ? "Anthropic" : "OpenAI"}`}
                      hint="Use an existing gateway or create a dedicated gateway for this app."
                    >
                      <select
                        id="setup-existing-gateway"
                        className={setupControl}
                        value={draft.gatewayId}
                        onChange={(e) => {
                          setSecret("");
                          update({ gatewayId: e.target.value, keyId: "" });
                        }}
                      >
                        <option value="">Create a new gateway</option>
                        {gateways.data
                          ?.filter(
                            (item) =>
                              item.is_active &&
                              item.target_provider === provider,
                          )
                          .map((item) => (
                            <option key={item.id} value={item.id}>
                              {item.name}
                            </option>
                          ))}
                      </select>
                    </SetupField>
                    <SetupField
                      htmlFor="setup-model"
                      label="Model ID · optional for now"
                      hint="Already know your model? Enter it here. Otherwise, choose from your provider's models when configuring the app."
                    >
                      <input
                        id="setup-model"
                        className={setupControl}
                        value={draft.model}
                        onChange={(e) => update({ model: e.target.value })}
                        placeholder="Your provider's model ID"
                      />
                    </SetupField>
                    <Button
                      size="sm"
                      disabled={
                        createGateway.isPending ||
                        !draft.name.trim() ||
                        gateways.isPending ||
                        gateways.isError
                      }
                      onClick={() => createGateway.mutate()}
                    >
                      {createGateway.isPending ? "Creating…" : "Create gateway"}
                    </Button>
                  </div>
                ) : (
                  <SetupNote
                    title={`${gateway.name} · ${gateway.target_provider}`}
                  >
                    This gateway will receive the app's model requests.
                  </SetupNote>
                )}
                {gateway && !providerReady ? (
                  <div className="space-y-3">
                    <SetupField
                      htmlFor="setup-provider-key"
                      label={`${provider === "anthropic" ? "Anthropic" : "OpenAI"} provider API key`}
                      hint="Stored on Bastio for this gateway. App subscription credentials cannot be used here."
                    >
                      <input
                        id="setup-provider-key"
                        type="password"
                        autoComplete="off"
                        className={setupControl}
                        value={providerSecret}
                        disabled={extension.providerKeyManagement !== "allowed"}
                        onChange={(e) => setProviderSecret(e.target.value)}
                      />
                    </SetupField>
                    <Button
                      size="sm"
                      disabled={
                        !providerSecret.trim() ||
                        addProvider.isPending ||
                        extension.providerKeyManagement !== "allowed"
                      }
                      onClick={() => addProvider.mutate()}
                    >
                      {addProvider.isPending ? "Saving…" : "Connect provider"}
                    </Button>
                    {extension.providerKeyManagement !== "allowed" ? (
                      <p className="text-[11px] text-muted-foreground">
                        Provider credentials cannot be changed by this account.
                        Ask your workspace owner to connect the provider, or use
                        the MCP path with your current app sign-in.
                      </p>
                    ) : null}
                  </div>
                ) : gateway ? (
                  <p className="text-[12px] text-muted-foreground">
                    Provider credentials are available for this gateway.
                  </p>
                ) : null}
                {gateway ? (
                  <>
                    <EnvironmentPicker
                      value={draft.environment}
                      onChange={(value) => update({ environment: value })}
                    />
                    <div className="border-t border-border-subtle pt-4">
                      <p className="mb-3 text-[12px] text-muted-foreground">
                        Create a Bastio key limited to this gateway and
                        environment.
                      </p>
                      <Button
                        size="sm"
                        disabled={
                          !providerReady ||
                          !draft.environment ||
                          createKey.isPending
                        }
                        onClick={() => createKey.mutate()}
                      >
                        {createKey.isPending
                          ? "Creating…"
                          : appKey
                            ? "Create another app key"
                            : "Create scoped app key"}
                      </Button>
                      {appKey ? (
                        <p className="mt-2 text-[11px] text-muted-foreground">
                          Previously created keys remain active. Manage unused
                          keys in{" "}
                          <Link
                            to="/api-keys"
                            className="underline underline-offset-4"
                          >
                            API Keys
                          </Link>
                          .
                        </p>
                      ) : null}
                    </div>
                  </>
                ) : null}
                <SetupError
                  error={gateways.error ?? providers.error ?? keys.error}
                  retry={() => {
                    void gateways.refetch();
                    if (extension.providerKeyManagement === "allowed")
                      void providers.refetch();
                    void keys.refetch();
                  }}
                />
              </>
            ) : !server ? (
              <form
                className="space-y-5"
                onSubmit={(e) => {
                  e.preventDefault();
                  if (draft.mode === "remote" || parsed) createServer.mutate();
                }}
              >
                {draft.mode === "remote" ? (
                  <>
                    <SetupField
                      htmlFor="setup-upstream"
                      label="Your MCP server's HTTPS endpoint"
                      hint="Use the original tool server's endpoint. Bastio will sit between your app and that server."
                    >
                      <input
                        id="setup-upstream"
                        type="url"
                        pattern="https://.*"
                        required
                        className={setupControl}
                        value={upstream}
                        onChange={(e) => setUpstream(e.target.value)}
                        placeholder="https://mcp.yourcompany.com/mcp"
                      />
                    </SetupField>
                    <SetupField
                      htmlFor="setup-upstream-token"
                      label="Upstream bearer token · optional"
                      hint="Provide the original server's token if it requires one. Bastio stores this credential in its vault."
                    >
                      <input
                        id="setup-upstream-token"
                        type="password"
                        autoComplete="off"
                        className={setupControl}
                        value={credential}
                        onChange={(e) => setCredential(e.target.value)}
                      />
                    </SetupField>
                  </>
                ) : (
                  <>
                    <SetupNote title="Install the local wrapper">
                      Install the Bastio CLI on the same computer as your app.
                      The wrapper launches your existing tool server and sends
                      policy events to this workspace.
                    </SetupNote>
                    <SetupCode
                      file="Terminal · Go toolchain required"
                      language="command"
                      code="go install github.com/bastio-ai/bastio/cmd/bastio@main"
                    />
                    <LocalServer
                      executable={executable}
                      setExecutable={setExecutable}
                      args={args}
                      setArgs={setArgs}
                      bastioPath={bastioPath}
                      setBastioPath={setBastioPath}
                      parsed={parsed}
                    />
                  </>
                )}
                <details className="border-y border-border-subtle py-3">
                  <summary className="cursor-pointer text-[12px]">
                    Tool rules · optional
                  </summary>
                  <div className="mt-4 space-y-4">
                    <p className="text-[11px] text-muted-foreground">
                      Enter exact tool names separated by commas. Leave Allowed
                      tools empty to allow all tools except those explicitly
                      blocked or requiring approval. Baseline security scanning
                      still applies.
                    </p>
                    <PolicyField
                      id="allowed"
                      label="Allowed tools"
                      value={allowed}
                      onChange={setAllowed}
                    />
                    <PolicyField
                      id="blocked"
                      label="Blocked tools"
                      value={blocked}
                      onChange={setBlocked}
                    />
                    <PolicyField
                      id="approval"
                      label="Require human approval"
                      value={approval}
                      onChange={setApproval}
                    />
                  </div>
                </details>
                <Button
                  type="submit"
                  size="sm"
                  disabled={
                    createServer.isPending ||
                    servers.isPending ||
                    servers.isError ||
                    !draft.name.trim() ||
                    (draft.mode === "stdio" &&
                      (!executable.trim() || !parsed || !bastioPath.trim()))
                  }
                >
                  {createServer.isPending
                    ? "Creating…"
                    : "Create MCP connection"}
                </Button>
              </form>
            ) : (
              <SetupNote
                title={`${server.name} · ${server.mode === "remote" ? "Remote HTTPS" : "Local wrapper"}`}
              >
                Your connection is enrolled. Save the enrollment token and
                configure the app next.
              </SetupNote>
            )}
            {active || secret ? (
              <Credential
                secret={secret}
                setSecret={setSecret}
                modelMode={modelMode}
              />
            ) : null}
            {!modelMode && server && !secret ? (
              <div>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={rotate.isPending}
                  onClick={() => rotate.mutate()}
                >
                  Rotate enrollment token
                </Button>
                <p className="mt-2 text-[11px] text-muted-foreground">
                  Rotation invalidates the previous token. Use this only if you
                  didn't save it or need to replace it.
                </p>
              </div>
            ) : null}
            <SetupError error={mutationError} />
            {!modelMode ? (
              <SetupError
                error={servers.error}
                retry={() => {
                  void servers.refetch();
                }}
              />
            ) : null}
          </div>
        ) : null}
        {draft.step === 2 ? (
          <div className="space-y-5">
            <SetupNote
              title={
                draft.guide === "gateway"
                  ? "Run a request through your gateway"
                  : "Merge this into your existing configuration"
              }
            >
              {draft.guide === "gateway"
                ? "Run the generated command in your terminal. It includes a setup marker so Bastio can identify this test."
                : "Keep your current settings and other server entries. The configuration contains a connection secret; save it privately on your computer."}
            </SetupNote>
            {!secret ? (
              <Credential
                secret={secret}
                setSecret={setSecret}
                modelMode={modelMode}
              />
            ) : null}
            {!modelMode && draft.mode === "stdio" ? (
              <LocalServer
                executable={executable}
                setExecutable={setExecutable}
                args={args}
                setArgs={setArgs}
                bastioPath={bastioPath}
                setBastioPath={setBastioPath}
                parsed={parsed}
              />
            ) : null}
            {modelMode && gateway ? (
              <SetupModelPicker
                gateway={gateway}
                value={draft.model || gateway.target_model || ""}
                onChange={(model) => update({ model })}
              />
            ) : null}
            {secret &&
            (modelMode
              ? gateway && Boolean(draft.model.trim() || gateway.target_model)
              : draft.mode === "remote" || (parsed && executable.trim())) ? (
              <SetupCode
                {...(modelMode
                  ? modelConfiguration(
                      draft,
                      gateway as Proxy,
                      gatewayOrigin,
                      secret,
                    )
                  : mcpConfiguration(
                      draft,
                      window.location.origin,
                      secret,
                      executable.trim(),
                      parsed ?? [],
                      bastioPath,
                    ))}
              />
            ) : null}
            <ol className="list-decimal space-y-2 pl-4 text-[12px] leading-relaxed text-muted-foreground">
              <li>
                {draft.guide === "gateway"
                  ? "Run the generated command in the terminal shown above."
                  : modelMode
                    ? "Save the configuration, then fully quit and reopen your application."
                    : draft.app === "claude"
                      ? "Save .mcp.json in your project, reopen Claude Code there, and approve the new MCP server when prompted."
                      : draft.app === "antigravity"
                        ? "In Antigravity, open MCP Servers → Manage MCP Servers → View raw config. Merge this entry, save, then refresh your MCP servers."
                        : "Save the MCP server entry and fully reopen your app. Check that bastio_tools is enabled in its MCP settings."}
              </li>
              <li>
                {draft.guide === "gateway"
                  ? "If the provider returns an error, check your model ID and provider credentials before retrying."
                  : modelMode
                    ? "For Claude Code, run /status to check the gateway address. For Codex, confirm the active provider is Bastio. This changes the local app's model route."
                    : "Use this Bastio-wrapped connection instead of calling the original tool server directly."}
              </li>
              <li>
                {draft.guide === "gateway"
                  ? "Continue to verification to inspect the resulting trace."
                  : "Continue to verification, then send a harmless test from the app."}
              </li>
            </ol>
            <SetupDocs
              href={
                modelMode && draft.app === "claude"
                  ? "https://code.claude.com/docs/en/llm-gateway-connect"
                  : docs.url
              }
            >
              {docs.label}
            </SetupDocs>
            {!active && !gateways.isPending && !servers.isPending ? (
              <SetupNote title="Connection is unavailable">
                Go back to Create a connection to check whether this gateway,
                key, or server is still active.
              </SetupNote>
            ) : null}
          </div>
        ) : null}
        {draft.step === 3 ? (
          <div className="space-y-6">
            {connectionUnavailable ? (
              <SetupNote title="This connection is unavailable">
                The saved gateway, key, or MCP server may have been removed or
                revoked.{" "}
                <button
                  type="button"
                  onClick={() => update({ step: 1 })}
                  className="underline underline-offset-4"
                >
                  Return to connection setup
                </button>{" "}
                before testing again.
              </SetupNote>
            ) : null}
            <p className="text-[12px] leading-relaxed text-muted-foreground">
              {modelMode
                ? `In ${draft.guide === "gateway" ? "your terminal" : guideLabels[draft.guide]}, send a short message such as “Reply with: Bastio connection test”.`
                : "In your app, ask the connected server to list its available tools, then use one harmless read-only tool, such as listing a directory or reading a test document. Listing tools alone does not verify a tool call."}
            </p>
            <div
              role="status"
              aria-live="polite"
              className="border-y border-border-subtle py-5"
            >
              <div className="flex items-center gap-2 text-[13px]">
                {received ? (
                  <Check className="size-4 text-success" />
                ) : (
                  <Loader2
                    className={cn(
                      "size-4 text-muted-foreground",
                      !connectionUnavailable && "animate-spin",
                    )}
                  />
                )}
                {connectionUnavailable
                  ? "Connection unavailable"
                  : received
                    ? failedRequest
                      ? "Request received · provider returned an error"
                      : modelMode
                        ? "Model request verified"
                        : "MCP tool call verified"
                    : "Waiting for your test interaction"}
              </div>
              <p className="mt-2 text-[11px] leading-relaxed text-muted-foreground">
                {trace
                  ? `${trace.model} · ${trace.status}${trace.error_message ? ` · ${trace.error_message}` : ""}`
                  : event
                    ? `${event.tool_name} · ${event.action}`
                    : server?.last_seen_at &&
                        Date.parse(server.last_seen_at) >=
                          Date.parse(draft.startedAt)
                      ? "Your MCP server has connected. Run a read-only tool to verify its traffic."
                      : modelMode
                        ? "Checks this gateway for the setup session ID in your generated configuration. Other workspace traffic won't complete this step."
                        : "Checks new tool events for this enrolled MCP server. Other servers won't complete this step."}
              </p>
              {trace ? (
                <Link
                  to="/traces/$id"
                  params={{ id: trace.id }}
                  className="mt-3 inline-flex items-center gap-1.5 text-[12px] hover:underline"
                >
                  Inspect test trace <ArrowRight className="size-3" />
                </Link>
              ) : event ? (
                <Link
                  to="/mcp"
                  className="mt-3 inline-flex items-center gap-1.5 text-[12px] hover:underline"
                >
                  Review tool activity <ArrowRight className="size-3" />
                </Link>
              ) : null}
            </div>
            <SetupError
              error={modelMode ? traffic.error : events.error}
              retry={() => {
                if (modelMode) void traffic.refetch();
                else void events.refetch();
              }}
            />
            <div className="flex flex-wrap gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  if (modelMode) void traffic.refetch();
                  else {
                    void servers.refetch();
                    void events.refetch();
                  }
                }}
              >
                <RefreshCw className="size-3" />
                Check again
              </Button>
              <Button
                variant="ghost"
                size="sm"
                aria-expanded={help}
                onClick={() => setHelp(!help)}
              >
                Nothing arriving?
              </Button>
            </div>
            {help ? (
              <div className="space-y-3 border-l-2 border-border-default pl-3 text-[12px] text-muted-foreground">
                <p>
                  1. Confirm you saved the configuration in the selected app and
                  fully reopened it.
                </p>
                <p>
                  2.{" "}
                  {modelMode
                    ? "Check the provider, model, gateway URL, and app key. Keep the setup session header while testing."
                    : "Check that bastio_tools is connected, the original server is reachable, and the enrollment token is current."}
                </p>
                <p>
                  3.{" "}
                  {draft.mode === "stdio" && !modelMode
                    ? "Use an absolute path to the bastio executable if your app cannot find it. Check the original tool command and arguments."
                    : "Look for a sign-in, permission, or connection error inside the app."}
                </p>
                <p>
                  4.{" "}
                  {modelMode
                    ? "A provider error still proves the request reached Bastio, but resolve it before completing setup. App subscriptions cannot be used as provider API keys."
                    : "Use a tool exposed by this connection. Built-in app tools and another MCP server don't count."}
                </p>
              </div>
            ) : null}
            <SetupError
              error={modelMode ? (gateways.error ?? keys.error) : servers.error}
              retry={() => {
                if (modelMode) {
                  void gateways.refetch();
                  void keys.refetch();
                } else void servers.refetch();
              }}
            />
            <SetupNote title="Connection and enforcement are separate">
              Receiving traffic proves this route works. Review your security
              policy to understand which findings are recorded, masked, blocked,
              or held for approval.
            </SetupNote>
          </div>
        ) : null}
        {draft.step === 4 ? (
          <div className="space-y-6">
            <div className="flex items-center gap-2 text-[15px]">
              <Check className="size-4 text-success" />
              Connection setup verified
            </div>
            <p className="text-[11px] text-muted-foreground">
              Verified{" "}
              {draft.verifiedAt
                ? new Date(draft.verifiedAt).toLocaleString()
                : "during this setup"}
            </p>
            {connectionUnavailable ? (
              <SetupNote title="Connection is currently unavailable">
                The earlier verification is recorded, but the saved connection
                is no longer active. Review the connection before using it
                again.
              </SetupNote>
            ) : null}
            <p className="text-[12px] leading-relaxed text-muted-foreground">
              {modelMode
                ? "Bastio received a model request with the setup session ID from your configuration."
                : "Bastio received a tool call through this MCP connection."}{" "}
              Only traffic using this connection is covered.
            </p>
            <div className="divide-y divide-border-subtle border-y border-border-subtle">
              <NextLink
                to={modelMode ? "/traces" : "/mcp"}
                title={
                  modelMode ? "Explore your traces" : "Review tools & approvals"
                }
                detail={
                  modelMode
                    ? "See requests, provider errors, and security decisions."
                    : "Manage allowed, blocked, and approval-required tools."
                }
              />
              <NextLink
                to="/security-settings"
                title="Review enforcement"
                detail="Confirm detector actions and your monitoring or blocking policy."
              />
            </div>
            <p className="text-[11px] text-muted-foreground">
              You can connect another app or follow a feature tour from the
              setup hub.
            </p>
          </div>
        ) : null}
        <footer className="mt-8 flex flex-wrap items-center justify-between gap-3 border-t border-border-subtle pt-4">
          <Button
            variant="ghost"
            size="sm"
            onClick={
              draft.step === 0 ? onExit : () => update({ step: draft.step - 1 })
            }
          >
            <ArrowLeft className="size-3" />
            {draft.step === 0 ? "All guides" : "Back"}
          </Button>
          <div className="flex items-center gap-2">
            {draft.step < 4 ? (
              <Button variant="ghost" size="sm" onClick={onExit}>
                Finish later
              </Button>
            ) : null}
            <Button
              size="sm"
              disabled={draft.step < 4 && !canNext}
              onClick={() => {
                if (draft.step === 4) {
                  onExit();
                  return;
                }
                update({
                  step: draft.step + 1,
                  ...(draft.step === 3
                    ? {
                        verifiedAt: new Date().toISOString(),
                        evidenceId: trace?.id ?? event?.id ?? "",
                      }
                    : {}),
                });
              }}
            >
              {draft.step === 4
                ? "Back to setup hub"
                : draft.step === 3
                  ? "Complete setup"
                  : "Continue"}
              <ArrowRight className="size-3" />
            </Button>
          </div>
        </footer>
      </div>
    </div>
  );
}

function AppProtectionIntro({
  draft,
  update,
  onExit,
}: {
  draft: SetupDraft;
  update: (patch: Partial<SetupDraft>) => void;
  onExit: () => void;
}) {
  const appName = guideLabels[draft.guide];
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    heading.current?.focus();
  }, []);
  return (
    <div className="max-w-[780px] space-y-6">
      <div>
        <p className="mb-2 text-[10px] text-muted-foreground">Before you connect</p>
        <h2
          ref={heading}
          tabIndex={-1}
          className="text-[21px] font-medium tracking-tight outline-none"
        >
          Just using {appName} on your computer?
        </h2>
        <p className="mt-3 text-[13px] leading-relaxed text-muted-foreground">
          You don't need a remote server. If you haven't added extra tools to
          {" "}{appName}, you can skip MCP setup entirely.
        </p>
      </div>
      <SetupNote title="What your normal app sign-in covers">
        Signing into {appName} does not connect it to Bastio. Bastio does not
        currently offer automatic protection for everything in this desktop app,
        including every document you upload. Choose a supported workflow below.
      </SetupNote>
      <div className="divide-y divide-border-subtle border-y border-border-subtle">
        {draft.app !== "antigravity" ? (
          <section className="space-y-3 py-5" aria-label="Model protection option">
            <h3 className="text-[14px] font-medium">
              Inspect prompts & responses in {appName}
            </h3>
            <p className="max-w-2xl text-[12px] leading-relaxed text-muted-foreground">
              Route model requests through Bastio so its policies can inspect,
              mask, or block their text. You'll need
              {draft.app === "claude" ? " an Anthropic" : " an OpenAI"} API
              account with separate billing. Your existing app subscription
              does not pay for these API requests. This is not protection for
              every action or file format in the app.
            </p>
            <Button
              size="sm"
              onClick={() => update({ intent: "model", coverage: "model", auth: "api" })}
            >
              Set up model protection
              <ArrowRight className="size-3" />
            </Button>
          </section>
        ) : null}
        <section className="space-y-3 py-5" aria-label="Workspace option">
          <h3 className="text-[14px] font-medium">
            Chat and work with documents in Bastio Workspace
          </h3>
          <p className="max-w-2xl text-[12px] leading-relaxed text-muted-foreground">
            Use Bastio's own chat instead of {appName}. No MCP server or local
            commands to configure. The guide helps you review personal-information
            protection and test a document; images and scanned PDFs are not
            automatically covered.
          </p>
          <Link
            to="/connect"
            search={{ guide: "documents" }}
            className="inline-flex items-center gap-2 border-b border-foreground/30 pb-1 text-[12px]"
          >
            Explore the Workspace guide
            <ArrowRight className="size-3" />
          </Link>
        </section>
        <details className="group py-5">
          <summary className="cursor-pointer text-[13px] font-medium">
            I already use extra tools (MCP)
          </summary>
          <div className="mt-3 space-y-3 pl-4">
            <p className="max-w-2xl text-[12px] leading-relaxed text-muted-foreground">
              Choose this only if you've added a tool integration to {appName},
              such as a database or a company tool service. You'll need its
              existing command or server address. The firewall protects those
              tool calls and results; it does not cover the rest of your app.
            </p>
            <Button
              size="sm"
              variant="outline"
              onClick={() => update({
                intent: "tools",
                coverage: "mcp",
                auth: "subscription",
                toolLocationChosen: false,
              })}
            >
              Set up protection for an existing tool
              <ArrowRight className="size-3" />
            </Button>
          </div>
        </details>
      </div>
      <Button size="sm" variant="ghost" onClick={onExit}>
        <ArrowLeft className="size-3" />
        All guides
      </Button>
    </div>
  );
}

function Choice({
  checked,
  onChange,
  label,
  detail,
  disabled,
  name = "setup-coverage",
}: {
  checked: boolean;
  onChange: () => void;
  label: string;
  detail: string;
  disabled?: boolean;
  name?: string;
}) {
  return (
    <label
      className={cn(
        "flex cursor-pointer items-start gap-3 py-4",
        disabled && "cursor-default opacity-50",
      )}
    >
      <input
        type="radio"
        name={name}
        checked={checked}
        disabled={disabled}
        onChange={onChange}
        className="mt-0.5 accent-foreground"
      />
      <span>
        <span className="block text-[12px]">{label}</span>
        <span className="mt-1 block text-[11px] leading-relaxed text-muted-foreground">
          {detail}
        </span>
      </span>
    </label>
  );
}
function Credential({
  secret,
  setSecret,
  modelMode,
}: {
  secret: string;
  setSecret: (value: string) => void;
  modelMode: boolean;
}) {
  return (
    <div className="space-y-2 border-y border-border-subtle py-4">
      <SetupField
        htmlFor="setup-secret"
        label={modelMode ? "Your Bastio app key" : "Your MCP enrollment token"}
        hint="Save this secret now. It stays in memory for this visit and is not saved with guide progress."
      >
        <div className="flex items-center gap-2">
          <input
            id="setup-secret"
            type="password"
            autoComplete="off"
            className={setupControl}
            value={secret}
            onChange={(e) => setSecret(e.target.value)}
            placeholder="Paste the secret you saved, or create one above"
          />
          {secret ? <CopySetup value={secret} label="Copy secret" /> : null}
        </div>
      </SetupField>
    </div>
  );
}
function PolicyField({
  id,
  label,
  value,
  onChange,
}: {
  id: string;
  label: string;
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <SetupField htmlFor={`setup-policy-${id}`} label={label}>
      <input
        id={`setup-policy-${id}`}
        className={setupControl}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder="Exact tool names, separated by commas"
      />
    </SetupField>
  );
}
function LocalServer({
  executable,
  setExecutable,
  args,
  setArgs,
  bastioPath,
  setBastioPath,
  parsed,
}: {
  executable: string;
  setExecutable: (value: string) => void;
  args: string;
  setArgs: (value: string) => void;
  bastioPath: string;
  setBastioPath: (value: string) => void;
  parsed: string[] | null;
}) {
  return (
    <div className="space-y-4">
      <SetupField
        htmlFor="setup-bastio-path"
        label="Bastio executable"
        hint="Use an absolute executable path if your desktop app doesn't inherit your terminal PATH."
      >
        <input
          id="setup-bastio-path"
          className={setupControl}
          value={bastioPath}
          onChange={(e) => setBastioPath(e.target.value)}
        />
      </SetupField>
      <SetupField
        htmlFor="setup-executable"
        label="Original tool server executable"
      >
        <input
          id="setup-executable"
          className={setupControl}
          required
          value={executable}
          onChange={(e) => setExecutable(e.target.value)}
          placeholder="e.g. npx or an absolute executable path"
        />
      </SetupField>
      <SetupField
        htmlFor="setup-args"
        label="Original server arguments · JSON array"
      >
        <textarea
          id="setup-args"
          className={`${setupControl} h-20 py-2 font-mono`}
          value={args}
          onChange={(e) => setArgs(e.target.value)}
        />
      </SetupField>
      {!parsed ? (
        <p role="alert" className="text-[11px] text-danger">
          Use a JSON array containing only strings.
        </p>
      ) : null}
    </div>
  );
}
function NextLink({
  to,
  title,
  detail,
}: {
  to: "/mcp" | "/traces" | "/security-settings";
  title: string;
  detail: string;
}) {
  return (
    <Link to={to} className="flex items-center gap-4 py-4">
      <div className="min-w-0 flex-1">
        <p className="text-[12px]">{title}</p>
        <p className="mt-1 text-[11px] text-muted-foreground">{detail}</p>
      </div>
      <ArrowRight className="size-3.5 text-muted-foreground" />
    </Link>
  );
}

function SetupModelPicker({
  gateway,
  value,
  onChange,
}: {
  gateway: Proxy;
  value: string;
  onChange: (value: string) => void;
}) {
  const listing = useGatewayModelListing(gateway);
  const models = snippetModelIds(listing.data);
  return (
    <SetupField
      htmlFor="setup-config-model"
      label="Model"
      hint="Choose a model supported by your app and provider account. You can also enter its exact model ID."
    >
      <input
        id="setup-config-model"
        list="setup-provider-models"
        className={setupControl}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder="Choose or enter a model ID"
      />
      <datalist id="setup-provider-models">
        {models.map((model) => (
          <option key={model} value={model} />
        ))}
      </datalist>
      {listing.isPending ? (
        <p className="mt-2 text-[11px] text-muted-foreground">
          Checking available models…
        </p>
      ) : listing.isError ? (
        <p className="mt-2 text-[11px] text-muted-foreground">
          Models couldn't be listed. Enter an ID from your provider, or{" "}
          <button
            type="button"
            className="underline underline-offset-4"
            onClick={() => {
              void listing.refetch();
            }}
          >
            try again
          </button>
          .
        </p>
      ) : (
        <p className="mt-2 text-[11px] text-muted-foreground">
          {models.length
            ? `${models.length} model options loaded. Your provider and app determine which are available.`
            : "No models were returned. Enter a model ID from your provider to continue."}
        </p>
      )}
    </SetupField>
  );
}
