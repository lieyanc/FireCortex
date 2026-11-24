package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGenerateDevHistory(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		output, err := git(dir, args...)
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return output
	}
	run("init")
	run("config", "user.name", "Release Test")
	run("config", "user.email", "test@example.com")
	run("config", "commit.gpgsign", "false")
	run("config", "tag.gpgsign", "false")
	run("commit", "--allow-empty", "-m", "chore: previous release")
	previous := run("rev-parse", "HEAD")
	run("tag", "dev-latest")
	run("commit", "--allow-empty", "-m", "feat(ota): show [notes] <script> &copy;")
	run("commit", "--allow-empty", "-m", "fix: preserve `commit` links")
	run("commit", "--allow-empty", "-m", "style: unify blue flame palette")
	head := run("rev-parse", "HEAD")

	generateNotes := func(base string) string {
		t.Helper()
		notes, err := generate(dir, "owner/repo", "dev-0009-20261124-"+head[:7], "HEAD", base, "2026-11-24T07:19:33Z")
		if err != nil {
			t.Fatal(err)
		}
		return notes
	}

	notes := generateNotes("refs/tags/dev-latest")
	for _, want := range []string{
		"## Development build",
		"`dev-0009-20261124-" + head[:7] + "`",
		"Changes since the previous dev build.",
		"### ✨ Features",
		"### 🐛 Fixes",
		"### ⚡ Improvements",
		"**ota:** show \\[notes\\] \\<script\\> &amp;copy;",
		"preserve \\`commit\\` links",
		"unify blue flame palette",
		"/commit/" + head,
		"/compare/" + previous + "..." + head,
		"Built 2026-11-24 07:19 UTC",
	} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes missing %q:\n%s", want, notes)
		}
	}
	if strings.Contains(notes, "previous release") {
		t.Fatal("included a commit from the previous release")
	}

	if first := generateNotes("refs/tags/missing"); !strings.Contains(first, "Recent commits") || !strings.Contains(first, "previous release") {
		t.Fatalf("first release must fall back to recent history: %s", first)
	}

	if rebuild := generateNotes("HEAD"); !strings.Contains(rebuild, "Rebuild") || strings.Contains(rebuild, "### ✨ Features") || !strings.Contains(rebuild, "unify blue flame palette") {
		t.Fatalf("a rebuild must show only its current commit: %s", rebuild)
	}

	// Rewritten dev history must not produce an invalid comparison range.
	run("checkout", "--orphan", "rewritten")
	run("commit", "--allow-empty", "-m", "feat!: new history")
	if rewritten := generateNotes("refs/tags/dev-latest"); !strings.Contains(rewritten, "Recent commits") || strings.Contains(rewritten, "/compare/") || !strings.Contains(rewritten, "**Breaking change ·** new history") {
		t.Fatalf("non-ancestor baseline must fall back to history: %s", rewritten)
	}

	for range maxCommits {
		run("commit", "--allow-empty", "-m", "fix: "+strings.Repeat("&", 300))
	}
	bounded := generateNotes("refs/tags/dev-latest")
	if !strings.Contains(bounded, "Showing the latest 30 commits") || strings.Contains(bounded, "new history") {
		t.Fatal("long history was not capped")
	}
	payload, err := json.Marshal(map[string]string{"release_notes": bounded})
	if err != nil || len(payload) > 60<<10 {
		t.Fatalf("notes leave insufficient room for metadata in the 64 KiB manifest: %d bytes, %v", len(payload), err)
	}
}
