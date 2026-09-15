# Design: Replace Upstream With SDAI

## Context

上游协议已通过 Python 探针完成逆向验证（`.local/findings.md`、`.local/exp*.log`）：单端点 `POST /backend/api/chat/start`、裸 Bearer、标准 SSE（`message`/`finish`/`flag` 三事件、`delta.type: think|text` 双通道）、无 PoW/无指纹校验、服务端按 uuid 存历史但本服务采用无状态策略（每请求新 UUID + 全量上下文单条 content）。现有代码的关键事实：

- 消费侧已全部依赖接口：`completionruntime.DeepSeekCaller`（4 方法）、`httpapi/*/deps.go` 中的 consumer 接口。唯一硬编码装配点是 `internal/server/router.go` 的 `NewApp()`。
- DeepSeek 专属逻辑集中在：`internal/deepseek/**`、`pow/**`、`internal/sse/parser.go`（fragments 语义）、`internal/config/models.go`（模型表）、`internal/promptcompat/standard_request.go`（`CompletionPayload()`）。
- 协议入口（`httpapi/**`）、归一化（`promptcompat/**`）、编排（`completionruntime/**`）、格式化（`format/**`）、账号池（`account/**`）、tool-call（`toolcall/**`）均与站点无关。

## Goals / Non-Goals

**Goals:**

- 以最小改动面完成上游替换：只动"上游调用面"，协议入口/归一化/编排/格式化层不动。
- 对外 API 兼容矩阵（OpenAI/Claude/Gemini/Ollama、流式/非流式、tool call、thinking 通道）保持不变。
- 删除 DeepSeek 专有的全部复杂度（PoW、登录、token 刷新、TLS 指纹、自动续写）。

**Non-Goals:**

- 不做多 provider 抽象/注册中心——本项目当前只对接 SDAI，不做可插拔架构（保持 AGENTS.md 的"协议边界"原则即可）。
- 不实现 SDAI 文件上传/知识库（`kb_tid_list` 恒空）；`current_input_file` 机制暂不可用，长上下文直接走 `content`。
- 不复刻 Vercel Node 流式桥的完整对等——PoW/会话准备删除后该桥简化为透传，实现时按需裁剪。
- 不探测/依赖 `msg/list` 做上下文重建（保持无状态）。

## Decisions

### D1: 直接替换 `internal/deepseek`，保留包路径与接口形状

把 `internal/deepseek` 原地重写为 SDAI client（包名暂不改，避免大范围 import 变更；`transport` 退化为普通 `http.Client`）。`DeepSeekCaller` 接口裁剪为 `CreateSession`（返回本地生成的 UUID）、`CallCompletion`、`DeleteSessionForToken`（+Ctx 变体）；`GetPow`/`UploadFile` 从接口与全部实现/测试桩中移除。

- 备选：新建 `internal/sdai` 包 + 装配工厂。放弃原因：单上游不需要 provider 抽象，原地替换 diff 最小、测试桩改动集中在同一目录。

### D2: SSE parser 重写为 SDAI 三事件语义

`internal/sse/parser.go` 重写：`event: message` → 解析 `choices[0].delta.{content,type}`，`type` 直接映射 reasoning/content 通道（`assistantturn` 双通道语义不变）；`event: flag` + `DONE` → 正常结束信号（替换 DeepSeek 的路径过滤/`[DONE]` 逻辑）；`finish` 事件的 `req_message_pk_id` 仅记录日志。`SkipContainsPatterns`/`SkipExactPathSet` 删除。

- 备选：在旧 parser 上加 provider 分支。放弃原因：两者语义无重叠，分支只增复杂度。

### D3: `CompletionPayload()` 改为 SDAI 六字段

`promptcompat/standard_request.go` 的 payload 改为 `{content, from_uuid, uuid, kb_tid_list, model_id, think}`。`model_id` 由模型解析层注入；`think` 由模型解析结果（`-nothinking` 后缀 / 默认策略）决定。全量上下文仍由 `promptcompat` 生成纯文本（沿用现有 transcript 构建逻辑）。

### D4: 模型表静态注册 + 动态校验

