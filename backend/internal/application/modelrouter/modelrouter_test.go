package modelrouter

import (
	"errors"
	"strings"
	"testing"
)

func testPolicy(t *testing.T) Policy {
	t.Helper()
	policy, err := ParsePolicy(`{
  "routerModel": "fast",
  "timeoutMs": 500,
  "defaultModels": ["general"],
  "categories": [{"key": "chat"}, {"key": "Coding", "description": "code"}],
  "rules": [
    {"category": "coding", "models": ["coder", "general"]},
    {"category": "coding", "difficulty": "hard", "models": ["big-coder"]},
    {"category": "chat", "difficulty": "easy", "models": ["mini"]}
  ]
}`)
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	return policy
}

func TestParsePolicyDefaultAndNormalize(t *testing.T) {
	policy, err := ParsePolicy("")
	if err != nil {
		t.Fatalf("default policy: %v", err)
	}
	if policy.TimeoutMS != DefaultTimeoutMS || len(policy.Categories) == 0 {
		t.Fatalf("unexpected default policy: %+v", policy)
	}
	parsed := testPolicy(t)
	if parsed.Categories[1].Key != "coding" {
		t.Fatalf("category key not normalized: %q", parsed.Categories[1].Key)
	}
	got := parsed.ReferencedModels()
	want := []string{"coder", "general", "big-coder", "mini"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ReferencedModels = %v, want %v", got, want)
	}
}

func TestParsePolicyRejectsInvalid(t *testing.T) {
	cases := map[string]string{
		"unknown field":    `{"categories":[{"key":"a"}],"bogus":1}`,
		"no categories":    `{"categories":[]}`,
		"bad key":          `{"categories":[{"key":"has space"}]}`,
		"duplicate":        `{"categories":[{"key":"a"},{"key":"A"}]}`,
		"unknown category": `{"categories":[{"key":"a"}],"rules":[{"category":"b","models":["m"]}]}`,
		"bad difficulty":   `{"categories":[{"key":"a"}],"rules":[{"category":"a","difficulty":"x","models":["m"]}]}`,
		"empty models":     `{"categories":[{"key":"a"}],"rules":[{"category":"a","models":[" "]}]}`,
		"auto reference":   `{"categories":[{"key":"a"}],"defaultModels":["Auto"]}`,
		"timeout":          `{"categories":[{"key":"a"}],"timeoutMs":5}`,
	}
	for name, raw := range cases {
		if _, err := ParsePolicy(raw); !errors.Is(err, ErrInvalidPolicy) {
			t.Errorf("%s: expected ErrInvalidPolicy, got %v", name, err)
		}
	}
}

func TestCandidateFromCapabilitiesAndFilter(t *testing.T) {
	vision := CandidateFromCapabilities("vision", 128000, `{"routing":{"vision":true,"tools":true},"nativeTools":[{"key":"anthropic.web_search_20250305"}]}`)
	textOnly := CandidateFromCapabilities("text", 8000, `{"routing":{"vision":false}}`)
	unknown := CandidateFromCapabilities("unknown", 0, `not json`)

	req := Requirements{NeedsVision: true, NativeToolTypes: []string{"web_search_preview"}}
	got := FilterCandidates([]Candidate{textOnly, vision, unknown}, req)
	if len(got) != 1 || got[0].Name != "vision" {
		t.Fatalf("filter vision+web_search = %+v", got)
	}

	got = FilterCandidates([]Candidate{textOnly, unknown}, Requirements{NeedsVision: true})
	if len(got) != 1 || got[0].Name != "unknown" {
		t.Fatalf("undeclared capability should pass: %+v", got)
	}

	got = FilterCandidates([]Candidate{textOnly, vision}, Requirements{EstimatedTokens: 10000})
	if len(got) != 1 || got[0].Name != "vision" {
		t.Fatalf("context window filter = %+v", got)
	}

	if FilterCandidates([]Candidate{textOnly}, Requirements{NeedsVision: true}) != nil {
		t.Fatal("expected nil when every candidate is excluded")
	}
}

