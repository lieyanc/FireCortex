package conversation

import (
	"context"
	"errors"
	"testing"

	"github.com/lieyanc/FireCortex/backend/internal/application/channel"
	"github.com/lieyanc/FireCortex/backend/internal/application/modelrouter"
	"github.com/lieyanc/FireCortex/backend/internal/infra/config"
	"github.com/lieyanc/FireCortex/backend/internal/ports/llm"
)

type stubAutoRouteResolver struct {
	models []channel.ModelView
	route  *channel.ResolvedRoute
}

func (s *stubAutoRouteResolver) ResolveRoute(_ context.Context, input channel.ResolveRouteInput) (*channel.ResolvedRoute, error) {
	if s.route != nil {
		return s.route, nil
	}
	return &channel.ResolvedRoute{
		PlatformModelName: input.PlatformModelName,
		Protocol:          llm.AdapterOpenAIChatCompletions,
		UpstreamModel:     input.PlatformModelName,
	}, nil
}

func (s *stubAutoRouteResolver) ListActiveModels(_ context.Context, _ uint) ([]channel.ModelView, error) {
	return s.models, nil
}

func (s *stubAutoRouteResolver) MarkRouteFailure(_ context.Context, _ *channel.ResolvedRoute, _ error) {}

func (s *stubAutoRouteResolver) MarkRouteSuccess(_ context.Context, _ *channel.ResolvedRoute) {}

type stubAutoRouteLLMClient struct {
	text string
	err  error
}

func (s *stubAutoRouteLLMClient) Generate(_ context.Context, _ llm.RouteConfig, _ llm.GenerateInput) (*llm.GenerateOutput, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &llm.GenerateOutput{Text: s.text}, nil
}

func (s *stubAutoRouteLLMClient) GenerateStream(_ context.Context, _ llm.RouteConfig, _ llm.GenerateInput, _ func(llm.GenerateStreamEvent) error) (*llm.GenerateOutput, error) {
	return &llm.GenerateOutput{}, nil
}

func (s *stubAutoRouteLLMClient) RetrieveOpenAIResponse(_ context.Context, _ llm.RouteConfig, _ string) (*llm.GenerateOutput, error) {
	return &llm.GenerateOutput{}, nil
}

func (s *stubAutoRouteLLMClient) CancelOpenAIResponse(_ context.Context, _ llm.RouteConfig, _ string) (*llm.GenerateOutput, error) {
	return &llm.GenerateOutput{}, nil
}

func TestEncodeAndDecodeAutoRouteDecision(t *testing.T) {
	if got := EncodeAutoRouteDecision(nil); got != "" {
		t.Fatalf("expected empty string for nil decision, got %q", got)
	}
	encoded := EncodeAutoRouteDecision(&modelrouter.Decision{
		Model:      "gpt-5-mini",
		Source:     modelrouter.SourceRule,
		Category:   "coding",
		Difficulty: modelrouter.DifficultyHard,
		Reason:     "Complex refactoring task",
	})
	decoded := DecodeAutoRouteDecision(encoded)
	if decoded == nil || decoded.Model != "gpt-5-mini" || decoded.Source != modelrouter.SourceRule || decoded.Category != "coding" || decoded.Difficulty != modelrouter.DifficultyHard || decoded.Reason != "Complex refactoring task" {
		t.Fatalf("unexpected decoded decision: %+v", decoded)
	}
	if DecodeAutoRouteDecision("not json") != nil {
		t.Fatal("expected nil for invalid JSON")
	}
}

