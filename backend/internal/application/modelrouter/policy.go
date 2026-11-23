// Package modelrouter 实现 Auto 模型路由的纯逻辑：策略解析、硬约束过滤、分类提示词与结果映射。
// 调用快速 LLM、计费与持久化由 conversation 应用服务负责，本包不依赖任何基础设施。
package modelrouter

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// AutoModelName 是前端选择“自动”时提交的虚拟模型名。
const AutoModelName = "auto"

// 会话模型模式。
const (
	ModelModeManual = "manual"
	ModelModeAuto   = "auto"
)

// 难度取值；规则中留空表示匹配任意难度。
const (
	DifficultyEasy   = "easy"
	DifficultyMedium = "medium"
	DifficultyHard   = "hard"
)

const (
	DefaultTimeoutMS       = 800
	MinTimeoutMS           = 100
	MaxTimeoutMS           = 10000
	DefaultHistoryMessages = 4
	MaxHistoryMessages     = 20
	maxPolicyBytes         = 20000
	maxCategoryKeyLength   = 32
	maxDescriptionLength   = 300
	maxModelNameLength     = 128
)

// Policy 是 chat.model_router_policy 解析后的结构。
type Policy struct {
	// RouterModel 是执行分类的快速模型；留空时跟随会话标题/标签任务模型。
	RouterModel string
	// TimeoutMS 是分类调用的超时；超时后回退到粘性模型或默认模型。
	TimeoutMS int
	// HistoryMessages 是提供给分类器的最近历史消息条数。
	HistoryMessages int
	// DefaultModels 是没有规则命中时按顺序尝试的模型。
	DefaultModels []string
	Categories    []Category
	Rules         []Rule
}

// Category 是分类器可选的任务类别。
type Category struct {
	Key         string
	Description string
}

// Rule 把（类别, 难度）映射到按优先级排列的模型列表；Difficulty 留空表示任意难度。
type Rule struct {
	Category   string
	Difficulty string
	Models     []string
}

// policyDocument 是策略 JSON 的线上格式。
type policyDocument struct {
	RouterModel     string   `json:"routerModel"`
	TimeoutMS       int      `json:"timeoutMs"`
	HistoryMessages int      `json:"historyMessages"`
	DefaultModels   []string `json:"defaultModels"`
	Categories      []struct {
		Key         string `json:"key"`
		Description string `json:"description"`
	} `json:"categories"`
	Rules []struct {
		Category   string   `json:"category"`
		Difficulty string   `json:"difficulty"`
		Models     []string `json:"models"`
	} `json:"rules"`
}

func (d policyDocument) toPolicy() Policy {
	policy := Policy{
		RouterModel:     d.RouterModel,
		TimeoutMS:       d.TimeoutMS,
		HistoryMessages: d.HistoryMessages,
		DefaultModels:   d.DefaultModels,
		Categories:      make([]Category, 0, len(d.Categories)),
		Rules:           make([]Rule, 0, len(d.Rules)),
	}
	for _, item := range d.Categories {
		policy.Categories = append(policy.Categories, Category{Key: item.Key, Description: item.Description})
	}
	for _, item := range d.Rules {
		policy.Rules = append(policy.Rules, Rule{Category: item.Category, Difficulty: item.Difficulty, Models: item.Models})
	}
	return policy
}

// DefaultPolicyJSON 返回内置默认策略：只有类别，没有映射规则。
func DefaultPolicyJSON() string {
	return `{
  "routerModel": "",
  "timeoutMs": 800,
  "historyMessages": 4,
  "defaultModels": [],
  "categories": [
    {"key": "chat", "description": "Casual conversation, greetings and simple factual questions"},
    {"key": "coding", "description": "Writing, reviewing, explaining or debugging code"},
    {"key": "math", "description": "Math, logic puzzles and quantitative reasoning"},
    {"key": "writing", "description": "Long-form writing, editing, rewriting and translation"},
    {"key": "analysis", "description": "Research, planning, and analysis of documents, data or images"}
  ],
  "rules": []
}`
}

// ErrInvalidPolicy 表示策略 JSON 不合法。
var ErrInvalidPolicy = errors.New("invalid model router policy")

// ParsePolicy 解析并规范化策略；空串使用默认策略。
func ParsePolicy(raw string) (Policy, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		value = DefaultPolicyJSON()
	}
	if len(value) > maxPolicyBytes {
		return Policy{}, fmt.Errorf("%w: policy exceeds %d bytes", ErrInvalidPolicy, maxPolicyBytes)
	}
	var document policyDocument
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return Policy{}, fmt.Errorf("%w: %v", ErrInvalidPolicy, err)
	}
	policy := document.toPolicy()
	if err := policy.normalize(); err != nil {
		return Policy{}, err
	}
	return policy, nil
}

