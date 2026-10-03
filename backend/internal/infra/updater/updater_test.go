package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setTestVersion(t *testing.T, version, commit string) {
	t.Helper()
	originalVersion, originalCommit := currentVersion, currentCommit
	currentVersion = func() string { return version }
	currentCommit = func() string { return commit }
	t.Cleanup(func() {
		currentVersion, currentCommit = originalVersion, originalCommit
	})
}

func setTestGitHubBaseURL(t *testing.T, url string) {
	t.Helper()
	original := githubBaseURL
	githubBaseURL = url
	t.Cleanup(func() { githubBaseURL = original })
}

func testUpdater(cfg Config, hooks RestartHooks) *Updater {
	return New(func() Config { return cfg }, nil, nil, hooks)
}

// sha256SumsBody renders entries in sha256sum's native "<hash>  <name>" format.
func sha256SumsBody(entries map[string]string) string {
	var b strings.Builder
	for name, sum := range entries {
		fmt.Fprintf(&b, "%s  %s\n", sum, name)
	}
	return b.String()
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestCheckOnlyAcrossSourcesAndChannels(t *testing.T) {
	cases := []struct {
		name         string
		cfg          Config
		localVersion string
		localCommit  string
		remote       releaseVersionInfo
		direct       bool
		wantUpdate   bool
		wantLatest   string
	}{
		{
			name:         "proxy stable selects newer release",
			cfg:          Config{Channel: ChannelStable, Source: SourceProxy, Repo: "owner/repo"},
			localVersion: "0.4.3",
			remote:       releaseVersionInfo{Version: "0.5.0", Tag: "v0.5.0"},
			wantUpdate:   true,
			wantLatest:   "0.5.0",
		},
		{
			name:         "proxy stable skips older release",
			cfg:          Config{Channel: ChannelStable, Source: SourceProxy, Repo: "owner/repo"},
			localVersion: "0.5.0",
			remote:       releaseVersionInfo{Version: "0.4.3", Tag: "v0.4.3"},
			wantUpdate:   false,
			wantLatest:   "0.4.3",
		},
		{
			name:         "proxy dev selects different commit",
			cfg:          Config{Channel: ChannelDev, Source: SourceProxy, Repo: "owner/repo"},
			localVersion: "dev-0007-20260401-aaaaaaa",
			localCommit:  "aaaaaaa",
			remote:       releaseVersionInfo{Version: "dev-0042-20260425-bbbbbbb", Commit: "bbbbbbb", Tag: DevReleaseTag},
			wantUpdate:   true,
			wantLatest:   "dev-0042-20260425-bbbbbbb",
		},
		{
			name:         "proxy dev skips same commit",
			cfg:          Config{Channel: ChannelDev, Source: SourceProxy, Repo: "owner/repo"},
			localVersion: "dev-0042-20260425-bbbbbbb",
			localCommit:  "bbbbbbb",
			remote:       releaseVersionInfo{Version: "dev-0042-20260425-bbbbbbb", Commit: "bbbbbbb", Tag: DevReleaseTag},
			wantUpdate:   false,
			wantLatest:   "dev-0042-20260425-bbbbbbb",
		},
		{
			name:         "github direct dev reads rolling tag metadata",
			cfg:          Config{Channel: ChannelDev, Repo: "owner/repo"},
			localVersion: "dev-0007-20260401-aaaaaaa",
			localCommit:  "aaaaaaa",
			remote:       releaseVersionInfo{Version: "dev-0042-20260425-bbbbbbb", Commit: "bbbbbbb", Tag: DevReleaseTag},
			direct:       true,
			wantUpdate:   true,
			wantLatest:   "dev-0042-20260425-bbbbbbb",
		},
		{
			name:         "github direct stable reads latest redirect metadata",
			cfg:          Config{Channel: ChannelStable, Repo: "owner/repo"},
			localVersion: "0.4.3",
			remote:       releaseVersionInfo{Version: "0.5.0", Commit: "bbbbbbb", Tag: "v0.5.0"},
			direct:       true,
			wantUpdate:   true,
			wantLatest:   "0.5.0",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setTestVersion(t, tc.localVersion, tc.localCommit)
			cfg := tc.cfg
			metadata, err := json.Marshal(tc.remote)
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/owner/repo/releases/latest/download/version.json",
					"/owner/repo/releases/download/" + DevReleaseTag + "/version.json":
					_, _ = w.Write(metadata)
				case "/api/releases/owner/repo/latest", "/api/releases/owner/repo/" + DevReleaseTag:
					_ = json.NewEncoder(w).Encode(releaseInfo{
						TagName:    tc.remote.Tag,
						Prerelease: tc.cfg.Channel == ChannelDev,
						Assets: []assetInfo{{
							Name:               "version.json",
							BrowserDownloadURL: "https://github.com/owner/repo/releases/download/" + tc.remote.Tag + "/version.json",
						}},
					})
				case "/download/owner/repo/" + tc.remote.Tag + "/version.json":
					_, _ = w.Write(metadata)
				default:
					t.Errorf("unexpected path: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			if tc.direct {
				setTestGitHubBaseURL(t, server.URL)
			} else {
				cfg.ProxyBaseURL = server.URL
			}

			u := testUpdater(cfg, RestartHooks{})
			if err := u.CheckOnly(context.Background()); err != nil {
				t.Fatalf("CheckOnly returned error: %v", err)
			}
			status := u.Status()
			if status.HasUpdate != tc.wantUpdate || status.LatestVersion != tc.wantLatest {
				t.Fatalf("status = %+v, want update=%v latest=%q", status, tc.wantUpdate, tc.wantLatest)
			}
			if status.State != StateIdle || status.LastCheck == "" {
				t.Fatalf("status = %+v, want idle with last check recorded", status)
			}
		})
	}
}

