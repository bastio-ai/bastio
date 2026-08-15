"""Unit tests for Bastio Python Client."""

import json
import unittest
from unittest.mock import MagicMock, patch
from urllib.error import HTTPError

from bastio.client import Bastio
from bastio.errors import BastioBlockedError, BastioError


class TestBastioClient(unittest.TestCase):
    def test_init_defaults(self):
        client = Bastio(base_url="http://localhost:4000/", api_key="sk-test-123")
        self.assertEqual(client.base_url, "http://localhost:4000")
        self.assertEqual(client.openai_base_url, "http://localhost:4000/v1")
        self.assertEqual(client._headers["Authorization"], "Bearer sk-test-123")
        self.assertEqual(client._headers["User-Agent"], "bastio-sdk-python")

    @patch("urllib.request.urlopen")
    def test_detect_success(self, mock_urlopen):
        mock_resp = MagicMock()
        mock_resp.status = 200
        mock_resp.read.return_value = json.dumps({
            "profile": "production-guard",
            "direction": "input",
            "action": "pass",
            "should_block": False,
            "messages": [
                {
                    "role": "user",
                    "original": "hello",
                    "sanitized_content": "hello",
                    "action": "pass",
                    "should_block": False,
                    "steps": [],
                }
            ],
        }).encode("utf-8")
        mock_resp.__enter__.return_value = mock_resp
        mock_urlopen.return_value = mock_resp

        client = Bastio(base_url="http://localhost:4000")
        res = client.detect([{"role": "user", "content": "hello"}], profile="production-guard")

        self.assertEqual(res["action"], "pass")
        self.assertFalse(res["should_block"])
        self.assertEqual(res["messages"][0]["sanitized_content"], "hello")

    @patch("urllib.request.urlopen")
    def test_scan_convenience(self, mock_urlopen):
        mock_resp = MagicMock()
        mock_resp.status = 200
        mock_resp.read.return_value = json.dumps({
            "action": "pass",
            "should_block": False,
            "messages": [],
        }).encode("utf-8")
        mock_resp.__enter__.return_value = mock_resp
        mock_urlopen.return_value = mock_resp

        client = Bastio(base_url="http://localhost:4000")
        res = client.scan("Test query")
        self.assertEqual(res["action"], "pass")

    @patch("urllib.request.urlopen")
    def test_mask_and_unmask_pii(self, mock_urlopen):
        mock_resp = MagicMock()
        mock_resp.status = 200
        mock_resp.read.return_value = json.dumps({
            "processed_text": "Contact [EMAIL_1]",
            "tokens": {"[EMAIL_1]": "test@example.com"},
            "detected_types": ["EMAIL"],
        }).encode("utf-8")
        mock_resp.__enter__.return_value = mock_resp
        mock_urlopen.return_value = mock_resp

        client = Bastio(base_url="http://localhost:4000")
        mask_res = client.mask_pii("Contact test@example.com")
        self.assertEqual(mask_res["processed_text"], "Contact [EMAIL_1]")
        self.assertEqual(mask_res["tokens"]["[EMAIL_1]"], "test@example.com")

        # Test unmask
        mock_resp.read.return_value = json.dumps({
            "unmasked_text": "Contact test@example.com",
        }).encode("utf-8")

        unmask_res = client.unmask_pii(mask_res["processed_text"], mask_res["tokens"])
        self.assertEqual(unmask_res["unmasked_text"], "Contact test@example.com")

    @patch("urllib.request.urlopen")
    def test_inspect_agent_action(self, mock_urlopen):
        mock_resp = MagicMock()
        mock_resp.status = 200
        mock_resp.read.return_value = json.dumps({
            "allowed": False,
            "action": "block",
            "risk_score": 0.95,
            "reasons": ["Dangerous command: rm -rf"],
        }).encode("utf-8")
        mock_resp.__enter__.return_value = mock_resp
        mock_urlopen.return_value = mock_resp

        client = Bastio(base_url="http://localhost:4000")
        res = client.inspect_agent_action(
            tool_name="bash_exec",
            arguments={"command": "rm -rf /"},
        )
        self.assertFalse(res["allowed"])
        self.assertEqual(res["action"], "block")
        self.assertIn("Dangerous command", res["reasons"][0])

    @patch("urllib.request.urlopen")
    def test_http_error_raises_bastio_error(self, mock_urlopen):
        fp = MagicMock()
        fp.read.return_value = json.dumps({"error": "Invalid API key"}).encode("utf-8")
        mock_urlopen.side_effect = HTTPError("http://localhost:4000/v1/detect", 401, "Unauthorized", {}, fp)

        client = Bastio(base_url="http://localhost:4000")
        with self.assertRaises(BastioError) as ctx:
            client.detect([{"role": "user", "content": "hi"}])

        self.assertEqual(ctx.exception.status_code, 401)


if __name__ == "__main__":
    unittest.main()
