package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// 回归：WebUI 添加的账号在重启后 token 丢失。
// 根因：Save()/saveLocked() 沿用 DeepSeek 时代的 ClearAccountTokens
// 策略（token 可由密码重新登录获取），SDAI 下 token 就是唯一凭据，
// 持久化必须原样落盘，否则第二次启动后全部请求鉴权失败。
func TestUpdatePersistsAccountTokenAcrossReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("DS2API_CONFIG_PATH", path)
	// 避免 DS2API_CONFIG_JSON 环境变量污染本测试。
	t.Setenv("DS2API_CONFIG_JSON", "")

	store := LoadStore()

	if err := store.Update(func(c *Config) error {
		c.Accounts = append(c.Accounts, Account{Name: "main", Token: "secret-sdai-token"})
		return nil
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	raw, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("config file not persisted: %v", readErr)
	}
	var onDisk struct {
		Accounts []struct{ Token string }
	}
	if jsonErr := json.Unmarshal(raw, &onDisk); jsonErr != nil {
		t.Fatalf("decode persisted config: %v", jsonErr)
	}
	if len(onDisk.Accounts) != 1 || onDisk.Accounts[0].Token != "secret-sdai-token" {
		t.Fatalf("token missing from persisted config: %s", raw)
	}

	// 模拟第二次启动：从磁盘重新加载。
	reloaded := LoadStore()
	acc, ok := reloaded.FindAccount("main")
	if !ok {
		t.Fatalf("account lost after reload")
	}
	if acc.Token != "secret-sdai-token" {
		t.Fatalf("token lost after reload: %q", acc.Token)
	}
}