func TestCheckOnlyNoReleaseAndFailure(t *testing.T) {
	setTestVersion(t, "0.4.3", "")
	notFound := httptest.NewServer(http.NotFoundHandler())
	defer notFound.Close()
	setTestGitHubBaseURL(t, notFound.URL)

	u := testUpdater(Config{Repo: "owner/repo"}, RestartHooks{})
	if err := u.CheckOnly(context.Background()); err != nil {
		t.Fatalf("missing release must not be an error: %v", err)
	}
	if status := u.Status(); status.HasUpdate || status.LatestVersion != "" {
		t.Fatalf("status = %+v, want no update", status)
	}

	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer broken.Close()
	setTestGitHubBaseURL(t, broken.URL)
	if err := u.CheckOnly(context.Background()); err == nil {
		t.Fatal("CheckOnly succeeded against a failing server")
	}
	if status := u.Status(); status.State != StateFailed || !strings.Contains(status.Error, "502") {
		t.Fatalf("status = %+v, want failed with status code", status)
	}
}

// releaseServer serves a direct-mode dev release with the given assets.
func releaseServer(t *testing.T, binary []byte, sums string, serveSums bool) *httptest.Server {
	t.Helper()
	metadata, err := json.Marshal(releaseVersionInfo{Version: "dev-0042-20260425-bbbbbbb", Commit: "bbbbbbb", Tag: DevReleaseTag})
	if err != nil {
		t.Fatal(err)
	}
	prefix := "/owner/repo/releases/download/" + DevReleaseTag + "/"
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case prefix + "version.json":
			_, _ = w.Write(metadata)
		case prefix + targetName():
			_, _ = w.Write(binary)
		case prefix + "SHA256SUMS":
			if !serveSums {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(sums))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
}

func TestPerformUpdateDownloadsAndVerifiesDevRelease(t *testing.T) {
	setTestVersion(t, "dev-0007-20260401-aaaaaaa", "aaaaaaa")
	binary := []byte("new firecortex binary")
	// Several platforms so the test covers line selection, not a one-line file.
	sums := sha256SumsBody(map[string]string{
		"firecortex-other-platform": sha256Hex([]byte("other")),
		targetName():                sha256Hex(binary),
		targetName() + "-extra":     sha256Hex([]byte("extra")),
	})
	server := releaseServer(t, binary, sums, true)
	defer server.Close()
	setTestGitHubBaseURL(t, server.URL)

	dir := t.TempDir()
	u := testUpdater(Config{Channel: ChannelDev, Repo: "owner/repo", DownloadDir: dir}, RestartHooks{})
	u.performUpdate(context.Background())

	status := u.Status()
	if status.State != StateReady || status.Progress != progressVerifyDone || !status.HasUpdate {
		t.Fatalf("status = %+v, want ready", status)
	}
	if u.pendingTag != DevReleaseTag {
		t.Fatalf("pendingTag = %q", u.pendingTag)
	}
	got, err := os.ReadFile(u.pendingBinaryPath)
	if err != nil || string(got) != string(binary) {
		t.Fatalf("pending binary = %q, %v", got, err)
	}
}

