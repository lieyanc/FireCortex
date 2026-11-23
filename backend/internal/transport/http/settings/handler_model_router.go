package settings

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	appconversation "github.com/lieyanc/FireCortex/backend/internal/application/conversation"
	"github.com/lieyanc/FireCortex/backend/internal/application/modelrouter"
	"github.com/lieyanc/FireCortex/backend/internal/shared/apperr"
	"github.com/lieyanc/FireCortex/backend/internal/shared/response"
	"github.com/lieyanc/FireCortex/backend/internal/transport/http/middleware"
)

// ModelRouterTestHistoryItem 是试跑请求中的单条模拟历史消息。
type ModelRouterTestHistoryItem struct {
	Role    string `json:"role" binding:"omitempty,oneof=user assistant"`
	Content string `json:"content" binding:"max=4000"`
}

// ModelRouterTestClassificationInput 是 Dry-Run 模式下管理员手动指定的模拟分类结果。
type ModelRouterTestClassificationInput struct {
	Category   string `json:"category" binding:"max=64"`
	Difficulty string `json:"difficulty" binding:"omitempty,oneof=easy medium hard"`
	Switch     bool   `json:"switch"`
	Reason     string `json:"reason" binding:"max=200"`
}

// ModelRouterTestRequest 是管理端自动模型路由沙盒与在线试跑请求体。
type ModelRouterTestRequest struct {
	// PolicyJSON 为空时使用当前已保存的生效策略；非空时使用该草稿策略试跑。
	PolicyJSON              string                              `json:"policyJSON" binding:"max=25000"`
	Message                 string                              `json:"message" binding:"max=8000"`
	History                 []ModelRouterTestHistoryItem        `json:"history" binding:"max=20,dive"`
	PreviousModel           string                              `json:"previousModel" binding:"max=128"`
	NeedsVision             bool                                `json:"needsVision"`
	NeedsTools              bool                                `json:"needsTools"`
	NativeToolTypes         []string                            `json:"nativeToolTypes" binding:"max=16,dive,max=128"`
	EstimatedTokens         int                                 `json:"estimatedTokens" binding:"min=0,max=10000000"`
	DryRun                  bool                                `json:"dryRun"`
	SimulatedClassification *ModelRouterTestClassificationInput `json:"simulatedClassification"`
}

// ModelRouterTestRequirementsResponse 返回生效的硬约束快照。
type ModelRouterTestRequirementsResponse struct {
	NeedsVision     bool     `json:"needsVision"`
	NeedsTools      bool     `json:"needsTools"`
	NativeToolTypes []string `json:"nativeToolTypes"`
	EstimatedTokens int      `json:"estimatedTokens"`
}

// ModelRouterTestCandidateResponse 描述单个候选模型在硬约束过滤阶段的结果。
type ModelRouterTestCandidateResponse struct {
	Name            string   `json:"name"`
	ContextWindow   int      `json:"contextWindow"`
	Vision          *bool    `json:"vision" extensions:"x-nullable,!x-omitempty"`
	Tools           *bool    `json:"tools" extensions:"x-nullable,!x-omitempty"`
	NativeToolTypes []string `json:"nativeToolTypes"`
	Passed          bool     `json:"passed"`
	FailedReasons   []string `json:"failedReasons"`
}

// ModelRouterTestClassificationResponse 表示分类器的结构化分类结果。
type ModelRouterTestClassificationResponse struct {
	Category   string `json:"category"`
	Difficulty string `json:"difficulty"`
	Switch     bool   `json:"switch"`
	Reason     string `json:"reason"`
}

// ModelRouterTestClassifierResponse 返回分类器调用诊断。
type ModelRouterTestClassifierResponse struct {
	Attempted      bool                                   `json:"attempted"`
	SkippedReason  string                                 `json:"skippedReason,omitempty"`
	RouterModel    string                                 `json:"routerModel,omitempty"`
	LatencyMS      int64                                  `json:"latencyMS"`
	RawOutput      string                                 `json:"rawOutput,omitempty"`
	Error          string                                 `json:"error,omitempty"`
	Classification *ModelRouterTestClassificationResponse `json:"classification,omitempty"`
}

// ModelRouterTestMatchedRuleResponse 描述命中的路由规则。
type ModelRouterTestMatchedRuleResponse struct {
	Index      int      `json:"index"`
	Category   string   `json:"category"`
	Difficulty string   `json:"difficulty"`
	Models     []string `json:"models"`
}

