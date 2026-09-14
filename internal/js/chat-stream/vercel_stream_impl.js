'use strict';

// Vercel Node 流式桥实现（SDAI 上游）。
//
// 流程：Go 侧 /__stream_prepare 完成鉴权与 payload 构建 → Node 直连
// SDAI chat/start 消费 SSE → 转译为 OpenAI chat.completion.chunk 输出。
// SDAI 无 PoW/continue 端点；空输出重试使用全新 uuid + 原始归一化上下文。

const {
  createToolSieveState,
  processToolSieveChunk,
  flushToolSieve,
  parseStandaloneToolCalls,
  formatOpenAIStreamToolCalls,
} = require('../helpers/stream-tool-sieve');
const { SDAI_CHAT_START_URL, BASE_HEADERS } = require('../shared/deepseek-constants');
const { writeOpenAIError, openAIErrorType } = require('./error_shape');
const { parseChunkForContent } = require('./sse_parse');
const { buildUsage } = require('./token_usage');
const {
  resolveToolcallPolicy,
  formatIncrementalToolCallDeltas,
  filterIncrementalToolCallDeltasByAllowed,
  resetStreamToolCallState,
} = require('./toolcall_policy');
const { createChatCompletionEmitter, createDeltaCoalescer } = require('./stream_emitter');
const {
  asString,
  isAbortError,
  fetchStreamPrepare,
  fetchStreamSwitch,
  relayPreparedFailure,
  createLeaseReleaser,
} = require('./http_internal');
const {
  trimContinuationOverlap,
} = require('./dedupe');

const EMPTY_OUTPUT_RETRY_SUFFIX = 'Previous reply had no visible output. Please regenerate the visible final answer or tool call now.';
const EMPTY_OUTPUT_RETRY_MAX_ATTEMPTS = 1;