func TestPerformUpdateRefusesUnverifiedBinaries(t *testing.T) {
	binary := []byte("new firecortex binary")
	cases := []struct {
		name      string
		sums      string
		serveSums bool
		wantError string
	}{
		{
			name:      "missing SHA256SUMS",
			serveSums: false,
			wantError: "SHA256SUMS returned status 404",
		},
		{
			name:      "no entry for this platform",
			sums:      sha256SumsBody(map[string]string{"firecortex-other-platform": sha256Hex(binary)}),
			serveSums: true,
			wantError: "no sha256 entry",
		},
		{
			name:      "hash mismatch",
			sums:      sha256SumsBody(map[string]string{targetName(): sha256Hex([]byte("tampered"))}),
			serveSums: true,
			wantError: "sha256 mismatch",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setTestVersion(t, "dev-0007-20260401-aaaaaaa", "aaaaaaa")
			server := releaseServer(t, binary, tc.sums, tc.serveSums)
			defer server.Close()
			setTestGitHubBaseURL(t, server.URL)

			dir := t.TempDir()
			u := testUpdater(Config{Channel: ChannelDev, Repo: "owner/repo", DownloadDir: dir}, RestartHooks{})
			u.performUpdate(context.Background())

			status := u.Status()
			if status.State != StateFailed || !strings.Contains(status.Error, tc.wantError) {
				t.Fatalf("status = %+v, want failure containing %q", status, tc.wantError)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("download dir not cleaned up: %v", entries)
			}
		})
	}
}

