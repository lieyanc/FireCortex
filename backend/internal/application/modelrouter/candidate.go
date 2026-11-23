package modelrouter

import (
	"encoding/json"
	"strings"
)

// Candidate 是一个可被路由选中的聊天模型及其路由相关能力。
// 能力字段为 nil 表示模型未声明，过滤时按“未知即放行”处理。
type Candidate struct {
	Name            string
	ContextWindow   int
	Vision          *bool
	Tools           *bool
	NativeToolTypes []string
}

// Requirements 描述本轮请求对模型的硬约束。
type Requirements struct {
	// NeedsVision 表示本轮带有图片附件。
	NeedsVision bool
	// NeedsTools 表示本轮选择了需要函数调用的工具（MCP 等）。
	NeedsTools bool
	// NativeToolTypes 是本轮请求开启的官方原生工具类型（如 web_search）。
	NativeToolTypes []string
	// EstimatedTokens 是本轮上下文的粗略 Token 估算，0 表示不检查。
	EstimatedTokens int
}

// CandidateFromCapabilities 从模型 CapabilitiesJSON 的 routing 块与 nativeTools 声明构造候选。
//
//	{"routing": {"vision": true, "tools": true}, "nativeTools": [{"key": "...", "type": "web_search"}]}
func CandidateFromCapabilities(name string, contextWindow int, capabilitiesJSON string) Candidate {
	candidate := Candidate{Name: strings.TrimSpace(name), ContextWindow: contextWindow}
	raw := strings.TrimSpace(capabilitiesJSON)
	if raw == "" {
		return candidate
	}
	var payload struct {
		Routing struct {
			Vision *bool `json:"vision"`
			Tools  *bool `json:"tools"`
		} `json:"routing"`
		NativeTools []struct {
			Key     string         `json:"key"`
			ToolKey string         `json:"toolKey"`
			Type    string         `json:"type"`
			Enabled *bool          `json:"enabled"`
			Payload map[string]any `json:"payload"`
		} `json:"nativeTools"`
		NativeToolKeys []string `json:"nativeToolKeys"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return candidate
	}
	candidate.Vision = payload.Routing.Vision
	candidate.Tools = payload.Routing.Tools
	for _, tool := range payload.NativeTools {
		if tool.Enabled != nil && !*tool.Enabled {
			continue
		}
		for _, value := range []string{tool.Type, stringValue(tool.Payload["type"]), tool.Key, tool.ToolKey} {
			candidate.NativeToolTypes = appendUnique(candidate.NativeToolTypes, value)
		}
	}
	for _, key := range payload.NativeToolKeys {
		candidate.NativeToolTypes = appendUnique(candidate.NativeToolTypes, key)
	}
	return candidate
}

// 硬约束未满足时的标准原因码，供测试与诊断视图展示。
const (
	ReasonMissingVision     = "missing_vision"
	ReasonMissingTools      = "missing_tools"
	ReasonMissingNativeTool = "missing_native_tool"
	ReasonContextExceeded   = "context_exceeded"
)

// UnsatisfiedReasons 返回候选未满足的硬约束原因码列表；为空表示满足全部约束。
func (c Candidate) UnsatisfiedReasons(req Requirements) []string {
	var reasons []string
	if req.NeedsVision && c.Vision != nil && !*c.Vision {
		reasons = append(reasons, ReasonMissingVision)
	}
	if req.NeedsTools && c.Tools != nil && !*c.Tools {
		reasons = append(reasons, ReasonMissingTools)
	}
	for _, toolType := range req.NativeToolTypes {
		if !c.supportsNativeTool(toolType) {
			family := NativeToolFamily(toolType)
			if family != "" {
				reasons = append(reasons, ReasonMissingNativeTool+":"+family)
			} else {
				reasons = append(reasons, ReasonMissingNativeTool)
			}
		}
	}
	if req.EstimatedTokens > 0 && c.ContextWindow > 0 && req.EstimatedTokens > c.ContextWindow {
		reasons = append(reasons, ReasonContextExceeded)
	}
	return reasons
}

// Satisfies 判断候选是否满足硬约束。
func (c Candidate) Satisfies(req Requirements) bool {
	return len(c.UnsatisfiedReasons(req)) == 0
}

func (c Candidate) supportsNativeTool(toolType string) bool {
	family := NativeToolFamily(toolType)
	if family == "" {
		return true
	}
	for _, item := range c.NativeToolTypes {
		if NativeToolFamily(item) == family {
			return true
		}
	}
	return false
}

// NativeToolFamily 把各协议的原生工具类型/键归一为跨协议的工具族，
// 例如 "anthropic.web_search_20250305"、"web_search_preview" 都归为 "web_search"。
func NativeToolFamily(value string) string {
	family := strings.ToLower(strings.TrimSpace(value))
	if index := strings.LastIndex(family, "."); index >= 0 {
		family = family[index+1:]
	}
	family = strings.TrimSuffix(family, "_preview")
	if index := strings.LastIndex(family, "_"); index >= 0 && isDateSuffix(family[index+1:]) {
		family = family[:index]
	}
	return family
}

func isDateSuffix(value string) bool {
	if len(value) != 8 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// FilterCandidates 返回满足硬约束的候选（保持原顺序）。
// 若约束排除了全部候选，则返回 nil，由调用方决定是否放宽。
func FilterCandidates(candidates []Candidate, req Requirements) []Candidate {
	result := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Name == "" {
			continue
		}
		if candidate.Satisfies(req) {
			result = append(result, candidate)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func stringValue(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func appendUnique(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
