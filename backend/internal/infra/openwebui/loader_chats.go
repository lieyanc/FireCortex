package openwebui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lieyanc/FireCortex/backend/internal/repository"
	"gorm.io/gorm"
)

type chatRow struct {
	SourceChatID string `gorm:"column:source_chat_id"`
	SourceUserID string `gorm:"column:source_user_id"`
	UserEmail    string `gorm:"column:user_email"`
	Title        string `gorm:"column:title"`
	ChatJSON     string `gorm:"column:chat_json"`
	MetaJSON     string `gorm:"column:meta_json"`
	Archived     bool   `gorm:"column:archived"`
	Pinned       bool   `gorm:"column:pinned"`
	CreatedAtRaw string `gorm:"column:created_at_raw"`
	UpdatedAtRaw string `gorm:"column:updated_at_raw"`
}

type chatFilterMode string

const (
	chatFilterNone      chatFilterMode = "none"
	chatFilterNumeric   chatFilterMode = "numeric"
	chatFilterTimestamp chatFilterMode = "timestamp"
)

type chatQueryOptions struct {
	HasUser      bool
	HasTitle     bool
	HasMeta      bool
	HasArchived  bool
	HasPinned    bool
	HasCreatedAt bool
	HasUpdatedAt bool
	FilterMode   chatFilterMode
}

// LoadOpenWebUIChats 从外部 OpenWebUI 数据库读取对话列表，可选按 cutoff 下推最近时间过滤。
func (RowLoader) LoadOpenWebUIChats(ctx context.Context, dsn string, cutoff *time.Time) (*repository.OpenWebUIChatLoadResult, error) {
	db, err := openDB(dsn)
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	defer sqlDB.Close()

	migrator := db.Migrator()
	if !migrator.HasTable("chat") {
		return nil, fmt.Errorf("%w: openwebui chat table not found", repository.ErrInvalidInput)
	}

	var totalCount int64
	if err = db.WithContext(ctx).Raw(`SELECT COUNT(*) FROM "chat"`).Scan(&totalCount).Error; err != nil {
		return nil, err
	}

	opts := inspectChatQueryOptions(migrator)
	query, args := buildChatImportQuery(opts, cutoff)

	rows := make([]chatRow, 0)
	if err = db.WithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}

	results := make([]repository.OpenWebUIChatRow, 0, len(rows))
	for _, row := range rows {
		results = append(results, repository.OpenWebUIChatRow{
			SourceChatID: row.SourceChatID,
			SourceUserID: row.SourceUserID,
			UserEmail:    row.UserEmail,
			Title:        row.Title,
			ChatJSON:     row.ChatJSON,
			MetaJSON:     row.MetaJSON,
			Archived:     row.Archived,
			Pinned:       row.Pinned,
			CreatedAtRaw: row.CreatedAtRaw,
			UpdatedAtRaw: row.UpdatedAtRaw,
		})
	}

	return &repository.OpenWebUIChatLoadResult{
		TotalCount: int(totalCount),
		Rows:       results,
	}, nil
}

func inspectChatQueryOptions(migrator gorm.Migrator) chatQueryOptions {
	opts := chatQueryOptions{
		HasUser:      migrator.HasTable("user"),
		HasTitle:     migrator.HasColumn("chat", "title"),
		HasMeta:      migrator.HasColumn("chat", "meta"),
		HasArchived:  migrator.HasColumn("chat", "archived"),
		HasPinned:    migrator.HasColumn("chat", "pinned"),
		HasCreatedAt: migrator.HasColumn("chat", "created_at"),
		HasUpdatedAt: migrator.HasColumn("chat", "updated_at"),
		FilterMode:   chatFilterNone,
	}

	columnTypes, err := migrator.ColumnTypes("chat")
	if err != nil {
		return opts
	}

	var updatedMode, createdMode chatFilterMode
	for _, col := range columnTypes {
		name := strings.ToLower(strings.TrimSpace(col.Name()))
		switch name {
		case "updated_at":
			updatedMode = classifyChatTimeColumnType(col.DatabaseTypeName())
		case "created_at":
			createdMode = classifyChatTimeColumnType(col.DatabaseTypeName())
		}
	}

	opts.FilterMode = resolveChatFilterMode(opts.HasUpdatedAt, updatedMode, opts.HasCreatedAt, createdMode)
	return opts
}

