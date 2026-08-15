"""Bastio Security Middleware for FastAPI and ASGI frameworks."""

from __future__ import annotations

import json
from typing import Any, Callable, Optional, Sequence

from bastio.client import Bastio
from bastio.errors import BastioBlockedError


class BastioSecurityMiddleware:
    """ASGI / FastAPI security middleware for inline threat scanning and prompt injection defense.

    Intercepts incoming HTTP request bodies, screens prompts/messages via Bastio's
    threat detection engine, and rejects malicious payloads with HTTP 403 Forbidden.

    Example with FastAPI:
        ```python
        from fastapi import FastAPI
        from bastio.fastapi import BastioSecurityMiddleware

        app = FastAPI()

        app.add_middleware(
            BastioSecurityMiddleware,
            base_url="http://localhost:4000",
            api_key="sk-bastio-...",
            profile="production-guard",
            block_on_injection=True,
        )

        @app.post("/chat/completions")
        async def chat(request: dict):
            return {"response": "Hello!"}
        ```
    """

    def __init__(
        self,
        app: Any,
        *,
        base_url: Optional[str] = None,
        api_key: Optional[str] = None,
        profile: Optional[str] = None,
        security_profile: Optional[str] = None,
        steps: Optional[list[dict[str, Any]]] = None,
        block_on_injection: bool = True,
        paths: Optional[Sequence[str]] = None,
        exclude_paths: Optional[Sequence[str]] = None,
        on_violation: Optional[Callable[[dict[str, Any], dict[str, Any]], None]] = None,
        client: Optional[Bastio] = None,
    ) -> None:
        self.app = app
        self.profile = profile or security_profile
        self.steps = steps
        self.block_on_injection = block_on_injection
        self.paths = set(paths) if paths is not None else None
        self.exclude_paths = set(exclude_paths) if exclude_paths is not None else {
            "/health", "/docs", "/redoc", "/openapi.json", "/favicon.ico"
        }
        self.on_violation = on_violation

        if client is not None:
            self.client = client
        else:
            kwargs: dict[str, Any] = {}
            if base_url:
                kwargs["base_url"] = base_url
            if api_key:
                kwargs["api_key"] = api_key
            self.client = Bastio(**kwargs)

    async def __call__(
        self,
        scope: dict[str, Any],
        receive: Callable[[], Any],
        send: Callable[[dict[str, Any]], Any],
    ) -> None:
        if scope["type"] != "http":
            await self.app(scope, receive, send)
            return

        method = scope.get("method", "GET").upper()
        path = scope.get("path", "")

        # Skip paths that don't need scanning
        if path in self.exclude_paths or method not in ("POST", "PUT", "PATCH"):
            await self.app(scope, receive, send)
            return

        # If specific paths are configured, check against them
        if self.paths is not None and path not in self.paths:
            await self.app(scope, receive, send)
            return

        # Buffer the incoming request body with max size guard
        body_chunks: list[bytes] = []
        body_size = 0
        max_size = 10 * 1024 * 1024  # 10MB default
        more_body = True

        while more_body:
            message = await receive()
            message_type = message.get("type", "")
            if message_type == "http.request":
                chunk = message.get("body", b"")
                if chunk:
                    body_size += len(chunk)
                    if body_size > max_size:
                        await send({
                            "type": "http.response.start",
                            "status": 413,
                            "headers": [(b"content-type", b"application/json")],
                        })
                        await send({
                            "type": "http.response.body",
                            "body": b'{"error":"payload_too_large","message":"Request body exceeds 10MB limit"}',
                        })
                        return
                    body_chunks.append(chunk)
                more_body = message.get("more_body", False)
            elif message_type == "http.disconnect":
                return

        raw_body = b"".join(body_chunks)

        # Inspect body for prompts/messages
        messages_to_scan = self._extract_messages(raw_body, scope)

        if messages_to_scan:
            try:
                result = self.client.detect(
                    messages=messages_to_scan,
                    profile=self.profile,
                    direction="input",
                    steps=self.steps,
                )
            except Exception:
                # If Bastio server is unreachable, pass through gracefully
                result = {"action": "allow", "threats": []}

            should_block = result.get("should_block", False) or result.get("action") == "block"

            if should_block and self.block_on_injection:
                if self.on_violation:
                    try:
                        parsed_body = json.loads(raw_body.decode("utf-8"))
                    except Exception:
                        parsed_body = {"raw": raw_body.decode("utf-8", errors="replace")}
                    self.on_violation(result, parsed_body)

                await self._send_blocked_response(send, result)
                return

        # Replay the request body to downstream handlers
        body_sent = False

        async def replay_receive() -> dict[str, Any]:
            nonlocal body_sent
            if not body_sent:
                body_sent = True
                return {
                    "type": "http.request",
                    "body": raw_body,
                    "more_body": False,
                }
            # Delegate subsequent receive() calls to the original ASGI receive
            return await receive()

        async def wrapped_send(message: dict[str, Any]) -> None:
            if message.get("type") == "http.response.start":
                headers = list(message.get("headers", []))
                headers.append((b"x-bastio-protected", b"true"))
                headers.append((b"x-bastio-scan", b"passed"))
                message = {**message, "headers": headers}
            await send(message)

        await self.app(scope, replay_receive, wrapped_send)

    def _extract_messages(self, raw_body: bytes, scope: dict[str, Any]) -> list[dict[str, str]]:
        if not raw_body:
            return []

        try:
            data = json.loads(raw_body.decode("utf-8"))
        except Exception:
            # If not JSON, scan as raw text if plain text Content-Type
            text = raw_body.decode("utf-8", errors="ignore").strip()
            if text:
                return [{"role": "user", "content": text}]
            return []

        if not isinstance(data, dict):
            return []

        extracted: list[dict[str, str]] = []

        # 1. OpenAI / Anthropic format: messages: [{"role": ..., "content": ...}]
        if "messages" in data and isinstance(data["messages"], list):
            for m in data["messages"]:
                if isinstance(m, dict):
                    role = str(m.get("role", "user"))
                    content = m.get("content", "")
                    if isinstance(content, str) and content:
                        extracted.append({"role": role, "content": content})
                    elif isinstance(content, list):
                        parts = [
                            p.get("text", "") if isinstance(p, dict) else str(p)
                            for p in content
                        ]
                        joined = " ".join(filter(None, parts))
                        if joined:
                            extracted.append({"role": role, "content": joined})

        # 2. Direct prompt / text / query fields
        for field_name in ("prompt", "text", "query", "input", "message", "content"):
            if field_name in data and isinstance(data[field_name], str):
                extracted.append({"role": "user", "content": data[field_name]})

        return extracted

    async def _send_blocked_response(
        self,
        send: Callable[[dict[str, Any]], Any],
        result: dict[str, Any],
    ) -> None:
        findings = []
        for msg in result.get("messages", []):
            for step in msg.get("steps", []):
                if step.get("findings"):
                    findings.extend(step["findings"])

        error_response = {
            "error": {
                "message": "Request blocked by Bastio AI Security Gateway: threat or policy violation detected",
                "type": "bastio_security_violation",
                "code": "threat_detected",
                "action": result.get("action", "block"),
                "profile": result.get("profile", self.profile or "default"),
                "findings": findings,
            }
        }

        body_bytes = json.dumps(error_response).encode("utf-8")

        await send({
            "type": "http.response.start",
            "status": 403,
            "headers": [
                (b"content-type", b"application/json"),
                (b"content-length", str(len(body_bytes)).encode("ascii")),
                (b"x-bastio-protected", b"true"),
                (b"x-bastio-scan", b"blocked"),
            ],
        })

        await send({
            "type": "http.response.body",
            "body": body_bytes,
            "more_body": False,
        })