func TestPerformUpdateRefusesReleaseWithoutChecksumAsset(t *testing.T) {
	setTestVersion(t, "0.4.3", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/releases/owner/repo/latest":
			_ = json.NewEncoder(w).Encode(releaseInfo{
				TagName: "v0.5.0",
				Assets: []assetInfo{{
					Name:               targetName(),
					BrowserDownloadURL: "https://github.com/owner/repo/releases/download/v0.5.0/" + targetName(),
				}},
			})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	u := testUpdater(Config{Source: SourceProxy, ProxyBaseURL: server.URL, Repo: "owner/repo", DownloadDir: t.TempDir()}, RestartHooks{})
	u.performUpdate(context.Background())
	if status := u.Status(); status.State != StateFailed || !strings.Contains(status.Error, "refusing unverified update") {
		t.Fatalf("status = %+v, want refusal", status)
	}
}

func TestSHA256ForTargetMatchesExactName(t *testing.T) {
	sums := []byte("" +
		"1111  firecortex-linux-arm\n" +
		"\n" +
		"2222 *firecortex-linux-arm64\n" +
		"3333  firecortex-linux-arm64.sig\n")
	cases := map[string]string{
		"firecortex-linux-arm":   "1111",
		"firecortex-linux-arm64": "2222",
	}
	for target, want := range cases {
		got, err := sha256ForTarget(sums, target)
		if err != nil || got != want {
			t.Fatalf("sha256ForTarget(%s) = %q, %v; want %q", target, got, err, want)
		}
	}
	if _, err := sha256ForTarget(sums, "firecortex-linux"); err == nil {
		t.Fatal("prefix of an entry must not match")
	}
}

func TestResolveDownloadURLRewritesOnlyForProxy(t *testing.T) {
	browserURL := "https://github.com/owner/repo/releases/download/v1.0.0/firecortex-linux-amd64"
	if got := resolveDownloadURL(Config{Source: SourceGitHub}, browserURL); got != browserURL {
		t.Fatalf("direct URL rewritten: %s", got)
	}
	got := resolveDownloadURL(Config{Source: SourceProxy, ProxyBaseURL: "https://mirror.example"}, browserURL)
	if want := "https://mirror.example/download/owner/repo/v1.0.0/firecortex-linux-amd64"; got != want {
		t.Fatalf("proxy URL = %s, want %s", got, want)
	}
}

func TestIsNewer(t *testing.T) {
	cases := []struct {
		name         string
		localVersion string
		localCommit  string
		channel      string
		release      releaseInfo
		want         bool
	}{
		{"stable newer minor", "0.4.3", "", ChannelStable, releaseInfo{TagName: "v0.5.0"}, true},
		{"stable same", "0.4.3", "", ChannelStable, releaseInfo{TagName: "v0.4.3"}, false},
		{"stable older", "0.5.0", "", ChannelStable, releaseInfo{TagName: "v0.4.9"}, false},
		{"stable ignores prerelease suffix", "0.4.3", "", ChannelStable, releaseInfo{TagName: "v0.4.3-rc.1"}, false},
		{"unversioned local build", "dev", "", ChannelStable, releaseInfo{TagName: "v0.0.1"}, true},
		{"dev commit differs", "dev-0001-20260101-aaaaaaa", "aaaaaaa", ChannelDev, releaseInfo{Commit: "bbbbbbbcafe"}, true},
		{"dev same commit", "dev-0001-20260101-aaaaaaa", "aaaaaaa", ChannelDev, releaseInfo{Commit: "aaaaaaa1234"}, false},
		{"dev run fallback", "dev-0001-20260101-aaaaaaa", "unknown", ChannelDev, releaseInfo{Version: "dev-0002-20260102-bbbbbbb"}, true},
		{"dev incomparable", "0.4.3", "", ChannelDev, releaseInfo{TagName: DevReleaseTag}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setTestVersion(t, tc.localVersion, tc.localCommit)
			if got := testUpdater(Config{}, RestartHooks{}).isNewer(tc.release, tc.channel); got != tc.want {
				t.Fatalf("isNewer = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestApplyPendingConsumesPendingUpdateOnce(t *testing.T) {
	dir := t.TempDir()
	pending := filepath.Join(dir, "firecortex-dev-latest")
	if err := os.WriteFile(pending, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	beforeExec := make(chan struct{}, 1)
	u := testUpdater(Config{}, RestartHooks{
		// A canceled context makes waitForIdle abort before anything is exec'd.
		IsBusy: func() bool { return true },
		BeforeExec: func(string) error {
			beforeExec <- struct{}{}
			return errors.New("must not be reached")
		},
	})
	u.StartBackground(ctx)
	u.status.State = StateReady
	u.pendingBinaryPath = pending
	u.pendingTag = DevReleaseTag

	if err := u.ApplyPending(); err != nil {
		t.Fatalf("ApplyPending: %v", err)
	}
	if status := u.Status(); status.State != StateApplying {
		t.Fatalf("state = %s, want applying immediately", status.State)
	}
	if err := u.ApplyPending(); !errors.Is(err, ErrNoPendingUpdate) {
		t.Fatalf("second ApplyPending = %v, want ErrNoPendingUpdate", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for u.Status().State == StateApplying && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if status := u.Status(); status.State != StateFailed || !strings.Contains(status.Error, "waiting for idle") {
		t.Fatalf("status = %+v, want aborted while waiting for idle", status)
	}
	select {
	case <-beforeExec:
		t.Fatal("BeforeExec ran although the application context was canceled")
	default:
	}
}

func TestDismissPendingRemovesBinary(t *testing.T) {
	pending := filepath.Join(t.TempDir(), "firecortex-dev-latest")
	if err := os.WriteFile(pending, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	u := testUpdater(Config{}, RestartHooks{})
	u.status = Status{State: StateReady, HasUpdate: true}
	u.pendingBinaryPath = pending

	u.DismissPending()
	if status := u.Status(); status.State != StateIdle || status.HasUpdate {
		t.Fatalf("status = %+v, want idle", status)
	}
	if _, err := os.Stat(pending); !os.IsNotExist(err) {
		t.Fatalf("pending binary still present: %v", err)
	}
}

func TestNormalizeConfigDefaults(t *testing.T) {
	cfg := normalizeConfig(Config{Channel: " DEV ", CheckInterval: 5, Source: "Proxy", ProxyBaseURL: "https://mirror.example/", Repo: "/owner/repo/"})
	if cfg.Channel != ChannelDev || cfg.CheckInterval != 60 || cfg.Source != SourceProxy ||
		cfg.ProxyBaseURL != "https://mirror.example" || cfg.Repo != "owner/repo" || cfg.DownloadDir == "" {
		t.Fatalf("normalizeConfig = %+v", cfg)
	}
	cfg = normalizeConfig(Config{Channel: "nightly"})
	if cfg.Channel != ChannelStable || cfg.Source != SourceGitHub || cfg.Repo != DefaultRepo || cfg.CheckInterval != DefaultCheckInterval {
		t.Fatalf("normalizeConfig defaults = %+v", cfg)
	}
}

func TestStartUpdateSwitchesStateBeforeReturning(t *testing.T) {
	setTestVersion(t, "0.4.3", "")
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		http.NotFound(w, r)
	}))
	defer server.Close()
	setTestGitHubBaseURL(t, server.URL)

	u := testUpdater(Config{Repo: "owner/repo"}, RestartHooks{})
	u.StartUpdate()
	state := u.Status().State
	// A second trigger while busy must not start another run.
	claimed := u.beginUpdate()
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for u.Status().State == StateChecking && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if state != StateChecking {
		t.Fatalf("state = %s right after StartUpdate, want checking", state)
	}
	if claimed {
		t.Fatal("beginUpdate succeeded while a run was in progress")
	}
	if final := u.Status().State; final != StateIdle {
		t.Fatalf("final state = %s, want idle", final)
	}
}
