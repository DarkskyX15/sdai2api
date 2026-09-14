# Tasks: Replace Upstream With SDAI

## 1. 协议采样与测试基线（先固化样本，再动代码）

- [x] 1.1 用过期/无效 token 跑补探针，采样 chat/start 与 msg_title/del 的真实失败响应（HTTP 状态 + body `code`），结果记入 `.local/findings.md`（结论：HTTP 200 + `{"code":401}`，content-type 降级为 application/json）
- [x] 1.2 压测 `content` 长度上限（10k/50k/128k 字符），记录上游行为（截断/报错形态）（结论：≈65535 bytes TEXT 列上限，超限以 text delta 回传 MySQL 1406 错误 + flag DONE；新增 `event: cate` 格式头事件需容忍）
- [x] 1.3 将已有抓包（`exp3_second_turn.log`、`exp6_think.log`）整理为 `tests/raw_stream_samples/` 的 SDAI 回放样本（think=0 多轮、think=1 思考流各一组，含 meta.json）（同内容 live 重采：`sdai-multiturn-think0-20260914`、`sdai-thinking-stream-think1-20260914`）
- [x] 1.4 备份 DeepSeek 现状基线：记录当前 `run-unit-all.sh` 通过的测试集合，作为替换期间回归对照（Go 40 包全过 + Node 152 过；注意本机需 `GOPROXY=https://goproxy.cn,direct`）

## 2. 上游客户端层（`internal/deepseek` 原地重写）

- [x] 2.1 重写 `protocol/constants.go`：SDAI 端点常量（chat/start、msg_title/list、msg_title/del、model/list、user/info）与基础请求头（Bearer、referer、UA），删除 PoW/伪装头/SkipPatterns
- [x] 2.2 重写 `client_auth.go`：删除 `Login`/`CreateSession`/`GetPow`，`authHeaders()` 改为 Bearer；新增本地 UUID 生成替代 `CreateSession`；新增 body `code` 业务错误解析工具（HTTP 200 但 `code != 200` 判失败）
- [x] 2.3 重写 `client_completion.go`：`CallCompletion` 调用 chat/start，payload 由调用方注入；删除 fallback/PoW 逻辑，退化为单 client 直连；新增 content 长度预校验与非 SSE 响应（业务错误体）识别
- [x] 2.4 删除 `client_session.go`、`client_session_delete.go` 中 DeepSeek 实现，新增 `DeleteSessionForToken`（DELETE msg_title/del）与 `DeleteAllSessionsForToken`（list 分页 + 逐个 DELETE）
- [x] 2.5 删除 `client_continue.go`（自动续写）与 `client_upload.go`（文件上传）、`client/pow.go`；同步删除 `transport/` 中 utls 指纹逻辑，退化为普通 HTTP client（保留账号级代理能力 `proxy.go`）
- [x] 2.6 更新 `errors.go`：`FailureKind`/`isTokenInvalid` 等价判定按 1.1 采样结果实现（401/403 或 body 401 类 code）
- [x] 2.7 删除仓库根 `pow/` 包及全部引用

## 3. SSE 解析与流引擎

- [x] 3.1 重写 `internal/sse/parser.go`：三事件语义（message/finish/flag），`delta.type: think` → reasoning 通道、`text` → content 通道，`flag/DONE` → 正常结束；容忍 `\r\n` 与分包边界
- [x] 3.2 更新 `internal/sse` 内 `CollectStream`/`ConsumeSSE`/pump 适配点（引擎框架不动、仅换 parser；删除 citation/content_filter 专属文件，CollectResult 字段契约保留）
- [x] 3.3 用 SDAI 语义写 parser/line/consumer/pump 单测：think/text 拼接正确性、未知事件容忍（cate）、finish 消息 ID、DONE 语义、长行、取消

## 4. 归一化与模型层

