package protocol

// SDAI (sdai.suda.edu.cn) 上游站点端点与请求头常量。
// 协议逆向结论见 .local/findings.md：裸 Bearer 鉴权、无 PoW、无客户端指纹要求。

const (
	SDAIHost = "sdai.suda.edu.cn"

	SDAIBaseURL = "https://sdai.suda.edu.cn/backend/api"

	// SDAIChatStartURL 发起对话（POST，返回 text/event-stream；失败时降级为
	// application/json 业务错误体 {"code","message"}，HTTP 状态恒 200）。
	SDAIChatStartURL = SDAIBaseURL + "/chat/start"
	// SDAIMsgTitleListURL 会话列表（GET，分页 page/page_size，results[].message_id 即会话 uuid）。
	SDAIMsgTitleListURL = SDAIBaseURL + "/msg_title/list"
	// SDAIMsgTitleDelURL 删除会话（DELETE，JSON body {"uuid": ...}）。
	SDAIMsgTitleDelURL = SDAIBaseURL + "/msg_title/del"
	// SDAIModelListURL 模型列表（GET，公开端点，无需鉴权）。
	SDAIModelListURL = SDAIBaseURL + "/model/list"
	// SDAIUserInfoURL 当前用户信息（GET）。
	SDAIUserInfoURL = SDAIBaseURL + "/user/info"
	// SDAIMsgListURL 会话消息记录（GET ?uuid=...）。
	SDAIMsgListURL = SDAIBaseURL + "/msg/list"
)

// SDAIContentMaxBytes 上游 content 列长度上限（MySQL TEXT ≈ 65535 bytes）。
// 超限时上游返回 HTTP 200 + 正常 SSE，但 delta 内容为 MySQL 1406 错误文本，
// 因此必须在发送前预校验。实测 ASCII 65500 bytes 可通过。
const SDAIContentMaxBytes = 65000

// SDAIContentTooLongMarker 上游 content 超限时 delta 文本的特征（MySQL 错误码）。
const SDAIContentTooLongMarker = "1406"

// SDAIChatStartReferer 伪装浏览器对话页来源。
const SDAIChatStartReferer = "https://sdai.suda.edu.cn/chat"

// BaseHeaders 所有上游请求的基础头。SDAI 无客户端指纹校验，
// 只需常规浏览器形态的头即可。
var BaseHeaders = map[string]string{
	"Content-Type":   "application/json",
	"Referer":        SDAIChatStartReferer,
	"User-Agent":     DefaultUserAgent,
	"accept-charset": "UTF-8",
}

// DefaultUserAgent 浏览器形态 UA。
const DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

const (
	KeepAliveTimeout  = 5
	StreamIdleTimeout = 300
	MaxKeepaliveCount = 40
)
