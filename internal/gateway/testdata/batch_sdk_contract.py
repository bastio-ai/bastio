"""Real SDK wire contracts against the Go provider fixtures; no paid API calls."""
import io
import json
import sys
from importlib.metadata import version

import anthropic
from google import genai
from google.genai import types
from openai import OpenAI

base = sys.argv[1]
key = "sk-bastio-test"
text = "sdk-person@example.test"

oa = OpenAI(api_key=key, base_url=base + "/v1", max_retries=0)
line = json.dumps({"custom_id": "sdk-one", "method": "POST", "url": "/v1/chat/completions", "body": {"model": "gpt-4o-mini", "messages": [{"role": "user", "content": text}]}})
uploaded = oa.files.create(file=("batch.jsonl", line.encode(), "application/jsonl"), purpose="batch")
job = oa.batches.create(input_file_id=uploaded.id, endpoint="/v1/chat/completions", completion_window="24h")
job = oa.batches.retrieve(job.id)
assert text in oa.files.content(job.output_file_id).text
assert text in oa.files.content(job.output_file_id).text  # repeat is safe
assert oa.batches.cancel(job.id).id == job.id
assert all(item.id != "not-this-tenant" for item in oa.batches.list().data)

claude = anthropic.Anthropic(api_key=key, base_url=base + "/v1/anthropic", max_retries=0)
job = claude.messages.batches.create(requests=[{"custom_id": "sdk-one", "params": {"model": "claude-sonnet-4-5", "max_tokens": 16, "messages": [{"role": "user", "content": text}]}}])
job = claude.messages.batches.retrieve(job.id)
results = list(claude.messages.batches.results(job.id))
assert results[0].result.message.content[0].text == text
assert claude.messages.batches.cancel(job.id).id == job.id

google = genai.Client(api_key=key, http_options=types.HttpOptions(base_url=base + "/v1/google", api_version="v1beta"))
job = google.batches.create(model="gemini-2.5-flash", src=[{"contents": [{"parts": [{"text": text}]}]}], config={"display_name": "SDK inline"})
assert job.dest.inlined_responses[0].response.text == text
job = google.batches.get(name=job.name)
assert job.dest.inlined_responses[0].response.text == text
google.batches.cancel(name=job.name)
embedding = google.batches.create_embeddings(model="gemini-embedding-001", src={"inlined_requests": {"contents": [text]}}, config={"display_name": "SDK embedding"})
assert embedding.name
google_line = json.dumps({"key": "sdk-one", "request": {"contents": [{"parts": [{"text": text}]}]}}).encode()
uploaded = google.files.upload(file=io.BytesIO(google_line), config={"mime_type": "application/jsonl", "display_name": "SDK file"})
job = google.batches.create(model="gemini-2.5-flash", src=uploaded.name, config={"display_name": "SDK file batch"})
downloaded = google.files.download(file=job.dest.file_name)
assert text.encode() in downloaded, repr(downloaded)

print("SDK batch workflows passed: " + ", ".join(f"{name}={version(name)}" for name in ["openai", "anthropic", "google-genai"]))