func TestNativeToolFamily(t *testing.T) {
	cases := map[string]string{
		"anthropic.web_search_20250305": "web_search",
		"web_search_preview":            "web_search",
		"openai.code_interpreter":       "code_interpreter",
		" Web_Search ":                  "web_search",
	}
	for input, want := range cases {
		if got := NativeToolFamily(input); got != want {
			t.Errorf("NativeToolFamily(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestParseClassification(t *testing.T) {
	policy := testPolicy(t)
	got, err := ParseClassification(policy, "```json\n{\"category\":\"Coding\",\"difficulty\":\"HARD\",\"switch\":\"false\",\"reason\":\"code\"}\n```")
	if err != nil {
		t.Fatalf("ParseClassification: %v", err)
	}
	if got.Category != "coding" || got.Difficulty != DifficultyHard || got.Switch {
		t.Fatalf("unexpected classification: %+v", got)
	}

	got, err = ParseClassification(policy, `Sure: {"category":"chat","difficulty":"weird"} done`)
	if err != nil {
		t.Fatalf("embedded JSON: %v", err)
	}
	if got.Difficulty != DifficultyMedium || !got.Switch {
		t.Fatalf("expected medium difficulty and switch default true: %+v", got)
	}

	if _, err := ParseClassification(policy, `{"category":"poetry"}`); err == nil {
		t.Fatal("expected error for unknown category")
	}
	if _, err := ParseClassification(policy, `no json`); err == nil {
		t.Fatal("expected error for missing JSON")
	}
}

func TestBuildClassifierMessages(t *testing.T) {
	policy := testPolicy(t)
	system, user := BuildClassifierMessages(policy, ClassifyInput{
		Message:          strings.Repeat("x", maxPromptCurrentRunes+10),
		History:          []HistoryMessage{{Role: "assistant", Content: "hi"}, {Role: "user", Content: " "}},
		Attachments:      []string{"image"},
		HasPreviousRoute: true,
	})
	if !strings.Contains(system, "- coding: code") || !strings.Contains(system, "different model") {
		t.Fatalf("system prompt missing content: %s", system)
	}
	if !strings.Contains(user, "[assistant] hi") || strings.Contains(user, "[user]") {
		t.Fatalf("history rendering wrong: %s", user)
	}
	if !strings.Contains(user, "<attachments>image</attachments>") || !strings.Contains(user, "…") {
		t.Fatalf("user prompt missing attachments or truncation: %s", user)
	}
}

func candidates(names ...string) []Candidate {
	items := make([]Candidate, 0, len(names))
	for _, name := range names {
		items = append(items, Candidate{Name: name})
	}
	return items
}

func TestSelect(t *testing.T) {
	policy := testPolicy(t)
	all := candidates("mini", "general", "coder", "big-coder")

	cases := []struct {
		name       string
		candidates []Candidate
		class      *Classification
		previous   string
		wantModel  string
		wantSource string
	}{
		{"exact difficulty rule wins", all, &Classification{Category: "coding", Difficulty: "hard", Switch: true}, "", "big-coder", SourceRule},
		{"any-difficulty rule", all, &Classification{Category: "coding", Difficulty: "easy", Switch: true}, "", "coder", SourceRule},
		{"unavailable rule model skipped", candidates("general", "mini"), &Classification{Category: "coding", Difficulty: "hard", Switch: true}, "", "general", SourceRule},
		{"no rule uses default", all, &Classification{Category: "chat", Difficulty: "hard", Switch: true}, "", "general", SourceDefault},
		{"sticky when no switch", all, &Classification{Category: "chat", Difficulty: "easy", Switch: false}, "coder", "coder", SourceSticky},
		{"switch ignores sticky", all, &Classification{Category: "chat", Difficulty: "easy", Switch: true}, "coder", "mini", SourceRule},
		{"sticky model filtered out", candidates("mini", "general"), &Classification{Category: "chat", Difficulty: "easy", Switch: false}, "coder", "mini", SourceRule},
		{"fallback keeps previous", all, nil, "coder", "coder", SourceFallback},
		{"fallback default", all, nil, "", "general", SourceFallback},
		{"fallback first candidate", candidates("other"), nil, "", "other", SourceFallback},
	}
	for _, tc := range cases {
		got, ok := Select(SelectInput{Policy: policy, Candidates: tc.candidates, Classification: tc.class, PreviousModel: tc.previous})
		if !ok || got.Model != tc.wantModel || got.Source != tc.wantSource {
			t.Errorf("%s: got %+v ok=%v, want %s/%s", tc.name, got, ok, tc.wantModel, tc.wantSource)
		}
	}

	if _, ok := Select(SelectInput{Policy: policy}); ok {
		t.Fatal("expected no decision without candidates")
	}
}

func TestUnsatisfiedReasonsAndMatchRules(t *testing.T) {
	c := CandidateFromCapabilities("small-text", 8000, `{"routing":{"vision":false,"tools":false}}`)
	reasons := c.UnsatisfiedReasons(Requirements{
		NeedsVision:     true,
		NeedsTools:      true,
		NativeToolTypes: []string{"anthropic.web_search_20250305"},
		EstimatedTokens: 12000,
	})
	wantReasons := strings.Join([]string{
		ReasonMissingVision,
		ReasonMissingTools,
		ReasonMissingNativeTool + ":web_search",
		ReasonContextExceeded,
	}, ",")
	if strings.Join(reasons, ",") != wantReasons {
		t.Fatalf("UnsatisfiedReasons = %v, want %s", reasons, wantReasons)
	}

	policy := testPolicy(t)
	matched := MatchRules(policy.Rules, "coding", DifficultyHard)
	if len(matched) != 2 || matched[0].Difficulty != DifficultyHard || matched[1].Difficulty != "" {
		t.Fatalf("MatchRules ordering unexpected: %+v", matched)
	}
}
