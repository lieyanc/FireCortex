package conversation

import (
	"context"
	"errors"
	"testing"
	"time"

	model "github.com/lieyanc/FireCortex/backend/internal/domain/conversation"
	"github.com/lieyanc/FireCortex/backend/internal/infra/config"
	"github.com/lieyanc/FireCortex/backend/internal/repository"
)

type assistantEditTraceRepository struct {
	repository.ConversationRepository
	message model.Message
}

func (r *assistantEditTraceRepository) GetMessageByPublicIDForUser(
	context.Context,
	uint,
	string,
) (*model.Message, error) {
	item := r.message
	return &item, nil
}

func (r *assistantEditTraceRepository) UpdateMessageContent(
	_ context.Context,
	_ uint,
	_ string,
	content string,
	editedAt time.Time,
) (*model.Message, error) {
	r.message.Content = content
	r.message.EditedAt = &editedAt
	item := r.message
	return &item, nil
}

func (r *assistantEditTraceRepository) GetUserMessageFeedbackMap(
	context.Context,
	uint,
	[]uint,
) (map[uint]string, error) {
	return map[uint]string{}, nil
}

func (r *assistantEditTraceRepository) GetMessageFeedbackCounts(
	context.Context,
	[]uint,
) (map[uint]map[string]int64, error) {
	return map[uint]map[string]int64{}, nil
}

func (r *assistantEditTraceRepository) GetMessageUIStateMap(
	context.Context,
	uint,
	[]uint,
) (map[uint]map[string]string, error) {
	return map[uint]map[string]string{r.message.ID: {"loan": `{"amount":300}`}}, nil
}

func (r *assistantEditTraceRepository) ListConversationMessageTracesByMessageIDs(
	context.Context,
	[]uint,
) ([]model.MessageTrace, error) {
	return []model.MessageTrace{{
		MessageID:       r.message.ID,
		TraceType:       messageTraceTypeProcess,
		Title:           "Processing complete",
		ContentMarkdown: "Retained processing details",
		Status:          messageTraceStatusCompleted,
	}}, nil
}

func (r *assistantEditTraceRepository) ListConversationMessageTraceEventsByMessageIDs(
	context.Context,
	[]uint,
) ([]model.MessageTraceEventRow, error) {
	return []model.MessageTraceEventRow{{
		MessageID: r.message.ID,
		EventID:   "event_tool_1",
		EventType: "tool",
		Title:     "Tool complete",
		Status:    messageTraceStatusCompleted,
		Seq:       1,
	}}, nil
}

func TestAssistantEditResponseRetainsProcessTrace(t *testing.T) {
	repo := &assistantEditTraceRepository{
		message: model.Message{
			ID:             41,
			ConversationID: 17,
			UserID:         9,
			PublicID:       "message_assistant_edit",
			Role:           "assistant",
			Status:         "success",
			Content:        "before",
		},
	}
	service := &Service{
		cfg: config.NewRuntime(config.Config{
			ProcessTraceEnabled: true,
		}),
		repo: repo,
	}

	updated, err := service.UpdateMessageContent(
		context.Background(),
		repo.message.UserID,
		repo.message.PublicID,
		"after",
	)
	if err != nil {
		t.Fatalf("edit assistant message: %v", err)
	}
	if updated.Content != "after" || updated.EditedAt == nil {
		t.Fatalf("unexpected edited message: %#v", updated)
	}
	if updated.ProcessTrace == nil || updated.ProcessTrace.Process == nil {
		t.Fatalf("edited response lost process trace: %#v", updated.ProcessTrace)
	}
	if len(updated.ProcessTrace.Events) != 1 || updated.ProcessTrace.Events[0].EventID != "event_tool_1" {
		t.Fatalf("edited response lost execution events: %#v", updated.ProcessTrace)
	}
	// 前端用编辑响应替换本地消息：组件状态必须一并返回，否则编辑一次正文就丢了界面状态。
	if updated.UIState["loan"] != `{"amount":300}` {
		t.Fatalf("edited response lost ui state: %#v", updated.UIState)
	}
}

func TestUpdateMessageContentAllowsUserAndAssistantOnly(t *testing.T) {
	cases := []struct {
		role    string
		wantErr error
	}{
		{role: "user"},
		{role: "assistant"},
		{role: "system", wantErr: ErrMessageEditTargetInvalid},
		{role: "tool", wantErr: ErrMessageEditTargetInvalid},
	}
	for _, tc := range cases {
		repo := &assistantEditTraceRepository{
			message: model.Message{ID: 1, ConversationID: 1, UserID: 9, PublicID: "message_" + tc.role, Role: tc.role, Status: "success", Content: "before"},
		}
		service := &Service{cfg: config.NewRuntime(config.Config{}), repo: repo}
		updated, err := service.UpdateMessageContent(context.Background(), 9, repo.message.PublicID, "after")
		if tc.wantErr != nil {
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("%s: got %v, want %v", tc.role, err, tc.wantErr)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", tc.role, err)
		}
		if updated.Content != "after" || updated.EditedAt == nil {
			t.Fatalf("%s: unexpected result %#v", tc.role, updated)
		}
	}
}
