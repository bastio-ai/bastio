# Guided setup — 11 November 2026

Result: passed for local UI and integration review.

## Review

Open http://localhost:4174/connect. Header **Connect**, the Overview welcome section, the sidebar **Setup guides**, command search, and **Guided setup** on the MCP page lead here.

Start the real Cloud app and its isolated sample API with `npm run dev:design` in `bastio-cloud/cloud-dashboard`. The sample API binds to loopback port 4175. Gateway, provider-key, app-key, and MCP creation are temporary in-memory operations. Account/billing changes remain read-only. Restarting clears those sample connection records. The sample backend does not forward model requests or MCP calls, and does not invent verification events.

## Implemented paths

- Claude Code and Codex app: start with a plain-language coverage explanation. Model protection explicitly selects provider API authentication. MCP setup is an advanced choice for existing tools, with no location selected by default.
- Antigravity: start with the same coverage explanation, then optionally configure an existing MCP tool connection.
- MCP firewall: remote HTTPS gateway or local stdio wrapper, original server configuration, optional allow/block/approval rules, one-time enrollment, and per-server verification.
- Application gateway: OpenAI or Anthropic, provider credentials, gateway/environment-scoped application key, provider model discovery/manual fallback, and an identified test request.
- Feature tours: security policies, trace investigations, and workspace/team setup. Acknowledged steps are labeled reviewed, separately from traffic verification.

Progress is saved by workspace and identity. An allowlist excludes plaintext credentials, upstream URLs, and original server arguments from browser storage. Reopening configuration asks for the saved credential again. The Cloud header uses the authenticated customer ID while membership metadata loads, preventing progress from temporarily being saved under a different workspace.

## Verification behavior

Gateway completion requires a fresh trace for the selected gateway **and** the generated setup session ID. Failed provider requests explain the error and do not complete setup. MCP completion requires a fresh request event for the selected server; unrelated servers, old events, and tool discovery do not count. Connection verification does not claim all application activity is covered or that every detector blocks. Policy review is a separate next step.

## Evidence

- Native Brave screenshots reviewed on desktop and iPhone 16 emulation (393 px). Walked through MCP enrollment, generated Claude Code JSON, waiting-for-traffic state, a security tour, and Antigravity's narrow layout. Restored desktop hub afterward. The in-app browser host was unavailable; Chrome was actively in use.
- The initial connection implementation passed 31 assertions against the built Cloud application in Happy DOM using isolated local API responses: actual route composition, credential scoping, partial failures and retries, generated app formats, storage exclusions, model discovery, exact verification evidence, resume, feature-tour acknowledgment, theme changes, and header Connect navigation. See `dom-results.json`. This checks DOM behavior, not browser layout.
- Current `npm run test:setup`: 22 tests passed, including migration of legacy implicit server defaults, preservation of existing connections, and explicit choice persistence. Existing getting-started tests: 11 passed. Existing gateway tests: 32 passed.
- Existing built account-page integration checks: 17 passed.
- Shared dashboard, Cloud dashboard, and employee workspace production builds passed. Cloud and employee workspace explicit type checks passed; shared build includes TypeScript checking. Existing large-bundle warnings remain.

## Integration sources

Configuration formats were checked against the fetched official documentation and the local Bastio gateway/MCP contracts:

- [Claude Code gateway setup](https://code.claude.com/docs/en/llm-gateway-connect), [MCP](https://code.claude.com/docs/en/mcp), [custom request headers](https://code.claude.com/docs/en/env-vars).
- [Codex configuration reference](https://developers.openai.com/codex/config-file/config-reference), [configuration schema](https://developers.openai.com/codex/config-schema.json). Custom providers are user-scoped; managed/cloud-coordinated sessions may ignore local provider configuration.
- [Antigravity MCP](https://antigravity.google/docs/mcp/): remote entries use `serverUrl`; setup links users to the editor's raw MCP configuration rather than assuming an old file location.

Live external-app installation, paid provider requests, and production MCP connections were not executed. The implemented verification queries use real Bastio APIs when running against a connected backend. No production authentication bypass, automatic credential rotation, or deployment was added.

## Everyday protection entry, November 11

The hub now starts with conversations/documents in Workspace and existing-app connections as separate paths. The new document tour explains policy actions, extracted attachment text, unsupported image/scanned-PDF coverage, and checking a synthetic interaction in Traces. It records review progress only. Existing app integrations are explicitly described as limited connections, not device-wide protection.

Shared and Cloud production builds passed; the 19 setup tests passed. Eight additional built-Cloud DOM checks passed for the first path, coverage copy, addressable document guide, policy requirement, image/scanned-PDF limitation, configured Cloud Workspace destination, completion wording, and retained advanced MCP navigation. Native Brave observation was available, but further input failed with `elementHasNoFrame` / `noWindowsAvailable`; the revised layout was not visually reverified in this pass. No attachment scanning backend or native-app connector changed.

## App entry correction, November 11

Selecting Claude Code, Codex, or Antigravity now opens a coverage overview before any server questions. The MCP option is collapsed under existing extra tools. It explains that installing the app locally does not imply a local MCP server. Tool setup uses command-versus-address examples, requires an explicit choice, and offers an unsure/no-tools return path. Legacy unstarted drafts return to the overview; real configured connections remain intact.

Both dashboard builds passed. Thirteen built-Cloud DOM checks passed: no initial server question or implicit Continue, native-app limitation, neither location preselected, continuation blocked without a choice, unsure path, model path without MCP questions, API billing disclosure, reaching gateway setup, explicit local choice, local setup without a remote URL, and the equivalent Codex entry. Native Brave successfully refreshed this time; the Claude overview and advanced tool page were visually reviewed, including both unselected radios and disabled Continue. DevTools was closed after checking the preview. No actual provider or MCP request was sent.