async function handleVercelStream(req, res, rawBody, payload) {
  const prep = await fetchStreamPrepare(req, rawBody);
  if (!prep.ok) {
    relayPreparedFailure(res, prep);
    return;
  }

  const model = asString(prep.body.model) || asString(payload.model);
  const responseID = asString(prep.body.session_id) || `chatcmpl-${Date.now()}`;
  const leaseID = asString(prep.body.lease_id);
  let upstreamToken = asString(prep.body.deepseek_token);
  let completionPayload = prep.body.payload && typeof prep.body.payload === 'object' ? prep.body.payload : null;
  const finalPrompt = asString(prep.body.final_prompt);
  const thinkingEnabled = toBool(prep.body.thinking_enabled);
  const toolPolicy = resolveToolcallPolicy(prep.body, payload.tools);
  const toolNames = toolPolicy.toolNames;
  const emitEarlyToolDeltas = toolPolicy.emitEarlyToolDeltas;
  const stripReferenceMarkers = true;

  if (!model || !leaseID || !upstreamToken || !completionPayload) {
    writeOpenAIError(res, 500, 'invalid vercel prepare response');
    return;
  }

  const releaseLease = createLeaseReleaser(req, leaseID);
  const upstreamController = new AbortController();
  let clientClosed = false;
  let reader = null;
  const markClientClosed = () => {
    if (clientClosed) {
      return;
    }
    clientClosed = true;
    upstreamController.abort();
    if (reader && typeof reader.cancel === 'function') {
      Promise.resolve(reader.cancel()).catch(() => {});
    }
  };
  const onReqAborted = () => markClientClosed();
  const onResClose = () => {
    if (!res.writableEnded) {
      markClientClosed();
    }
  };
  req.on('aborted', onReqAborted);
  res.on('close', onResClose);

  try {
    const fetchSDAIStream = async (bodyPayload) => {
      try {
        return await fetch(SDAI_CHAT_START_URL, {
          method: 'POST',
          headers: {
            ...BASE_HEADERS,
            accept: 'text/event-stream',
            authorization: `Bearer ${upstreamToken}`,
          },
          body: JSON.stringify(bodyPayload),
          signal: upstreamController.signal,
        });
      } catch (err) {
        if (clientClosed || isAbortError(err)) {
          return null;
        }
        throw err;
      }
    };
    const fetchCompletion = (bodyPayload) => fetchSDAIStream(bodyPayload);

    let completionRes = await fetchCompletion(completionPayload);
    if (completionRes === null) {
      return;
    }
    if (clientClosed) {
      return;
    }

    if (!completionRes.ok || !completionRes.body) {
      const detail = completionRes.body ? await completionRes.text() : '';
      const status = completionRes.ok ? 500 : completionRes.status || 500;
      writeOpenAIError(res, status, detail);
      return;
    }

    res.statusCode = 200;
    res.setHeader('Content-Type', 'text/event-stream');
    res.setHeader('Cache-Control', 'no-cache, no-transform');
    res.setHeader('Connection', 'keep-alive');
    res.setHeader('X-Accel-Buffering', 'no');
    if (typeof res.flushHeaders === 'function') {
      res.flushHeaders();
    }

    const created = Math.floor(Date.now() / 1000);
    let currentType = thinkingEnabled ? 'thinking' : 'text';
    let thinkingText = '';
    let detectionThinkingText = '';
    let outputText = '';
    let usagePrompt = finalPrompt;
    const toolSieveEnabled = toolPolicy.toolSieveEnabled;
    const toolSieveState = createToolSieveState();
    let toolCallsEmitted = false;
    let toolCallsDoneEmitted = false;
    const streamToolCallIDs = new Map();
    const streamToolNames = new Map();
    const decoder = new TextDecoder();
    let buffered = '';
    let ended = false;
    const { sendFrame, sendDeltaFrame } = createChatCompletionEmitter({
      res,
      sessionID: responseID,
      created,
      model,
      isClosed: () => clientClosed,
    });
    const deltaCoalescer = createDeltaCoalescer({ sendDeltaFrame });

    const finish = async (reason, options = {}) => {
      if (ended) {
        return true;
      }
      if (clientClosed || res.writableEnded || res.destroyed) {
        ended = true;
        await releaseLease();
        return true;
      }
      deltaCoalescer.flush();
      let detected = parseStandaloneToolCalls(outputText, toolNames);
      if (detected.length === 0 && detectionThinkingText) {
        // 正文无工具块时，回退扫描思考流缓冲（think-only 工具调用形态）。
        detected = parseStandaloneToolCalls(detectionThinkingText, toolNames);
      }
      if (detected.length > 0 && !toolCallsDoneEmitted) {
        toolCallsEmitted = true;
        toolCallsDoneEmitted = true;
        sendDeltaFrame({ tool_calls: formatOpenAIStreamToolCalls(detected, streamToolCallIDs, payload.tools) });
      } else if (toolSieveEnabled) {
        const tailEvents = flushToolSieve(toolSieveState, toolNames);
        for (const evt of tailEvents) {
          if (evt.type === 'tool_calls' && Array.isArray(evt.calls) && evt.calls.length > 0) {
            deltaCoalescer.flush();
            toolCallsEmitted = true;
            toolCallsDoneEmitted = true;
            sendDeltaFrame({ tool_calls: formatOpenAIStreamToolCalls(evt.calls, streamToolCallIDs, payload.tools) });
            resetStreamToolCallState(streamToolCallIDs, streamToolNames);
            continue;
          }
          if (evt.text) {
            deltaCoalescer.append('content', evt.text);
          }
        }
        deltaCoalescer.flush();
      }
      if (detected.length > 0 || toolCallsEmitted) {
        reason = 'tool_calls';
      }
      if (detected.length === 0 && !toolCallsEmitted && outputText.trim() === '') {
        if (options.deferEmpty && reason !== 'content_filter') {
          return false;
        }
        ended = true;
        const detail = upstreamEmptyOutputDetail(reason === 'content_filter', outputText, thinkingText);
        sendFailedChunk(res, detail.status, detail.message, detail.code);
        await releaseLease();
        if (!res.writableEnded && !res.destroyed) {
          res.end();
        }
        return true;
      }
      ended = true;
      sendFrame({
        id: responseID,
        object: 'chat.completion.chunk',
        created,
        model,
        choices: [{ delta: {}, index: 0, finish_reason: reason }],
        usage: buildUsage(usagePrompt, thinkingText, outputText),
      });
      if (!res.writableEnded && !res.destroyed) {
        res.write('data: [DONE]\n\n');
      }
      await releaseLease();
      if (!res.writableEnded && !res.destroyed) {
        res.end();
      }
      return true;
    };

    const processStream = async (initialResponse, allowDeferEmpty) => {
      const reader = initialResponse.body.getReader();
      let upstreamResponseMessageID = 0;
      buffered = '';
      let streamEnded = false;
      try {
        // eslint-disable-next-line no-constant-condition
        while (true) {
          if (clientClosed) {
            await finish('stop');
            return { terminal: true, retryable: false };
          }
          const { value, done } = await reader.read();
          if (done) {
            break;
          }
          buffered += decoder.decode(value, { stream: true });
          const lines = buffered.split('\n');
          buffered = lines.pop() || '';

          for (const rawLine of lines) {
            const line = rawLine.trim();
            if (!line.startsWith('data:')) {
              continue;
            }
            const dataStr = line.slice(5).trim();
            if (!dataStr) {
              continue;
            }
            if (dataStr === 'DONE') {
              // SDAI 正常结束信号（event: flag / data: DONE）。
              streamEnded = true;
              break;
            }
            let chunk;
            try {
              chunk = JSON.parse(dataStr);
            } catch (_err) {
              continue;
            }
            const parsed = parseChunkForContent(chunk, thinkingEnabled, currentType, stripReferenceMarkers);
            if (!parsed.parsed) {
              continue;
            }
            if (parsed.responseMessageID > 0) {
              upstreamResponseMessageID = parsed.responseMessageID;
              continue;
            }
            currentType = parsed.newType;
            if (parsed.errorMessage) {
              return { terminal: await finish('content_filter'), retryable: false };
            }
            if (parsed.contentFilter) {
              return { terminal: await finish(outputText.trim() === '' ? 'content_filter' : 'stop'), retryable: false };
            }
            if (parsed.finished) {
              streamEnded = true;
              break;
            }

            // think 增量恒进检测缓冲（与 Go 侧 DetectionThinking 语义一致），
            // 供 finalize 提升思考流里的 DSML 工具调用。
            for (const p of parsed.detectionParts || []) {
              if (!p.text) {
                continue;
              }
              const det = trimContinuationOverlap(detectionThinkingText, p.text);
              if (det) {
                detectionThinkingText += det;
              }
            }

            for (const p of parsed.parts) {
              if (!p.text) {
                continue;
              }
              if (p.type === 'thinking') {
                if (thinkingEnabled) {
                  const trimmed = trimContinuationOverlap(thinkingText, p.text);
                  if (!trimmed) {
                    continue;
                  }
                  thinkingText += trimmed;
                  deltaCoalescer.append('reasoning_content', trimmed);
                }
              } else {
                const trimmed = trimContinuationOverlap(outputText, p.text);
                if (!trimmed) {
                  continue;
                }
                outputText += trimmed;
                if (!toolSieveEnabled) {
                  deltaCoalescer.append('content', trimmed);
                  continue;
                }
                const events = processToolSieveChunk(toolSieveState, trimmed, toolNames);
                for (const evt of events) {
                  if (evt.type === 'tool_call_deltas') {
                    if (!emitEarlyToolDeltas) {
                      continue;
                    }
                    const filtered = filterIncrementalToolCallDeltasByAllowed(evt.deltas, toolNames, streamToolNames);
                    const formatted = formatIncrementalToolCallDeltas(filtered, streamToolCallIDs);
                    if (formatted.length > 0) {
                      toolCallsEmitted = true;
                      deltaCoalescer.flush();
                      sendDeltaFrame({ tool_calls: formatted });
                    }
                    continue;
                  }
                  if (evt.type === 'tool_calls') {
                    toolCallsEmitted = true;
                    toolCallsDoneEmitted = true;
                    deltaCoalescer.flush();
                    sendDeltaFrame({ tool_calls: formatOpenAIStreamToolCalls(evt.calls, streamToolCallIDs, payload.tools) });
                    resetStreamToolCallState(streamToolCallIDs, streamToolNames);
                    continue;
                  }
                  if (evt.text) {
                    deltaCoalescer.append('content', evt.text);
                  }
                }
              }
            }
            if (streamEnded) {
              break;
            }
          }
          if (streamEnded) {
            break;
          }
        }
      } catch (err) {
        // 客户端断连或上游异常：SDAI 无 continue 能力，按终态处理。
        void err;
        await finish('stop');
        return { terminal: true, retryable: false };
      }

      const terminal = await finish('stop', { deferEmpty: allowDeferEmpty });
      return { terminal, retryable: !terminal && allowDeferEmpty, responseMessageID: upstreamResponseMessageID };
    };

    let retryAttempts = 0;
    let accountSwitchAttempted = false;
    // eslint-disable-next-line no-constant-condition
    while (true) {
      const allowDeferEmpty = retryAttempts < EMPTY_OUTPUT_RETRY_MAX_ATTEMPTS || !accountSwitchAttempted;
      const processed = await processStream(completionRes, allowDeferEmpty);
      if (processed.terminal) {
        return;
      }
      if (!processed.retryable) {
        await finish('stop');
        return;
      }
      if (retryAttempts >= EMPTY_OUTPUT_RETRY_MAX_ATTEMPTS) {
        if (!accountSwitchAttempted) {
          accountSwitchAttempted = true;
          const switched = await fetchStreamSwitch(req, leaseID);
          if (switched.ok && switched.body && switched.body.payload && typeof switched.body.payload === 'object') {
            completionPayload = switched.body.payload;
            upstreamToken = asString(switched.body.deepseek_token) || upstreamToken;
            usagePrompt = finalPrompt;
            completionRes = await fetchCompletion(completionPayload);
            if (completionRes === null) {
              return;
            }
            if (!completionRes.ok || !completionRes.body) {
              await finish('stop');
              return;
            }
            continue;
          }
        }
        await finish('stop');
        return;
      }
      retryAttempts += 1;
      console.info('[openai_empty_retry] attempting synthetic retry', {
        surface: 'chat.completions',
        stream: true,
        retry_attempt: retryAttempts,
      });
      usagePrompt = usagePromptWithEmptyOutputRetry(finalPrompt, retryAttempts);
      // SDAI：fresh retry 使用全新 uuid（Go prepare/switch 端点已注入 payload），
      // 无 parent_message_id 语义。
      completionRes = await fetchCompletion(
        clonePayloadForEmptyOutputRetry(completionPayload),
      );
      if (completionRes === null) {
        return;
      }
      if (!completionRes.ok || !completionRes.body) {
        await finish('stop');
        return;
      }
    }
  } finally {
    req.removeListener('aborted', onReqAborted);
    res.removeListener('close', onResClose);
    await releaseLease();
  }
}

