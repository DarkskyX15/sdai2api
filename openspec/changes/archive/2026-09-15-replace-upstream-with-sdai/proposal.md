# Replace Upstream With SDAI

## Why

项目当前唯一的上游是 DeepSeek 网页版（`internal/deepseek/*` + `pow/`），协议复杂（PoW、登录、伪装头、TLS 指纹）且与本项目实际使用场景脱节。目标上游 SDAI（`https://sdai.suda.edu.cn`，苏州大学校园 AI 平台）协议已被完整逆向验证（见 `.local/findings.md` 与 `.local/exp*.log` 抓包样本）：裸 Bearer token、无 PoW、无签名头、标准 SSE、OpenAI 风格 chunk 结构。直接替换上游可大幅删减维护面（删除 `pow/`、登录、会话创建、TLS 伪装约上千行代码），同时保留全部对外 API 兼容能力（OpenAI/Claude/Gemini/Ollama/Admin/WebUI）。

## What Changes

- **BREAKING**：上游站点从 DeepSeek 网页版替换为 SDAI（sdai.suda.edu.cn）。配置中的 `accounts` 不再支持 email/mobile/password 登录，改为直接配置 Bearer token（DeepSeek 的自动登录/token 刷新随登录流程一并移除）。
- **BREAKING**：模型表整体更换。DeepSeek 专属模型 ID（`deepseek-v4-flash` 等）替换为 SDAI 模型表（`deepseek-v4-pro` id=8、`deepseek-v4-flash` id=10、`deepseek-v3.2` id=9、`deepseek-v3.1-terminus` id=7、`doubao-1-5-pro-32k` id=6 等，动态注册自 `GET /model/list`）；模型名→thinking/search 映射逻辑按 SDAI 语义重做（`think: 0|1`）。
- 上游调用层替换：`internal/deepseek` 的 client/protocol/transport 重写为 SDAI 客户端（单 HTTP 端点 `POST /backend/api/chat/start` + `DELETE /backend/api/msg_title/del` 会话清理 + `GET /backend/api/model/list` 模型注册）。
- 会话策略维持"无状态覆盖层"：每请求新建 UUID、全量归一化上下文放入 `content`、用完即弃；`auto_delete.mode = single` 继续支持（`all` 模式改为 `msg_title/list` 翻页 + 逐个删除模拟）。
- PoW 体系整体删除（`pow/` 包、`GetPow`、`x-ds-pow-response` 头、PoW 重试分支）。
- SSE 解析层重写：`message`（`delta.content` + `delta.type: think|text`）/ `finish`（`req_message_pk_id`）/ `flag: DONE` 三事件；`type: think` 映射到 reasoning 通道。
- 错误处理模型更换：SDAI 业务错误码在 HTTP 200 body 的 `code` 字段中（200/404/400/-1），token 失效判定改为解析 body。
- `completionruntime.DeepSeekCaller` 接口契约保持（`CreateSession`/`CallCompletion` 保留，`GetPow`/`UploadFile` 视实际情况裁剪），`promptcompat` 归一化层与全部协议入口（`httpapi/*`）、格式化层（`format/*`）不动。
- 删除随 DeepSeek 专有的能力：直通 token 模式保留（SDAI 同样适用），但"托管账号自动登录"不再存在；`client_continue` 自动续写、`chat-stream` Vercel Node 桥中的 PoW/会话准备逻辑相应简化。
- 文档同步：`docs/prompt-compatibility.md`、`API.md`、`README.MD` 中 DeepSeek 专属描述更新为 SDAI。

## Capabilities

### New Capabilities

- `sdai-upstream`: SDAI 上游对接能力——chat/start 调用契约（请求体六字段、Bearer 鉴权、SSE 三事件解析、think/text 双通道、finish/DONE 终止语义）、无状态会话策略（每请求新 UUID + 全量上下文单条 content）、会话清理（single/all 模式）、模型注册与 think 开关映射、业务错误码解析。

### Modified Capabilities

（`openspec/specs/` 当前为空，无既有 capability 需修改。上游替换造成的对外行为变化全部收敛在 `sdai-upstream` 新 capability 内。）

## Impact

- **代码**：`internal/deepseek/**`（重写为 sdai client 或改名 `internal/sdai`）、`pow/**`（删除）、`internal/sse/parser.go`（重写）、`internal/config/{config,models}.go`（Account 字段改 token、模型表重做）、`internal/server/router.go`（装配调整）、`internal/completionruntime/**`（接口裁剪 + 重试逻辑去掉 PoW 分支）、`internal/httpapi/**`（`DeepSeekCaller` 接口面收窄）、`internal/js/chat-stream/**` 与 `api/chat-stream.js`（PoW/会话准备简化）。
- **配置**：`config.example.json`、Admin 设置面、WebUI 账号管理页（登录表单 → token 输入）。
- **测试**：`tests/raw_stream_samples/`（新增 SDAI 抓包样本）、`internal/compat` fixtures、E2E 测试集（`cmd/ds2api-tests`）。
- **文档**：`docs/prompt-compatibility.md`（协议边界文档同步）、`API.md`、`README.MD`/`README.en.md`。
- **不受影响**：OpenAI/Claude/Gemini/Ollama 对外协议面、账号池并发模型、tool-call 转译（DSML）、thinking 注入、current_input_file 机制（SDAI 无文件上传，长上下文直接走 content，超限行为待压测后决定）。
