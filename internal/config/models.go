package config

import (
	"strings"
	"time"
)

type ModelInfo struct {
	ID         string `json:"id"`
	Object     string `json:"object"`
	Created    int64  `json:"created"`
	OwnedBy    string `json:"owned_by"`
	Permission []any  `json:"permission,omitempty"`
}
type OllamaModelInfo struct {
	Name       string `json:"name"`
	Model      string `json:"model"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modified_at"`
}
type OllamaCapabilitiesModelInfo struct {
	ID           string   `json:"id"`
	Capabilities []string `json:"capabilities"`
}

type ModelAliasReader interface {
	ModelAliases() map[string]string
}

const noThinkingModelSuffix = "-nothinking"

// sdaiBaseModels SDAI 平台 cate=text 的可对话模型（见 .local/findings.md 模型表）。
// ID 为对外模型名；数字 ID 由 SDAINumericModelID 映射到上游 model_id 字段。
var sdaiBaseModels = []ModelInfo{
	{ID: "deepseek-v4-flash", Object: "model", Created: 1677610602, OwnedBy: "sdai", Permission: []any{}},
	{ID: "deepseek-v4-pro", Object: "model", Created: 1677610602, OwnedBy: "sdai", Permission: []any{}},
	{ID: "deepseek-v3.2", Object: "model", Created: 1677610602, OwnedBy: "sdai", Permission: []any{}},
	{ID: "deepseek-v3-1-terminus", Object: "model", Created: 1677610602, OwnedBy: "sdai", Permission: []any{}},
	{ID: "deepseek-r1", Object: "model", Created: 1677610602, OwnedBy: "sdai", Permission: []any{}},
	{ID: "doubao-1-5-pro-32k-250115", Object: "model", Created: 1677610602, OwnedBy: "sdai", Permission: []any{}},
}

// sdaiNumericModelIDs 对外模型名 → SDAI 上游数字 model_id。
var sdaiNumericModelIDs = map[string]int{
	"deepseek-v4-flash":          10,
	"deepseek-v4-pro":            8,
	"deepseek-v3.2":              9,
	"deepseek-v3-1-terminus":     7,
	"deepseek-r1":                2,
	"doubao-1-5-pro-32k-250115":  6,
}

// SDAINumericModelID 返回上游 model_id。-nothinking 后缀在映射前剥离。
func SDAINumericModelID(model string) (int, bool) {
	baseModel, _ := splitNoThinkingModel(model)
	id, ok := sdaiNumericModelIDs[baseModel]
	return id, ok
}

// SDAIModelSupportsThinking 非 DeepSeek 推理系模型（如 doubao）不支持思考开关。
var sdaiThinkingCapable = map[string]bool{
	"deepseek-v4-flash":         true,
	"deepseek-v4-pro":           true,
	"deepseek-v3.2":             true,
	"deepseek-v3-1-terminus":    true,
	"deepseek-r1":               true,
	"doubao-1-5-pro-32k-250115": false,
}

var OllamaCapabilitiesModels = []OllamaCapabilitiesModelInfo{
	{ID: "deepseek-v4-flash", Capabilities: []string{"tools", "thinking"}},
	{ID: "deepseek-v4-pro", Capabilities: []string{"tools", "thinking"}},
	{ID: "deepseek-v3.2", Capabilities: []string{"tools", "thinking"}},
	{ID: "deepseek-v3-1-terminus", Capabilities: []string{"tools", "thinking"}},
	{ID: "deepseek-r1", Capabilities: []string{"thinking"}},
	{ID: "doubao-1-5-pro-32k-250115", Capabilities: []string{"tools"}},
	{ID: "deepseek-v4-flash-nothinking", Capabilities: []string{"tools"}},
	{ID: "deepseek-v4-pro-nothinking", Capabilities: []string{"tools"}},
	{ID: "deepseek-v3.2-nothinking", Capabilities: []string{"tools"}},
	{ID: "deepseek-v3-1-terminus-nothinking", Capabilities: []string{"tools"}},
}

