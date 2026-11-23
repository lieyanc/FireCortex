package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	auditapp "github.com/lieyanc/FireCortex/backend/internal/application/audit"
	domainaudit "github.com/lieyanc/FireCortex/backend/internal/domain/audit"
	domainuser "github.com/lieyanc/FireCortex/backend/internal/domain/user"
	"github.com/lieyanc/FireCortex/backend/internal/repository"
)

type openWebUIChatLoaderFake struct {
	result *repository.OpenWebUIChatLoadResult
	err    error
	cutoff *time.Time
}

func (f *openWebUIChatLoaderFake) LoadOpenWebUIChats(_ context.Context, _ string, cutoff *time.Time) (*repository.OpenWebUIChatLoadResult, error) {
	f.cutoff = cutoff
	return f.result, f.err
}

type conversationImportRepoFake struct {
	existingKeys  map[string]struct{}
	imported      []repository.ConversationImportRecord
	duplicateKeys map[string]bool
}

func (f *conversationImportRepoFake) ListExistingConversationSessionKeys(_ context.Context, keys []string) (map[string]struct{}, error) {
	out := make(map[string]struct{})
	for _, key := range keys {
		if _, ok := f.existingKeys[key]; ok {
			out[key] = struct{}{}
		}
	}
	return out, nil
}

func (f *conversationImportRepoFake) ImportConversation(_ context.Context, record repository.ConversationImportRecord) error {
	if f.duplicateKeys[record.Conversation.SessionKey] {
		return repository.ErrDuplicate
	}
	f.imported = append(f.imported, record)
	return nil
}

type openWebUIConversationUserServiceFake struct {
	*adminUserServiceFake
}

func (s *openWebUIConversationUserServiceFake) ListUsersByLowerEmails(_ context.Context, emails []string) (map[string]domainuser.User, error) {
	out := make(map[string]domainuser.User)
	wanted := make(map[string]struct{}, len(emails))
	for _, email := range emails {
		wanted[strings.ToLower(strings.TrimSpace(email))] = struct{}{}
	}
	for _, item := range s.users {
		lower := strings.ToLower(strings.TrimSpace(item.Email))
		if _, ok := wanted[lower]; ok && lower != "" {
			out[lower] = item
		}
	}
	return out, nil
}

type recordingAuditServiceFake struct {
	entries []auditapp.WriteInput
}

func (f *recordingAuditServiceFake) Write(_ context.Context, input auditapp.WriteInput) {
	f.entries = append(f.entries, input)
}

func (f *recordingAuditServiceFake) List(context.Context, int, int, auditapp.ListFilter) ([]domainaudit.Log, int64, error) {
	return nil, 0, nil
}

func intPtr(v int) *int {
	return &v
}

func TestImportOpenWebUIConversationsValidationAndLoaderErrors(t *testing.T) {
	t.Parallel()

	users := &openWebUIConversationUserServiceFake{
		adminUserServiceFake: newAdminUserServiceFake(map[uint]domainuser.User{
			1: {ID: 1, Username: "admin", Email: "admin@example.com", Role: domainuser.RoleSuperAdmin},
		}),
	}

	t.Run("rejects invalid recentDays", func(t *testing.T) {
		service := NewService(users, auditServiceFake{})
		service.SetOpenWebUIChatLoader(&openWebUIChatLoaderFake{})
		service.SetConversationImportRepo(&conversationImportRepoFake{})

		for _, invalid := range []int{0, -1, 36501} {
			_, err := service.ImportOpenWebUIConversations(
				context.Background(),
				"req_1",
				1,
				OpenWebUIConversationImportInput{DSN: "sqlite:///tmp/webui.db", RecentDays: intPtr(invalid)},
				"127.0.0.1",
				"ua",
			)
			if !errors.Is(err, ErrInvalidImportRecentDays) {
				t.Fatalf("recentDays=%d expected ErrInvalidImportRecentDays, got %v", invalid, err)
			}
		}
	})

	t.Run("maps invalid loader input to ErrInvalidImportDSN", func(t *testing.T) {
		service := NewService(users, auditServiceFake{})
		service.SetOpenWebUIChatLoader(&openWebUIChatLoaderFake{err: repository.ErrInvalidInput})
		service.SetConversationImportRepo(&conversationImportRepoFake{})

		_, err := service.ImportOpenWebUIConversations(
			context.Background(),
			"req_2",
			1,
			OpenWebUIConversationImportInput{DSN: "bad-dsn"},
			"127.0.0.1",
			"ua",
		)
		if !errors.Is(err, ErrInvalidImportDSN) {
			t.Fatalf("expected ErrInvalidImportDSN, got %v", err)
		}
	})

	t.Run("requires chat loader and conversation import repo", func(t *testing.T) {
		service := NewService(users, auditServiceFake{})
		_, err := service.ImportOpenWebUIConversations(
			context.Background(),
			"req_3",
			1,
			OpenWebUIConversationImportInput{DSN: "postgres://localhost/owui"},
			"127.0.0.1",
			"ua",
		)
		if !errors.Is(err, ErrOpenWebUIImportFailed) {
			t.Fatalf("expected ErrOpenWebUIImportFailed, got %v", err)
		}
	})
}

