'use strict';

// SDAI (sdai.suda.edu.cn) 上游常量（Node 流式桥使用）。
// 协议逆向结论见 .local/findings.md：裸 Bearer、无 PoW、无客户端指纹要求。

const SDAI_HOST = 'sdai.suda.edu.cn';
const SDAI_BASE_URL = 'https://sdai.suda.edu.cn/backend/api';
const SDAI_CHAT_START_URL = `${SDAI_BASE_URL}/chat/start`;
const SDAI_CHAT_START_REFERER = 'https://sdai.suda.edu.cn/chat';
const SDAI_CONTENT_MAX_BYTES = 65000;

const DEFAULT_BASE_HEADERS = Object.freeze({
  'Content-Type': 'application/json',
  Referer: SDAI_CHAT_START_REFERER,
  'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 ' +
    '(KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36',
  'accept-charset': 'UTF-8',
});

module.exports = {
  SDAI_HOST,
  SDAI_BASE_URL,
  SDAI_CHAT_START_URL,
  SDAI_CHAT_START_REFERER,
  SDAI_CONTENT_MAX_BYTES,
  BASE_HEADERS: DEFAULT_BASE_HEADERS,
};