- [x] 4.1 重写 `promptcompat/standard_request.go` 的 `CompletionPayload()`：六字段 `{content, from_uuid, uuid, kb_tid_list, model_id, think}`；未知模型回退 flash(10)
- [x] 4.2 处理 `current_input_file` 的降级：`ApplyCurrentInputFile` 短路为 no-op（`HistoryText` 归档 transcript 补齐），inline file 输入 501 拒绝
- [x] 4.3 重写 `config/models.go`：静态 SDAI 模型表 + `SDAINumericModelID` 映射；`-nothinking` 强制 `think:0`；doubao 系不支持思考
- [x] 4.4 更新 `config.go`/`account.go`：token 为唯一凭据（保留 env 配置中的 token 不再清除），email/mobile/password 打迁移警告；`model_aliases` 机制保持
- [x] 4.5 更新 `config.example.json`（token 账号示例 + 注释）

## 5. 编排层与协议入口适配

- [x] 5.1 裁剪 `completionruntime` 接口：`DeepSeekCaller` 移除 `GetPow`/`UploadFile`，`StartCompletion` 删除 PoW 步骤；空输出重试/切号逻辑保持（新 uuid + content 后缀）
- [x] 5.2 更新全部测试桩与新接口对齐
- [x] 5.3 更新 `httpapi/*/deps.go` 各 consumer 接口与 `server/router.go` 装配点（Resolver 去 LoginFunc）
- [x] 5.4 确认 auto_delete：`single` 走 DeleteSessionForToken（DELETE msg_title/del）；`all` 走 list+逐删；detached context 行为保留
- [x] 5.5 裁剪 Vercel 分支：`vercel_stream.go` 删 Pow 段（恒空 pow_header 兼容 Node 桥）、`internal/js/chat-stream/` 重写为 SDAI（无 PoW/continue/content_filter）、DONE 帧适配
- [x] 5.6 直通 token 模式回归：非 keys 凭据直接作为 SDAI token（`auth.Resolver` 路径）

## 6. Admin / WebUI

- [x] 6.1 Admin 账号管理：账号测试（token 探活/建会话）、导入导出、配置热更新适配 token 字段（import/export 保留 token）
- [x] 6.2 WebUI：添加账号表单改为 token 输入（i18n 中英文）；`npm run build` 构建通过
- [x] 6.3 Admin dev capture（`/admin/dev/captures`）在新 client 上保持可用（逆向迭代基建不丢）

## 7. 测试与门禁

- [x] 7.1 全量单测：`./tests/scripts/run-unit-all.sh` 通过（Go 40 包全绿 + Node 121 过）
- [x] 7.2 `tests/compat/fixtures`（sse_chunks/expected）按 SDAI 事件结构重做（5 组 fixtures；toolcalls/token fixtures 复用），Go/Node compat 测试同步
- [x] 7.3 E2E live 冒烟（真实 token）：非流式 200（content+reasoning_content+usage）、流式 SSE（reasoning→content→finish→[DONE]）、auto_delete single 生效（msg_title/list 清空）、admin config API 正常
- [ ] 7.4 lint 门禁：`./scripts/lint.sh`（本机 mingw 无法 bootstrap golangci-lint；已用 `go vet ./...` 全绿代替，gofmt -l 告警为 CRLF 检出历史问题非本次引入；CI/Unix 环境需补跑 lint.sh 与行数门禁）
- [ ] 7.5 删除 DeepSeek 残留：全仓 grep `deepseek.com`/`DeepSeekHashV1`/`x-ds-pow` 等关键词清零（历史文档引用除外，见 8.x）

## 8. 文档同步

- [x] 8.1 `docs/prompt-compatibility.md`：上游替换说明（payload/SSE/current_input_file/重试差异对照表）、归一化链路不变点
- [x] 8.2 `API.md` / `API.en.md`：头部迁移横幅（模型表、鉴权、auto_delete all 警告、文件 501）+ `X-Ds2-Target-Account` 标识规则更新
- [x] 8.3 `README.MD`：定位说明（SDAI 上游）、能力表、SDAI 模型表、配置说明、鉴权模式、迁移指引（email/password → token）（README.en.md 未同步，待后续）
- [x] 8.4 `docs/DEPLOY.md`：源码部署段配置字段变更提示与迁移注释
