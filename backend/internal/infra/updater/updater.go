// Package updater implements in-place self-update from GitHub Releases.
//
// Release protocol (shared with .github/workflows/release.yml; change both together):
//   - assets are named firecortex-{goos}-{goarch};
//   - every release carries a single SHA256SUMS file in sha256sum's native
//     "<hash>  <bare file name>" format plus a version.json metadata file;
//   - stable releases are vX.Y.Z tags, the dev channel follows the rolling
//     DevReleaseTag prerelease whose versions look like dev-{run}-{yyyymmdd}-{sha}.
package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lieyanc/FireCortex/backend/internal/shared/buildinfo"
	"github.com/lieyanc/FireCortex/backend/internal/shared/security"
	"go.uber.org/zap"
)

const (
	ChannelStable = "stable"
	ChannelDev    = "dev"

	// SourceGitHub downloads release assets straight from
	// github.com/<repo>/releases/download/... links, which are CDN
	// redirects and not subject to GitHub REST API rate limits.
	SourceGitHub = "github"
	// SourceProxy routes release lookups and downloads through the
	// configured ProxyBaseURL mirror.
	SourceProxy = "proxy"

	// DevReleaseTag is the rolling prerelease tag published on every push to
	// the dev branch. It cannot be "dev" because that is also the branch name.
	DevReleaseTag = "dev-latest"

	DefaultRepo          = "lieyanc/FireCortex"
	DefaultProxyBaseURL  = "https://dl.repo.chycloud.top"
	DefaultCheckInterval = 3600

	StateIdle        = "idle"
	StateChecking    = "checking"
	StateDownloading = "downloading"
	StateReady       = "ready"
	StateApplying    = "applying"
	StateFailed      = "failed"

	assetPrefix   = "firecortex-"
	checksumsName = "SHA256SUMS"
	versionName   = "version.json"

	initialCheckDelay = 30 * time.Second
	checkTimeout      = 30 * time.Second
	idleWaitTimeout   = 10 * time.Minute
	idlePollInterval  = 5 * time.Second
	downloadTimeout   = 30 * time.Minute

	progressChecking      = 5
	progressReleaseFound  = 10
	progressDownloadStart = 10
	progressDownloadDone  = 90
	progressVerifyStart   = 92
	progressVerifyDone    = 95
	progressApplying      = 98
	progressComplete      = 100
)

// ErrNoPendingUpdate is returned by ApplyPending when no verified binary awaits confirmation.
var ErrNoPendingUpdate = errors.New("no pending update to apply")

// Package vars so tests can point checks at a local server and fake the running build.
var (
	githubBaseURL  = "https://github.com"
	currentVersion = buildinfo.ResolveVersion
	currentCommit  = func() string { return buildinfo.Commit }
)

// Config is the normalized update configuration; see normalizeConfig for defaults.
type Config struct {
	// Enabled turns on periodic background checks. Manual checks work regardless.
	Enabled bool
	// Channel is ChannelStable or ChannelDev.
	Channel string
	// CheckInterval is the background check period in seconds (minimum one minute).
	CheckInterval int
	// Source is SourceGitHub or SourceProxy.
	Source       string
	ProxyBaseURL string
	// Repo is the GitHub owner/name publishing releases.
	Repo string
	// DownloadDir holds downloaded binaries until they are installed.
	DownloadDir string
}

// Status is a snapshot of the updater state machine.
type Status struct {
	State            string
	CurrentVersion   string
	CurrentCommit    string
	LatestVersion    string
	HasUpdate        bool
	IsPrerelease     bool
	Progress         float64
	DownloadProgress float64
	Error            string
	LastCheck        string
	ReleaseNotes     string
	Enabled          bool
	Channel          string
	Source           string
	Repo             string
}

// RestartHooks lets the host application take part in the restart.
type RestartHooks struct {
	// BeforeExec runs right before the executable is replaced; it must stop
	// serving and release resources. Returning an error aborts the update.
	BeforeExec func(tag string) error
	// OnExecFailure runs when installing or re-executing fails after BeforeExec
	// may already have stopped the application.
	OnExecFailure func(error)
	// IsBusy reports in-flight work that should not be interrupted. The updater
	// waits for it to return false (bounded by idleWaitTimeout) before applying.
	IsBusy func() bool
}

