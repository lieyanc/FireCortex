package repository

import (
	"context"

	domainconversation "github.com/lieyanc/FireCortex/backend/internal/domain/conversation"
)

// OpenWebUIChatRow 描述从外部 OpenWebUI 数据库读取的原始会话行。
type OpenWebUIChatRow struct {
	SourceChatID string
	SourceUserID string
	UserEmail    string
	Title        string
	ChatJSON     string
	MetaJSON     string
	Archived     bool
	Pinned       bool
	CreatedAtRaw string
	UpdatedAtRaw string
}

// OpenWebUIChatLoadResult 描述 OpenWebUI 会话加载结果。
type OpenWebUIChatLoadResult struct {
	TotalCount int
	Rows       []OpenWebUIChatRow
}

// ImportedConversationMessage 描述导入会话中的单条消息。
// ParentIndex 为消息切片中的父消息下标；根消息为 -1。
type ImportedConversationMessage struct {
	ParentIndex          int
	ReasoningDurationSec int
	Message              domainconversation.Message
}

// ConversationImportRecord 描述一次原子会话导入需要写入的会话与消息树。
type ConversationImportRecord struct {
	Conversation domainconversation.Conversation
	Messages     []ImportedConversationMessage
}

// ConversationImportRepository 封装会话历史导入的去重查询与原子持久化能力。
type ConversationImportRepository interface {
	ListExistingConversationSessionKeys(ctx context.Context, sessionKeys []string) (map[string]struct{}, error)
	ImportConversation(ctx context.Context, record ConversationImportRecord) error
}