// ModelRouterTestDecisionResponse 返回最终路由决策结果。
type ModelRouterTestDecisionResponse struct {
	Model      string `json:"model"`
	Source     string `json:"source"`
	Category   string `json:"category,omitempty"`
	Difficulty string `json:"difficulty,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// ModelRouterTestResponse 是自动模型路由在线试跑的完整分步结果。
type ModelRouterTestResponse struct {
	Requirements       ModelRouterTestRequirementsResponse  `json:"requirements"`
	Candidates         []ModelRouterTestCandidateResponse   `json:"candidates"`
	ConstraintsRelaxed bool                                 `json:"constraintsRelaxed"`
	Classifier         ModelRouterTestClassifierResponse    `json:"classifier"`
	MatchedRules       []ModelRouterTestMatchedRuleResponse `json:"matchedRules"`
	Decision           ModelRouterTestDecisionResponse      `json:"decision"`
}

// ModelRouterTestResponseDoc 自动模型路由在线试跑响应文档。
type ModelRouterTestResponseDoc struct {
	ErrorMsg string                  `json:"errorMsg"`
	Data     ModelRouterTestResponse `json:"data"`
}

// TestModelRouter godoc
// @Summary 试跑自动模型路由策略
// @Description 支持传入未保存的草稿策略与模拟硬约束，执行 Dry-Run 推演或真实调用分类器，返回每一步过滤与决策明细
// @Tags admin/settings
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body ModelRouterTestRequest true "路由试跑参数"
// @Success 200 {object} ModelRouterTestResponseDoc
// @Failure 400 {object} response.Envelope
// @Failure 401 {object} response.Envelope
// @Failure 503 {object} response.Envelope
// @Router /admin/settings/model-router/test [post]
func (h *Handler) TestModelRouter(c *gin.Context) {
	if h.modelRouter == nil {
		response.ErrorFrom(c, http.StatusServiceUnavailable, errModelRouterServiceUnavailable)
		return
	}
	var req ModelRouterTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.InvalidRequestBody(c, err)
		return
	}
	history := make([]modelrouter.HistoryMessage, 0, len(req.History))
	for _, item := range req.History {
		if strings.TrimSpace(item.Content) == "" {
			continue
		}
		history = append(history, modelrouter.HistoryMessage{
			Role:    item.Role,
			Content: item.Content,
		})
	}
	var simulated *modelrouter.Classification
	if req.SimulatedClassification != nil && strings.TrimSpace(req.SimulatedClassification.Category) != "" {
		simulated = &modelrouter.Classification{
			Category:   req.SimulatedClassification.Category,
			Difficulty: req.SimulatedClassification.Difficulty,
			Switch:     req.SimulatedClassification.Switch,
			Reason:     req.SimulatedClassification.Reason,
		}
	}
	result, err := h.modelRouter.TestAutoModelRoute(c.Request.Context(), appconversation.TestAutoRouteInput{
		UserID:                  middleware.MustUserID(c),
		RequestID:               middleware.MustRequestID(c),
		PolicyJSON:              req.PolicyJSON,
		Message:                 req.Message,
		History:                 history,
		PreviousModel:           req.PreviousModel,
		NeedsVision:             req.NeedsVision,
		NeedsTools:              req.NeedsTools,
		NativeToolTypes:         req.NativeToolTypes,
		EstimatedTokens:         req.EstimatedTokens,
		DryRun:                  req.DryRun,
		SimulatedClassification: simulated,
	})
	if err != nil {
		switch {
		case errors.Is(err, modelrouter.ErrInvalidPolicy):
			detail := strings.TrimPrefix(err.Error(), modelrouter.ErrInvalidPolicy.Error()+": ")
			response.ErrorFrom(c, http.StatusBadRequest, apperr.New("settings.model_router_policy_invalid", detail))
		case errors.Is(err, appconversation.ErrModelRouteNotConfigured):
			response.ErrorFrom(c, http.StatusServiceUnavailable, err)
		default:
			response.InternalError(c, err)
		}
		return
	}
	response.Success(c, toModelRouterTestResponse(result))
}

func toModelRouterTestResponse(result *appconversation.TestAutoRouteResult) ModelRouterTestResponse {
	nativeTools := result.Requirements.NativeToolTypes
	if nativeTools == nil {
		nativeTools = []string{}
	}
	candidates := make([]ModelRouterTestCandidateResponse, 0, len(result.Candidates))
	for _, item := range result.Candidates {
		itemNativeTools := item.NativeToolTypes
		if itemNativeTools == nil {
			itemNativeTools = []string{}
		}
		reasons := item.FailedReasons
		if reasons == nil {
			reasons = []string{}
		}
		candidates = append(candidates, ModelRouterTestCandidateResponse{
			Name:            item.Name,
			ContextWindow:   item.ContextWindow,
			Vision:          item.Vision,
			Tools:           item.Tools,
			NativeToolTypes: itemNativeTools,
			Passed:          item.Passed,
			FailedReasons:   reasons,
		})
	}
	var classification *ModelRouterTestClassificationResponse
	if result.Classifier.Classification != nil {
		classification = &ModelRouterTestClassificationResponse{
			Category:   result.Classifier.Classification.Category,
			Difficulty: result.Classifier.Classification.Difficulty,
			Switch:     result.Classifier.Classification.Switch,
			Reason:     result.Classifier.Classification.Reason,
		}
	}
	matchedRules := make([]ModelRouterTestMatchedRuleResponse, 0, len(result.MatchedRules))
	for _, item := range result.MatchedRules {
		models := item.Models
		if models == nil {
			models = []string{}
		}
		matchedRules = append(matchedRules, ModelRouterTestMatchedRuleResponse{
			Index:      item.Index,
			Category:   item.Category,
			Difficulty: item.Difficulty,
			Models:     models,
		})
	}
	return ModelRouterTestResponse{
		Requirements: ModelRouterTestRequirementsResponse{
			NeedsVision:     result.Requirements.NeedsVision,
			NeedsTools:      result.Requirements.NeedsTools,
			NativeToolTypes: nativeTools,
			EstimatedTokens: result.Requirements.EstimatedTokens,
		},
		Candidates:         candidates,
		ConstraintsRelaxed: result.ConstraintsRelaxed,
		Classifier: ModelRouterTestClassifierResponse{
			Attempted:      result.Classifier.Attempted,
			SkippedReason:  result.Classifier.SkippedReason,
			RouterModel:    result.Classifier.RouterModel,
			LatencyMS:      result.Classifier.LatencyMS,
			RawOutput:      result.Classifier.RawOutput,
			Error:          result.Classifier.Error,
			Classification: classification,
		},
		MatchedRules: matchedRules,
		Decision: ModelRouterTestDecisionResponse{
			Model:      result.Decision.Model,
			Source:     result.Decision.Source,
			Category:   result.Decision.Category,
			Difficulty: result.Decision.Difficulty,
			Reason:     result.Decision.Reason,
		},
	}
}