// Updater checks, downloads, verifies and installs new releases.
type Updater struct {
	cfg    func() Config
	client *http.Client
	logger *zap.Logger
	hooks  RestartHooks

	mu     sync.RWMutex
	status Status
	bgCtx  context.Context

	pendingBinaryPath string
	pendingTag        string
}

// New creates an updater. cfg is called on every use so configuration changes
// apply without a restart. client carries the outbound policy; nil uses an
// unrestricted one. Requests are bounded by context deadlines, not client timeouts.
func New(cfg func() Config, client *http.Client, logger *zap.Logger, hooks RestartHooks) *Updater {
	if client == nil {
		client = security.NewOutboundHTTPClient(security.NewStrictOutboundPolicy(false), 0)
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Updater{
		cfg:    cfg,
		client: client,
		logger: logger,
		hooks:  hooks,
		status: Status{State: StateIdle},
	}
}

// Status returns the current state together with the build and configuration it applies to.
func (u *Updater) Status() Status {
	cfg := normalizeConfig(u.cfg())
	u.mu.RLock()
	s := u.status
	u.mu.RUnlock()
	s.CurrentVersion = currentVersion()
	s.CurrentCommit = currentCommit()
	s.Enabled = cfg.Enabled
	s.Channel = cfg.Channel
	s.Source = cfg.Source
	s.Repo = cfg.Repo
	return s
}

// CheckOnly looks for a newer release without downloading it.
func (u *Updater) CheckOnly(ctx context.Context) error {
	cfg := normalizeConfig(u.cfg())
	release, hasUpdate, err := u.checkForUpdate(ctx, cfg)

	u.mu.Lock()
	defer u.mu.Unlock()
	u.status.LastCheck = time.Now().UTC().Format(time.RFC3339)
	idle := u.status.State == StateIdle || u.status.State == StateFailed
	if err != nil {
		if idle {
			u.status.State = StateFailed
			u.status.Error = "check failed: " + err.Error()
		}
		return err
	}
	if idle {
		u.status.State = StateIdle
		u.status.Error = ""
	}
	u.recordRelease(release, hasUpdate)
	return nil
}

// StartUpdate checks, downloads and (on the stable channel) applies an update
// in the background. The state switches to checking before it returns, so
// callers immediately observe the work; it is a no-op while another run is busy.
func (u *Updater) StartUpdate() {
	if u.beginUpdate() {
		go u.runUpdate(u.bgContext())
	}
}

// ApplyPending installs the verified binary waiting in the ready state and restarts.
func (u *Updater) ApplyPending() error {
	u.mu.Lock()
	path := u.pendingBinaryPath
	tag := u.pendingTag
	if u.status.State != StateReady || path == "" {
		u.mu.Unlock()
		return ErrNoPendingUpdate
	}
	// Switch state synchronously so repeated calls fail fast and pollers see applying immediately.
	u.status.State = StateApplying
	u.status.Progress = progressApplying
	u.status.DownloadProgress = 0
	u.pendingBinaryPath = ""
	u.pendingTag = ""
	u.mu.Unlock()

	go func() {
		// Let the HTTP response go out before the server starts draining.
		time.Sleep(200 * time.Millisecond)
		ctx := u.bgContext()
		if err := u.waitForIdle(ctx); err != nil {
			u.setError("apply canceled while waiting for idle: " + err.Error())
			return
		}
		if err := u.applyUpdate(path, tag); err != nil {
			u.setError("apply failed: " + err.Error())
		}
	}()
	return nil
}

// DismissPending discards the binary waiting in the ready state.
func (u *Updater) DismissPending() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.status.State != StateReady {
		return
	}
	if u.pendingBinaryPath != "" {
		_ = os.Remove(u.pendingBinaryPath)
	}
	u.pendingBinaryPath = ""
	u.pendingTag = ""
	u.status.State = StateIdle
	u.status.HasUpdate = false
	u.status.Progress = 0
	u.status.DownloadProgress = 0
	u.status.Error = ""
}