func TestImportOpenWebUIConversationsMultiUserTreeReasoningAndCutoff(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	recentSec := now.Add(-2 * 24 * time.Hour).Unix()
	recentMS := now.Add(-1 * 24 * time.Hour).UnixMilli()
	oldMS := now.Add(-20 * 24 * time.Hour).UnixMilli()

	users := &openWebUIConversationUserServiceFake{
		adminUserServiceFake: newAdminUserServiceFake(map[uint]domainuser.User{
			1:  {ID: 1, Username: "admin", Email: "admin@example.com", Role: domainuser.RoleSuperAdmin},
			10: {ID: 10, Username: "alice", Email: "Alice@Example.com", Role: domainuser.RoleUser},
		}),
	}

	alreadyImportedKey := resolveOpenWebUIConversationSessionKey(10, "chat_existing")
	raceDuplicateKey := resolveOpenWebUIConversationSessionKey(10, "chat_race_dup")

	branchChatJSON := fmt.Sprintf(`{
		"id": "chat_tree",
		"title": "",
		"models": ["fallback-model"],
		"tags": ["payload-tag", "Work"],
		"history": {
			"currentId": "a2",
			"messages": {
				"u1": {
					"id": "u1",
					"parentId": "",
					"childrenIds": ["a1", "a2"],
					"role": "user",
					"content": [{"type":"text","text":"Explain Go interfaces"}],
					"timestamp": %d
				},
				"a1": {
					"id": "a1",
					"parentId": "u1",
					"childrenIds": ["u2_alt"],
					"role": "assistant",
					"model": "gpt-4o",
					"content": "<think>alt thinking</think>Alt answer",
					"timestamp": %d
				},
				"u2_alt": {
					"id": "u2_alt",
					"parentId": "a1",
					"childrenIds": [],
					"role": "user",
					"content": "Follow up on alt",
					"timestamp": %d
				},
				"a2": {
					"id": "a2",
					"parentId": "u1",
					"childrenIds": [],
					"role": "assistant",
					"model": "claude-3-7-sonnet",
					"content": "<details type=\"reasoning\" done=\"true\" duration=\"4\">\n<summary>Thought for 4 seconds</summary>\n> line one\n> line two\n</details>\nMain answer",
					"timestamp": %d
				}
			}
		}
	}`, recentSec-30, recentSec-20, recentSec-10, recentSec)

	flatChatJSON := fmt.Sprintf(`{
		"id": "chat_flat",
		"title": "Flat Reasoning Chat",
		"models": ["deepseek-r1"],
		"messages": [
			{"id":"m1","role":"system","content":"You are helpful","timestamp":%d},
			{"id":"m2","role":"user","content":"Hi","timestamp":%d},
			{"id":"m3","role":"assistant","content":"Hello!","reasoning_content":"greeting plan","timestamp":%d}
		]
	}`, recentSec-10, recentSec-5, recentSec)

	// TotalCount=9 while Rows has 8 rows => 1 row was already filtered out by SQL cutoff.
	loader := &openWebUIChatLoaderFake{
		result: &repository.OpenWebUIChatLoadResult{
			TotalCount: 9,
			Rows: []repository.OpenWebUIChatRow{
				{
					SourceChatID: "chat_tree",
					UserEmail:    "alice@example.com",
					ChatJSON:     branchChatJSON,
					MetaJSON:     `{"tags":["work","arch"]}`,
					Pinned:       true,
					CreatedAtRaw: fmt.Sprintf("%d", recentSec-60),
					UpdatedAtRaw: fmt.Sprintf("%d", recentSec),
				},
				{
					SourceChatID: "chat_flat",
					UserEmail:    "ALICE@EXAMPLE.COM",
					ChatJSON:     flatChatJSON,
					Archived:     true,
					CreatedAtRaw: fmt.Sprintf("%d", recentMS),
					UpdatedAtRaw: fmt.Sprintf("%d", recentMS),
				},
				{
					SourceChatID: "chat_old_ms",
					UserEmail:    "alice@example.com",
					ChatJSON:     flatChatJSON,
					CreatedAtRaw: fmt.Sprintf("%d", oldMS),
					UpdatedAtRaw: fmt.Sprintf("%d", oldMS),
				},
				{
					SourceChatID: "chat_existing",
					UserEmail:    "alice@example.com",
					ChatJSON:     flatChatJSON,
					CreatedAtRaw: fmt.Sprintf("%d", recentSec),
					UpdatedAtRaw: fmt.Sprintf("%d", recentSec),
				},
				{
					SourceChatID: "chat_race_dup",
					UserEmail:    "alice@example.com",
					ChatJSON:     flatChatJSON,
					CreatedAtRaw: fmt.Sprintf("%d", recentSec),
					UpdatedAtRaw: fmt.Sprintf("%d", recentSec),
				},
				{
					SourceChatID: "chat_unmatched",
					UserEmail:    "nobody@example.com",
					ChatJSON:     flatChatJSON,
					CreatedAtRaw: fmt.Sprintf("%d", recentSec),
					UpdatedAtRaw: fmt.Sprintf("%d", recentSec),
				},
				{
					SourceChatID: "chat_empty",
					UserEmail:    "alice@example.com",
					ChatJSON:     `{"history":{"messages":{"blank":{"id":"blank","role":"user","content":"   "}}}}`,
					CreatedAtRaw: fmt.Sprintf("%d", recentSec),
					UpdatedAtRaw: fmt.Sprintf("%d", recentSec),
				},
				{
					SourceChatID: "chat_bad_json",
					UserEmail:    "alice@example.com",
					ChatJSON:     `{not-json`,
					CreatedAtRaw: fmt.Sprintf("%d", recentSec),
					UpdatedAtRaw: fmt.Sprintf("%d", recentSec),
				},
			},
		},
	}

	repo := &conversationImportRepoFake{
		existingKeys:  map[string]struct{}{alreadyImportedKey: {}},
		duplicateKeys: map[string]bool{raceDuplicateKey: true},
	}
	audit := &recordingAuditServiceFake{}

	service := NewService(users, audit)
	service.SetOpenWebUIChatLoader(loader)
	service.SetConversationImportRepo(repo)

	// 1. DryRun 预览：不应落库、不写审计日志。
	preview, err := service.ImportOpenWebUIConversations(
		context.Background(),
		"req_preview",
		1,
		OpenWebUIConversationImportInput{DSN: "postgres://localhost/owui", RecentDays: intPtr(7), DryRun: true},
		"127.0.0.1",
		"ua",
	)
	if err != nil {
		t.Fatalf("DryRun ImportOpenWebUIConversations() error = %v", err)
	}
	if len(repo.imported) != 0 || len(audit.entries) != 0 {
		t.Fatalf("expected dryRun not to write DB or audit log, got imported=%d audit=%d", len(repo.imported), len(audit.entries))
	}
	if preview.Scanned != 9 || preview.Imported != 3 || preview.ImportedMessages != 10 {
		t.Fatalf("unexpected dryRun counts: %+v", preview)
	}
	if preview.SkippedOutsideTimeRange != 2 || preview.SkippedExistingConversation != 1 || preview.SkippedUnmatchedUser != 1 || preview.SkippedEmptyConversation != 1 || preview.SkippedInvalidRow != 1 {
		t.Fatalf("unexpected dryRun skip counts: %+v", preview)
	}
	if preview.RecentDays == nil || *preview.RecentDays != 7 || preview.CutoffAt == nil || loader.cutoff == nil {
		t.Fatalf("expected recentDays=7 and non-nil cutoff, got %+v", preview)
	}

	// 2. 真实导入：raceDuplicateKey 返回 ErrDuplicate 后计入 SkippedExistingConversation。
	res, err := service.ImportOpenWebUIConversations(
		context.Background(),
		"req_apply",
		1,
		OpenWebUIConversationImportInput{DSN: "postgres://localhost/owui", RecentDays: intPtr(7), DryRun: false},
		"127.0.0.1",
		"ua",
	)
	if err != nil {
		t.Fatalf("ImportOpenWebUIConversations() error = %v", err)
	}
	if res.Imported != 2 || res.ImportedMessages != 7 || res.SkippedExistingConversation != 2 {
		t.Fatalf("unexpected apply result: %+v", res)
	}
	if len(audit.entries) != 1 || audit.entries[0].Action != openWebUIConversationAuditAction {
		t.Fatalf("expected 1 audit entry for conversation import, got %#v", audit.entries)
	}

	// 验证第一条导入对话（分支树 + 推理 + 标签 + 星标 + 模型 + 标题回退）
	treeConv := repo.imported[0]
	if treeConv.Conversation.UserID != 10 || treeConv.Conversation.Title != "Explain Go interfaces" {
		t.Fatalf("unexpected tree conv user/title: %+v", treeConv.Conversation)
	}
	if treeConv.Conversation.Model != "claude-3-7-sonnet" || treeConv.Conversation.Provider != "anthropic" {
		t.Fatalf("unexpected tree conv model/provider: %+v", treeConv.Conversation)
	}
	if !treeConv.Conversation.IsStarred || treeConv.Conversation.StarredAt == nil || treeConv.Conversation.Status != "active" {
		t.Fatalf("unexpected tree conv star/status: %+v", treeConv.Conversation)
	}
	if treeConv.Conversation.LabelsJSON != `["work","arch","payload-tag"]` || !treeConv.Conversation.LabelsManuallyManaged {
		t.Fatalf("unexpected tree conv labels: %q manual=%v", treeConv.Conversation.LabelsJSON, treeConv.Conversation.LabelsManuallyManaged)
	}
	if len(treeConv.Messages) != 4 {
		t.Fatalf("expected 4 tree messages, got %d", len(treeConv.Messages))
	}
	// 非活跃分支 (a1, u2_alt) 必须排在活跃分支 (a2) 之前，确保 a2 为最后一条消息。
	if treeConv.Messages[0].Message.Role != "user" || treeConv.Messages[0].ParentIndex != -1 {
		t.Fatalf("unexpected root message: %+v", treeConv.Messages[0])
	}
	if treeConv.Messages[1].Message.Content != "Alt answer" || treeConv.Messages[1].Message.ReasoningContent != "alt thinking" || treeConv.Messages[1].ParentIndex != 0 {
		t.Fatalf("unexpected alt assistant message: %+v", treeConv.Messages[1])
	}
	if treeConv.Messages[2].Message.Content != "Follow up on alt" || treeConv.Messages[2].ParentIndex != 1 {
		t.Fatalf("unexpected alt child message: %+v", treeConv.Messages[2])
	}
	activeMsg := treeConv.Messages[3]
	if activeMsg.ParentIndex != 0 || activeMsg.Message.Content != "Main answer" || activeMsg.Message.ReasoningContent != "line one\nline two" || activeMsg.ReasoningDurationSec != 4 {
		t.Fatalf("unexpected active assistant message: %+v", activeMsg)
	}

	// 验证第二条导入对话（归档 + 扁平消息 + reasoning_content）
	flatConv := repo.imported[1]
	if flatConv.Conversation.Status != "archived" || flatConv.Conversation.Model != "deepseek-r1" || flatConv.Conversation.Provider != "internal" {
		t.Fatalf("unexpected flat conv metadata: %+v", flatConv.Conversation)
	}
	if len(flatConv.Messages) != 3 || flatConv.Messages[2].Message.ReasoningContent != "greeting plan" {
		t.Fatalf("unexpected flat messages: %+v", flatConv.Messages)
	}
}

