package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lieyanc/FireCortex/backend/internal/application/channel"
	"github.com/lieyanc/FireCortex/backend/internal/application/modelrouter"
	model "github.com/lieyanc/FireCortex/backend/internal/domain/conversation"
	"github.com/lieyanc/FireCortex/backend/internal/pkg/textutil"
	"github.com/lieyanc/FireCortex/backend/internal/ports/llm"
	"github.com/lieyanc/FireCortex/backend/internal/shared/background"
	"github.com/lieyanc/FireCortex/backend/internal/shared/tokenestimate"
	"go.uber.org/zap"
)

const (
	modelRouterServiceCode = "model_router"
	modelRouterServiceName = "Auto model routing"
)

// AutoRouteInput 是一次 Auto 路由决策的输入。
type AutoRouteInput struct {
	UserID uint
	// ConversationID 为 0 表示临时对话：历史由 History 提供，分类调用不计费。
	ConversationID uint
	RequestID      string
	Message        string
	// History 仅临时对话使用；普通会话从库中读取最近消息。
	History []modelrouter.HistoryMessage
	// PreviousModel 是会话上一轮 Auto 选中的模型，非 Auto 会话留空。
	PreviousModel string
	FileIDs       []string
	// AttachmentMIMETypes 是临时对话本轮附件的 MIME 类型。
	AttachmentMIMETypes []string
	SelectedToolIDs     []uint
	Options             map[string]any
}

// AutoRouteEventPayload 构造 routed 流事件的字段（不含 type）。
func AutoRouteEventPayload(decision modelrouter.Decision) map[string]any {
	return map[string]any{
		"model":      decision.Model,
		"source":     decision.Source,
		"category":   decision.Category,
		"difficulty": decision.Difficulty,
		"reason":     decision.Reason,
	}
}