// StartBackground records the application context for update work and, when
// enabled, starts periodic checks. ctx must outlive HTTP requests.
func (u *Updater) StartBackground(ctx context.Context) {
	u.mu.Lock()
	u.bgCtx = ctx
	u.mu.Unlock()
	cfg := normalizeConfig(u.cfg())
	if !cfg.Enabled {
		u.logger.Info("self_update_background_disabled")
		return
	}
	u.logger.Info("self_update_background_enabled",
		zap.String("channel", cfg.Channel),
		zap.String("source", cfg.Source),
		zap.Int("interval_seconds", cfg.CheckInterval),
	)
	go u.loop(ctx)
}

func (u *Updater) bgContext() context.Context {
	u.mu.RLock()
	defer u.mu.RUnlock()
	if u.bgCtx != nil {
		return u.bgCtx
	}
	return context.Background()
}

func (u *Updater) loop(ctx context.Context) {
	select {
	case <-time.After(initialCheckDelay):
	case <-ctx.Done():
		return
	}
	u.checkAndUpdate(ctx)

	for {
		cfg := normalizeConfig(u.cfg())
		interval := time.Duration(cfg.CheckInterval) * time.Second
		select {
		case <-time.After(interval):
			u.checkAndUpdate(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (u *Updater) checkAndUpdate(ctx context.Context) {
	if !normalizeConfig(u.cfg()).Enabled {
		return
	}
	u.performUpdate(ctx)
}

func (u *Updater) performUpdate(ctx context.Context) {
	if u.beginUpdate() {
		u.runUpdate(ctx)
	}
}

// beginUpdate claims the state machine for one update run.
func (u *Updater) beginUpdate() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	switch u.status.State {
	case StateChecking, StateReady, StateDownloading, StateApplying:
		return false
	}
	u.status.State = StateChecking
	u.status.Progress = progressChecking
	u.status.Error = ""
	u.status.DownloadProgress = 0
	return true
}

func (u *Updater) runUpdate(ctx context.Context) {
	cfg := normalizeConfig(u.cfg())
	release, hasUpdate, err := u.checkForUpdate(ctx, cfg)
	if err != nil {
		u.setError("check failed: " + err.Error())
		return
	}

	u.mu.Lock()
	u.status.LastCheck = time.Now().UTC().Format(time.RFC3339)
	u.recordRelease(release, hasUpdate)
	if release == nil || !hasUpdate {
		u.status.State = StateIdle
		u.status.Progress = 0
		u.status.DownloadProgress = 0
		u.mu.Unlock()
		return
	}
	u.status.Progress = progressReleaseFound
	u.mu.Unlock()

	binaryPath, err := u.download(ctx, cfg, release)
	if err != nil {
		u.setError("download failed: " + err.Error())
		return
	}

	if cfg.Channel == ChannelStable {
		u.mu.Lock()
		u.status.State = StateApplying
		u.status.Progress = progressApplying
		u.status.DownloadProgress = 0
		u.mu.Unlock()
		if err := u.waitForIdle(ctx); err != nil {
			_ = os.Remove(binaryPath)
			u.setError("apply canceled while waiting for idle: " + err.Error())
			return
		}
		if err := u.applyUpdate(binaryPath, release.TagName); err != nil {
			u.setError("apply failed: " + err.Error())
		}
		return
	}

	u.mu.Lock()
	u.status.State = StateReady
	u.status.Progress = progressVerifyDone
	u.status.DownloadProgress = 0
	u.pendingBinaryPath = binaryPath
	u.pendingTag = release.TagName
	u.mu.Unlock()
	u.logger.Info("self_update_ready", zap.String("version", release.displayVersion()))
}

// recordRelease stores the outcome of a check; the caller holds u.mu.
func (u *Updater) recordRelease(release *releaseInfo, hasUpdate bool) {
	if release == nil {
		u.status.HasUpdate = false
		u.status.LatestVersion = ""
		u.status.IsPrerelease = false
		u.status.ReleaseNotes = ""
		return
	}
	u.status.HasUpdate = hasUpdate
	u.status.LatestVersion = release.displayVersion()
	u.status.IsPrerelease = release.Prerelease
	u.status.ReleaseNotes = release.Body
}

func (u *Updater) setError(msg string) {
	u.logger.Warn("self_update_failed", zap.String("error", msg))
	u.mu.Lock()
	defer u.mu.Unlock()
	u.status.State = StateFailed
	u.status.Error = msg
	u.status.DownloadProgress = 0
	u.status.LastCheck = time.Now().UTC().Format(time.RFC3339)
}

// failAfterStop reports a failure that happened after BeforeExec may have
// stopped the application, so the host can decide how to recover.
func (u *Updater) failAfterStop(err error) error {
	if u.hooks.OnExecFailure != nil {
		u.hooks.OnExecFailure(err)
	}
	return err
}

// waitForIdle blocks until the application reports no in-flight work. A
// canceled context aborts the update; only the deliberate idle timeout
// permits applying while work is still reported as active.
func (u *Updater) waitForIdle(ctx context.Context) error {
	if u.hooks.IsBusy == nil || !u.hooks.IsBusy() {
		return nil
	}
	u.logger.Info("self_update_waiting_for_idle", zap.Duration("max_wait", idleWaitTimeout))
	deadline := time.After(idleWaitTimeout)
	ticker := time.NewTicker(idlePollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			u.logger.Warn("self_update_idle_wait_timeout")
			return nil
		case <-ticker.C:
			if !u.hooks.IsBusy() {
				return nil
			}
		}
	}
}

func clampProgress(progress float64) float64 {
	return min(max(progress, 0), 100)
}

func overallDownloadProgress(downloadProgress float64) float64 {
	span := progressDownloadDone - progressDownloadStart
	return progressDownloadStart + clampProgress(downloadProgress)*float64(span)/100
}

type releaseInfo struct {
	TagName         string      `json:"tag_name"`
	TargetCommitish string      `json:"target_commitish"`
	Prerelease      bool        `json:"prerelease"`
	Body            string      `json:"body"`
	Assets          []assetInfo `json:"assets"`
	Version         string      `json:"-"`
	Commit          string      `json:"-"`
	BuildTime       string      `json:"-"`
}

type assetInfo struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type releaseVersionInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
	Tag       string `json:"tag"`
}

func (r releaseInfo) displayVersion() string {
	if version := strings.TrimSpace(r.Version); version != "" {
		return version
	}
	return r.TagName
}

func (r releaseInfo) asset(name string) *assetInfo {
	for i := range r.Assets {
		if r.Assets[i].Name == name {
			return &r.Assets[i]
		}
	}
	return nil
}

func (u *Updater) checkForUpdate(ctx context.Context, cfg Config) (*releaseInfo, bool, error) {
	if cfg.Repo == "" {
		return nil, false, errors.New("update repository is not configured")
	}
	checkCtx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	var (
		release *releaseInfo
		err     error
	)
	if cfg.Source == SourceProxy {
		release, err = u.fetchReleaseViaProxy(checkCtx, cfg)
	} else {
		release, err = u.fetchReleaseFromGitHub(checkCtx, cfg)
	}
	if err != nil || release == nil {
		return nil, false, err
	}
	if !u.isNewer(*release, cfg.Channel) {
		return release, false, nil
	}
	return release, true, nil
}

// fetchReleaseFromGitHub resolves the latest release without touching the
// GitHub REST API: it reads version.json straight from the release download
// URL (the rolling dev tag, or the "latest" redirect for stable) and
// synthesizes the asset list from the tag it names.
func (u *Updater) fetchReleaseFromGitHub(ctx context.Context, cfg Config) (*releaseInfo, error) {
	base := githubBaseURL + "/" + cfg.Repo + "/releases"
	versionURL := base + "/latest/download/" + versionName
	if cfg.Channel != ChannelStable {
		versionURL = base + "/download/" + DevReleaseTag + "/" + versionName
	}

	body, status, err := u.httpGet(ctx, versionURL, 16*1024)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		u.logger.Info("self_update_no_release", zap.String("channel", cfg.Channel))
		return nil, nil
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("%s returned status %d", versionName, status)
	}

	var info releaseVersionInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("decode version metadata: %w", err)
	}
	tag := strings.TrimSpace(info.Tag)
	if cfg.Channel != ChannelStable {
		tag = DevReleaseTag
	} else if tag == "" {
		return nil, errors.New("version metadata missing release tag")
	}

	names := []string{targetName(), checksumsName, versionName}
	assets := make([]assetInfo, 0, len(names))
	for _, name := range names {
		assets = append(assets, assetInfo{Name: name, BrowserDownloadURL: base + "/download/" + tag + "/" + name})
	}
	return &releaseInfo{
		TagName:    tag,
		Prerelease: cfg.Channel != ChannelStable,
		Assets:     assets,
		Version:    strings.TrimSpace(info.Version),
		Commit:     strings.TrimSpace(info.Commit),
		BuildTime:  strings.TrimSpace(info.BuildTime),
	}, nil
}

