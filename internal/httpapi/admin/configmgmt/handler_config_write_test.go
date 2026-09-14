package configmgmt

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// 回归：WebUI 保存整份配置时，浏览器端账号表单不携带 token，
// updateConfig 必须回填已存 token，否则凭据被静默清空。
func TestUpdateConfigPreservesExistingAccountToken(t *testing.T) {
	h := newAdminTestHandler(t, `{
		"keys":["k1"],
		"accounts":[{"name":"main","token":"secret-sdai-token"}]
	}`)

	r := chi.NewRouter()
	r.Put("/admin/config", h.updateConfig)

	// 模拟 WebUI 提交：accounts 仅含元信息，无 token 字段。
	body := []byte(`{"accounts":[{"name":"main","remark":"edited"}],"keys":["k1","k2"]}`)
	req := httptest.NewRequest(http.MethodPut, "/admin/config", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", rec.Code, rec.Body.String())
	}

	snap := h.Store.Snapshot()
	if len(snap.Accounts) != 1 {
		t.Fatalf("unexpected accounts: %#v", snap.Accounts)
	}
	acc := snap.Accounts[0]
	if acc.Token != "secret-sdai-token" {
		t.Fatalf("existing token was dropped by config save: %#v", acc)
	}
	if acc.Remark != "edited" {
		t.Fatalf("remark update did not persist: %#v", acc)
	}

	// 用户显式提供新 token 时应正常覆盖。
	body2 := []byte(`{"accounts":[{"name":"main","token":"new-token"}]}`)
	req2 := httptest.NewRequest(http.MethodPut, "/admin/config", bytes.NewReader(body2))
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("update2 status=%d body=%s", rec2.Code, rec2.Body.String())
	}
	if got := h.Store.Snapshot().Accounts[0].Token; got != "new-token" {
		t.Fatalf("explicit token update did not persist: %q", got)
	}
}
