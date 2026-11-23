package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	appconversation "github.com/lieyanc/FireCortex/backend/internal/application/conversation"
	"github.com/lieyanc/FireCortex/backend/internal/application/modelrouter"
)

type stubModelRouterTester struct {
	result *appconversation.TestAutoRouteResult
	err    error
	last   appconversation.TestAutoRouteInput
}

func (s *stubModelRouterTester) TestAutoModelRoute(_ context.Context, input appconversation.TestAutoRouteInput) (*appconversation.TestAutoRouteResult, error) {
	s.last = input
	return s.result, s.err
}

func TestModelRouterTestEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 1. Unavailable service -> 503.
	hNil := &Handler{}
	rNil := gin.New()
	rNil.POST("/admin/settings/model-router/test", hNil.TestModelRouter)
	rec := httptest.NewRecorder()
	rNil.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/admin/settings/model-router/test", bytes.NewBufferString(`{}`)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when tester is nil, got %d", rec.Code)
	}

	// 2. Invalid policy -> 400.
	tester := &stubModelRouterTester{err: modelrouter.ErrInvalidPolicy}
	h := &Handler{modelRouter: tester}
	r := gin.New()
	r.POST("/admin/settings/model-router/test", h.TestModelRouter)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/admin/settings/model-router/test", bytes.NewBufferString(`{"policyJSON":"{}"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid policy, got %d", rec.Code)
	}

	// 3. Valid response -> 200 with structured payload.
	vision := true
	tester.err = nil
	tester.result = &appconversation.TestAutoRouteResult{
		Requirements: modelrouter.Requirements{NeedsVision: true, EstimatedTokens: 120},
		Candidates: []appconversation.TestAutoRouteCandidate{
			{Name: "gpt-5", ContextWindow: 128000, Vision: &vision, Passed: true},
		},
		Classifier: appconversation.TestAutoRouteClassifierTrace{
			Attempted:   true,
			RouterModel: "gpt-5-mini",
			LatencyMS:   42,
			Classification: &modelrouter.Classification{
				Category:   "analysis",
				Difficulty: "medium",
				Switch:     true,
				Reason:     "Image analysis",
			},
		},
		MatchedRules: []modelrouter.MatchedRule{{Index: 0, Category: "analysis", Difficulty: "", Models: []string{"gpt-5"}}},
		Decision:     modelrouter.Decision{Model: "gpt-5", Source: modelrouter.SourceRule, Category: "analysis", Difficulty: "medium", Reason: "Image analysis"},
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/admin/settings/model-router/test", bytes.NewBufferString(`{"message":"Check chart","needsVision":true}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data ModelRouterTestResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if payload.Data.Decision.Model != "gpt-5" || payload.Data.Classifier.LatencyMS != 42 || len(payload.Data.Candidates) != 1 {
		t.Fatalf("unexpected response payload: %+v", payload.Data)
	}
}