func (u *Updater) fetchReleaseViaProxy(ctx context.Context, cfg Config) (*releaseInfo, error) {
	tag := "latest"
	if cfg.Channel != ChannelStable {
		tag = DevReleaseTag
	}
	url := fmt.Sprintf("%s/api/releases/%s/%s", cfg.ProxyBaseURL, cfg.Repo, tag)

	body, status, err := u.httpGet(ctx, url, 1<<20)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		u.logger.Info("self_update_no_release", zap.String("channel", cfg.Channel))
		return nil, nil
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("release lookup returned status %d", status)
	}

	var release releaseInfo
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("decode release: %w", err)
	}
	if err := u.loadReleaseVersion(ctx, cfg, &release); err != nil {
		// Metadata only refines display and dev comparison; the release itself is still usable.
		u.logger.Warn("self_update_version_metadata_unavailable", zap.String("tag", release.TagName), zap.Error(err))
	}
	return &release, nil
}

func (u *Updater) loadReleaseVersion(ctx context.Context, cfg Config, release *releaseInfo) error {
	asset := release.asset(versionName)
	if asset == nil {
		return errors.New("version.json asset not found")
	}
	body, err := u.fetchAsset(ctx, cfg, asset, 16*1024)
	if err != nil {
		return err
	}
	var info releaseVersionInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return fmt.Errorf("decode version metadata: %w", err)
	}
	release.Version = strings.TrimSpace(info.Version)
	release.Commit = strings.TrimSpace(info.Commit)
	release.BuildTime = strings.TrimSpace(info.BuildTime)
	return nil
}

