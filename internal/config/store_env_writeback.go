package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func envWritebackEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("DS2API_ENV_WRITEBACK")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func (s *Store) IsEnvWritebackEnabled() bool {
	return envWritebackEnabled()
}

func (s *Store) HasEnvConfigSource() bool {
	rawCfg := strings.TrimSpace(os.Getenv("DS2API_CONFIG_JSON"))
	return rawCfg != ""
}

func (s *Store) ConfigPath() string {
	return s.path
}

func writeConfigFile(path string, cfg Config) error {
	// SDAI：token 是凭据本体，env writeback 落盘时同样保留。
	persistCfg := cfg.Clone()
	b, err := json.MarshalIndent(persistCfg, "", "  ")
	if err != nil {
		return err
	}
	return writeConfigBytes(path, b)
}

func writeConfigBytes(path string, b []byte) error {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		// config.json 含 SDAI token 凭据，权限收紧为仅属主可读写。
		return os.WriteFile(path, b, 0o600)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir config dir: %w", err)
	}
	return os.WriteFile(path, b, 0o600)
}
