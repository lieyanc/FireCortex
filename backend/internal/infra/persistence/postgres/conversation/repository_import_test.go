//go:build cgo

package conversation

import (
	"context"
	"errors"
	"testing"
	"time"

	domainconversation "github.com/lieyanc/FireCortex/backend/internal/domain/conversation"
	"github.com/lieyanc/FireCortex/backend/internal/infra/persistence/models"
	"github.com/lieyanc/FireCortex/backend/internal/repository"
)

func TestImportConversationPreservesTimestampsBranchesAndTraces(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	repo := NewRepo(db)
	ctx := context.Background()

	convCreated := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	convUpdated := time.Date(2026, 9, 15, 12, 30, 0, 0, time.UTC)
	msg1Time := time.Date(2026, 9, 1, 10, 0, 5, 0, time.UTC)
	msg2Time := time.Date(2026, 9, 1, 10, 0, 12, 0, time.UTC)
	msg3Time := time.Date(2026, 9, 15, 12, 29, 50, 0, time.UTC)

	record := repository.ConversationImportRecord{
		Conversation: domainconversation.Conversation{
			UserID:                42,
			PublicID:              "owc_conv_1",
			Title:                 "Imported Tree",
			LabelsJSON:            `["work","go"]`,
			LabelsManuallyManaged: true,
			Model:                 "deepseek-r1",
			ModelMode:             "manual",
			Provider:              "internal",
			SessionKey:            "openwebui:test_session_1",
			IsStarred:             true,
			StarredAt:             &convUpdated,
			Status:                "active",
			ContextPolicy:         "{}",
			CreatedAt:             convCreated,
			UpdatedAt:             convUpdated,
		},
		Messages: []repository.ImportedConversationMessage{
			{
				ParentIndex: -1,
				Message: domainconversation.Message{
					PublicID:     "owm_u1",
					Role:         "user",
					ContentType:  "text",
					Content:      "Question?",
					BranchReason: "default",
					Status:       "success",
					CreatedAt:    msg1Time,
					UpdatedAt:    msg1Time,
				},
			},
			{
				ParentIndex:          0,
				ReasoningDurationSec: 5,
				Message: domainconversation.Message{
					PublicID:         "owm_a1_alt",
					Role:             "assistant",
					ContentType:      "text",
					Content:          "First draft answer",
					ReasoningContent: "Draft thought process",
					BranchReason:     "default",
					Status:           "success",
					CreatedAt:        msg2Time,
					UpdatedAt:        msg2Time,
				},
			},
			{
				ParentIndex:          0,
				ReasoningDurationSec: 7,
				Message: domainconversation.Message{
					PublicID:         "owm_a1_active",
					Role:             "assistant",
					ContentType:      "text",
					Content:          "Final active answer",
					ReasoningContent: "Refined thought process",
					BranchReason:     "default",
					Status:           "success",
					CreatedAt:        msg3Time,
					UpdatedAt:        msg3Time,
				},
			},
		},
	}

	if err := repo.ImportConversation(ctx, record); err != nil {
		t.Fatalf("ImportConversation() error = %v", err)
	}

	gotConv, err := repo.GetConversationByPublicID(ctx, "owc_conv_1", 42)
	if err != nil {
		t.Fatalf("GetConversationByPublicID() error = %v", err)
	}
	if !gotConv.CreatedAt.Equal(convCreated) {
		t.Fatalf("conv CreatedAt = %v, want %v", gotConv.CreatedAt, convCreated)
	}
	if !gotConv.UpdatedAt.Equal(convUpdated) {
		t.Fatalf("conv UpdatedAt = %v, want %v", gotConv.UpdatedAt, convUpdated)
	}
	if gotConv.MessageCount != 3 || !gotConv.IsStarred {
		t.Fatalf("unexpected conv metadata: count=%d starred=%v", gotConv.MessageCount, gotConv.IsStarred)
	}

	msgs, err := repo.ListAllMessages(ctx, gotConv.ID)
	if err != nil {
		t.Fatalf("ListAllMessages() error = %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].PublicID != "owm_u1" || msgs[1].PublicID != "owm_a1_alt" || msgs[2].PublicID != "owm_a1_active" {
		t.Fatalf("unexpected message ordering: %#v", msgs)
	}
	if msgs[0].ParentMessageID != nil {
		t.Fatalf("expected root message ParentMessageID=nil, got %v", msgs[0].ParentMessageID)
	}
	if msgs[1].ParentMessageID == nil || *msgs[1].ParentMessageID != msgs[0].ID {
		t.Fatalf("expected alt branch parent=%d, got %v", msgs[0].ID, msgs[1].ParentMessageID)
	}
	if msgs[2].ParentMessageID == nil || *msgs[2].ParentMessageID != msgs[0].ID {
		t.Fatalf("expected active branch parent=%d, got %v", msgs[0].ID, msgs[2].ParentMessageID)
	}
	if !msgs[2].CreatedAt.Equal(msg3Time) || !msgs[2].UpdatedAt.Equal(msg3Time) {
		t.Fatalf("expected preserved message timestamps %v, got created=%v updated=%v", msg3Time, msgs[2].CreatedAt, msgs[2].UpdatedAt)
	}

	traces, err := repo.ListConversationMessageTracesByMessageIDs(ctx, []uint{msgs[2].ID})
	if err != nil {
		t.Fatalf("ListConversationMessageTracesByMessageIDs() error = %v", err)
	}
	if len(traces) != 1 || traces[0].TraceType != "upstream_think" || traces[0].ContentMarkdown != "Refined thought process" {
		t.Fatalf("unexpected trace blocks: %#v", traces)
	}
	if !traces[0].StartedAt.Equal(msg3Time.Add(-7 * time.Second)) {
		t.Fatalf("trace StartedAt = %v, want %v", traces[0].StartedAt, msg3Time.Add(-7*time.Second))
	}

	events, err := repo.ListConversationMessageTraceEventsByMessageIDs(ctx, []uint{msgs[2].ID})
	if err != nil {
		t.Fatalf("ListConversationMessageTraceEventsByMessageIDs() error = %v", err)
	}
	if len(events) != 1 || events[0].EventType != "think" || events[0].ContentMarkdown != "Refined thought process" {
		t.Fatalf("unexpected trace events: %#v", events)
	}

	// 重复 session_key 必须触发 ErrDuplicate
	dup := record
	dup.Conversation.PublicID = "owc_conv_dup"
	dup.Messages[0].Message.PublicID = "owm_dup_1"
	dup.Messages[1].Message.PublicID = "owm_dup_2"
	dup.Messages[2].Message.PublicID = "owm_dup_3"
	if err := repo.ImportConversation(ctx, dup); !errors.Is(err, repository.ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate on duplicate session_key, got %v", err)
	}
}

func TestListExistingConversationSessionKeysIncludesSoftDeleted(t *testing.T) {
	db := openConversationRepositoryTestDB(t)
	repo := NewRepo(db)
	ctx := context.Background()

	active := models.Conversation{
		UserID:     1,
		PublicID:   "conv_active",
		SessionKey: "openwebui:active",
		Status:     "active",
	}
	deleted := models.Conversation{
		UserID:     1,
		PublicID:   "conv_deleted",
		SessionKey: "openwebui:deleted",
		Status:     "active",
	}
	if err := db.Create(&active).Error; err != nil {
		t.Fatalf("create active: %v", err)
	}
	if err := db.Create(&deleted).Error; err != nil {
		t.Fatalf("create deleted: %v", err)
	}
	if err := db.Delete(&deleted).Error; err != nil {
		t.Fatalf("soft-delete conversation: %v", err)
	}

	found, err := repo.ListExistingConversationSessionKeys(ctx, []string{
		"openwebui:active",
		"openwebui:deleted",
		"openwebui:missing",
		"openwebui:active",
		" ",
	})
	if err != nil {
		t.Fatalf("ListExistingConversationSessionKeys() error = %v", err)
	}
	if len(found) != 2 {
		t.Fatalf("expected 2 found keys (including soft-deleted), got %#v", found)
	}
	if _, ok := found["openwebui:active"]; !ok {
		t.Fatal("missing openwebui:active")
	}
	if _, ok := found["openwebui:deleted"]; !ok {
		t.Fatal("missing openwebui:deleted")
	}
}