func (u *Updater) isNewer(release releaseInfo, channel string) bool {
	current := currentVersion()
	if current == "dev" {
		return true
	}
	if channel == ChannelStable {
		return semverGreater(release.TagName, current)
	}

	// Dev builds follow commits rather than ordering: a force-push or rerun
	// makes run numbers unreliable, but a different commit always means news.
	remoteCommit := normalizeCommit(release.Commit)
	if remoteCommit == "" {
		remoteCommit = normalizeCommit(release.TargetCommitish)
	}
	if localCommit := normalizeCommit(currentCommit()); remoteCommit != "" && localCommit != "" {
		return remoteCommit != localCommit
	}

	remoteNum, remoteSHA := parseDevVersion(release.displayVersion())
	localNum, localSHA := parseDevVersion(current)
	if remoteSHA != "" && remoteSHA == localSHA {
		return false
	}
	if remoteNum > 0 && localNum > 0 {
		return remoteNum > localNum
	}
	// Refuse rather than risk an endless self-update loop.
	u.logger.Warn("self_update_versions_incomparable",
		zap.String("current", current),
		zap.String("remote", release.displayVersion()),
	)
	return false
}

func normalizeCommit(commit string) string {
	commit = strings.TrimSpace(commit)
	if commit == "" || commit == "unknown" {
		return ""
	}
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}

func semverGreater(a, b string) bool {
	av := parseSemver(a)
	bv := parseSemver(b)
	for i := range av {
		if av[i] != bv[i] {
			return av[i] > bv[i]
		}
	}
	return false
}

func parseSemver(s string) [3]int {
	var result [3]int
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	for i, part := range strings.SplitN(s, ".", 3) {
		if idx := strings.IndexAny(part, "-+"); idx >= 0 {
			part = part[:idx]
		}
		result[i], _ = strconv.Atoi(part)
	}
	return result
}

