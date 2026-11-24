// release-notes generates the shared Markdown used by the rolling dev-latest
// release and its OTA manifest. Run before moving the dev-latest tag to the
// new commit.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const (
	maxCommits    = 30
	maxSubjectLen = 160
)

var (
	conventionalCommit = regexp.MustCompile(`^([a-z]+)(\([^)]+\))?(!)?:\s*(.+)$`)
	markdownSpecial    = regexp.MustCompile("[\\\\`*_{}\\[\\]()<>#+.!|~-]")
	repoPattern        = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
)

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	return strings.TrimSpace(string(output)), err
}

func escape(text string) string {
	text = strings.ReplaceAll(text, "&", "&amp;")
	return markdownSpecial.ReplaceAllStringFunc(text, func(s string) string { return "\\" + s })
}

func generate(dir, repo, version, commit, base, buildTime string) (string, error) {
	head, err := git(dir, "rev-parse", "--verify", commit+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("resolve build commit: %w", err)
	}
	previous, _ := git(dir, "rev-parse", "--verify", base+"^{commit}")
	revision, limit := head, maxCommits+1
	intro := "Recent commits included in this build."
	baseURL := "https://github.com/" + repo
	changesURL := baseURL + "/commits/" + head
	if previous == head {
		limit = 1
		intro = "Rebuild of the current dev revision."
	} else if previous != "" {
		if _, err := git(dir, "merge-base", "--is-ancestor", previous, head); err == nil {
			revision = previous + ".." + head
			intro = "Changes since the previous dev build."
			changesURL = baseURL + "/compare/" + previous + "..." + head
		}
	}
	log, err := git(dir, "log", "--format=%H%x00%s", fmt.Sprintf("-n%d", limit), revision, "--")
	if err != nil {
		return "", fmt.Errorf("read commit history: %w", err)
	}
	var lines []string
	if log != "" {
		lines = strings.Split(log, "\n")
	}
	truncated := len(lines) > maxCommits
	if truncated {
		lines = lines[:maxCommits]
	}
	groups := make([][]string, 4)
	for _, line := range lines {
		sha, subject, ok := strings.Cut(line, "\x00")
		if !ok || len(sha) < 7 {
			continue
		}
		// Keep even unusually long subjects within the OTA manifest's 64 KiB limit.
		if runes := []rune(subject); len(runes) > maxSubjectLen {
			subject = string(runes[:maxSubjectLen]) + "…"
		}
		group, label := 3, ""
		if match := conventionalCommit.FindStringSubmatch(subject); match != nil {
			switch match[1] {
			case "feat":
				group = 0
			case "fix", "revert":
				group = 1
			case "perf", "refactor", "style":
				group = 2
			}
			if match[2] != "" {
				label = "**" + escape(strings.Trim(match[2], "()")) + ":** "
			}
			if match[3] != "" {
				label = "**Breaking change ·** " + label
			}
			subject = match[4]
		}
		entry := fmt.Sprintf("- %s%s · [`%s`](%s/commit/%s)", label, escape(subject), sha[:7], baseURL, sha)
		groups[group] = append(groups[group], entry)
	}
	var notes strings.Builder
	fmt.Fprintf(&notes, "## Development build\n\n`%s`\n\n%s\n\n", version, intro)
	titles := []string{"✨ Features", "🐛 Fixes", "⚡ Improvements", "🔧 Maintenance & other changes"}
	for i, entries := range groups {
		if len(entries) != 0 {
			fmt.Fprintf(&notes, "### %s\n\n%s\n\n", titles[i], strings.Join(entries, "\n"))
		}
	}
	if truncated {
		fmt.Fprintf(&notes, "Showing the latest %d commits. See the full changelog for all changes.\n\n", maxCommits)
	}
	fmt.Fprintf(&notes, "[Full changelog](%s) · [Build commit `%s`](%s/commit/%s)\n", changesURL, head[:7], baseURL, head)
	if stamp, err := time.Parse(time.RFC3339, buildTime); err == nil {
		fmt.Fprintf(&notes, "\nBuilt %s\n", stamp.UTC().Format("2006-01-02 15:04 UTC"))
	}
	return notes.String(), nil
}

func main() {
	dir := flag.String("dir", ".", "git repository directory")
	repo := flag.String("repo", "", "GitHub owner/repository")
	version := flag.String("version", "dev", "build version")
	commit := flag.String("commit", "HEAD", "build commit")
	base := flag.String("base", "refs/tags/dev-latest", "previous dev revision")
	buildTime := flag.String("build-time", "", "RFC3339 build time")
	flag.Parse()
	if !repoPattern.MatchString(*repo) {
		fmt.Fprintln(os.Stderr, "-repo must be a GitHub owner/repository")
		os.Exit(1)
	}
	notes, err := generate(*dir, *repo, *version, *commit, *base, *buildTime)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Print(notes)
}
