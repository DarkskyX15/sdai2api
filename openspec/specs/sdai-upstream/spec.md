# sdai-upstream Specification

## Purpose
定义 SDAI（sdai.suda.edu.cn）作为唯一上游站点时，本服务与之对接的可观察行为契约：completion 调用、SSE 流解析、无状态会话策略、会话清理、模型注册、鉴权与业务错误码处理。对外协议面（OpenAI/Claude/Gemini/Ollama）的行为由既有协议层保证，本能力只约束"本服务 → SDAI"这一段的行为与失败语义。

## Requirements

### Requirement: 上游 completion 调用契约
服务 MUST 通过 `POST https://sdai.suda.edu.cn/backend/api/chat/start` 调用上游对话能力，请求体 MUST 包含六个字段：`content`（归一化后的全量上下文纯文本）、`from_uuid`（普通对话恒为空字符串）、`uuid`（本次请求的会话标识）、`kb_tid_list`（恒为空数组）、`model_id`（解析后的数字模型 ID）、`think`（0 或 1）。请求 MUST 携带 `Authorization: Bearer <token>` 头与 `content-type: application/json`。服务 MUST 不发送任何 PoW、签名或客户端指纹头。

#### Scenario: 正常发起对话
- **WHEN** 客户端发起一次合法的对话请求且账号 token 有效
- **THEN** 服务向上游发送的请求体恰好包含上述六字段，且 `content` 为 promptcompat 归一化后的完整纯文本上下文

#### Scenario: token 无效
- **WHEN** 上游对 chat/start 返回鉴权失败（HTTP 401/403 或 body `code` 表示未授权）
- **THEN** 服务将该上游失败判定为"token 失效"，并按托管账号模式的既有重试/切号策略处理；若无可切换账号则向客户端返回鉴权类错误

### Requirement: SSE 流解析与双通道输出
服务 MUST 将上游 SSE 流解析为三类事件：`event: message`（`data` 为含 `choices[0].delta.content` 与 `delta.type` 的 JSON）、`event: finish`（`data` 含 `req_message_pk_id`）、`event: flag`（`data` 为 `DONE`）。`delta.type == "think"` 的增量 MUST 汇入思考通道（reasoning），`delta.type == "text"` 的增量 MUST 汇入正文通道（content）。流 MUST 在收到 `event: flag` 且 `data == DONE` 时判定正常结束；收到 `finish` 而 flag 未到达或连接中断时 MUST 视为异常截断并进入既有的空输出/中断重试策略。

#### Scenario: 思考模型的完整流
- **WHEN** 上游返回 think 段与 text 段交错的 SSE 流并以 `flag/DONE` 收尾
- **THEN** 对外输出的 reasoning_content 由全部 think 增量按序拼接，content 由全部 text 增量按序拼接，两者不互相混入

#### Scenario: 非思考模型的流
- **WHEN** 上游仅返回 `type` 为 `text` 的增量并以 `DONE` 收尾
- **THEN** reasoning_content 为空，content 为全部增量拼接

#### Scenario: 流被截断
- **WHEN** SSE 流在 `flag/DONE` 之前断开
- **THEN** 服务按异常输出处理（重试或返回上游错误），不得把半截输出当作完整回答返回

### Requirement: 无状态会话策略
服务 MUST 为每次对外对话请求生成一个新的随机 UUID 作为上游 `uuid`，MUST 不跨对外请求复用上游会话。全量对话上下文 MUST 序列化进单条 `content`（由 promptcompat 归一化层提供），服务 MUST 不依赖上游服务端保存的历史来构建上下文。切号重试、空输出补偿重试 MUST 以新的 `uuid` 重新发起，并使用与原始请求相同的归一化上下文。

#### Scenario: 相同上下文重复请求
- **WHEN** 客户端以相同 messages 发起两次请求
- **THEN** 两次请求使用不同的上游 `uuid`，且两次都携带全量归一化上下文，输出互不依赖

#### Scenario: 切号重试
- **WHEN** completion 因空输出等可重试原因触发切换账号 fresh retry
- **THEN** 重试使用新账号 token 与新 `uuid`，payload 中的归一化上下文与原始请求一致

### Requirement: 会话清理（auto_delete）
服务 MUST 支持 `auto_delete.mode` 配置：`none`（默认，不清理）、`single`（响应完成后删除本次使用的上游会话）、`all`（清空该 token 名下全部上游会话）。`single` 模式的删除 MUST 通过 `DELETE /backend/api/msg_title/del`（JSON body `{"uuid": <会话uuid>}`）执行，且 MUST 在与客户端连接解耦的独立上下文中尽力执行，删除失败只记录日志不影响响应。`all` 模式 MUST 通过 `GET /backend/api/msg_title/list` 分页枚举会话后逐个调用删除端点实现。

#### Scenario: single 模式清理
- **WHEN** `auto_delete.mode` 为 `single` 且一次对话正常完成或失败
- **THEN** 本次请求使用的上游会话被删除，客户端已收到的响应不受删除结果影响

#### Scenario: 客户端提前断连
- **WHEN** 流式响应过程中客户端断开连接且模式为 `single`
- **THEN** 会话删除仍被执行（不继承已取消的父 context）

### Requirement: 模型注册与 think 开关映射
服务 MUST 以 `GET /backend/api/model/list` 返回的模型表作为上游模型注册源，`cate` 为 `text` 的条目 MUST 注册为可对话模型并暴露其 `id` 与 `name`。对外模型名解析 MUST 将请求模型名映射到 SDAI 的数字 `model_id`；模型名带 `-nothinking` 后缀（或别名命中强制关闭语义）时 MUST 强制 `think: 0`，默认策略与请求参数可控制 `think` 的取值。

#### Scenario: 模型列表暴露
- **WHEN** 客户端请求 `/v1/models`
- **THEN** 返回的模型 ID 集合来自 SDAI 模型表（含 name 与别名映射），不包含已移除的 DeepSeek 专属模型

#### Scenario: 强制关闭思考
- **WHEN** 请求模型名解析结果带 `-nothinking` 语义
- **THEN** 发往上游的 `think` 字段为 0，且不受请求参数影响

### Requirement: 账号鉴权与 token 配置
托管账号配置 MUST 以 Bearer token 为唯一凭据形式（`accounts[].token`），服务 MUST 不实现自动登录。直通模式 MUST 保留：请求凭据不在 `keys` 中时，直接作为 SDAI token 使用。token 有效性判定 MUST 基于上游响应的 HTTP 状态与 body 业务码（SDAI 对非 chat 端点 HTTP 恒为 200，业务结果在 body `code` 字段），MUST 不依赖 DeepSeek 的 token 刷新端点。

#### Scenario: 托管账号模式
- **WHEN** 客户端使用 `keys` 中的 API key 请求
- **THEN** 服务从账号池选取一个账号并以该账号的 Bearer token 调用上游，并发槽位与等待队列行为与既有账号池模型一致

#### Scenario: 直通模式
- **WHEN** 客户端 Bearer 传入的不是配置 keys 中的值
- **THEN** 该值直接作为上游 SDAI token 使用，不经过账号池

#### Scenario: 业务错误码识别
- **WHEN** 上游返回 HTTP 200 但 body `code` 非 200（如 404"对话不存在"、400"缺少参数 uuid"）
- **THEN** 服务按业务失败处理并记录 body 中的 `message`，不得当作成功响应