// parseDevVersion extracts the run number and commit from dev-{run}-{yyyymmdd}-{sha}.
func parseDevVersion(version string) (runNumber int, sha string) {
	parts := strings.SplitN(version, "-", 4)
	if len(parts) == 4 && parts[0] == "dev" {
		n, _ := strconv.Atoi(parts[1])
		return n, parts[3]
	}
	return 0, ""
}

func targetName() string {
	name := assetPrefix + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

func (u *Updater) download(ctx context.Context, cfg Config, release *releaseInfo) (string, error) {
	u.mu.Lock()
	u.status.State = StateDownloading
	u.status.Progress = progressDownloadStart
	u.status.DownloadProgress = 0
	u.mu.Unlock()

	target := targetName()
	binaryAsset := release.asset(target)
	if binaryAsset == nil {
		return "", fmt.Errorf("no asset found for %s in release %s", target, release.TagName)
	}
	checksumsAsset := release.asset(checksumsName)
	if checksumsAsset == nil {
		return "", fmt.Errorf("release %s is missing %s, refusing unverified update", release.TagName, checksumsName)
	}

	if err := os.MkdirAll(cfg.DownloadDir, 0o755); err != nil {
		return "", fmt.Errorf("create update dir: %w", err)
	}
	finalPath := filepath.Join(cfg.DownloadDir, assetPrefix+sanitizePathPart(release.TagName))
	tmpPath := finalPath + ".tmp"

	dlCtx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	if err := u.downloadFile(dlCtx, resolveDownloadURL(cfg, binaryAsset.BrowserDownloadURL), tmpPath, binaryAsset.Size); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("download binary: %w", err)
	}

	u.mu.Lock()
	u.status.Progress = progressVerifyStart
	u.mu.Unlock()

	if err := u.verifyChecksum(dlCtx, cfg, checksumsAsset, target, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}

	u.mu.Lock()
	u.status.Progress = progressVerifyDone
	u.mu.Unlock()

	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	u.logger.Info("self_update_downloaded", zap.String("version", release.displayVersion()), zap.String("path", finalPath))
	return finalPath, nil
}

// verifyChecksum enforces the SHA256SUMS entry for target; a missing file,
// missing entry or mismatch all fail the update.
func (u *Updater) verifyChecksum(ctx context.Context, cfg Config, checksumsAsset *assetInfo, target, path string) error {
	sums, err := u.fetchAsset(ctx, cfg, checksumsAsset, 64*1024)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", checksumsName, err)
	}
	expected, err := sha256ForTarget(sums, target)
	if err != nil {
		return err
	}
	actual, err := fileSHA256(path)
	if err != nil {
		return fmt.Errorf("compute sha256: %w", err)
	}
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("sha256 mismatch: expected %s, got %s", expected, actual)
	}
	return nil
}

