'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { EventEmitter } = require('node:events');

const handler = require('../../api/chat-stream.js');
const { handleVercelStream } = require('../../internal/js/chat-stream/vercel_stream.js');

const {
  parseChunkForContent,
  extractResponseMessageID,
  stripReferenceMarkers,
  trimContinuationOverlap,
  isNodeStreamSupportedPath,
  extractPathname,
} = handler.__test;

// ─── harness ─────────────────────────────────────────────────────────

class MockStreamRequest extends EventEmitter {
  constructor() {
    super();
    this.url = '/v1/chat/completions';
    this.headers = { host: 'example.test', 'content-type': 'application/json' };
  }
}

class MockStreamResponse extends EventEmitter {
  constructor() {
    super();
    this.headers = new Map();
    this.statusCode = 0;
    this.chunks = [];
    this.writableEnded = false;
    this.destroyed = false;
  }

  setHeader(key, value) {
    this.headers.set(String(key).toLowerCase(), value);
  }

  getHeader(key) {
    return this.headers.get(String(key).toLowerCase());
  }

  write(chunk) {
    this.chunks.push(Buffer.isBuffer(chunk) ? chunk.toString('utf8') : String(chunk));
    return true;
  }

  end(chunk) {
    if (chunk) {
      this.write(chunk);
    }
    this.writableEnded = true;
  }

  flushHeaders() {}

  flush() {}

  bodyText() {
    return this.chunks.join('');
  }
}

function jsonResponse(body, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  });
}

function sseResponse(lines) {
  const encoder = new TextEncoder();
  return new Response(new ReadableStream({
    start(controller) {
      for (const line of lines) {
        controller.enqueue(encoder.encode(line));
      }
      controller.close();
    },
  }), {
    status: 200,
    headers: { 'content-type': 'text/event-stream' },
  });
}

function parseSSEDataFrames(body) {
  return body
    .split('\n\n')
    .map((frame) => frame.trim())
    .filter((frame) => frame.startsWith('data:'))
    .map((frame) => frame.slice(5).trim());
}

const SDAI_COMPLETION_URL = 'https://sdai.suda.edu.cn/backend/api/chat/start';

function sdaiDelta(text, type) {
  return `data: ${JSON.stringify({ choices: [{ index: 0, delta: { content: text, type } }] })}\n\n`;
}

function sdaiText(text) {
  return sdaiDelta(text, 'text');
}

function sdaiThink(text) {
  return sdaiDelta(text, 'think');
}

function sdaiFinish(id) {
  return `data: {"req_message_pk_id": ${id}}\n\n`;
}

const SDAI_DONE = 'data: DONE\n\n';

async function runMockVercelStream(upstreamLines, prepareOverrides = {}) {
  return runMockVercelStreamSequence([upstreamLines], prepareOverrides);
}

async function runMockVercelStreamSequence(upstreamSequences, prepareOverrides = {}) {
  const originalFetch = global.fetch;
  const fetchURLs = [];
  const fetchBodies = [];
  const fetchAuth = [];
  let completionCalls = 0;
  const prepareBody = {
    session_id: 'chatcmpl-test',
    lease_id: 'lease-test',
    model: 'gpt-test',
    final_prompt: 'hello',
    thinking_enabled: false,
    search_enabled: false,
    tool_names: [],
    deepseek_token: 'sdai-token',
    payload: { content: 'hello', uuid: 'chatcmpl-test', model_id: 10, think: 0 },
    ...prepareOverrides,
  };
  global.fetch = async (url, init = {}) => {
    const textURL = String(url);
    fetchURLs.push(textURL);
    // 仅收集上游 completion 调用的 body/auth，避免 prepare/release 干扰索引。
    if (textURL === SDAI_COMPLETION_URL) {
      if (init && init.body) {
        fetchBodies.push(JSON.parse(String(init.body)));
      }
      if (init && init.headers) {
        fetchAuth.push(init.headers.authorization || '');
      }
    }
    if (textURL.includes('__stream_prepare=1')) {
      return jsonResponse(prepareBody);
    }
    if (textURL.includes('__stream_release=1')) {
      return jsonResponse({ success: true });
    }
    if (textURL.includes('__stream_switch=1')) {
      return jsonResponse({
        session_id: 'session-2',
        lease_id: 'lease-test',
        model: 'gpt-test',
        final_prompt: 'hello',
        thinking_enabled: true,
        search_enabled: false,
        tool_names: [],
        deepseek_token: 'sdai-token-2',
        payload: { content: 'hello', uuid: 'session-2', model_id: 10, think: 1 },
      });
    }
    const idx = Math.min(completionCalls, upstreamSequences.length - 1);
    completionCalls += 1;
    return sseResponse(upstreamSequences[idx]);
  };
  try {
    const req = new MockStreamRequest();
    const res = new MockStreamResponse();
    const payload = { model: 'gpt-test', stream: true };
    await handleVercelStream(req, res, Buffer.from(JSON.stringify(payload)), payload);
    return { res, frames: parseSSEDataFrames(res.bodyText()), fetchURLs, fetchBodies, fetchAuth };
  } finally {
    global.fetch = originalFetch;
  }
}