func classifyChatTimeColumnType(dbType string) chatFilterMode {
	normalized := strings.ToUpper(strings.TrimSpace(dbType))
	switch {
	case normalized == "":
		return chatFilterNone
	case strings.Contains(normalized, "INT"),
		strings.Contains(normalized, "NUM"),
		strings.Contains(normalized, "DEC"),
		strings.Contains(normalized, "FLOAT"),
		strings.Contains(normalized, "DOUBLE"),
		strings.Contains(normalized, "REAL"):
		return chatFilterNumeric
	case strings.Contains(normalized, "TIMESTAMP"),
		strings.Contains(normalized, "DATETIME"),
		normalized == "DATE":
		return chatFilterTimestamp
	default:
		return chatFilterNone
	}
}

func resolveChatFilterMode(hasUpdatedAt bool, updatedMode chatFilterMode, hasCreatedAt bool, createdMode chatFilterMode) chatFilterMode {
	switch {
	case hasUpdatedAt && hasCreatedAt:
		if updatedMode == createdMode && (updatedMode == chatFilterNumeric || updatedMode == chatFilterTimestamp) {
			return updatedMode
		}
		return chatFilterNone
	case hasUpdatedAt:
		return updatedMode
	case hasCreatedAt:
		return createdMode
	default:
		return chatFilterNone
	}
}

func buildChatImportQuery(opts chatQueryOptions, cutoff *time.Time) (string, []any) {
	titleExpr := `''`
	if opts.HasTitle {
		titleExpr = `COALESCE(c.title, '')`
	}
	userEmailExpr := `''`
	userJoinClause := ""
	if opts.HasUser {
		userEmailExpr = `COALESCE(u.email, '')`
		userJoinClause = ` LEFT JOIN "user" AS u ON u.id = c.user_id`
	}
	metaExpr := `''`
	if opts.HasMeta {
		metaExpr = `COALESCE(CAST(c.meta AS TEXT), '')`
	}
	archivedExpr := `FALSE`
	if opts.HasArchived {
		archivedExpr = `COALESCE(c.archived, FALSE)`
	}
	pinnedExpr := `FALSE`
	if opts.HasPinned {
		pinnedExpr = `COALESCE(c.pinned, FALSE)`
	}
	createdAtExpr := `''`
	if opts.HasCreatedAt {
		createdAtExpr = `COALESCE(CAST(c.created_at AS TEXT), '')`
	}
	updatedAtExpr := `''`
	if opts.HasUpdatedAt {
		updatedAtExpr = `COALESCE(CAST(c.updated_at AS TEXT), '')`
	}

	var b strings.Builder
	fmt.Fprintf(&b, `SELECT COALESCE(CAST(c.id AS TEXT), '') AS source_chat_id,
       COALESCE(CAST(c.user_id AS TEXT), '') AS source_user_id,
       %s AS user_email,
       %s AS title,
       COALESCE(CAST(c.chat AS TEXT), '') AS chat_json,
       %s AS meta_json,
       %s AS archived,
       %s AS pinned,
       %s AS created_at_raw,
       %s AS updated_at_raw
FROM "chat" AS c%s`,
		userEmailExpr,
		titleExpr,
		metaExpr,
		archivedExpr,
		pinnedExpr,
		createdAtExpr,
		updatedAtExpr,
		userJoinClause,
	)

	var args []any
	if cutoff != nil {
		utcCutoff := cutoff.UTC()
		switch opts.FilterMode {
		case chatFilterNumeric:
			switch {
			case opts.HasUpdatedAt && opts.HasCreatedAt:
				b.WriteString(` WHERE COALESCE(NULLIF(c.updated_at, 0), c.created_at) >= ?`)
			case opts.HasUpdatedAt:
				b.WriteString(` WHERE c.updated_at >= ?`)
			case opts.HasCreatedAt:
				b.WriteString(` WHERE c.created_at >= ?`)
			}
			args = append(args, utcCutoff.Unix())
		case chatFilterTimestamp:
			switch {
			case opts.HasUpdatedAt && opts.HasCreatedAt:
				b.WriteString(` WHERE COALESCE(c.updated_at, c.created_at) >= ?`)
			case opts.HasUpdatedAt:
				b.WriteString(` WHERE c.updated_at >= ?`)
			case opts.HasCreatedAt:
				b.WriteString(` WHERE c.created_at >= ?`)
			}
			args = append(args, utcCutoff)
		}
	}

	b.WriteString(` ORDER BY c.id ASC`)
	return b.String(), args
}