`config/models.go` 重做：内置静态表（v4-pro=8、v4-flash=10、v3.2=9、v3.1-terminus=7、doubao-1-5-pro-32k=6、deepseek-r1=2、千问3.7=11）+ `model_aliases` 覆盖机制保持；`-nothinking` 后缀语义保留并强制 `think:0`。启动时可选调用 `/model/list` 做表校验/日志告警（不阻塞启动）。

- 备选：完全动态注册（每次从上游拉取）。放弃原因：SDAI 模型表极少变动，静态表可测试、可离线启动；动态仅作校验。

### D5: 鉴权简化——token 直配，删除登录链

`config.Account` 的 `email/mobile/password` 字段替换为 `token`；`internal/auth/request.go` 的 `LoginFunc`、`RefreshToken` 刷新定时器删除（保留 `SwitchAccount`/`Release`/并发槽）。token 失效判定（`isTokenInvalid` 等价物）改为：HTTP 401/403，或 HTTP 200 + body `code` ∈ {401 类}；非 chat 端点的业务错误统一解析 body `code`。直通 token 模式保留原语义。

### D6: auto_delete 沿用 deferred 框架，`all` 模式改模拟实现

`single`：`handler_chat.go` 既有 deferred `autoDeleteRemoteSession` 不动，底层调用改为 `DELETE /msg_title/del`（detached context、失败仅告警，行为契约见 spec）。`all`：新增"list 分页枚举 + 逐个 DELETE"实现（DeepSeek 有 delete-all 端点，SDAI 没有）。

### D7: Vercel 分支同步裁剪

`internal/httpapi/openai/chat/vercel_stream.go` 的 prepare/release/pow/switch 四段中，Pow 段删除；prepare 仅做鉴权与 payload 构建；`internal/js/chat-stream/` 中对齐的 PoW/会话逻辑同步删除。Node 侧 SSE 解析（tool sieve）语义不变，但事件结构换为 SDAI 三事件。

### D8: 自动续写（auto-continue）整体移除

`client_continue.go` 的截断续写依赖 DeepSeek continue 端点，SDAI 无对应能力。长回复被上游截断时按"流被截断"契约走重试/报错，不自动续写。

## Risks / Trade-offs

- [SDAI `content` 长度上限未知，超长上下文（128k+）可能被上游拒绝] → 实施时先用探针压测上限；超限时返回明确的上游错误；`current_input_file` 在 SDAI 上不可用需文档标注（若后续发现 SDAI 支持文件再恢复）。
- [token 失效的错误特征未完整采样（仅推测 401/403 或 body code）] → 实施时用过期 token 做一轮探针，把真实响应固化到 `isTokenInvalid` 等价判定与测试样本；判定先宽后紧（宁可判失效触发切号，不误判成功）。
- [`all` 清理模式会删掉账号在网页端的真实对话] → 文档与 WebUI 提示中明确警告；保持默认 `none`。
- [无自动续写导致长输出可能被截断] → SDAI 上游模型输出上限（如 12k）内通常够用；截断按异常处理并在响应中如实呈现。
- [Breakure：存量用户 config.json 的 email/password 账号全部失效] → 迁移说明写入 docs；Admin 导入导出同步字段变更；启动时检测旧字段并打日志提示。
- [SSE 行尾/分包边界差异（探针基于 Python iter_lines）] → Go 侧 parser 按标准 SSE 规范实现（`\n\n` 分帧、容忍 `\r\n`），并用抓包样本回放测试。

## Migration Plan

1. 实施分支上完成替换 + 单测/回放样本全绿。
2. 更新 `config.example.json` 与文档（token 配置方式、模型表、auto_delete 语义）。
3. 用户迁移：在 config 中将 `accounts` 条目改为 `token` 字段（从浏览器复制 Bearer）；模型 alias 表按新模型表调整。
4. 回滚策略：git revert 整个 change；上游替换是单点切换，无数据迁移（服务端历史不落地本地）。

## Open Questions

- SDAI `content` 具体长度上限（实施时压测确定，不阻塞架构）。
- token 过期响应的确切形态（实施时补探针采样，只影响判定函数实现细节）。