// ─── parser tests ────────────────────────────────────────────────────

test('chat-stream exposes parser test hooks', () => {
  assert.equal(typeof parseChunkForContent, 'function');
});

test('parseChunkForContent splits think and text channels', () => {
  const thinking = parseChunkForContent(JSON.parse('{"choices":[{"index":0,"delta":{"content":"思","type":"think"}}]}'), true, 'thinking');
  assert.deepEqual(thinking.parts, [{ text: '思', type: 'thinking' }]);
  assert.equal(thinking.newType, 'thinking');

  const text = parseChunkForContent(JSON.parse('{"choices":[{"index":0,"delta":{"content":"答","type":"text"}}]}'), true, 'thinking');
  assert.deepEqual(text.parts, [{ text: '答', type: 'text' }]);
  assert.equal(text.newType, 'text');
});

test('parseChunkForContent drops thinking when disabled', () => {
  const got = parseChunkForContent(JSON.parse('{"choices":[{"index":0,"delta":{"content":"hidden","type":"think"}}]}'), false, 'text');
  assert.deepEqual(got.parts, []);
  assert.equal(got.newType, 'thinking');
});

test('parseChunkForContent tolerates unknown data (cate header)', () => {
  const got = parseChunkForContent({ format: 'STREAM' }, true, 'text');
  assert.equal(got.parsed, false);
  assert.deepEqual(got.parts, []);
});

test('parseChunkForContent extracts finish message id without ending stream', () => {
  const got = parseChunkForContent({ req_message_pk_id: 13864 }, true, 'text');
  assert.equal(got.parsed, true);
  assert.equal(got.finished, false);
  assert.equal(extractResponseMessageID({ req_message_pk_id: 13864 }), 13864);
  assert.equal(extractResponseMessageID({}), 0);
});

test('parseChunkForContent ignores empty deltas', () => {
  const got = parseChunkForContent(JSON.parse('{"choices":[{"index":0,"delta":{"content":"","type":"text"}}]}'), true, 'text');
  assert.deepEqual(got.parts, []);
});

test('parseChunkForContent strips reference markers', () => {
  assert.equal(stripReferenceMarkers('答案[reference:0]'), '答案');
});

test('trimContinuationOverlap trims replayed prefix (long continuation only)', () => {
  // 去重算法仅处理 >= 32 字符的续写快照，短文本原样返回。
  const existing = 'A'.repeat(40);
  assert.equal(trimContinuationOverlap(existing, existing + ' tail'), ' tail');
  assert.equal(trimContinuationOverlap(existing, existing), '');
  assert.equal(trimContinuationOverlap('AAA', 'AA tail'), 'AA tail');
});

test('path helpers keep OpenAI chat only', () => {
  assert.equal(isNodeStreamSupportedPath('/v1/chat/completions'), true);
  assert.equal(isNodeStreamSupportedPath('/chat/completions'), true);
  assert.equal(isNodeStreamSupportedPath('/v1/responses'), false);
  assert.equal(extractPathname('/v1/chat/completions?x=1'), '/v1/chat/completions');
});

