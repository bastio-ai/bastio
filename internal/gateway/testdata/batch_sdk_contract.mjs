import assert from 'node:assert/strict';
import {mkdtemp, readFile, rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import OpenAI from 'openai';
import Anthropic from '@anthropic-ai/sdk';
import {GoogleGenAI} from '@google/genai';

const base = process.argv[2];
const apiKey = 'sk-bastio-test';
const text = 'node-person@example.test';
const openai = new OpenAI({apiKey, baseURL: base + '/v1', maxRetries: 0});
const line = JSON.stringify({custom_id: 'node-one', method: 'POST', url: '/v1/chat/completions', body: {model: 'gpt-4o-mini', messages: [{role: 'user', content: text}]}});
const file = await openai.files.create({file: new File([line], 'batch.jsonl', {type: 'application/jsonl'}), purpose: 'batch'});
let job = await openai.batches.create({input_file_id: file.id, endpoint: '/v1/chat/completions', completion_window: '24h'});
job = await openai.batches.retrieve(job.id);
assert.match(await (await openai.files.content(job.output_file_id)).text(), /node-person@example.test/);
await openai.batches.cancel(job.id);

const claude = new Anthropic({apiKey, baseURL: base + '/v1/anthropic', maxRetries: 0});
job = await claude.messages.batches.create({requests: [{custom_id: 'node-one', params: {model: 'claude-sonnet-4-5', max_tokens: 16, messages: [{role: 'user', content: text}]}}]});
job = await claude.messages.batches.retrieve(job.id);
for await (const item of await claude.messages.batches.results(job.id)) assert.equal(item.result.message.content[0].text, text);
await claude.messages.batches.cancel(job.id);

const google = new GoogleGenAI({apiKey, httpOptions: {baseUrl: base + '/v1/google', apiVersion: 'v1beta'}});
const embedded = await google.models.embedContent({model: 'gemini-embedding-001', contents: [text, 'second document']});
assert.equal(embedded.embeddings.length, 2);
job = await google.batches.create({model: 'gemini-2.5-flash', src: [{contents: [{parts: [{text}]}]}], config: {displayName: 'Node inline'}});
assert.equal(job.dest.inlinedResponses[0].response.candidates[0].content.parts[0].text, text);
job = await google.batches.get({name: job.name});
assert.equal(job.dest.inlinedResponses[0].response.candidates[0].content.parts[0].text, text);
await google.batches.cancel({name: job.name});
const googleLine = JSON.stringify({key: 'node-one', request: {contents: [{parts: [{text}]}]}});
const uploaded = await google.files.upload({file: new Blob([googleLine], {type: 'application/jsonl'}), config: {mimeType: 'application/jsonl', displayName: 'Node file'}});
job = await google.batches.create({model: 'gemini-2.5-flash', src: uploaded.name, config: {displayName: 'Node batch'}});
const directory = await mkdtemp(join(tmpdir(), 'bastio-batch-node-'));
try {
  const output = join(directory, 'result.jsonl');
  await google.files.download({file: job.dest.fileName, downloadPath: output});
  assert.match(await readFile(output, 'utf8'), /node-person@example.test/);
} finally { await rm(directory, {recursive: true, force: true}); }
console.log('OpenAI, Anthropic and Google JavaScript SDK batch workflows passed');
