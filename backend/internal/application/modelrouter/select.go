package modelrouter

// 决策来源。
const (
	SourceRule     = "rule"
	SourceDefault  = "default"
	SourceSticky   = "sticky"
	SourceFallback = "fallback"
)

// Decision 是一次路由结果。
type Decision struct {
	Model      string
	Source     string
	Category   string
	Difficulty string
	Reason     string
}

// SelectInput 是模型选择的输入。
type SelectInput struct {
	Policy Policy
	// Candidates 已按硬约束过滤，且仅包含用户可用的聊天模型，顺序即兜底优先级。
	Candidates []Candidate
	// Classification 为 nil 表示分类失败或超时。
	Classification *Classification
	// PreviousModel 是会话上一轮路由到的模型，用于粘性。
	PreviousModel string
}

// Select 按“粘性 → 规则 → 默认 → 上一轮 → 首个候选”的顺序选择模型。
// 候选为空时返回 false。
func Select(input SelectInput) (Decision, bool) {
	available := make(map[string]struct{}, len(input.Candidates))
	for _, candidate := range input.Candidates {
		available[candidate.Name] = struct{}{}
	}
	if len(available) == 0 {
		return Decision{}, false
	}
	pick := func(models []string) string {
		for _, name := range models {
			if _, ok := available[name]; ok {
				return name
			}
		}
		return ""
	}
	previous := pick([]string{input.PreviousModel})

	classification := input.Classification
	if classification == nil {
		if previous != "" {
			return Decision{Model: previous, Source: SourceFallback}, true
		}
		if name := pick(input.Policy.DefaultModels); name != "" {
			return Decision{Model: name, Source: SourceFallback}, true
		}
		return Decision{Model: input.Candidates[0].Name, Source: SourceFallback}, true
	}

	decision := Decision{
		Category:   classification.Category,
		Difficulty: classification.Difficulty,
		Reason:     classification.Reason,
	}
	if previous != "" && !classification.Switch {
		decision.Model = previous
		decision.Source = SourceSticky
		return decision, true
	}
	if name := pick(matchRuleModels(input.Policy.Rules, classification.Category, classification.Difficulty)); name != "" {
		decision.Model = name
		decision.Source = SourceRule
		return decision, true
	}
	if name := pick(input.Policy.DefaultModels); name != "" {
		decision.Model = name
		decision.Source = SourceDefault
		return decision, true
	}
	if previous != "" {
		decision.Model = previous
		decision.Source = SourceSticky
		return decision, true
	}
	decision.Model = input.Candidates[0].Name
	decision.Source = SourceDefault
	return decision, true
}

// matchRuleModels 返回命中规则的模型：精确匹配难度的规则优先，其次是不限难度的规则。
func matchRuleModels(rules []Rule, category string, difficulty string) []string {
	var exact, any []string
	for _, rule := range rules {
		if rule.Category != category {
			continue
		}
		switch rule.Difficulty {
		case difficulty:
			exact = append(exact, rule.Models...)
		case "":
			any = append(any, rule.Models...)
		}
	}
	return append(exact, any...)
}