// ValidatePolicyJSON 校验策略 JSON，返回可展示给管理员的错误说明。
func ValidatePolicyJSON(raw string) error {
	_, err := ParsePolicy(raw)
	return err
}

func (p *Policy) normalize() error {
	p.RouterModel = strings.TrimSpace(p.RouterModel)
	if len(p.RouterModel) > maxModelNameLength {
		return fmt.Errorf("%w: routerModel is too long", ErrInvalidPolicy)
	}
	switch {
	case p.TimeoutMS == 0:
		p.TimeoutMS = DefaultTimeoutMS
	case p.TimeoutMS < MinTimeoutMS || p.TimeoutMS > MaxTimeoutMS:
		return fmt.Errorf("%w: timeoutMs must be between %d and %d", ErrInvalidPolicy, MinTimeoutMS, MaxTimeoutMS)
	}
	if p.HistoryMessages < 0 || p.HistoryMessages > MaxHistoryMessages {
		return fmt.Errorf("%w: historyMessages must be between 0 and %d", ErrInvalidPolicy, MaxHistoryMessages)
	}
	models, err := normalizeModelList(p.DefaultModels, "defaultModels")
	if err != nil {
		return err
	}
	p.DefaultModels = models

	if len(p.Categories) == 0 {
		return fmt.Errorf("%w: at least one category is required", ErrInvalidPolicy)
	}
	seen := make(map[string]struct{}, len(p.Categories))
	for index := range p.Categories {
		category := &p.Categories[index]
		category.Key = strings.ToLower(strings.TrimSpace(category.Key))
		category.Description = strings.TrimSpace(category.Description)
		if category.Key == "" || len(category.Key) > maxCategoryKeyLength || !isCategoryKey(category.Key) {
			return fmt.Errorf("%w: category key %q must be 1-%d chars of a-z, 0-9, _ or -", ErrInvalidPolicy, category.Key, maxCategoryKeyLength)
		}
		if len([]rune(category.Description)) > maxDescriptionLength {
			return fmt.Errorf("%w: description of category %q is too long", ErrInvalidPolicy, category.Key)
		}
		if _, ok := seen[category.Key]; ok {
			return fmt.Errorf("%w: duplicate category %q", ErrInvalidPolicy, category.Key)
		}
		seen[category.Key] = struct{}{}
	}

	for index := range p.Rules {
		rule := &p.Rules[index]
		rule.Category = strings.ToLower(strings.TrimSpace(rule.Category))
		rule.Difficulty = strings.ToLower(strings.TrimSpace(rule.Difficulty))
		if _, ok := seen[rule.Category]; !ok {
			return fmt.Errorf("%w: rule %d references unknown category %q", ErrInvalidPolicy, index+1, rule.Category)
		}
		if rule.Difficulty != "" && !isDifficulty(rule.Difficulty) {
			return fmt.Errorf("%w: rule %d difficulty must be easy, medium, hard or empty", ErrInvalidPolicy, index+1)
		}
		models, err := normalizeModelList(rule.Models, fmt.Sprintf("rule %d models", index+1))
		if err != nil {
			return err
		}
		if len(models) == 0 {
			return fmt.Errorf("%w: rule %d needs at least one model", ErrInvalidPolicy, index+1)
		}
		rule.Models = models
	}
	return nil
}

// ReferencedModels 返回策略中出现的全部模型（去重、保持首次出现顺序）。
func (p Policy) ReferencedModels() []string {
	result := make([]string, 0, len(p.DefaultModels))
	seen := make(map[string]struct{})
	add := func(values []string) {
		for _, value := range values {
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	for _, rule := range p.Rules {
		add(rule.Models)
	}
	add(p.DefaultModels)
	return result
}

func (p Policy) hasCategory(key string) bool {
	for _, category := range p.Categories {
		if category.Key == key {
			return true
		}
	}
	return false
}

func normalizeModelList(values []string, field string) ([]string, error) {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		name := strings.TrimSpace(value)
		if name == "" {
			continue
		}
		if len(name) > maxModelNameLength {
			return nil, fmt.Errorf("%w: %s contains a model name that is too long", ErrInvalidPolicy, field)
		}
		if strings.EqualFold(name, AutoModelName) {
			return nil, fmt.Errorf("%w: %s cannot reference %q", ErrInvalidPolicy, field, AutoModelName)
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	return result, nil
}

func isCategoryKey(value string) bool {
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

func isDifficulty(value string) bool {
	switch value {
	case DifficultyEasy, DifficultyMedium, DifficultyHard:
		return true
	default:
		return false
	}
}
