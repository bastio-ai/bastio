"""Bastio Python SDK Exceptions."""

from __future__ import annotations

from typing import Any, Optional


class BastioError(Exception):
    """Base exception for all Bastio SDK errors."""

    def __init__(self, message: str, status_code: Optional[int] = None, details: Any = None) -> None:
        super().__init__(message)
        self.message = message
        self.status_code = status_code
        self.details = details

    def __str__(self) -> str:
        if self.status_code:
            return f"BastioError({self.status_code}): {self.message}"
        return f"BastioError: {self.message}"


class BastioBlockedError(BastioError):
    """Raised when a request or model completion is blocked by a Bastio security policy.

    Attributes:
        result: The full DetectResponse or AgentActionResponse dictionary.
        action: The security action taken (e.g. 'block').
        profile: The security profile that triggered the block.
        findings: List of threat findings associated with the block.
    """

    def __init__(self, message: str, result: Optional[dict[str, Any]] = None) -> None:
        super().__init__(message, status_code=403, details=result)
        self.result = result or {}
        self.action = self.result.get("action", "block")
        self.profile = self.result.get("profile", "")
        self.findings = self._extract_findings()

    def _extract_findings(self) -> list[dict[str, Any]]:
        findings = []
        messages = self.result.get("messages", [])
        for msg in messages:
            for step in msg.get("steps", []):
                if step.get("findings"):
                    findings.extend(step["findings"])
        return findings

    def __str__(self) -> str:
        findings_summary = ""
        if self.findings:
            threat_types = {f.get("threat_type") for f in self.findings if f.get("threat_type")}
            findings_summary = f" [Threats: {', '.join(filter(None, threat_types))}]"
        return f"BastioBlockedError: {self.message}{findings_summary}"


# Backward-compatible and semantic alias
BastioSecurityException = BastioBlockedError
