# Bastio — Python SDK

Drop-in client for the [Bastio](https://github.com/bastio-ai/bastio) AI security gateway.

## Install

```bash
pip install bastio
```

## Drop-in mode (recommended)

Bastio is OpenAI-compatible. Point the OpenAI SDK at your Bastio gateway and every request is scanned for injections, jailbreaks, PII, and policy violations before reaching the upstream provider.

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:4000/v1",
    api_key="sk-bastio-...",
)

response = client.chat.completions.create(
    model="gpt-4o",
    messages=[{"role": "user", "content": "Hello"}],
)
```

## Enhanced mode

Use the `Bastio` client directly to call security-specific endpoints (traces, threats, proxies).

```python
from bastio import Bastio

bastio = Bastio(base_url="http://localhost:4000", api_key="sk-bastio-...")

print(bastio.health())
print(bastio.threats())
print(bastio.traces(limit=20))
```

## License

MIT — see [LICENSE](../../LICENSE) at the repo root for the FSL terms covering the gateway itself; SDKs are MIT.