var SDAIModels = appendNoThinkingVariants(sdaiBaseModels)
var OllamaModels = mapToOllamaModels(SDAIModels)

var claudeBaseModels = []ModelInfo{
	// Current aliases
	{ID: "claude-opus-4-6", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-sonnet-4-6", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-haiku-4-5", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},

	// Claude 4.x snapshots and prior aliases kept for compatibility
	{ID: "claude-sonnet-4-5", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-opus-4-1", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-opus-4-1-20250805", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-opus-4-0", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-opus-20250514", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-sonnet-4-5-20250929", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-sonnet-4-0", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-sonnet-20250514", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-haiku-4-5-20251001", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},

	// Claude 3.x (legacy/deprecated snapshots and aliases)
	{ID: "claude-3-7-sonnet-latest", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-7-sonnet-20250219", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-5-sonnet-latest", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-5-sonnet-20240620", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-5-sonnet-20241022", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-opus-20240229", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-sonnet-20240229", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-5-haiku-latest", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-5-haiku-20241022", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-haiku-20240307", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
}

var ClaudeModels = appendNoThinkingVariants(claudeBaseModels)

// GetModelConfig 返回模型默认 thinking 开关。
// SDAI 无联网搜索通道，search 恒为 false。
func GetModelConfig(model string) (thinking bool, search bool, ok bool) {
	baseModel, noThinking := splitNoThinkingModel(model)
	if baseModel == "" {
		return false, false, false
	}
	if _, known := sdaiNumericModelIDs[baseModel]; !known {
		return false, false, false
	}
	thinkingCapable := sdaiThinkingCapable[baseModel]
	return thinkingCapable && !noThinking, false, true
}

// GetModelType SDAI 无 model_type 概念，所有已知模型返回 "default"。
func GetModelType(model string) (modelType string, ok bool) {
	baseModel, _ := splitNoThinkingModel(model)
	if _, known := sdaiNumericModelIDs[baseModel]; known {
		return "default", true
	}
	return "", false
}

func IsSupportedDeepSeekModel(model string) bool {
	_, _, ok := GetModelConfig(model)
	return ok
}

func IsNoThinkingModel(model string) bool {
	_, noThinking := splitNoThinkingModel(model)
	return noThinking
}

func DefaultModelAliases() map[string]string {
	return map[string]string{
		// OpenAI GPT / ChatGPT families
		"chatgpt-4o":          "deepseek-v4-flash",
		"gpt-4":               "deepseek-v4-flash",
		"gpt-4-turbo":         "deepseek-v4-flash",
		"gpt-4-turbo-preview": "deepseek-v4-flash",
		"gpt-4.5-preview":     "deepseek-v4-flash",
		"gpt-4o":              "deepseek-v4-flash",
		"gpt-4o-mini":         "deepseek-v4-flash",
		"gpt-4.1":             "deepseek-v4-flash",
		"gpt-4.1-mini":        "deepseek-v4-flash",
		"gpt-4.1-nano":        "deepseek-v4-flash",
		"gpt-5":               "deepseek-v4-flash",
		"gpt-5-chat":          "deepseek-v4-flash",
		"gpt-5.1":             "deepseek-v4-flash",
		"gpt-5.1-chat":        "deepseek-v4-flash",
		"gpt-5.2":             "deepseek-v4-flash",
		"gpt-5.2-chat":        "deepseek-v4-flash",
		"gpt-5.3-chat":        "deepseek-v4-flash",
		"gpt-5.4":             "deepseek-v4-flash",
		"gpt-5.5":             "deepseek-v4-flash",
		"gpt-5-mini":          "deepseek-v4-flash",
		"gpt-5-nano":          "deepseek-v4-flash",
		"gpt-5.4-mini":        "deepseek-v4-flash",
		"gpt-5.4-nano":        "deepseek-v4-flash",
		"gpt-5-pro":           "deepseek-v4-pro",
		"gpt-5.2-pro":         "deepseek-v4-pro",
		"gpt-5.4-pro":         "deepseek-v4-pro",
		"gpt-5.5-pro":         "deepseek-v4-pro",
		"gpt-5-codex":         "deepseek-v4-pro",
		"gpt-5.1-codex":       "deepseek-v4-pro",
		"gpt-5.1-codex-mini":  "deepseek-v4-pro",
		"gpt-5.1-codex-max":   "deepseek-v4-pro",
		"gpt-5.2-codex":       "deepseek-v4-pro",
		"gpt-5.3-codex":       "deepseek-v4-pro",
		"codex-mini-latest":   "deepseek-v4-pro",

		// OpenAI reasoning / research families
		"o1":                    "deepseek-v4-pro",
		"o1-preview":            "deepseek-v4-pro",
		"o1-mini":               "deepseek-v4-pro",
		"o1-pro":                "deepseek-v4-pro",
		"o3":                    "deepseek-v4-pro",
		"o3-mini":               "deepseek-v4-pro",
		"o3-pro":                "deepseek-v4-pro",
		"o3-deep-research":      "deepseek-v4-pro",
		"o4-mini":               "deepseek-v4-pro",
		"o4-mini-deep-research": "deepseek-v4-pro",

		// Claude current and historical aliases
		"claude-opus-4-6":            "deepseek-v4-pro",
		"claude-opus-4-1":            "deepseek-v4-pro",
		"claude-opus-4-1-20250805":   "deepseek-v4-pro",
		"claude-opus-4-0":            "deepseek-v4-pro",
		"claude-opus-20250514":       "deepseek-v4-pro",
		"claude-sonnet-4-6":          "deepseek-v4-flash",
		"claude-sonnet-4-5":          "deepseek-v4-flash",
		"claude-sonnet-4-5-20250929": "deepseek-v4-flash",
		"claude-sonnet-4-0":          "deepseek-v4-flash",
		"claude-sonnet-20250514":     "deepseek-v4-flash",
		"claude-haiku-4-5":           "deepseek-v4-flash",
		"claude-haiku-4-5-20251001":  "deepseek-v4-flash",
		"claude-3-7-sonnet":          "deepseek-v4-flash",
		"claude-3-7-sonnet-latest":   "deepseek-v4-flash",
		"claude-3-7-sonnet-20250219": "deepseek-v4-flash",
		"claude-3-5-sonnet":          "deepseek-v4-flash",
		"claude-3-5-sonnet-latest":   "deepseek-v4-flash",
		"claude-3-5-sonnet-20240620": "deepseek-v4-flash",
		"claude-3-5-sonnet-20241022": "deepseek-v4-flash",
		"claude-3-5-haiku":           "deepseek-v4-flash",
		"claude-3-5-haiku-latest":    "deepseek-v4-flash",
		"claude-3-5-haiku-20241022":  "deepseek-v4-flash",
		"claude-3-opus":              "deepseek-v4-pro",
		"claude-3-opus-20240229":     "deepseek-v4-pro",
		"claude-3-sonnet":            "deepseek-v4-flash",
		"claude-3-sonnet-20240229":   "deepseek-v4-flash",
		"claude-3-haiku":             "deepseek-v4-flash",
		"claude-3-haiku-20240307":    "deepseek-v4-flash",

		// Gemini current and historical text models
		"gemini-pro":            "deepseek-v4-pro",
		"gemini-pro-latest":     "deepseek-v4-pro",
		"gemini-flash-latest":   "deepseek-v4-flash",
		"gemini-1.5-pro":        "deepseek-v4-pro",
		"gemini-1.5-flash":      "deepseek-v4-flash",
		"gemini-1.5-flash-8b":   "deepseek-v4-flash",
		"gemini-2.0-flash":      "deepseek-v4-flash",
		"gemini-2.0-flash-lite": "deepseek-v4-flash",
		"gemini-2.5-pro":        "deepseek-v4-pro",
		"gemini-2.5-flash":      "deepseek-v4-flash",
		"gemini-2.5-flash-lite": "deepseek-v4-flash",
		"gemini-3.1-pro":        "deepseek-v4-pro",
		"gemini-3-pro":          "deepseek-v4-pro",
		"gemini-3-flash":        "deepseek-v4-flash",
		"gemini-3.1-flash":      "deepseek-v4-flash",
		"gemini-3.1-flash-lite": "deepseek-v4-flash",

		"llama-3.1-70b-instruct": "deepseek-v4-flash",
		"qwen-max":               "deepseek-v4-flash",
	}
}

func ResolveModel(store ModelAliasReader, requested string) (string, bool) {
	model := lower(strings.TrimSpace(requested))
	if model == "" {
		return "", false
	}
	aliases := loadModelAliases(store)
	if IsSupportedDeepSeekModel(model) {
		return model, true
	}
	if mapped, ok := aliases[model]; ok && IsSupportedDeepSeekModel(mapped) {
		return mapped, true
	}
	baseModel, noThinking := splitNoThinkingModel(model)
	if mapped, ok := aliases[baseModel]; ok && IsSupportedDeepSeekModel(mapped) {
		return withNoThinkingVariant(mapped, noThinking), true
	}
	return "", false
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func OpenAIModelsResponse() map[string]any {
	return map[string]any{"object": "list", "data": SDAIModels}
}

func OpenAIModelByID(store ModelAliasReader, id string) (ModelInfo, bool) {
	canonical, ok := ResolveModel(store, id)
	if !ok {
		return ModelInfo{}, false
	}
	for _, model := range SDAIModels {
		if model.ID == canonical {
			return model, true
		}
	}
	return ModelInfo{}, false
}

func OllamaModelsResponse() map[string]any {
	return map[string]any{"models": OllamaModels}
}

func OllamaModelByID(store ModelAliasReader, id string) (OllamaCapabilitiesModelInfo, bool) {
	canonical, ok := ResolveModel(store, id)
	if !ok {
		return OllamaCapabilitiesModelInfo{}, false
	}
	for _, model := range OllamaCapabilitiesModels {
		if model.ID == canonical {
			return model, true
		}
	}
	return OllamaCapabilitiesModelInfo{}, false
}

func ClaudeModelsResponse() map[string]any {
	resp := map[string]any{"object": "list", "data": ClaudeModels}
	if len(ClaudeModels) > 0 {
		resp["first_id"] = ClaudeModels[0].ID
		resp["last_id"] = ClaudeModels[len(ClaudeModels)-1].ID
	} else {
		resp["first_id"] = nil
		resp["last_id"] = nil
	}
	resp["has_more"] = false
	return resp
}

func appendNoThinkingVariants(models []ModelInfo) []ModelInfo {
	out := make([]ModelInfo, 0, len(models)*2)
	for _, model := range models {
		out = append(out, model)
		variant := model
		variant.ID = withNoThinkingVariant(model.ID, true)
		out = append(out, variant)
	}
	return out
}
func mapToOllamaModels(models []ModelInfo) []OllamaModelInfo {
	out := make([]OllamaModelInfo, 0, len(models))
	for _, model := range models {
		var modifiedAt string
		if model.Created > 0 {
			modifiedAt = time.Unix(model.Created, 0).Format(time.RFC3339)
		}
		ollamaModel := OllamaModelInfo{
			Name:       model.ID,
			Model:      model.ID,
			Size:       0,
			ModifiedAt: modifiedAt,
		}
		out = append(out, ollamaModel)
	}
	return out
}

func splitNoThinkingModel(model string) (string, bool) {
	model = lower(strings.TrimSpace(model))
	if strings.HasSuffix(model, noThinkingModelSuffix) {
		return strings.TrimSuffix(model, noThinkingModelSuffix), true
	}
	return model, false
}

func withNoThinkingVariant(model string, enabled bool) string {
	baseModel, _ := splitNoThinkingModel(model)
	if !enabled {
		return baseModel
	}
	if baseModel == "" {
		return ""
	}
	return baseModel + noThinkingModelSuffix
}

func loadModelAliases(store ModelAliasReader) map[string]string {
	aliases := DefaultModelAliases()
	if store != nil {
		for k, v := range store.ModelAliases() {
			aliases[lower(strings.TrimSpace(k))] = lower(strings.TrimSpace(v))
		}
	}
	return aliases
}
