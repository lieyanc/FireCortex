package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	appadmin "github.com/lieyanc/FireCortex/backend/internal/application/admin"
	domainuser "github.com/lieyanc/FireCortex/backend/internal/domain/user"
	"github.com/lieyanc/FireCortex/backend/internal/repository"
	"github.com/lieyanc/FireCortex/backend/internal/transport/http/middleware"
)

type handlerOpenWebUIUserServiceFake struct {
	*handlerUserServiceFake
}

func (s *handlerOpenWebUIUserServiceFake) ListUsersByLowerEmails(_ context.Context, emails []string) (map[string]domainuser.User, error) {
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

type handlerOpenWebUIChatLoaderFake struct {
	result *repository.OpenWebUIChatLoadResult
	err    error
}

func (f handlerOpenWebUIChatLoaderFake) LoadOpenWebUIChats(context.Context, string, *time.Time) (*repository.OpenWebUIChatLoadResult, error) {
	return f.result, f.err
}

type handlerConversationImportRepoFake struct {
	imported []repository.ConversationImportRecord
}

func (f *handlerConversationImportRepoFake) ListExistingConversationSessionKeys(context.Context, []string) (map[string]struct{}, error) {
	return map[string]struct{}{}, nil
}

func (f *handlerConversationImportRepoFake) ImportConversation(_ context.Context, record repository.ConversationImportRecord) error {
	f.imported = append(f.imported, record)
	return nil
}

func newOpenWebUIConversationImportTestRouter(
	loaderErr error,
	chatResult *repository.OpenWebUIChatLoadResult,
	repo *handlerConversationImportRepoFake,
) *gin.Engine {
	gin.SetMode(gin.TestMode)
	users := &handlerOpenWebUIUserServiceFake{
		handlerUserServiceFake: &handlerUserServiceFake{
			users: map[uint]domainuser.User{
				1: {ID: 1, Username: "admin", Email: "admin@example.com", Role: domainuser.RoleSuperAdmin},
				2: {ID: 2, Username: "alice", Email: "alice@example.com", Role: domainuser.RoleUser},
			},
		},
	}
	service := appadmin.NewService(users, handlerAuditServiceFake{})
	service.SetOpenWebUIChatLoader(handlerOpenWebUIChatLoaderFake{result: chatResult, err: loaderErr})
	service.SetConversationImportRepo(repo)
	handler := NewHandler(service)

	router := gin.New()
	router.POST("/admin/conversations/import/openwebui", func(c *gin.Context) {
		c.Set(middleware.ContextKeyUserID, uint(1))
		c.Set(middleware.ContextKeyRequestID, "req_test")
		handler.ImportOpenWebUIConversations(c)
	})
	return router
}

func TestImportOpenWebUIConversationsValidationErrors(t *testing.T) {
	t.Parallel()

	router := newOpenWebUIConversationImportTestRouter(repository.ErrInvalidInput, nil, &handlerConversationImportRepoFake{})

	cases := []struct {
		name      string
		body      string
		wantError string
	}{
		{
			name:      "non-positive recentDays",
			body:      `{"dsn":"postgres://localhost/owui","recentDays":0}`,
			wantError: "request.invalid_import_recent_days",
		},
		{
			name:      "excessive recentDays",
			body:      `{"dsn":"postgres://localhost/owui","recentDays":36501}`,
			wantError: "request.invalid_import_recent_days",
		},
		{
			name:      "invalid dsn",
			body:      `{"dsn":"bad-dsn","recentDays":7}`,
			wantError: "request.invalid_import_dsn",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/admin/conversations/import/openwebui", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.wantError) {
				t.Fatalf("expected error %q in body=%s", tc.wantError, rec.Body.String())
			}
		})
	}
}

func TestImportOpenWebUIConversationsDryRunAndImport(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Unix()
	repo := &handlerConversationImportRepoFake{}
	router := newOpenWebUIConversationImportTestRouter(nil, &repository.OpenWebUIChatLoadResult{
		TotalCount: 1,
		Rows: []repository.OpenWebUIChatRow{
			{
				SourceChatID: "c1",
				UserEmail:    "alice@example.com",
				Title:        "HTTP Import Test",
				ChatJSON:     `{"id":"c1","messages":[{"id":"m1","role":"user","content":"Hello"},{"id":"m2","role":"assistant","content":"Hi"}]}`,
				CreatedAtRaw: strings.TrimSpace(time.Unix(now, 0).UTC().Format(time.RFC3339)),
				UpdatedAtRaw: strings.TrimSpace(time.Unix(now, 0).UTC().Format(time.RFC3339)),
			},
		},
	}, repo)

	// Dry run
	previewReq := httptest.NewRequest(
		http.MethodPost,
		"/admin/conversations/import/openwebui",
		strings.NewReader(`{"dsn":"postgres://localhost/owui","recentDays":7,"dryRun":true}`),
	)
	previewReq.Header.Set("Content-Type", "application/json")
	previewRec := httptest.NewRecorder()
	router.ServeHTTP(previewRec, previewReq)

	if previewRec.Code != http.StatusOK {
		t.Fatalf("dryRun expected 200, got %d body=%s", previewRec.Code, previewRec.Body.String())
	}
	if len(repo.imported) != 0 {
		t.Fatalf("dryRun must not import records, got %d", len(repo.imported))
	}

	// Real import
	applyReq := httptest.NewRequest(
		http.MethodPost,
		"/admin/conversations/import/openwebui",
		strings.NewReader(`{"dsn":"postgres://localhost/owui","recentDays":7,"dryRun":false}`),
	)
	applyReq.Header.Set("Content-Type", "application/json")
	applyRec := httptest.NewRecorder()
	router.ServeHTTP(applyRec, applyReq)

	if applyRec.Code != http.StatusOK {
		t.Fatalf("apply expected 200, got %d body=%s", applyRec.Code, applyRec.Body.String())
	}
	var body struct {
		Data ImportOpenWebUIConversationsResponse `json:"data"`
	}
	if err := json.Unmarshal(applyRec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body.Data.Imported != 1 || body.Data.ImportedMessages != 2 || body.Data.RecentDays == nil || *body.Data.RecentDays != 7 {
		t.Fatalf("unexpected response data: %+v", body.Data)
	}
	if len(repo.imported) != 1 {
		t.Fatalf("expected 1 persisted conversation, got %d", len(repo.imported))
	}
}
