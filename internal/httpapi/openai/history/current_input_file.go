package history

import (
	"context"

	"ds2api/internal/auth"
	"ds2api/internal/promptcompat"
)

// Service 保留 current_input_file 机制的配置读取入口。
//
// SDAI 上游没有文件上传端点，DS2API_HISTORY.txt 上下文拆分上传策略不可用：
// ApplyCurrentInputFile 短路为 no-op，长上下文直接通过 chat/start 的
// content 字段透传（上游 content 上限见 client 层预校验）。
// 配置字段 current_input_file 保留但不再产生行为差异。
type Service struct {
	Store CurrentInputConfigReader
}

// CurrentInputConfigReader 保留配置读取接口。
type CurrentInputConfigReader interface {
	CurrentInputFileEnabled() bool
	CurrentInputFileMinChars() int
}

// ApplyCurrentInputFile 短路返回原始请求（SDAI 无上传通道）。
// 历史归档所需的 HistoryText 在此补齐为全量消息 transcript，
// 保证 openai.responses / claude.messages 的归档语义与 DeepSeek 时代一致。
func (s Service) ApplyCurrentInputFile(ctx context.Context, a *auth.RequestAuth, stdReq promptcompat.StandardRequest) (promptcompat.StandardRequest, error) {
	_ = s.Store
	if stdReq.HistoryText == "" && len(stdReq.Messages) > 0 {
		stdReq.HistoryText = promptcompat.BuildOpenAICurrentInputContextTranscript(stdReq.Messages)
	}
	return stdReq, nil
}