type persistedAutoRouteDecision struct {
	Model      string `json:"model,omitempty"`
	Source     string `json:"source,omitempty"`
	Category   string `json:"category,omitempty"`
	Difficulty string `json:"difficulty,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// EncodeAutoRouteDecision 把一次 Auto 路由决策编码为可落库的紧凑 JSON。
func EncodeAutoRouteDecision(decision *modelrouter.Decision) string {
	if decision == nil || strings.TrimSpace(decision.Model) == "" {
		return ""
	}
	payload, err := json.Marshal(persistedAutoRouteDecision{
		Model:      strings.TrimSpace(decision.Model),
		Source:     strings.TrimSpace(decision.Source),
		Category:   strings.TrimSpace(decision.Category),
		Difficulty: strings.TrimSpace(decision.Difficulty),
		Reason:     strings.TrimSpace(decision.Reason),
	})
	if err != nil {
		return ""
	}
	return string(payload)
}

// DecodeAutoRouteDecision 反序列化落库的 Auto 路由决策摘要；无效或空串返回 nil。
func DecodeAutoRouteDecision(raw string) *modelrouter.Decision {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	var payload persistedAutoRouteDecision
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil
	}
	if strings.TrimSpace(payload.Model) == "" && strings.TrimSpace(payload.Source) == "" {
		return nil
	}
	return &modelrouter.Decision{
		Model:      strings.TrimSpace(payload.Model),
		Source:     strings.TrimSpace(payload.Source),
		Category:   strings.TrimSpace(payload.Category),
		Difficulty: strings.TrimSpace(payload.Difficulty),
		Reason:     strings.TrimSpace(payload.Reason),
	}
}

// ModelRouterEnabled 返回管理员是否开放 Auto 模型路由。
func (s *Service) ModelRouterEnabled() bool {
	return s.cfg.Snapshot().ModelRouterEnabled
}

// IsAutoModelRequest 判断本轮请求是否应走 Auto 路由：显式选择 Auto，或 Auto 会话未指定模型。
func IsAutoModelRequest(requestedModel string, conversation *model.Conversation) bool {
	requested := strings.TrimSpace(requestedModel)
	if strings.EqualFold(requested, modelrouter.AutoModelName) {
		return true
	}
	return requested == "" && conversation != nil && conversation.ModelMode == modelrouter.ModelModeAuto
}

// SetConversationModelMode 在模式变化时持久化会话的 manual/auto 模式。
func (s *Service) SetConversationModelMode(ctx context.Context, conversation *model.Conversation, mode string) error {
	if conversation == nil || conversation.ID == 0 {
		return nil
	}
	if mode != modelrouter.ModelModeAuto {
		mode = modelrouter.ModelModeManual
	}
	current := conversation.ModelMode
	if current == "" {
		current = modelrouter.ModelModeManual
	}
	if current == mode {
		return nil
	}
	if err := s.repo.UpdateConversationModelMode(ctx, conversation.ID, mode); err != nil {
		return err
	}
	conversation.ModelMode = mode
	return nil
}

// DecideAutoModel 先按硬约束过滤候选，再用快速模型分类并映射到具体模型。
// 分类失败或超时不会报错，而是回退到粘性模型或默认模型；只有没有任何可用候选时才返回错误。
func (s *Service) DecideAutoModel(ctx context.Context, input AutoRouteInput) (modelrouter.Decision, error) {
	cfg := s.cfg.Snapshot()
	if !cfg.ModelRouterEnabled {
		return modelrouter.Decision{}, ErrModelRouterDisabled
	}
	policy, err := modelrouter.ParsePolicy(cfg.ModelRouterPolicy)
	if err != nil {
		s.logModelRouterWarn("model_router_policy_invalid", input, err)
		policy, _ = modelrouter.ParsePolicy("")
	}

	candidates, err := s.listAutoRouteCandidates(ctx, input.UserID)
	if err != nil {
		return modelrouter.Decision{}, err
	}
	if len(candidates) == 0 {
		return modelrouter.Decision{}, ErrModelRouteNotConfigured
	}

	history := input.History
	if input.ConversationID != 0 {
		history = s.loadAutoRouteHistory(ctx, input.ConversationID, policy.HistoryMessages)
	} else if len(history) > policy.HistoryMessages {
		history = history[len(history)-policy.HistoryMessages:]
	}
	attachments, needsVision := s.describeAutoRouteAttachments(ctx, input)

	requirements := modelrouter.Requirements{
		NeedsVision:     needsVision,
		NeedsTools:      len(input.SelectedToolIDs) > 0,
		NativeToolTypes: autoRouteNativeToolTypes(input.Options),
		EstimatedTokens: autoRouteEstimatedTokens(input.Message, history),
	}
	// 约束排除了全部候选时放宽过滤，让下游以具体能力错误告知用户，而不是让 Auto 直接失败。
	filtered := modelrouter.FilterCandidates(candidates, requirements)
	if filtered == nil {
		filtered = candidates
	}

	var classification *modelrouter.Classification
	// 没有映射规则时分类结果不影响选择，跳过分类调用以节省延迟与费用。
	if len(filtered) > 1 && len(policy.Rules) > 0 {
		classification = s.classifyAutoRoute(ctx, policy, cfg.ConversationTaskModel, input, modelrouter.ClassifyInput{
			Message:          input.Message,
			History:          history,
			Attachments:      attachments,
			HasPreviousRoute: strings.TrimSpace(input.PreviousModel) != "",
		})
	}

	decision, ok := modelrouter.Select(modelrouter.SelectInput{
		Policy:         policy,
		Candidates:     filtered,
		Classification: classification,
		PreviousModel:  strings.TrimSpace(input.PreviousModel),
	})
	if !ok {
		return modelrouter.Decision{}, ErrModelRouteNotConfigured
	}
	return decision, nil
}

// listAutoRouteCandidates 返回当前用户可用的聊天模型（目录顺序即兜底优先级）。
func (s *Service) listAutoRouteCandidates(ctx context.Context, userID uint) ([]modelrouter.Candidate, error) {
	resolver, ok := s.routeResolver.(activeModelCatalogResolver)
	if !ok {
		return nil, ErrModelRouteNotConfigured
	}
	models, err := resolver.ListActiveModels(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list auto route candidates: %w", err)
	}
	candidates := make([]modelrouter.Candidate, 0, len(models))
	for _, item := range models {
		name := strings.TrimSpace(item.PlatformModelName)
		if name == "" || strings.EqualFold(name, modelrouter.AutoModelName) || !channel.ModelSupportsTask(item.KindsJSON, channel.TaskTypeChat) {
			continue
		}
		candidates = append(candidates, modelrouter.CandidateFromCapabilities(name, item.ContextWindow, item.CapabilitiesJSON))
	}
	return candidates, nil
}

func (s *Service) loadAutoRouteHistory(ctx context.Context, conversationID uint, limit int) []modelrouter.HistoryMessage {
	if limit <= 0 {
		return nil
	}
	messages, _, err := s.repo.ListRecentMessages(ctx, conversationID, limit)
	if err != nil {
		return nil
	}
	history := make([]modelrouter.HistoryMessage, 0, len(messages))
	for _, message := range messages {
		if message.Role != "user" && message.Role != "assistant" {
			continue
		}
		if strings.TrimSpace(message.Content) == "" {
			continue
		}
		history = append(history, modelrouter.HistoryMessage{Role: message.Role, Content: message.Content})
	}
	return history
}

// describeAutoRouteAttachments 返回提供给分类器的附件类别，以及本轮是否带有图片。
func (s *Service) describeAutoRouteAttachments(ctx context.Context, input AutoRouteInput) ([]string, bool) {
	mimeTypes := append([]string(nil), input.AttachmentMIMETypes...)
	categories := make([]string, 0, len(input.FileIDs)+len(mimeTypes))
	if len(input.FileIDs) > 0 {
		files, err := s.repo.GetActiveFileObjectsByIDs(ctx, input.UserID, input.FileIDs)
		if err == nil {
			for _, file := range files {
				mimeTypes = append(mimeTypes, textutil.FirstNonEmpty(file.DetectedMIME, file.MimeType))
				if category := strings.TrimSpace(file.FileCategory); category != "" {
					categories = append(categories, category)
				}
			}
		}
	}
	needsVision := false
	for _, mimeType := range mimeTypes {
		normalized := strings.ToLower(strings.TrimSpace(mimeType))
		if strings.HasPrefix(normalized, "image/") {
			needsVision = true
		}
		if len(categories) < len(mimeTypes) && normalized != "" {
			categories = append(categories, normalized)
		}
	}
	for _, category := range categories {
		if strings.EqualFold(category, "image") {
			needsVision = true
		}
	}
	return categories, needsVision
}

func autoRouteNativeToolTypes(options map[string]any) []string {
	if len(options) == 0 {
		return nil
	}
	var types []string
	for _, payload := range providerToolOptionPayloads(options["tools"]) {
		if value, ok := payload["type"].(string); ok && strings.TrimSpace(value) != "" {
			types = append(types, strings.TrimSpace(value))
		}
	}
	return types
}

// autoRouteEstimatedTokens 只统计本轮消息与最近历史，偏低估计，避免误排除长上下文之外的模型。
func autoRouteEstimatedTokens(message string, history []modelrouter.HistoryMessage) int {
	total := tokenestimate.Estimate(message)
	for _, item := range history {
		total += tokenestimate.Estimate(item.Content)
	}
	return int(total)
}

type classifyAutoRouteOutcome struct {
	Classification *modelrouter.Classification
	RouterModel    string
	LatencyMS      int64
	RawOutput      string
	Error          string
}

// classifyAutoRoute 调用快速模型完成分类；任何失败都返回 nil，由选择逻辑回退。
func (s *Service) classifyAutoRoute(
	ctx context.Context,
	policy modelrouter.Policy,
	conversationTaskModel string,
	input AutoRouteInput,
	classifyInput modelrouter.ClassifyInput,
) *modelrouter.Classification {
	return s.classifyAutoRouteDetailed(ctx, policy, conversationTaskModel, input, classifyInput).Classification
}

func (s *Service) classifyAutoRouteDetailed(
	ctx context.Context,
	policy modelrouter.Policy,
	conversationTaskModel string,
	input AutoRouteInput,
	classifyInput modelrouter.ClassifyInput,
) classifyAutoRouteOutcome {
	routes, err := s.resolveTextTaskRouteCandidates(ctx, textTaskRouteInput{
		ConfiguredModel:   textutil.FirstNonEmpty(policy.RouterModel, conversationTaskModel),
		ConversationModel: input.PreviousModel,
		UserID:            input.UserID,
		ConversationID:    input.ConversationID,
		RequestID:         input.RequestID,
	})
	if err != nil || len(routes) == 0 {
		s.logModelRouterWarn("model_router_route_unavailable", input, err)
		errMsg := "classifier route unavailable"
		if err != nil {
			errMsg = err.Error()
		}
		return classifyAutoRouteOutcome{Error: errMsg}
	}
	route := routes[0]
	if route == nil || strings.TrimSpace(route.PlatformModelName) == "" {
		return classifyAutoRouteOutcome{Error: "classifier route unavailable"}
	}
	outcome := classifyAutoRouteOutcome{
		RouterModel: strings.TrimSpace(route.PlatformModelName),
	}
	if s.llmClient == nil {
		outcome.Error = "llm client unavailable"
		return outcome
	}

	system, user := modelrouter.BuildClassifierMessages(policy, classifyInput)
	messages := []llm.Message{{Role: "system", Content: system}, {Role: "user", Content: user}}
	generateInput := buildTextTaskGenerateInput(route, s.cfg.Snapshot(), messages)
	attributionReferer, attributionTitle := s.llmAttribution()
	routeConfig := routeConfigFromResolved(route, llm.DefaultEndpointForAdapter(route.Protocol), attributionReferer, attributionTitle)

	authorization, err := s.authorizeBasicServiceUsage(ctx, input.UserID, route.PlatformModelName, modelRouterServiceCode)
	if err != nil {
		s.logModelRouterWarn("model_router_usage_authorization_failed", input, err)
		outcome.Error = err.Error()
		return outcome
	}
	startedAt := time.Now()
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(policy.TimeoutMS)*time.Millisecond)
	out, err := s.llmClient.Generate(callCtx, routeConfig, generateInput)
	cancel()
	latencyMS := time.Since(startedAt).Milliseconds()
	outcome.LatencyMS = latencyMS
	settleCtx := background.Detach(ctx)
	if err != nil {
		if releaseErr := s.releaseBasicServiceUsageAuthorization(settleCtx, authorization); releaseErr != nil {
			err = errors.Join(err, releaseErr)
		}
		s.logModelRouterWarn("model_router_classify_failed", input, err, zap.Int64("latency_ms", latencyMS))
		outcome.Error = err.Error()
		return outcome
	}
	text := strings.TrimSpace(out.Text)
	outcome.RawOutput = text
	// 临时对话与管理端试跑没有会话归属，基础服务用量不入账，只释放预留。
	if input.ConversationID == 0 {
		_ = s.releaseBasicServiceUsageAuthorization(settleCtx, authorization)
	} else if recordErr := s.recordBasicServiceUsage(settleCtx, basicServiceUsageInput{
		Authorization:     authorization,
		UserID:            input.UserID,
		ConversationID:    input.ConversationID,
		ServiceCode:       modelRouterServiceCode,
		ServiceName:       modelRouterServiceName,
		PlatformModelName: route.PlatformModelName,
		RoutedBindingCode: route.BindingCode,
		ProviderProtocol:  route.Protocol,
		UpstreamName:      route.UpstreamName,
		UpstreamModelName: route.UpstreamModel,
		Usage:             out.Usage,
		FallbackMessages:  generateInput.Messages,
		FallbackOutput:    text,
		LatencyMS:         latencyMS,
	}); recordErr != nil {
		s.logModelRouterWarn("model_router_usage_record_failed", input, recordErr)
	}

	classification, err := modelrouter.ParseClassification(policy, text)
	if err != nil {
		s.logModelRouterWarn("model_router_classification_invalid", input, err)
		outcome.Error = err.Error()
		return outcome
	}
	outcome.Classification = &classification
	return outcome
}

// TestAutoRouteInput 是管理端路由沙盒 / 在线试跑的请求输入。
type TestAutoRouteInput struct {
	UserID                  uint
	RequestID               string
	PolicyJSON              string
	Message                 string
	History                 []modelrouter.HistoryMessage
	PreviousModel           string
	NeedsVision             bool
	NeedsTools              bool
	NativeToolTypes         []string
	EstimatedTokens         int
	DryRun                  bool
	SimulatedClassification *modelrouter.Classification
}

// TestAutoRouteCandidate 记录单个候选模型在硬约束过滤阶段的结果。
type TestAutoRouteCandidate struct {
	Name            string
	ContextWindow   int
	Vision          *bool
	Tools           *bool
	NativeToolTypes []string
	Passed          bool
	FailedReasons   []string
}

// TestAutoRouteClassifierTrace 记录分类器调用的诊断信息。
type TestAutoRouteClassifierTrace struct {
	Attempted      bool
	SkippedReason  string
	RouterModel    string
	LatencyMS      int64
	RawOutput      string
	Error          string
	Classification *modelrouter.Classification
}

// TestAutoRouteResult 返回一次试跑的完整分步路由诊断。
type TestAutoRouteResult struct {
	Requirements       modelrouter.Requirements
	Candidates         []TestAutoRouteCandidate
	ConstraintsRelaxed bool
	Classifier         TestAutoRouteClassifierTrace
	MatchedRules       []modelrouter.MatchedRule
	Decision           modelrouter.Decision
}

// TestAutoModelRoute 允许管理员使用未保存的草稿策略或当前配置试跑完整路由管道。
// 不要求总开关已开启，便于开启前调试；真实分类调用同样遵循超时且不向会话入账。
func (s *Service) TestAutoModelRoute(ctx context.Context, input TestAutoRouteInput) (*TestAutoRouteResult, error) {
	cfg := s.cfg.Snapshot()
	rawPolicy := strings.TrimSpace(input.PolicyJSON)
	var policy modelrouter.Policy
	var err error
	if rawPolicy != "" {
		policy, err = modelrouter.ParsePolicy(rawPolicy)
		if err != nil {
			return nil, err
		}
	} else {
		policy, err = modelrouter.ParsePolicy(cfg.ModelRouterPolicy)
		if err != nil {
			policy, _ = modelrouter.ParsePolicy("")
		}
	}

	candidates, err := s.listAutoRouteCandidates(ctx, input.UserID)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, ErrModelRouteNotConfigured
	}

	history := input.History
	if policy.HistoryMessages == 0 {
		history = nil
	} else if len(history) > policy.HistoryMessages {
		history = history[len(history)-policy.HistoryMessages:]
	}

	estimatedTokens := input.EstimatedTokens
	if estimatedTokens <= 0 {
		estimatedTokens = autoRouteEstimatedTokens(input.Message, history)
	}
	nativeToolTypes := make([]string, 0, len(input.NativeToolTypes))
	for _, item := range input.NativeToolTypes {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			nativeToolTypes = append(nativeToolTypes, trimmed)
		}
	}
	requirements := modelrouter.Requirements{
		NeedsVision:     input.NeedsVision,
		NeedsTools:      input.NeedsTools,
		NativeToolTypes: nativeToolTypes,
		EstimatedTokens: estimatedTokens,
	}

	candidateTraces := make([]TestAutoRouteCandidate, 0, len(candidates))
	filtered := make([]modelrouter.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		reasons := candidate.UnsatisfiedReasons(requirements)
		passed := len(reasons) == 0
		if passed {
			filtered = append(filtered, candidate)
		}
		candidateTraces = append(candidateTraces, TestAutoRouteCandidate{
			Name:            candidate.Name,
			ContextWindow:   candidate.ContextWindow,
			Vision:          candidate.Vision,
			Tools:           candidate.Tools,
			NativeToolTypes: candidate.NativeToolTypes,
			Passed:          passed,
			FailedReasons:   reasons,
		})
	}
	constraintsRelaxed := false
	if len(filtered) == 0 {
		constraintsRelaxed = true
		filtered = candidates
	}

	var classifierTrace TestAutoRouteClassifierTrace
	var classification *modelrouter.Classification
	if input.DryRun {
		classifierTrace.SkippedReason = "dry_run"
		if input.SimulatedClassification != nil {
			cat := strings.ToLower(strings.TrimSpace(input.SimulatedClassification.Category))
			diff := strings.ToLower(strings.TrimSpace(input.SimulatedClassification.Difficulty))
			if diff != modelrouter.DifficultyEasy && diff != modelrouter.DifficultyMedium && diff != modelrouter.DifficultyHard {
				diff = modelrouter.DifficultyMedium
			}
			if cat != "" {
				sim := modelrouter.Classification{
					Category:   cat,
					Difficulty: diff,
					Switch:     input.SimulatedClassification.Switch,
					Reason:     strings.TrimSpace(input.SimulatedClassification.Reason),
				}
				classification = &sim
				classifierTrace.Classification = &sim
			}
		}
	} else if len(filtered) <= 1 {
		classifierTrace.SkippedReason = "single_candidate"
	} else if len(policy.Rules) == 0 {
		classifierTrace.SkippedReason = "no_rules"
	} else {
		classifierTrace.Attempted = true
		var attachments []string
		if input.NeedsVision {
			attachments = append(attachments, "image")
		}
		outcome := s.classifyAutoRouteDetailed(ctx, policy, cfg.ConversationTaskModel, AutoRouteInput{
			UserID:        input.UserID,
			RequestID:     input.RequestID,
			Message:       input.Message,
			PreviousModel: strings.TrimSpace(input.PreviousModel),
		}, modelrouter.ClassifyInput{
			Message:          input.Message,
			History:          history,
			Attachments:      attachments,
			HasPreviousRoute: strings.TrimSpace(input.PreviousModel) != "",
		})
		classifierTrace.RouterModel = outcome.RouterModel
		classifierTrace.LatencyMS = outcome.LatencyMS
		classifierTrace.RawOutput = outcome.RawOutput
		classifierTrace.Error = outcome.Error
		classifierTrace.Classification = outcome.Classification
		classification = outcome.Classification
	}

	var matchedRules []modelrouter.MatchedRule
	if classification != nil {
		matchedRules = modelrouter.MatchRules(policy.Rules, classification.Category, classification.Difficulty)
	}
	decision, ok := modelrouter.Select(modelrouter.SelectInput{
		Policy:         policy,
		Candidates:     filtered,
		Classification: classification,
		PreviousModel:  strings.TrimSpace(input.PreviousModel),
	})
	if !ok {
		return nil, ErrModelRouteNotConfigured
	}
	return &TestAutoRouteResult{
		Requirements:       requirements,
		Candidates:         candidateTraces,
		ConstraintsRelaxed: constraintsRelaxed,
		Classifier:         classifierTrace,
		MatchedRules:       matchedRules,
		Decision:           decision,
	}, nil
}

func (s *Service) logModelRouterWarn(event string, input AutoRouteInput, err error, fields ...zap.Field) {
	if s.logger == nil {
		return
	}
	fields = append([]zap.Field{
		zap.Uint("user_id", input.UserID),
		zap.Uint("conversation_id", input.ConversationID),
		zap.String("request_id", input.RequestID),
		zap.Error(err),
	}, fields...)
	s.logger.Warn(event, fields...)
}