// ─── vercel stream behavior tests ────────────────────────────────────

test('vercel stream emits Go-parity empty-output failure on DONE', async () => {
  const { frames } = await runMockVercelStream([SDAI_DONE]);
  assert.equal(frames.length, 2);
  const failed = JSON.parse(frames[0]);
  assert.equal(failed.status_code, 503);
  assert.equal(failed.error.type, 'service_unavailable_error');
  assert.equal(failed.error.code, 'upstream_unavailable');
  assert.equal(frames[1], '[DONE]');
});

test('vercel stream posts to SDAI chat/start with bearer token', async () => {
  const { fetchURLs, fetchAuth, fetchBodies } = await runMockVercelStream([sdaiText('ok'), SDAI_DONE]);
  assert.ok(fetchURLs.filter((url) => url === SDAI_COMPLETION_URL).length >= 1);
  assert.ok(fetchAuth.every((auth) => auth === 'Bearer sdai-token'));
  // SDAI payload 字段直传。
  assert.equal(fetchBodies[0].content, 'hello');
  assert.equal(fetchBodies[0].uuid, 'chatcmpl-test');
});

test('vercel stream emits text deltas and single terminal frame on DONE', async () => {
  const { frames } = await runMockVercelStream([sdaiText('visible'), SDAI_DONE]);
  const parsed = frames.filter((frame) => frame !== '[DONE]').map((frame) => JSON.parse(frame));
  assert.equal(frames.filter((frame) => frame === '[DONE]').length, 1);
  assert.equal(parsed[0].choices[0].delta.content, 'visible');
  assert.equal(parsed[1].choices[0].finish_reason, 'stop');
  assert.equal(parsed[0].id, parsed[1].id);
});

test('vercel stream retries empty output once with content suffix', async () => {
  const { frames, fetchURLs, fetchBodies } = await runMockVercelStreamSequence([
    [sdaiFinish(42), SDAI_DONE],
    [sdaiText('visible'), SDAI_DONE],
  ]);
  const parsed = frames.filter((frame) => frame !== '[DONE]').map((frame) => JSON.parse(frame));
  assert.equal(fetchURLs.filter((url) => url === SDAI_COMPLETION_URL).length, 2);
  assert.equal(frames.filter((frame) => frame === '[DONE]').length, 1);
  assert.equal(parsed[0].choices[0].delta.content, 'visible');
  assert.equal(parsed[1].choices[0].finish_reason, 'stop');
  // SDAI fresh retry：content 后缀 + think=0（规避 reasoning-only），无 parent_message_id。
  assert.match(fetchBodies[1].content, /Previous reply had no visible output\. Please regenerate the visible final answer or tool call now\.$/);
  assert.equal(fetchBodies[1].think, 0);
  assert.equal(Object.hasOwn(fetchBodies[1], 'parent_message_id'), false);
});

test('vercel stream retries thinking-only output once and flushes reasoning', async () => {
  const { frames, fetchBodies } = await runMockVercelStreamSequence([
    [sdaiThink('plan'), sdaiFinish(42), SDAI_DONE],
    [sdaiText('visible'), SDAI_DONE],
  ], { thinking_enabled: true });
  const parsed = frames.filter((frame) => frame !== '[DONE]').map((frame) => JSON.parse(frame));
  assert.equal(parsed[0].choices[0].delta.reasoning_content, 'plan');
  assert.equal(parsed[1].choices[0].delta.content, 'visible');
  assert.equal(parsed[2].choices[0].finish_reason, 'stop');
});

test('vercel stream coalesces many small content deltas while keeping one choice', async () => {
  const lines = [];
  for (let i = 0; i < 100; i += 1) {
    lines.push(sdaiText('字'));
  }
  lines.push(SDAI_DONE);
  const { frames } = await runMockVercelStream(lines);
  const parsed = frames.filter((frame) => frame !== '[DONE]').map((frame) => JSON.parse(frame));
  let content = '';
  for (const frame of parsed) {
    const choices = frame.choices || [];
    assert.equal(choices.length, 1);
    if (choices[0].delta && choices[0].delta.content) {
      content += choices[0].delta.content;
    }
  }
  assert.equal(content, '字'.repeat(100));
});

