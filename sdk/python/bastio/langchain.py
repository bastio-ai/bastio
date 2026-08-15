"""Bastio LangChain & LangGraph Guardrail Callback Handler."""

from __future__ import annotations

import time
from typing import Any, Callable, Optional, Union
from uuid import UUID

from bastio.client import Bastio
from bastio.errors import BastioBlockedError, BastioSecurityException

# Try importing LangChain base callback handler, fallback gracefully if not installed
try:
    from langchain_core.callbacks import BaseCallbackHandler
except ImportError:
    try:
        from langchain.callbacks.base import BaseCallbackHandler  # type: ignore
    except ImportError:
        class BaseCallbackHandler:  # type: ignore
            """Fallback duck-typed callback handler if langchain is not installed."""
            pass


class BastioGuardrailCallbackHandler(BaseCallbackHandler):
    """Bastio security callback handler for LangChain and LangGraph.

    Intercepts prompt injection attacks, jailbreaks, PII leakage, and unauthorized
    agent tool calls before they execute. Records traces and spans for security telemetry.

    Example:
        ```python
        from langchain_openai import ChatOpenAI
        from bastio.langchain import BastioGuardrailCallbackHandler

        guardrail = BastioGuardrailCallbackHandler(
            profile="production-guard",
            block_on_threat=True,
        )

        llm = ChatOpenAI(
            model="gpt-4o",
            callbacks=[guardrail],
        )

        # Will raise BastioSecurityException if prompt contains an injection attack
        response = llm.invoke("Summarize user feedback")
        ```
    """

    def __init__(
        self,
        *,
        base_url: Optional[str] = None,
        api_key: Optional[str] = None,
        profile: Optional[str] = None,
        security_profile: Optional[str] = None,
        steps: Optional[list[dict[str, Any]]] = None,
        block_on_threat: bool = True,
        scan_output: bool = True,
        scan_tools: bool = True,
        on_decision: Optional[Callable[[str, dict[str, Any]], None]] = None,
        client: Optional[Bastio] = None,
    ) -> None:
        super().__init__()
        self.profile = profile or security_profile
        self.steps = steps
        self.block_on_threat = block_on_threat
        self.scan_output = scan_output
        self.scan_tools = scan_tools
        self.on_decision = on_decision

        if client is not None:
            self.client = client
        else:
            kwargs: dict[str, Any] = {}
            if base_url:
                kwargs["base_url"] = base_url
            if api_key:
                kwargs["api_key"] = api_key
            self.client = Bastio(**kwargs)

        # In-memory span recording for observability
        self.spans: list[dict[str, Any]] = []
        self._active_runs: dict[str, dict[str, Any]] = {}

    def on_llm_start(
        self,
        serialized: dict[str, Any],
        prompts: list[str],
        *,
        run_id: Optional[UUID] = None,
        parent_run_id: Optional[UUID] = None,
        tags: Optional[list[str]] = None,
        metadata: Optional[dict[str, Any]] = None,
        **kwargs: Any,
    ) -> Any:
        """Scan input prompts for prompt injection, jailbreaks, and PII before LLM invocation."""
        run_key = str(run_id) if run_id else "default"
        start_time = time.time()

        messages = [{"role": "user", "content": prompt} for prompt in prompts if prompt]
        if not messages:
            return

        result = self.client.detect(
            messages=messages,
            profile=self.profile,
            direction="input",
            steps=self.steps,
        )

        if self.on_decision:
            self.on_decision("input", result)

        should_block = result.get("should_block", False) or result.get("action") == "block"

        span = {
            "run_id": run_key,
            "parent_run_id": str(parent_run_id) if parent_run_id else None,
            "stage": "input",
            "start_time": start_time,
            "duration": time.time() - start_time,
            "action": result.get("action", "pass"),
            "should_block": should_block,
            "findings": self._extract_findings(result),
        }
        self.spans.append(span)
        self._active_runs[run_key] = span

        if should_block and self.block_on_threat:
            threat_msg = self._format_threat_message(result)
            raise BastioSecurityException(
                message=f"Prompt blocked by Bastio security policy: {threat_msg}",
                result=result,
            )

    def on_chat_model_start(
        self,
        serialized: dict[str, Any],
        messages: list[list[Any]],
        *,
        run_id: Optional[UUID] = None,
        parent_run_id: Optional[UUID] = None,
        tags: Optional[list[str]] = None,
        metadata: Optional[dict[str, Any]] = None,
        **kwargs: Any,
    ) -> Any:
        """Scan chat messages before invoking a chat model."""
        flat_messages: list[dict[str, str]] = []
        for message_group in messages:
            for msg in message_group:
                content = getattr(msg, "content", "")
                role = getattr(msg, "type", "user")
                if isinstance(content, str) and content:
                    flat_messages.append({"role": role, "content": content})
                elif isinstance(content, list):
                    text_parts = [
                        p if isinstance(p, str) else p.get("text", "")
                        for p in content
                        if isinstance(p, (str, dict))
                    ]
                    combined = " ".join(filter(None, text_parts))
                    if combined:
                        flat_messages.append({"role": role, "content": combined})

        if not flat_messages:
            return

        run_key = str(run_id) if run_id else "default"
        start_time = time.time()

        result = self.client.detect(
            messages=flat_messages,
            profile=self.profile,
            direction="input",
            steps=self.steps,
        )

        if self.on_decision:
            self.on_decision("input", result)

        should_block = result.get("should_block", False) or result.get("action") == "block"

        span = {
            "run_id": run_key,
            "parent_run_id": str(parent_run_id) if parent_run_id else None,
            "stage": "input",
            "start_time": start_time,
            "duration": time.time() - start_time,
            "action": result.get("action", "pass"),
            "should_block": should_block,
            "findings": self._extract_findings(result),
        }
        self.spans.append(span)
        self._active_runs[run_key] = span

        if should_block and self.block_on_threat:
            threat_msg = self._format_threat_message(result)
            raise BastioSecurityException(
                message=f"Chat messages blocked by Bastio security policy: {threat_msg}",
                result=result,
            )

    def on_llm_end(
        self,
        response: Any,
        *,
        run_id: Optional[UUID] = None,
        parent_run_id: Optional[UUID] = None,
        **kwargs: Any,
    ) -> Any:
        """Scan LLM generated output for PII leakage and unsafe responses."""
        if not self.scan_output:
            return

        output_texts: list[str] = []
        generations = getattr(response, "generations", [])
        for gen_list in generations:
            for gen in gen_list:
                text = getattr(gen, "text", "")
                if not text and hasattr(gen, "message"):
                    text = getattr(gen.message, "content", "")
                if isinstance(text, str) and text:
                    output_texts.append(text)

        if not output_texts:
            return

        run_key = str(run_id) if run_id else "default"
        start_time = time.time()

        messages = [{"role": "assistant", "content": t} for t in output_texts]
        result = self.client.detect(
            messages=messages,
            profile=self.profile,
            direction="output",
            steps=self.steps,
        )

        if self.on_decision:
            self.on_decision("output", result)

        should_block = result.get("should_block", False) or result.get("action") == "block"

        span = {
            "run_id": run_key,
            "stage": "output",
            "start_time": start_time,
            "duration": time.time() - start_time,
            "action": result.get("action", "pass"),
            "should_block": should_block,
            "findings": self._extract_findings(result),
        }
        self.spans.append(span)

        if should_block and self.block_on_threat:
            threat_msg = self._format_threat_message(result)
            raise BastioSecurityException(
                message=f"LLM output blocked by Bastio security policy: {threat_msg}",
                result=result,
            )

    def on_tool_start(
        self,
        serialized: dict[str, Any],
        input_str: str,
        *,
        run_id: Optional[UUID] = None,
        parent_run_id: Optional[UUID] = None,
        tags: Optional[list[str]] = None,
        metadata: Optional[dict[str, Any]] = None,
        **kwargs: Any,
    ) -> Any:
        """Inspect proposed tool call arguments for dangerous commands or path traversal."""
        if not self.scan_tools:
            return

        tool_name = serialized.get("name") or serialized.get("id") or "unknown_tool"
        run_key = str(run_id) if run_id else "default"
        start_time = time.time()

        # Parse tool arguments if string
        arguments: dict[str, Any] = {"input": input_str}

        result = self.client.inspect_agent_action(
            tool_name=tool_name,
            arguments=arguments,
        )

        allowed = result.get("allowed", True)
        action = result.get("action", "allow")
        should_block = not allowed or action == "block"

        span = {
            "run_id": run_key,
            "stage": "tool_action",
            "tool_name": tool_name,
            "start_time": start_time,
            "duration": time.time() - start_time,
            "action": action,
            "allowed": allowed,
            "risk_score": result.get("risk_score", 0.0),
            "reasons": result.get("reasons", []),
        }
        self.spans.append(span)

        if should_block and self.block_on_threat:
            reasons = ", ".join(result.get("reasons", [])) or "unauthorized tool execution"
            raise BastioSecurityException(
                message=f"Agent tool action '{tool_name}' blocked by Bastio: {reasons}",
                result=result,
            )

    def on_llm_error(
        self,
        error: BaseException,
        *,
        run_id: Optional[UUID] = None,
        parent_run_id: Optional[UUID] = None,
        **kwargs: Any,
    ) -> Any:
        """Record LLM error span."""
        run_key = str(run_id) if run_id else "default"
        self.spans.append({
            "run_id": run_key,
            "stage": "llm_error",
            "error": str(error),
            "timestamp": time.time(),
        })

    def on_chain_error(
        self,
        error: BaseException,
        *,
        run_id: Optional[UUID] = None,
        parent_run_id: Optional[UUID] = None,
        **kwargs: Any,
    ) -> Any:
        """Record chain error span."""
        run_key = str(run_id) if run_id else "default"
        self.spans.append({
            "run_id": run_key,
            "stage": "chain_error",
            "error": str(error),
            "timestamp": time.time(),
        })

    def on_tool_error(
        self,
        error: BaseException,
        *,
        run_id: Optional[UUID] = None,
        parent_run_id: Optional[UUID] = None,
        **kwargs: Any,
    ) -> Any:
        """Record tool error span."""
        run_key = str(run_id) if run_id else "default"
        self.spans.append({
            "run_id": run_key,
            "stage": "tool_error",
            "error": str(error),
            "timestamp": time.time(),
        })

    def _extract_findings(self, result: dict[str, Any]) -> list[dict[str, Any]]:
        findings = []
        for msg in result.get("messages", []):
            for step in msg.get("steps", []):
                if step.get("findings"):
                    findings.extend(step["findings"])
        return findings

    def _format_threat_message(self, result: dict[str, Any]) -> str:
        findings = self._extract_findings(result)
        if findings:
            summaries = []
            for f in findings:
                threat = f.get("threat_type", "threat")
                score = f.get("score")
                msg = f.get("message") or f"{threat} detected"
                if score is not None:
                    summaries.append(f"{msg} (score: {score:.2f})")
                else:
                    summaries.append(msg)
            return "; ".join(summaries)
        return "threat detected by security profile"