function toBool(v) {
  return v === true;
}

function clonePayloadForEmptyOutputRetry(payload) {
  return {
    ...(payload || {}),
    content: appendEmptyOutputRetrySuffix(asString(payload && payload.content)),
  };
}

function appendEmptyOutputRetrySuffix(prompt) {
  const base = asString(prompt).trimEnd();
  if (!base) {
    return EMPTY_OUTPUT_RETRY_SUFFIX;
  }
  return `${base}\n\n${EMPTY_OUTPUT_RETRY_SUFFIX}`;
}

function usagePromptWithEmptyOutputRetry(originalPrompt, attempts) {
  if (!attempts || attempts <= 0) {
    return originalPrompt;
  }
  const parts = [originalPrompt];
  let next = originalPrompt;
  for (let i = 0; i < attempts; i += 1) {
    next = appendEmptyOutputRetrySuffix(next);
    parts.push(next);
  }
  return parts.join('\n');
}

function upstreamEmptyOutputDetail(contentFilter, _text, thinking) {
  if (contentFilter) {
    return {
      status: 400,
      message: 'Upstream content filtered the response and returned no output.',
      code: 'content_filter',
    };
  }
  if (thinking !== '') {
    return {
      status: 429,
      message: 'Upstream account hit a rate limit and returned reasoning without visible output.',
      code: 'upstream_empty_output',
    };
  }
  return {
    status: 503,
    message: 'Upstream service is unavailable and returned no output.',
    code: 'upstream_unavailable',
  };
}

function sendFailedChunk(res, status, message, code) {
  res.write(`data: ${JSON.stringify({
    status_code: status,
    error: {
      message,
      type: openAIErrorType(status),
      code,
      param: null,
    },
  })}\n\n`);
  if (!res.writableEnded && !res.destroyed) {
    res.write('data: [DONE]\n\n');
  }
  if (typeof res.flush === 'function') {
    res.flush();
  }
}

module.exports = {
  handleVercelStream,
};
