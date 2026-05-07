"""Bastio client — drop-in mode for OpenAI SDK, or enhanced mode for security APIs."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Optional


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
    """

    base_url: str = "http://localhost:4000"
    api_key: Optional[str] = None

    def __post_init__(self) -> None:
        self.base_url = self.base_url.rstrip("/")
        self._headers = {}
        if self.api_key:
            self._headers["Authorization"] = f"Bearer {self.api_key}"

    @property
    def openai_base_url(self) -> str:
        """Base URL for use with the OpenAI SDK."""
        return f"{self.base_url}/v1"

    def health(self) -> dict:
        """Check gateway health status."""
        import urllib.request
        import json

        req = urllib.request.Request(f"{self.base_url}/health")
        with urllib.request.urlopen(req) as resp:
            return json.loads(resp.read())

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

    def _get(self, path: str) -> Any:
        import urllib.request
        import json

        req = urllib.request.Request(f"{self.base_url}{path}", headers=self._headers)
        with urllib.request.urlopen(req) as resp:
            return json.loads(resp.read())

    def _post(self, path: str, data: dict) -> Any:
        import urllib.request
        import json

        body = json.dumps(data).encode()
        headers = {**self._headers, "Content-Type": "application/json"}
        req = urllib.request.Request(f"{self.base_url}{path}", data=body, headers=headers, method="POST")
        with urllib.request.urlopen(req) as resp:
            return json.loads(resp.read())
