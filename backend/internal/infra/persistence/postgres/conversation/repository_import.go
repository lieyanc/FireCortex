package conversation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lieyanc/FireCortex/backend/internal/infra/persistence/dberror"
	"github.com/lieyanc/FireCortex/backend/internal/infra/persistence/models"
	"github.com/lieyanc/FireCortex/backend/internal/pkg/textutil"
	"github.com/lieyanc/FireCortex/backend/internal/repository"
	"gorm.io/gorm"
)

const (
	conversationImportSessionKeyBatchSize = 500
	conversationImportTraceBatchSize      = 200
)

// ListExistingConversationSessionKeys 批量查询已存在的会话 session_key（含软删除记录），避免重复导入或唯一索引冲突。
func (r *Repo) ListExistingConversationSessionKeys(ctx context.Context, sessionKeys []string) (map[string]struct{}, error) {
	results := make(map[string]struct{})
	normalized := make([]string, 0, len(sessionKeys))
	seen := make(map[string]struct{}, len(sessionKeys))
	for _, key := range sessionKeys {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	if len(normalized) == 0 {
		return results, nil
	}

	for start := 0; start < len(normalized); start += conversationImportSessionKeyBatchSize {
		end := start + conversationImportSessionKeyBatchSize
		if end > len(normalized) {
			end = len(normalized)
		}
		var found []string
		if err := r.db.WithContext(ctx).
			Unscoped().
			Model(&models.Conversation{}).
			Where("session_key IN ?", normalized[start:end]).
			Pluck("session_key", &found).Error; err != nil {
			return nil, dberror.Translate(err)
		}
		for _, key := range found {
			trimmed := strings.TrimSpace(key)
			if trimmed != "" {
				results[trimmed] = struct{}{}
			}
		}
	}
	return results, nil
}

// ImportConversation 在一个事务内原子写入导入的会话、消息树及推理展示轨迹，并保留历史时间戳。
func (r *Repo) ImportConversation(ctx context.Context, record repository.ConversationImportRecord) error {
	if record.Conversation.UserID == 0 ||
		strings.TrimSpace(record.Conversation.PublicID) == "" ||
		strings.TrimSpace(record.Conversation.SessionKey) == "" ||
		len(record.Messages) == 0 {
		return repository.ErrInvalidInput
	}

	for index, item := range record.Messages {
		if strings.TrimSpace(item.Message.PublicID) == "" || strings.TrimSpace(item.Message.Role) == "" {
			return repository.ErrInvalidInput
		}
		if item.ParentIndex >= index || item.ParentIndex < -1 {
			return repository.ErrInvalidInput
		}
	}

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		conv := record.Conversation
		conv.MessageCount = len(record.Messages)
		entity := toConversationModel(&conv)
		if !conv.CreatedAt.IsZero() {
			entity.CreatedAt = conv.CreatedAt
		}
		if !conv.UpdatedAt.IsZero() {
			entity.UpdatedAt = conv.UpdatedAt
		}
		if err := tx.Create(&entity).Error; err != nil {
			return err
		}

		createdIDs := make([]uint, len(record.Messages))
		traceEvents := make([]models.ChatRunEvent, 0)
		for index, item := range record.Messages {
			msg := item.Message
			msg.ID = 0
			msg.ConversationID = entity.ID
			msg.UserID = entity.UserID
			msg.ParentMessageID = nil
			if item.ParentIndex >= 0 {
				parentID := createdIDs[item.ParentIndex]
				msg.ParentMessageID = &parentID
			}
			msg.Content = textutil.SanitizeForTextColumn(msg.Content)
			msg.ReasoningContent = textutil.SanitizeForTextColumn(msg.ReasoningContent)

			msgEntity := toMessageModel(&msg)
			if !msg.CreatedAt.IsZero() {
				msgEntity.CreatedAt = msg.CreatedAt
			}
			if !msg.UpdatedAt.IsZero() {
				msgEntity.UpdatedAt = msg.UpdatedAt
			}
			if err := tx.Create(&msgEntity).Error; err != nil {
				return err
			}
			createdIDs[index] = msgEntity.ID

			if msgEntity.Role == "assistant" {
				if reasoning := strings.TrimSpace(msgEntity.ReasoningContent); reasoning != "" {
					endedAt := msgEntity.CreatedAt
					if endedAt.IsZero() {
						endedAt = time.Now().UTC()
					}
					startedAt := endedAt
					if item.ReasoningDurationSec > 0 {
						startedAt = endedAt.Add(-time.Duration(item.ReasoningDurationSec) * time.Second)
					}
					runID := "owui_" + msgEntity.PublicID
					summary := textutil.TruncateTrimmed(textutil.CompactSnippet(reasoning, 80), 255)
					traceEvents = append(traceEvents,
						models.ChatRunEvent{
							BaseModel: models.BaseModel{
								CreatedAt: endedAt,
								UpdatedAt: endedAt,
							},
							MessageID:       msgEntity.ID,
							ConversationID:  entity.ID,
							UserID:          entity.UserID,
							RunID:           runID,
							EventScope:      chatRunEventScopeTraceBlock,
							EventID:         fmt.Sprintf("message:%d:upstream_think", msgEntity.ID),
							EventType:       "upstream_think",
							Phase:           "upstream_think",
							Stage:           "think",
							RoundID:         "round_1",
							Status:          "completed",
							Title:           "模型思考",
							Summary:         summary,
							ContentMarkdown: reasoning,
							PayloadJSON:     "{}",
							Seq:             3,
							StartedAt:       startedAt,
							EndedAt:         &endedAt,
						},
						models.ChatRunEvent{
							BaseModel: models.BaseModel{
								CreatedAt: endedAt,
								UpdatedAt: endedAt,
							},
							MessageID:       msgEntity.ID,
							ConversationID:  entity.ID,
							UserID:          entity.UserID,
							RunID:           runID,
							EventScope:      chatRunEventScopeTraceEvent,
							EventID:         fmt.Sprintf("owui_think:%d", msgEntity.ID),
							EventType:       "think",
							Phase:           "upstream_think",
							Stage:           "think",
							RoundID:         "round_1",
							Status:          "completed",
							Title:           "模型思考",
							Summary:         summary,
							ContentMarkdown: reasoning,
							PayloadJSON:     "{}",
							Seq:             1,
							StartedAt:       startedAt,
							EndedAt:         &endedAt,
						},
					)
				}
			}
		}

		if len(traceEvents) > 0 {
			if err := tx.CreateInBatches(&traceEvents, conversationImportTraceBatchSize).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return dberror.Translate(err)
	}
	return nil
}