func TestTestAutoModelRouteDryRunAndLiveClassify(t *testing.T) {
	resolver := &stubAutoRouteResolver{
		models: []channel.ModelView{
			{
				PlatformModelName: "mini",
				KindsJSON:         `["chat"]`,
				ContextWindow:     16000,
				CapabilitiesJSON:  `{"routing":{"vision":false,"tools":true}}`,
			},
			{
				PlatformModelName: "vision-pro",
				KindsJSON:         `["chat"]`,
				ContextWindow:     128000,
				CapabilitiesJSON:  `{"routing":{"vision":true,"tools":true},"nativeTools":[{"key":"openai.web_search_preview","type":"web_search_preview"}]}`,
			},
		},
	}
	llmClient := &stubAutoRouteLLMClient{
		text: `{"category":"coding","difficulty":"hard","switch":true,"reason":"Hard algorithm problem"}`,
	}
	svc := &Service{
		cfg:           config.NewRuntime(config.Config{}),
		routeResolver: resolver,
		llmClient:     llmClient,
	}

	draftPolicy := `{
		"routerModel": "mini",
		"timeoutMs": 800,
		"historyMessages": 4,
		"defaultModels": ["mini"],
		"categories": [{"key":"chat"},{"key":"coding","description":"code"}],
		"rules": [{"category":"coding","difficulty":"hard","models":["mini","vision-pro"]}]
	}`

	// 1. Live classify with vision requirement filters out mini and picks vision-pro.
	liveResult, err := svc.TestAutoModelRoute(context.Background(), TestAutoRouteInput{
		UserID:          1,
		PolicyJSON:      draftPolicy,
		Message:         "Analyze this architecture screenshot and write Go code",
		NeedsVision:     true,
		NativeToolTypes: []string{"web_search"},
		DryRun:          false,
	})
	if err != nil {
		t.Fatalf("live test error: %v", err)
	}
	if len(liveResult.Candidates) != 2 || liveResult.Candidates[0].Passed || !liveResult.Candidates[1].Passed {
		t.Fatalf("unexpected candidate filter results: %+v", liveResult.Candidates)
	}
	// Only 1 candidate survived filtering -> classifier is skipped by short-circuit.
	if liveResult.Classifier.SkippedReason != "single_candidate" || liveResult.Decision.Model != "vision-pro" {
		t.Fatalf("expected single_candidate short-circuit to vision-pro, got %+v", liveResult)
	}

	// 2. Live classify with both candidates eligible invokes classifier.
	liveBoth, err := svc.TestAutoModelRoute(context.Background(), TestAutoRouteInput{
		UserID:     1,
		PolicyJSON: draftPolicy,
		Message:    "Implement a lock-free ring buffer",
		DryRun:     false,
	})
	if err != nil {
		t.Fatalf("live both error: %v", err)
	}
	if !liveBoth.Classifier.Attempted || liveBoth.Classifier.Classification == nil {
		t.Fatalf("expected live classifier attempt, got %+v", liveBoth.Classifier)
	}
	if liveBoth.Decision.Model != "mini" || liveBoth.Decision.Source != modelrouter.SourceRule || len(liveBoth.MatchedRules) != 1 {
		t.Fatalf("unexpected live decision: %+v", liveBoth)
	}

	// 3. Dry run with simulated classification and sticky model.
	dryResult, err := svc.TestAutoModelRoute(context.Background(), TestAutoRouteInput{
		UserID:        1,
		PolicyJSON:    draftPolicy,
		Message:       "Continue",
		PreviousModel: "vision-pro",
		DryRun:        true,
		SimulatedClassification: &modelrouter.Classification{
			Category:   "coding",
			Difficulty: "hard",
			Switch:     false,
			Reason:     "Same topic",
		},
	})
	if err != nil {
		t.Fatalf("dry run error: %v", err)
	}
	if dryResult.Classifier.Attempted || dryResult.Decision.Model != "vision-pro" || dryResult.Decision.Source != modelrouter.SourceSticky {
		t.Fatalf("unexpected dry run sticky decision: %+v", dryResult)
	}

	// 4. Invalid draft policy returns ErrInvalidPolicy.
	if _, err := svc.TestAutoModelRoute(context.Background(), TestAutoRouteInput{
		UserID:     1,
		PolicyJSON: `{"categories":[]}`,
		DryRun:     true,
	}); !errors.Is(err, modelrouter.ErrInvalidPolicy) {
		t.Fatalf("expected ErrInvalidPolicy, got %v", err)
	}
}
