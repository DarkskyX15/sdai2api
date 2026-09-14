'use strict';

// SDAI SSE chunk 解析实现（Node 流式桥使用）。
//
// SDAI 事件结构（见 tests/raw_stream_samples/sdai-*）：
//
//	event: message / data: {"choices":[{"index":0,"delta":{"content":"...","type":"think|text"}}]}
//	event: finish  / data: {"req_message_pk_id":123}
//	event: flag    / data: DONE
//
// DONE 帧由调用方在行级识别；本模块只处理 JSON data。

const LEAKED_BOS_MARKER_PATTERN = /<[\|\uFF5C]\s*begin[_▁]of[_▁]sentence\s*[\|\uFF5C]>/gi;
const LEAKED_THOUGHT_MARKER_PATTERN = /<[\|\uFF5C]\s*(?:begin[_▁])?[_▁]*of[_▁]thought\s*[\|\uFF5C]>/gi;
const LEAKED_META_MARKER_PATTERN = /<[\|\uFF5C]\s*(?:assistant|tool|end[_▁]of[_▁]sentence|end[_▁]of[_▁]thinking|end[_▁]of[_▁]thought|end[_▁]of[_▁]toolresults|end[_▁]of[_▁]instructions)\s*[\|\uFF5C]>/gi;

function stripThinkTags(text) {
  if (typeof text !== 'string' || !text) {
    return text;
  }
  return text.replace(/<\/?\s*think\s*>/gi, '');
}

function asContentString(v, stripReferenceMarkers = true) {
  if (typeof v === 'string') {
    return stripReferenceMarkers ? stripReferenceMarkersText(v) : v;
  }
  if (v == null) {
    return '';
  }
  const text = String(v);
  return stripReferenceMarkers ? stripReferenceMarkersText(text) : text;
}

function stripReferenceMarkersText(text) {
  if (!text) {
    return text;
  }
  return text
    .replace(/\[(?:citation|reference):\s*\d+\]/gi, '')
    .replace(LEAKED_BOS_MARKER_PATTERN, '')
    .replace(LEAKED_THOUGHT_MARKER_PATTERN, '')
    .replace(LEAKED_META_MARKER_PATTERN, '');
}

// finish 事件携带的上游消息 ID。
function extractResponseMessageID(chunk) {
  if (!chunk || typeof chunk !== 'object') {
    return 0;
  }
  const id = chunk.req_message_pk_id;
  return typeof id === 'number' && Number.isFinite(id) && id > 0 ? Math.trunc(id) : 0;
}

function parseChunkForContent(chunk, thinkingEnabled, currentType, stripReferenceMarkers = true) {
  if (!chunk || typeof chunk !== 'object') {
    return {
      parsed: false,
      parts: [],
      finished: false,
      contentFilter: false,
      errorMessage: '',
      outputTokens: 0,
      responseMessageID: 0,
      newType: currentType,
    };
  }

  // finish 事件：记录消息 ID，结束信号由 flag/DONE 给出。
  const responseMessageID = extractResponseMessageID(chunk);
  if (responseMessageID > 0) {
    return {
      parsed: true,
      parts: [],
      finished: false,
      contentFilter: false,
      errorMessage: '',
      outputTokens: 0,
      responseMessageID,
      newType: currentType,
    };
  }

  if (!Array.isArray(chunk.choices)) {
    // 未知 data（如 cate 事件的 {"format":"STREAM"}）不产生内容。
    return {
      parsed: false,
      parts: [],
      finished: false,
      contentFilter: false,
      errorMessage: '',
      outputTokens: 0,
      responseMessageID: 0,
      newType: currentType,
    };
  }

  let newType = currentType;
  const parts = [];
  // think 增量无论 thinking 开关都进入检测通道：模型可能只在思考流里
  // 输出 DSML 工具调用块（正文为空），finalize 依赖它提升工具调用。
  const detectionParts = [];
  for (const choice of chunk.choices) {
    if (!choice || typeof choice !== 'object') {
      continue;
    }
    const delta = choice.delta && typeof choice.delta === 'object' ? choice.delta : null;
    if (!delta) {
      continue;
    }
    const content = asContentString(delta.content, stripReferenceMarkers);
    if (!content) {
      continue;
    }
    const deltaType = String(delta.type || '').toLowerCase();
    if (deltaType === 'think') {
      newType = 'thinking';
      detectionParts.push({ text: content, type: 'thinking' });
      if (thinkingEnabled) {
        parts.push({ text: content, type: 'thinking' });
      }
    } else {
      newType = 'text';
      parts.push({ text: content, type: 'text' });
    }
  }

  return {
    parsed: true,
    parts,
    detectionParts,
    finished: false,
    contentFilter: false,
    errorMessage: '',
    outputTokens: 0,
    responseMessageID: 0,
    newType,
  };
}

function isCitation(text) {
  return String(text || '').trim().startsWith('[citation:');
}

module.exports = {
  parseChunkForContent,
  extractResponseMessageID,
  isCitation,
  stripReferenceMarkers: stripReferenceMarkersText,
  stripThinkTags,
};