test('vercel stream switches managed account after empty retry exhaustion', async () => {
  const originalFetch = global.fetch;
  const fetchURLs = [];
  const fetchBodies = [];
  const fetchAuth = [];
  let completionCalls = 0;
  global.fetch = async (url, init = {}) => {
    const textURL = String(url);
    fetchURLs.push(textURL);
    if (textURL === SDAI_COMPLETION_URL) {
      if (init && init.body) {
        fetchBodies.push(JSON.parse(String(init.body)));
      }
      if (init && init.headers) {
        fetchAuth.push(init.headers.authorization || '');
      }
    }
    if (textURL.includes('__stream_prepare=1')) {
      return jsonResponse({
        session_id: 'chatcmpl-test',
        lease_id: 'lease-test',
        model: 'gpt-test',
        final_prompt: 'hello',
        thinking_enabled: true,
        search_enabled: false,
        tool_names: [],
        deepseek_token: 'sdai-token-1',
        payload: { content: 'hello', uuid: 'session-1', model_id: 10, think: 1 },
      });
    }
    if (textURL.includes('__stream_switch=1')) {
      return jsonResponse({
        session_id: 'session-2',
        lease_id: 'lease-test',
        model: 'gpt-test',
        final_prompt: 'hello',
        thinking_enabled: true,
        search_enabled: false,
        tool_names: [],
        deepseek_token: 'sdai-token-2',
        payload: { content: 'hello', uuid: 'session-2', model_id: 10, think: 1 },
      });
    }
    const idx = Math.min(completionCalls, 2);
    completionCalls += 1;
    if (idx === 0 || idx === 1) {
      return sseResponse([sdaiThink('empty plan'), SDAI_DONE]);
    }
    return sseResponse([sdaiText('ok from second account'), SDAI_DONE]);
  };
  try {
    const req = new MockStreamRequest();
    const res = new MockStreamResponse();
    const payload = { model: 'gpt-test', stream: true };
    await handleVercelStream(req, res, Buffer.from(JSON.stringify(payload)), payload);
    const frames = parseSSEDataFrames(res.bodyText());
    const parsed = frames.filter((frame) => frame !== '[DONE]').map((frame) => JSON.parse(frame));
    // 三次上游调用：初始 + 重试 + 切号后。
    assert.equal(fetchURLs.filter((url) => url === SDAI_COMPLETION_URL).length, 3);
    // 切号后使用新 token 与新 uuid。
    assert.ok(fetchAuth.includes('Bearer sdai-token-2'));
    assert.equal(fetchBodies[2].uuid, 'session-2');
    assert.equal(parsed[parsed.length - 2].choices[0].delta.content, 'ok from second account');
    assert.equal(parsed[parsed.length - 1].choices[0].finish_reason, 'stop');
  } finally {
    global.fetch = originalFetch;
  }
});

test('vercel stream keeps tool sieve behavior for buffered content', async () => {
  const payloadText = '<tool_calls><invoke name="Bash"><parameter name="command">pwd</parameter></invoke></tool_calls>';
  const { frames } = await runMockVercelStreamSequence(
    [[sdaiText(payloadText), SDAI_DONE]],
    {
      tool_names: ['Bash'],
      toolcall_mode: 'buffered',
    },
  );
  const parsed = frames.filter((frame) => frame !== '[DONE]').map((frame) => JSON.parse(frame));
  const toolFrame = parsed.find((frame) => frame.choices && frame.choices[0].delta.tool_calls);
  assert.ok(toolFrame, 'expected tool_calls delta frame');
  const call = toolFrame.choices[0].delta.tool_calls[0];
  assert.equal(call.function.name, 'Bash');
  assert.deepEqual(JSON.parse(call.function.arguments), { command: 'pwd' });
});
