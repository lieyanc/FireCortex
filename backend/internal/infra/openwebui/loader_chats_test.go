package openwebui

import (
	"strings"
	"testing"
	"time"
)

func TestClassifyChatTimeColumnType(t *testing.T) {
	t.Parallel()

	cases := []struct {
		dbType string
		want   chatFilterMode
	}{
		{dbType: "BIGINT", want: chatFilterNumeric},
		{dbType: "integer", want: chatFilterNumeric},
		{dbType: "int8", want: chatFilterNumeric},
		{dbType: "TIMESTAMPTZ", want: chatFilterTimestamp},
		{dbType: "timestamp with time zone", want: chatFilterTimestamp},
		{dbType: "DATETIME", want: chatFilterTimestamp},
		{dbType: "TEXT", want: chatFilterNone},
		{dbType: "", want: chatFilterNone},
	}

	for _, tc := range cases {
		if got := classifyChatTimeColumnType(tc.dbType); got != tc.want {
			t.Fatalf("classifyChatTimeColumnType(%q) = %q, want %q", tc.dbType, got, tc.want)
		}
	}
}

func TestBuildChatImportQueryOptionalColumnsAndCutoff(t *testing.T) {
	t.Parallel()

	cutoff := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

	fullOpts := chatQueryOptions{
		HasUser:      true,
		HasTitle:     true,
		HasMeta:      true,
		HasArchived:  true,
		HasPinned:    true,
		HasCreatedAt: true,
		HasUpdatedAt: true,
		FilterMode:   chatFilterNumeric,
	}
	query, args := buildChatImportQuery(fullOpts, &cutoff)
	if !strings.Contains(query, `LEFT JOIN "user" AS u ON u.id = c.user_id`) {
		t.Fatalf("expected user join in full query: %s", query)
	}
	if !strings.Contains(query, `WHERE COALESCE(NULLIF(c.updated_at, 0), c.created_at) >= ?`) {
		t.Fatalf("expected numeric cutoff predicate in full query: %s", query)
	}
	if len(args) != 1 || args[0] != cutoff.Unix() {
		t.Fatalf("unexpected query args: %#v", args)
	}

	minimalOpts := chatQueryOptions{
		HasUser:      false,
		HasTitle:     false,
		HasMeta:      false,
		HasArchived:  false,
		HasPinned:    false,
		HasCreatedAt: true,
		HasUpdatedAt: false,
		FilterMode:   chatFilterTimestamp,
	}
	minimalQuery, minimalArgs := buildChatImportQuery(minimalOpts, &cutoff)
	if strings.Contains(minimalQuery, `LEFT JOIN "user"`) {
		t.Fatalf("did not expect user join when user table is absent: %s", minimalQuery)
	}
	if !strings.Contains(minimalQuery, `FALSE AS archived`) || !strings.Contains(minimalQuery, `FALSE AS pinned`) {
		t.Fatalf("expected fallback archived/pinned expressions: %s", minimalQuery)
	}
	if !strings.Contains(minimalQuery, `WHERE c.created_at >= ?`) {
		t.Fatalf("expected timestamp fallback predicate: %s", minimalQuery)
	}
	if len(minimalArgs) != 1 || !minimalArgs[0].(time.Time).Equal(cutoff) {
		t.Fatalf("unexpected minimal args: %#v", minimalArgs)
	}

	noneQuery, noneArgs := buildChatImportQuery(chatQueryOptions{
		HasCreatedAt: true,
		HasUpdatedAt: true,
		FilterMode:   chatFilterNone,
	}, &cutoff)
	if strings.Contains(noneQuery, "WHERE ") || len(noneArgs) != 0 {
		t.Fatalf("expected no SQL filter for text/mixed mode: query=%s args=%#v", noneQuery, noneArgs)
	}
}
