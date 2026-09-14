package files

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"ds2api/internal/auth"
	"ds2api/internal/chathistory"
	"ds2api/internal/httpapi/openai/shared"
)

type Handler struct {
	Store       shared.ConfigReader
	Auth        shared.AuthResolver
	DS          shared.DeepSeekCaller
	ChatHistory *chathistory.Store
}

// UploadFile SDAI 上游没有文件上传端点，OpenAI /v1/files 上传恒返回 501。
// 长上下文请直接放入 messages（受上游 content ≈65535 bytes 限制，见 client 层预校验）。
func (h *Handler) UploadFile(w http.ResponseWriter, r *http.Request) {
	a, err := h.Auth.Determine(r)
	if err != nil {
		status := http.StatusUnauthorized
		detail := err.Error()
		if err == auth.ErrNoAccount {
			status = http.StatusTooManyRequests
		}
		shared.WriteOpenAIError(w, status, detail)
		return
	}
	defer h.Auth.Release(a)
	shared.WriteOpenAIError(w, http.StatusNotImplemented,
		"file upload is not available with the SDAI upstream (no upload endpoint)")
}

// RetrieveFile SDAI 上游没有文件查询端点，恒返回 501。
func (h *Handler) RetrieveFile(w http.ResponseWriter, r *http.Request) {
	a, err := h.Auth.Determine(r)
	if err != nil {
		status := http.StatusUnauthorized
		detail := err.Error()
		if err == auth.ErrNoAccount {
			status = http.StatusTooManyRequests
		}
		shared.WriteOpenAIError(w, status, detail)
		return
	}
	defer h.Auth.Release(a)
	_ = chi.URLParam(r, "file_id")
	shared.WriteOpenAIError(w, http.StatusNotImplemented,
		"file retrieval is not available with the SDAI upstream")
}
