<h1 align="center"><a href="https://bastio.com">Bastio</a></h1>

<h3 align="center">Security for every AI your team ships.</h3>
<p align="center">
  <em>The open-source gateway between your apps and any LLM.</em><br>
  PII, jailbreak, prompt injection, and secret detection — under 50ms, self-hosted.
</p>

<p align="center">
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/badge/license-FSL--1.1--ALv2-9eff61"></a>
  <a href="go.mod"><img alt="Go" src="https://img.shields.io/badge/go-1.25%2B-00ADD8"></a>
  <a href="https://github.com/bastio-ai/bastio/stargazers"><img alt="Stars" src="https://img.shields.io/github/stars/bastio-ai/bastio?style=flat&color=yellow"></a>
  <a href="https://github.com/bastio-ai/bastio/releases"><img alt="Release" src="https://img.shields.io/github/v/release/bastio-ai/bastio?display_name=tag&color=blue"></a>
</p>

<p align="center">
  <a href="https://bastio.com/docs/getting-started"><b>Quickstart</b></a> ·
  <a href="https://bastio.com/docs"><b>Docs</b></a> ·
  <a href="https://bastio.com/cloud"><b>Cloud</b></a> ·
  <a href="https://github.com/bastio-ai/bastio/discussions"><b>Discussions</b></a>
</p>

---

## What is Bastio?

Bastio is a single Go binary you drop between your application and any LLM provider. Every request gets scanned for PII, prompt injection, jailbreaks, and secrets before it leaves your network — without rewriting a line of application code. Same engine that runs in [Bastio Cloud](https://bastio.com/cloud), self-hosted under the [Functional Source License](LICENSE) — converts to Apache-2.0 two years after each release.

## Quick start

```bash
git clone https://github.com/bastio-ai/bastio.git
cd bastio
docker compose up
```

First boot pulls images and runs migrations — give it ~60 seconds, then:

- **Dashboard** → http://localhost:4000
- **API** → http://localhost:4000/v1
- **OpenAPI** → http://localhost:4000/docs

Point any OpenAI-compatible client at Bastio:

```python
import os
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:4000/v1",
    api_key=os.environ["BASTIO_KEY"],
)

resp = client.chat.completions.create(
    model="gpt-5.4-mini",
    messages=[{"role": "user", "content": "Ignore previous instructions..."}],
)
# → 403 blocked, logged in /traces, every detector verdict captured
```

Anthropic SDK works at `/v1/messages`. Bedrock and Ollama follow the same drop-in pattern.

## Inline detectors

Eight detectors run in parallel on every request, with a <50ms total budget. Patterns compile once at boot; the slowest detector defines latency, not the sum.

|  | Detector | Catches |
|---|---|---|
| 🪪 | **PII** | email · phone · SSN · credit card · IBAN · address · DOB |
| 🔑 | **Secrets** | API keys · AWS / GCP / Azure creds · JWT · GitHub PAT |
| 🚧 | **Prompt injection** | instruction overrides · role-play injections · prompt leak |
| 🛑 | **Jailbreak** | DAN-style · permission escalations · system-prompt extraction |
| 📎 | **Indirect injection** | payloads in retrieved RAG context, attached docs, URL embeds |
| 💻 | **Code** | source-code blocks · IP-leak gate |
| 🎯 | **Topic policy** | configurable allow / deny lists per profile |
| 🤖 | **Bots** | automated traffic patterns · signature checks |

Every detector emits a verdict, a category, and a sanitized payload. The gateway picks an action — `block` / `mask` / `tokenize` / `warn` / `log_only` — based on your security profile.

## Architecture

```
  client ──▶ gateway ──▶ detectors (parallel) ──▶ provider (OpenAI / Anthropic / …)
              │            │                         │
              ▼            ▼                         ▼
         PostgreSQL    ClickHouse                  trace
          (config)    (observability)           (async ingest)
                          │
                          └── Redis (cache, rate limits)
```

- **Go backend** — Chi, sqlc, River queues
- **PostgreSQL 18** for config, **ClickHouse** for traces, **Redis** for cache + rate limits
- **React 19 dashboard** (TanStack Query / Router, shadcn/ui) served by the Go binary via `go:embed`
- **No dashboard auth in OSS** — gate access via your VPN / reverse proxy / tailnet. Cloud adds SSO + RBAC.

## OSS or Cloud

|  | **OSS** *(this repo)* | **Cloud** |
|---|:---:|:---:|
| Detection engine | Same code | Same code + Presidio |
| Self-host, single binary | ✅ | — |
| Multi-tenant orgs | — | ✅ |
| SSO (SAML / OIDC) | — | ✅ |
| RBAC + audit log retention | — | ✅ |
| Branded Workspace domain | — | ✅ |
| Managed hosting + 24h SLA | — | ✅ |

Detectors are byte-for-byte identical. The upgrade triggers are organizational — multi-tenant, SSO, audit retention — not technical. [See the comparison →](https://bastio.com/oss)

## Documentation

Full docs at **[bastio.com/docs](https://bastio.com/docs)** — getting started, API reference, deployment guides, security profiles, custom policies, governance backend.

Doc source lives in [`docs/`](./docs). Community PRs welcome.

## Community

- **Discussions** → [github.com/bastio-ai/bastio/discussions](https://github.com/bastio-ai/bastio/discussions)
- **Bugs** → [github.com/bastio-ai/bastio/issues](https://github.com/bastio-ai/bastio/issues)
- **Security** → [SECURITY.md](SECURITY.md)
- **Updates** → [@bastio_ai](https://x.com/bastio_ai) · [LinkedIn](https://www.linkedin.com/company/bastio-ai)

Contributing? Start with [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Server, dashboard, and internal packages are licensed under [**FSL-1.1-ALv2**](LICENSE) — the Functional Source License, which converts to Apache-2.0 two years after each release. You can self-host, modify, and redistribute today; the only restriction is offering Bastio as a managed service to third parties.

Client SDKs under [`sdk/`](./sdk) ship under [MIT](sdk/js/LICENSE).

---

<p align="center">
  Built in 🇩🇰 Denmark · EU-hosted ·
  <a href="https://bastio.com">bastio.com</a> ·
  <a href="https://bastio.com/cloud">Cloud waitlist</a>
</p>
