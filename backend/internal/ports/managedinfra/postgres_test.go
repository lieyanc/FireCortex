package managedinfra

import (
	"strings"
	"testing"
)

func TestParsePostgresMajorVersion(t *testing.T) {
	tests := []struct {
		image string
		major int
		ok    bool
	}{
		{"pgvector/pgvector:pg18-trixie", 18, true},
		{"pgvector/pgvector:pg18", 18, true},
		{"pgvector/pgvector:0.8.7-pg18-trixie", 18, true},
		{"pgvector/pgvector:0.8.7-pg18-bookworm", 18, true},
		{"pgvector/pgvector:pg19-trixie", 19, true},
		{"pgvector/pgvector:pg17-bookworm", 17, true},
		{"pgvector/pgvector:pg16", 16, true},
		{"pgvector/pgvector:0.8.0-pg16", 16, true},
		{"docker.m.daocloud.io/pgvector/pgvector:pg18-trixie@sha256:abcdef1234", 18, true},
		{"localhost:5000/postgres:18.2-trixie", 18, true},
		{"postgres:18", 18, true},
		{"postgres:17-bookworm", 17, true},
		{"postgres:16", 16, true},
		{"localhost:5000/pgvector/pgvector", 0, false},
		{"pgvector/pgvector", 0, false},
		{"pgvector/pgvector@sha256:abcdef", 0, false},
		{"registry.example.com/custom-pg:latest", 0, false},
		{"registry.example.com/custom-pg:0.8.7", 0, false},
		{"registry.example.com/custom-pg:20260101", 0, false},
		{"registry.example.com/custom-pg:09", 0, false},
	}

	for _, tc := range tests {
		got, ok := ParsePostgresMajorVersion(tc.image)
		if got != tc.major || ok != tc.ok {
			t.Errorf("ParsePostgresMajorVersion(%q) = (%d, %v), want (%d, %v)", tc.image, got, ok, tc.major, tc.ok)
		}
	}
}

func TestResolvePostgresContainerLayout(t *testing.T) {
	tests := []struct {
		name      string
		image     string
		wantMount string
		wantPGDat string
		wantInit  string
	}{
		{
			name:      "empty defaults to pg18",
			image:     "",
			wantMount: PostgresModernMountTarget,
			wantPGDat: "",
			wantInit:  PostgresBuiltinInitDBArgs,
		},
		{
			name:      "pg18-trixie uses versioned mount without PGDATA override",
			image:     "pgvector/pgvector:pg18-trixie",
			wantMount: PostgresModernMountTarget,
			wantPGDat: "",
			wantInit:  PostgresBuiltinInitDBArgs,
		},
		{
			name:      "pg19-trixie uses modern layout",
			image:     "pgvector/pgvector:pg19-trixie",
			wantMount: PostgresModernMountTarget,
			wantPGDat: "",
			wantInit:  PostgresBuiltinInitDBArgs,
		},
		{
			name:      "pg17 keeps legacy mount with builtin locale",
			image:     "pgvector/pgvector:pg17-bookworm",
			wantMount: PostgresLegacyMountTarget,
			wantPGDat: PostgresLegacyMountTarget,
			wantInit:  PostgresBuiltinInitDBArgs,
		},
		{
			name:      "pg16 keeps legacy mount without builtin locale",
			image:     "pgvector/pgvector:pg16",
			wantMount: PostgresLegacyMountTarget,
			wantPGDat: PostgresLegacyMountTarget,
			wantInit:  "",
		},
		{
			name:      "custom untagged major uses modern mount without forcing initdb args",
			image:     "my-registry.local/custom-pg:latest",
			wantMount: PostgresModernMountTarget,
			wantPGDat: "",
			wantInit:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolvePostgresContainerLayout(tc.image)
			if got.MountTarget != tc.wantMount || got.PGData != tc.wantPGDat || got.InitDBArgs != tc.wantInit {
				t.Fatalf("ResolvePostgresContainerLayout(%q) = %+v, want mount=%q pgdata=%q init=%q",
					tc.image, got, tc.wantMount, tc.wantPGDat, tc.wantInit)
			}
		})
	}
}

func TestValidatePostgresImageForDataVersion(t *testing.T) {
	if err := ValidatePostgresImageForDataVersion("pgvector/pgvector:pg18-trixie", "18"); err != nil {
		t.Fatalf("expected matching pg18 to pass, got %v", err)
	}
	if err := ValidatePostgresImageForDataVersion("pgvector/pgvector:pg16", "16"); err != nil {
		t.Fatalf("expected matching pg16 to pass, got %v", err)
	}
	if err := ValidatePostgresImageForDataVersion("pgvector/pgvector:pg18-trixie", ""); err != nil {
		t.Fatalf("expected unknown data version to pass, got %v", err)
	}
	if err := ValidatePostgresImageForDataVersion("custom/pg:latest", "16"); err != nil {
		t.Fatalf("expected unknown custom tag to pass, got %v", err)
	}

	err := ValidatePostgresImageForDataVersion("pgvector/pgvector:pg18-trixie", "16")
	if err == nil {
		t.Fatal("expected pg18 image against pg16 data directory to fail")
	}
	if !strings.Contains(err.Error(), "major version 16") || !strings.Contains(err.Error(), "PostgreSQL 18") {
		t.Fatalf("unexpected mismatch error: %v", err)
	}

	if err := ValidatePostgresImageForDataVersion("", "16"); err == nil {
		t.Fatal("expected default pg18 image against pg16 data directory to fail")
	}
}