// sha256ForTarget selects the line for target by exact bare file name; a
// prefix match would let firecortex-linux-arm pick firecortex-linux-arm64.
func sha256ForTarget(sums []byte, target string) (string, error) {
	for line := range strings.SplitSeq(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// sha256sum writes "*name" in binary mode.
		if strings.TrimPrefix(fields[1], "*") == target {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("no sha256 entry for %s in %s", target, checksumsName)
}

// resolveDownloadURL maps a GitHub browser_download_url onto the proxy
// mirror when the proxy source is selected.
func resolveDownloadURL(cfg Config, browserURL string) string {
	if cfg.Source != SourceProxy {
		return browserURL
	}
	const ghPrefix = "https://github.com/"
	const relSegment = "/releases/download/"
	path, ok := strings.CutPrefix(browserURL, ghPrefix)
	if !ok {
		return browserURL
	}
	ownerRepo, tagAndAsset, ok := strings.Cut(path, relSegment)
	if !ok {
		return browserURL
	}
	return cfg.ProxyBaseURL + "/download/" + ownerRepo + "/" + tagAndAsset
}

func (u *Updater) downloadFile(ctx context.Context, url, destPath string, expectedSize int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := u.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()

	totalSize := resp.ContentLength
	if totalSize <= 0 {
		totalSize = expectedSize
	}

	var written int64
	var lastProgress float64
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return err
			}
			written += int64(n)
			if totalSize > 0 {
				progress := float64(written) / float64(totalSize) * 100
				if progress-lastProgress >= 1 || progress >= 100 {
					u.mu.Lock()
					u.status.DownloadProgress = clampProgress(progress)
					u.status.Progress = overallDownloadProgress(progress)
					u.mu.Unlock()
					lastProgress = progress
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	u.mu.Lock()
	u.status.DownloadProgress = progressComplete
	u.status.Progress = overallDownloadProgress(progressComplete)
	u.mu.Unlock()
	return f.Close()
}

// httpGet fetches url and returns up to limit bytes of the body along with
// the status code. Network errors are returned; HTTP error statuses are not.
func (u *Updater) httpGet(ctx context.Context, url string, limit int64) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	return body, resp.StatusCode, err
}

func (u *Updater) fetchAsset(ctx context.Context, cfg Config, asset *assetInfo, limit int64) ([]byte, error) {
	body, status, err := u.httpGet(ctx, resolveDownloadURL(cfg, asset.BrowserDownloadURL), limit)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("%s returned status %d", asset.Name, status)
	}
	return body, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// applyUpdate swaps the running executable for newBinaryPath and re-executes
// it in place (same PID, so service managers do not notice the restart).
func (u *Updater) applyUpdate(newBinaryPath, tag string) error {
	u.mu.Lock()
	u.status.State = StateApplying
	u.status.Progress = progressApplying
	u.mu.Unlock()

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	if execPath, err = filepath.EvalSymlinks(execPath); err != nil {
		return fmt.Errorf("resolve symlinks: %w", err)
	}
	if !execSupported {
		return fmt.Errorf("self-update is not supported on %s", runtime.GOOS)
	}

	if u.hooks.BeforeExec != nil {
		if err := u.hooks.BeforeExec(tag); err != nil {
			return fmt.Errorf("prepare restart: %w", err)
		}
	}

	backupPath := execPath + ".bak"
	if err := os.Rename(execPath, backupPath); err != nil {
		return u.failAfterStop(fmt.Errorf("backup current binary: %w", err))
	}
	if err := copyFile(newBinaryPath, execPath); err != nil {
		_ = os.Rename(backupPath, execPath)
		return u.failAfterStop(fmt.Errorf("install new binary: %w", err))
	}
	if err := os.Chmod(execPath, 0o755); err != nil {
		_ = os.Rename(backupPath, execPath)
		return u.failAfterStop(fmt.Errorf("chmod new binary: %w", err))
	}
	_ = os.Remove(backupPath)
	_ = os.Remove(newBinaryPath)

	u.logger.Info("self_update_restarting", zap.String("tag", tag))
	u.mu.Lock()
	u.status.Progress = progressComplete
	u.mu.Unlock()
	_ = u.logger.Sync()
	return u.failAfterStop(replaceProcess(execPath, os.Args, os.Environ()))
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func normalizeConfig(cfg Config) Config {
	cfg.Channel = strings.ToLower(strings.TrimSpace(cfg.Channel))
	if cfg.Channel != ChannelDev {
		cfg.Channel = ChannelStable
	}
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = DefaultCheckInterval
	}
	cfg.CheckInterval = max(cfg.CheckInterval, 60)
	cfg.Source = strings.ToLower(strings.TrimSpace(cfg.Source))
	if cfg.Source != SourceProxy {
		cfg.Source = SourceGitHub
	}
	cfg.ProxyBaseURL = strings.TrimRight(strings.TrimSpace(cfg.ProxyBaseURL), "/")
	if cfg.ProxyBaseURL == "" {
		cfg.ProxyBaseURL = DefaultProxyBaseURL
	}
	cfg.Repo = strings.Trim(strings.TrimSpace(cfg.Repo), "/")
	if cfg.Repo == "" {
		cfg.Repo = DefaultRepo
	}
	cfg.DownloadDir = strings.TrimSpace(cfg.DownloadDir)
	if cfg.DownloadDir == "" {
		cfg.DownloadDir = filepath.Join(os.TempDir(), "firecortex-updates")
	}
	return cfg
}

func sanitizePathPart(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "update"
	}
	return b.String()
}
