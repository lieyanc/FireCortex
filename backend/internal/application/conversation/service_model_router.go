package conversation

import (
	"context"
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

// classifyAutoRoute 调用快速模型完成分类；任何失败都返回 nil，由选择逻辑回退。
func (s *Service) classifyAutoRoute(
	ctx context.Context,
	policy modelrouter.Policy,
	conversationTaskModel string,
	input AutoRouteInput,
	classifyInput modelrouter.ClassifyInput,
) *modelrouter.Classification {
	routes, err := s.resolveTextTaskRouteCandidates(ctx, textTaskRouteInput{
		ConfiguredModel:   textutil.FirstNonEmpty(policy.RouterModel, conversationTaskModel),
		ConversationModel: input.PreviousModel,
		UserID:            input.UserID,
		ConversationID:    input.ConversationID,
		RequestID:         input.RequestID,
	})
	if err != nil || len(routes) == 0 {
		s.logModelRouterWarn("model_router_route_unavailable", input, err)
		return nil
	}
	route := routes[0]
	if route == nil || strings.TrimSpace(route.PlatformModelName) == "" {
		return nil
	}

	system, user := modelrouter.BuildClassifierMessages(policy, classifyInput)
	messages := []llm.Message{{Role: "system", Content: system}, {Role: "user", Content: user}}
	generateInput := buildTextTaskGenerateInput(route, s.cfg.Snapshot(), messages)
	attributionReferer, attributionTitle := s.llmAttribution()
	routeConfig := routeConfigFromResolved(route, llm.DefaultEndpointForAdapter(route.Protocol), attributionReferer, attributionTitle)

	authorization, err := s.authorizeBasicServiceUsage(ctx, input.UserID, route.PlatformModelName, modelRouterServiceCode)
	if err != nil {
		s.logModelRouterWarn("model_router_usage_authorization_failed", input, err)
		return nil
	}
	startedAt := time.Now()
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(policy.TimeoutMS)*time.Millisecond)
	out, err := s.llmClient.Generate(callCtx, routeConfig, generateInput)
	cancel()
	latencyMS := time.Since(startedAt).Milliseconds()
	settleCtx := background.Detach(ctx)
	if err != nil {
		if releaseErr := s.releaseBasicServiceUsageAuthorization(settleCtx, authorization); releaseErr != nil {
			err = errors.Join(err, releaseErr)
		}
		s.logModelRouterWarn("model_router_classify_failed", input, err, zap.Int64("latency_ms", latencyMS))
		return nil
	}
	text := strings.TrimSpace(out.Text)
	// 临时对话没有会话归属，基础服务用量不入账，只释放预留。
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
		return nil
	}
	return &classification
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
