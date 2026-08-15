"""Bastio client — drop-in mode for OpenAI SDK, or enhanced mode for security APIs."""

from __future__ import annotations

import json
import os
import urllib.error
import urllib.request
from dataclasses import dataclass, field
from typing import Any, Optional

from bastio.errors import BastioBlockedError, BastioError


@dataclass
class Bastio:
    """Bastio client.

    Drop-in mode (recommended): Use with the OpenAI SDK by setting base_url.

        from openai import OpenAI
        client = OpenAI(
            base_url="http://localhost:4000/v1",
            api_key="sk-bastio-..."
        )

    Enhanced mode: Use the Bastio client directly for security-specific APIs.

        from bastio import Bastio
        bastio = Bastio(base_url="http://localhost:4000", api_key="sk-bastio-...")
        res = bastio.detect([{"role": "user", "content": "Hello"}])
    """

    base_url: str = field(
        default_factory=lambda: os.getenv("BASTIO_URL") or os.getenv("BASTIO_BASE_URL") or "http://localhost:4000"
    )
    api_key: Optional[str] = field(
        default_factory=lambda: os.getenv("BASTIO_API_KEY") or os.getenv("BASTIO_KEY")
    )
    timeout: float = 10.0
    headers: dict[str, str] = field(default_factory=dict)

    def __post_init__(self) -> None:
        self.base_url = self.base_url.rstrip("/")
        self._headers = {
            "User-Agent": "bastio-sdk-python",
            **self.headers,
        }
        if self.api_key:
            self._headers["Authorization"] = f"Bearer {self.api_key}"

    @property
    def openai_base_url(self) -> str:
        """Base URL for use with the OpenAI SDK."""
        return f"{self.base_url}/v1"

    def health(self) -> dict[str, Any]:
        """Check gateway health status."""
        return self._get("/health")

    def traces(self, limit: int = 50) -> Any:
        """List recent request traces."""
        return self._get(f"/v1/traces?limit={limit}")

    def threats(self) -> Any:
        """List recent threat events."""
        return self._get("/v1/threats")

    def proxies(self) -> Any:
        """List configured proxies."""
        return self._get("/v1/proxies")

    def create_proxy(self, name: str, provider: str, model: str = "") -> Any:
        """Create a new proxy."""
        return self._post("/v1/proxies", {"name": name, "target_provider": provider, "target_model": model})

    def detect(
        self,
        messages: list[dict[str, str]],
        *,
        profile: Optional[str] = None,
        direction: str = "input",
        steps: Optional[list[dict[str, Any]]] = None,
    ) -> dict[str, Any]:
        """Run the configured security profile against messages.

        Args:
            messages: List of message objects, e.g. [{"role": "user", "content": "hello"}]
            profile: Named Bastio security profile (omit for default)
            direction: "input" or "output"
            steps: Optional inline detection step overrides

        Returns:
            DetectResponse dict containing should_block, action, messages with sanitized_content
        """
        payload: dict[str, Any] = {
            "messages": messages,
            "direction": direction,
        }
        if profile:
            payload["profile"] = profile
        if steps is not None:
            payload["steps"] = steps

        return self._post("/v1/detect", payload)

    def scan(
        self,
        text: str,
        *,
        profile: Optional[str] = None,
        direction: str = "input",
        role: str = "user",
    ) -> dict[str, Any]:
        """Convenience method to scan a single text string.

        Args:
            text: Text to scan
            profile: Optional security profile
            direction: "input" or "output"
            role: "user", "system", or "assistant"

        Returns:
            DetectResponse dict
        """
        return self.detect(
            [{"role": role, "content": text}],
            profile=profile,
            direction=direction,
        )

    def mask_pii(
        self,
        text: str,
        *,
        mode: str = "tokenize",
        bypass_cache: bool = False,
    ) -> dict[str, Any]:
        """Redact or tokenize PII and sensitive secrets in text.

        Args:
            text: Input text containing potential PII or secrets
            mode: "tokenize" (reversible), "mask" (obfuscate ***), or "redact" ([REDACTED])
            bypass_cache: Set True to bypass the response cache

        Returns:
            dict containing processed_text, tokens, detected_types
        """
        payload = {
            "text": text,
            "mode": mode,
            "bypass_cache": bypass_cache,
        }
        return self._post("/v1/pii/mask", payload)

    def unmask_pii(
        self,
        text: str,
        tokens: dict[str, str],
    ) -> dict[str, Any]:
        """Restore synthetic PII tokens with their original values in LLM output.

        Args:
            text: Output text containing synthetic tokens (e.g. [EMAIL_1])
            tokens: Token dictionary returned from mask_pii

        Returns:
            dict containing unmasked_text
        """
        payload = {
            "text": text,
            "tokens": tokens,
        }
        return self._post("/v1/pii/unmask", payload)

    def inspect_agent_action(
        self,
        tool_name: str,
        arguments: dict[str, Any],
        *,
        agent_role: Optional[str] = None,
        context: Optional[str] = None,
    ) -> dict[str, Any]:
        """Inspect proposed tool call arguments for dangerous commands or path traversal.

        Args:
            tool_name: Name of the tool/function to be executed
            arguments: Dictionary of arguments passed to the tool
            agent_role: Optional role description of the agent
            context: Optional execution context

        Returns:
            AgentActionResponse dict containing allowed, action, risk_score, reasons, sanitized_arguments
        """
        payload: dict[str, Any] = {
            "tool_name": tool_name,
            "arguments": arguments,
        }
        if agent_role:
            payload["agent_role"] = agent_role
        if context:
            payload["context"] = context

        return self._post("/v1/guardrails/agent-action", payload)

    def _get(self, path: str) -> Any:
        url = f"{self.base_url}{path}"
        req = urllib.request.Request(url, headers=self._headers)
        return self._send_request(req)

    def _post(self, path: str, data: dict[str, Any]) -> Any:
        url = f"{self.base_url}{path}"
        body = json.dumps(data).encode("utf-8")
        headers = {**self._headers, "Content-Type": "application/json"}
        req = urllib.request.Request(url, data=body, headers=headers, method="POST")
        return self._send_request(req)

    def _send_request(self, req: urllib.request.Request) -> Any:
        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                status = resp.status
                raw = resp.read().decode("utf-8")
                try:
                    return json.loads(raw)
                except Exception:
                    return raw
        except urllib.error.HTTPError as e:
            err_body = e.read().decode("utf-8", errors="replace")
            parsed = None
            try:
                parsed = json.loads(err_body)
            except Exception:
                pass
            raise BastioError(
                message=f"Bastio request failed with status {e.code}: {e.reason}",
                status_code=e.code,
                details=parsed or err_body,
            ) from e
        except urllib.error.URLError as e:
            raise BastioError(f"Bastio network error: {e.reason}") from e
        except Exception as e:
            raise BastioError(f"Bastio transport error: {str(e)}") from e