func TestImportOpenWebUIConversationsLocalModeCyclesAndSkippedRoles(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Unix()
	users := newAdminUserServiceFake(map[uint]domainuser.User{
		99: {ID: 99, Username: "owner", Role: domainuser.RoleSuperAdmin},
	})

	cycleAndSkippedRoleJSON := fmt.Sprintf(`{
		"id": "chat_cycle",
		"title": "Cycle & Role Reparent",
		"history": {
			"currentId": "a_final",
			"messages": {
				"u1": {"id":"u1","parentId":"","childrenIds":["t1"],"role":"user","content":"Start","timestamp":%d},
				"t1": {"id":"t1","parentId":"u1","childrenIds":["a_final"],"role":"tool","content":"ignored tool output","timestamp":%d},
				"a_final": {"id":"a_final","parentId":"t1","childrenIds":[],"role":"assistant","content":"Done","details":[{"type":"reasoning","content":"tool checked","duration":2}],"timestamp":%d},
				"loop_a": {"id":"loop_a","parentId":"loop_b","childrenIds":["loop_b"],"role":"user","content":"Orphan loop A","timestamp":%d},
				"loop_b": {"id":"loop_b","parentId":"loop_a","childrenIds":["loop_a"],"role":"assistant","content":"Orphan loop B","timestamp":%d}
			}
		}
	}`, now, now+1, now+2, now+3, now+4)

	loader := &openWebUIChatLoaderFake{
		result: &repository.OpenWebUIChatLoadResult{
			TotalCount: 1,
			Rows: []repository.OpenWebUIChatRow{
				{
					SourceChatID: "chat_cycle",
					UserEmail:    "", // 本地模式忽略 OpenWebUI 邮箱，归属到当前本地 owner (actorUserID=99)
					ChatJSON:     cycleAndSkippedRoleJSON,
					CreatedAtRaw: fmt.Sprintf("%d", now),
					UpdatedAtRaw: fmt.Sprintf("%d", now+4),
				},
			},
		},
	}
	repo := &conversationImportRepoFake{}
	service := NewService(users, auditServiceFake{})
	service.SetOpenWebUIChatLoader(loader)
	service.SetConversationImportRepo(repo)
	service.SetLocalMode(true)

	res, err := service.ImportOpenWebUIConversations(
		context.Background(),
		"req_local",
		99,
		OpenWebUIConversationImportInput{DSN: "/tmp/webui.db"},
		"127.0.0.1",
		"ua",
	)
	if err != nil {
		t.Fatalf("localMode ImportOpenWebUIConversations() error = %v", err)
	}
	if res.Imported != 1 || res.SkippedUnmatchedUser != 0 || res.ImportedMessages != 4 {
		t.Fatalf("unexpected localMode result: %+v", res)
	}
	imported := repo.imported[0]
	if imported.Conversation.UserID != 99 {
		t.Fatalf("expected targetUserID=99 in localMode, got %d", imported.Conversation.UserID)
	}
	// tool 消息被跳过后，a_final 的父节点应重挂回 u1 (index 0)
	if imported.Messages[1].Message.Content != "Done" || imported.Messages[1].ParentIndex != 0 {
		t.Fatalf("expected a_final reparented to u1 (index 0), got %+v", imported.Messages[1])
	}
	if imported.Messages[1].Message.ReasoningContent != "tool checked" || imported.Messages[1].ReasoningDurationSec != 2 {
		t.Fatalf("expected details reasoning extraction, got %+v", imported.Messages[1])
	}

	// 验证非数组 details（例如某些 OpenWebUI 插件写入的 "{}"）不会导致整条会话解析失败。
	objectDetailsJSON := fmt.Sprintf(`{
		"id": "chat_obj_details",
		"title": "Non-Array Details",
		"messages": [
			{"id":"m1","role":"user","content":"Ping","timestamp":%d},
			{"id":"m2","role":"assistant","content":"Pong","details":{"unexpected":"object"},"timestamp":%d}
		]
	}`, now, now+1)
	loader2 := &openWebUIChatLoaderFake{
		result: &repository.OpenWebUIChatLoadResult{
			TotalCount: 1,
			Rows: []repository.OpenWebUIChatRow{
				{
					SourceChatID: "chat_obj_details",
					ChatJSON:     objectDetailsJSON,
					CreatedAtRaw: fmt.Sprintf("%d", now),
					UpdatedAtRaw: fmt.Sprintf("%d", now+1),
				},
			},
		},
	}
	repo2 := &conversationImportRepoFake{}
	service2 := NewService(users, auditServiceFake{})
	service2.SetOpenWebUIChatLoader(loader2)
	service2.SetConversationImportRepo(repo2)
	service2.SetLocalMode(true)
	res2, err := service2.ImportOpenWebUIConversations(
		context.Background(),
		"req_obj_details",
		99,
		OpenWebUIConversationImportInput{DSN: "/tmp/webui.db"},
		"127.0.0.1",
		"ua",
	)
	if err != nil {
		t.Fatalf("non-array details ImportOpenWebUIConversations() error = %v", err)
	}
	if res2.Imported != 1 || res2.SkippedInvalidRow != 0 || res2.ImportedMessages != 2 {
		t.Fatalf("expected non-array details chat to import cleanly, got %+v", res2)
	}
}
