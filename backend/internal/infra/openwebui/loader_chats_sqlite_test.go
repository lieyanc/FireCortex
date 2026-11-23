//go:build sqlite

package openwebui

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lieyanc/FireCortex/backend/internal/repository"
	_ "github.com/mattn/go-sqlite3"
)

func TestLoadOpenWebUIChatsSQLiteFullAndCutoff(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "webui.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE "user" (
			id TEXT PRIMARY KEY,
			name TEXT,
			email TEXT
		);
		CREATE TABLE "chat" (
			id TEXT PRIMARY KEY,
			user_id TEXT,
			title TEXT,
			chat TEXT,
			created_at BIGINT,
			updated_at BIGINT,
			archived BOOLEAN,
			pinned BOOLEAN,
			meta TEXT
		);
	`)
	if err != nil {
		t.Fatalf("create tables: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	recentSec := now.Add(-2 * 24 * time.Hour).Unix()
	recentMillis := now.Add(-1 * 24 * time.Hour).UnixMilli()
	oldSec := now.Add(-15 * 24 * time.Hour).Unix()

	_, err = db.Exec(`
		INSERT INTO "user" (id, name, email) VALUES ('u1', 'Alice', 'alice@example.com');
		INSERT INTO "chat" (id, user_id, title, chat, created_at, updated_at, archived, pinned, meta) VALUES
			('c_old', 'u1', 'Old Chat', '{"messages":[]}', ?, ?, 1, 0, '{"tags":["old"]}'),
			('c_recent_sec', 'u1', 'Recent Chat', '{"messages":[]}', ?, ?, 0, 1, '{"tags":["go"]}'),
			('c_recent_ms', 'missing_user', 'Millis Chat', '{"messages":[]}', ?, ?, 0, 0, '{}');
	`, oldSec, oldSec, oldSec, recentSec, recentMillis, recentMillis)
	if err != nil {
		t.Fatalf("insert test rows: %v", err)
	}

	loader := NewRowLoader()
	ctx := context.Background()

	allRes, err := loader.LoadOpenWebUIChats(ctx, "sqlite://"+dbPath, nil)
	if err != nil {
		t.Fatalf("LoadOpenWebUIChats(all) error = %v", err)
	}
	if allRes.TotalCount != 3 || len(allRes.Rows) != 3 {
		t.Fatalf("expected 3 chats total and loaded, got total=%d rows=%d", allRes.TotalCount, len(allRes.Rows))
	}

	cutoff := now.Add(-7 * 24 * time.Hour)
	recentRes, err := loader.LoadOpenWebUIChats(ctx, dbPath, &cutoff)
	if err != nil {
		t.Fatalf("LoadOpenWebUIChats(cutoff) error = %v", err)
	}
	if recentRes.TotalCount != 3 {
		t.Fatalf("expected TotalCount=3, got %d", recentRes.TotalCount)
	}
	if len(recentRes.Rows) != 2 {
		t.Fatalf("expected 2 recent rows (sec + ms), got %d (%#v)", len(recentRes.Rows), recentRes.Rows)
	}
	if recentRes.Rows[0].SourceChatID != "c_recent_ms" || recentRes.Rows[1].SourceChatID != "c_recent_sec" {
		t.Fatalf("unexpected row order/ids: %#v", recentRes.Rows)
	}
	if !recentRes.Rows[1].Pinned || recentRes.Rows[1].Archived {
		t.Fatalf("expected pinned=true archived=false on c_recent_sec, got %#v", recentRes.Rows[1])
	}
	if recentRes.Rows[1].UserEmail != "alice@example.com" {
		t.Fatalf("expected matched user email alice@example.com, got %q", recentRes.Rows[1].UserEmail)
	}
}

func TestLoadOpenWebUIChatsSQLiteMinimalSchemaAndMissingTable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	loader := NewRowLoader()

	emptyPath := filepath.Join(t.TempDir(), "empty.db")
	emptyDB, err := sql.Open("sqlite3", emptyPath)
	if err != nil {
		t.Fatalf("open empty sqlite: %v", err)
	}
	_ = emptyDB.Close()

	if _, err = loader.LoadOpenWebUIChats(ctx, emptyPath, nil); !errors.Is(err, repository.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for missing chat table, got %v", err)
	}

	minPath := filepath.Join(t.TempDir(), "minimal.db")
	minDB, err := sql.Open("sqlite3", minPath)
	if err != nil {
		t.Fatalf("open minimal sqlite: %v", err)
	}
	defer minDB.Close()

	_, err = minDB.Exec(`
		CREATE TABLE "chat" (
			id TEXT PRIMARY KEY,
			user_id TEXT,
			title TEXT,
			chat TEXT,
			created_at BIGINT,
			updated_at BIGINT
		);
		INSERT INTO "chat" (id, user_id, title, chat, created_at, updated_at) VALUES
			('c1', 'u1', 'Legacy Chat', '{"messages":[]}', 1700000000, 1700000100);
	`)
	if err != nil {
		t.Fatalf("setup minimal table: %v", err)
	}

	res, err := loader.LoadOpenWebUIChats(ctx, minPath, nil)
	if err != nil {
		t.Fatalf("LoadOpenWebUIChats(minimal) error = %v", err)
	}
	if res.TotalCount != 1 || len(res.Rows) != 1 {
		t.Fatalf("expected 1 row from minimal schema, got total=%d rows=%d", res.TotalCount, len(res.Rows))
	}
	if res.Rows[0].SourceChatID != "c1" || res.Rows[0].UserEmail != "" || res.Rows[0].Pinned || res.Rows[0].Archived {
		t.Fatalf("unexpected minimal row mapping: %#v", res.Rows[0])
	}
}
