// Package updater implements in-place self-update from GitHub Releases.
//
// Release protocol (shared with .github/workflows/release.yml; change both together):
//   - assets are named firecortex-{goos}-{goarch};
//   - every release carries a single SHA256SUMS file in sha256sum's native
//     "<hash>  <bare file name>" format plus a version.json metadata file
//     (carrying optional assets digests and Markdown release_notes);
//   - stable releases use vX.Y.Z.N tags (version X.Y.Z.N); the dev channel
//     follows the rolling DevReleaseTag prerelease with dev-{run}-{yyyymmdd}-{sha}
//     versions and full commit metadata.
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
	"slices"
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

	initialCheckDelay  = 30 * time.Second
	checkTimeout       = 30 * time.Second
	idleWaitTimeout    = 10 * time.Minute
	idlePollInterval   = 5 * time.Second
	downloadTimeout    = 30 * time.Minute
	maxManifestSize    = 64 << 10
	maxChecksumsSize   = 64 << 10
	maxReleaseBodySize = 1 << 20

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
	pendingHash       string
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
	expectedHash := u.pendingHash
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
	u.pendingHash = ""
	u.mu.Unlock()

	go func() {
		// Let the HTTP response go out before the server starts draining.
		time.Sleep(200 * time.Millisecond)
		ctx := u.bgContext()
		if err := u.waitForIdle(ctx); err != nil {
			u.setError("apply canceled while waiting for idle: " + err.Error())
			return
		}
		if err := u.applyUpdate(path, tag, expectedHash); err != nil {
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
	u.pendingHash = ""
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

	binaryPath, verifiedHash, err := u.download(ctx, cfg, release)
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
		if err := u.applyUpdate(binaryPath, release.TagName, verifiedHash); err != nil {
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
	u.pendingHash = verifiedHash
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
	TagName         string            `json:"tag_name"`
	TargetCommitish string            `json:"target_commitish"`
	Prerelease      bool              `json:"prerelease"`
	Body            string            `json:"body"`
	Assets          []assetInfo       `json:"assets"`
	Version         string            `json:"-"`
	Commit          string            `json:"-"`
	BuildTime       string            `json:"-"`
	AssetDigests    map[string]string `json:"-"`
}

type assetInfo struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type releaseVersionInfo struct {
	Version      string            `json:"version"`
	Commit       string            `json:"commit"`
	BuildTime    string            `json:"build_time"`
	Tag          string            `json:"tag"`
	Assets       map[string]string `json:"assets,omitempty"`
	ReleaseNotes string            `json:"release_notes,omitempty"`
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

	body, status, err := u.httpGet(ctx, versionURL, maxManifestSize)
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
	digests, err := validateManifestAssets(info.Assets)
	if err != nil {
		return nil, err
	}
	tag := strings.TrimSpace(info.Tag)
	if cfg.Channel != ChannelStable {
		if tag != "" && tag != DevReleaseTag {
			return nil, fmt.Errorf("development metadata tag must be %s, got %q", DevReleaseTag, tag)
		}
		tag = DevReleaseTag
	} else {
		if tag == "" {
			return nil, errors.New("version metadata missing release tag")
		}
		if _, ok := parseReleaseVersion(tag); !ok {
			return nil, fmt.Errorf("version metadata contains invalid release tag %q", tag)
		}
	}

	names := []string{targetName(), checksumsName, versionName}
	assets := make([]assetInfo, 0, len(names))
	for _, name := range names {
		assets = append(assets, assetInfo{Name: name, BrowserDownloadURL: base + "/download/" + tag + "/" + name})
	}
	return &releaseInfo{
		TagName:      tag,
		Prerelease:   cfg.Channel != ChannelStable,
		Body:         strings.TrimSpace(info.ReleaseNotes),
		Assets:       assets,
		Version:      strings.TrimSpace(info.Version),
		Commit:       strings.TrimSpace(info.Commit),
		BuildTime:    strings.TrimSpace(info.BuildTime),
		AssetDigests: digests,
	}, nil
}

func (u *Updater) fetchReleaseViaProxy(ctx context.Context, cfg Config) (*releaseInfo, error) {
	tag := "latest"
	if cfg.Channel != ChannelStable {
		tag = DevReleaseTag
	}
	url := fmt.Sprintf("%s/api/releases/%s/%s", cfg.ProxyBaseURL, cfg.Repo, tag)

	body, status, err := u.httpGet(ctx, url, maxReleaseBodySize)
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
		if errors.Is(err, errVersionAssetMissing) {
			// Legacy releases without version.json still rely on SHA256SUMS and proxy release body.
			u.logger.Warn("self_update_version_metadata_unavailable", zap.String("tag", release.TagName), zap.Error(err))
		} else {
			return nil, err
		}
	}
	return &release, nil
}

var errVersionAssetMissing = errors.New("version.json asset not found")

func (u *Updater) loadReleaseVersion(ctx context.Context, cfg Config, release *releaseInfo) error {
	asset := release.asset(versionName)
	if asset == nil {
		return errVersionAssetMissing
	}
	body, err := u.fetchAsset(ctx, cfg, asset, maxManifestSize)
	if err != nil {
		return err
	}
	var info releaseVersionInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return fmt.Errorf("decode version metadata: %w", err)
	}
	digests, err := validateManifestAssets(info.Assets)
	if err != nil {
		return err
	}
	if tag := strings.TrimSpace(info.Tag); tag != "" && release.TagName != "" && tag != release.TagName {
		return fmt.Errorf("version metadata tag %q does not match release tag %q", tag, release.TagName)
	}
	release.Version = strings.TrimSpace(info.Version)
	release.Commit = strings.TrimSpace(info.Commit)
	release.BuildTime = strings.TrimSpace(info.BuildTime)
	release.AssetDigests = digests
	// Prefer notes bound to this build's manifest over the proxy release body,
	// keeping the proxy body as fallback when an older manifest omits release_notes.
	if notes := strings.TrimSpace(info.ReleaseNotes); notes != "" {
		release.Body = notes
	}
	return nil
}

func validateManifestAssets(assets map[string]string) (map[string]string, error) {
	if len(assets) == 0 {
		return nil, nil
	}
	if len(assets) > 32 {
		return nil, errors.New("version metadata contains too many asset entries")
	}
	normalized := make(map[string]string, len(assets))
	for name, digest := range assets {
		if !strings.HasPrefix(name, assetPrefix) || sanitizePathPart(name) != name {
			return nil, fmt.Errorf("version metadata contains invalid asset name %q", name)
		}
		digest = strings.TrimSpace(digest)
		if len(digest) != sha256.Size*2 {
			return nil, fmt.Errorf("version metadata asset %q has invalid sha256 digest length", name)
		}
		if _, err := hex.DecodeString(digest); err != nil {
			return nil, fmt.Errorf("version metadata asset %q has invalid sha256 digest: %w", name, err)
		}
		normalized[name] = strings.ToLower(digest)
	}
	return normalized, nil
}

func (u *Updater) isNewer(release releaseInfo, channel string) bool {
	current := currentVersion()
	if current == "dev" {
		return true
	}
	if channel == ChannelStable {
		comparison, ok := compareReleaseVersions(release.TagName, current)
		if !ok {
			// Dev builds can switch to a versioned stable release.
			run, _ := parseDevVersion(current)
			_, valid := parseReleaseVersion(release.TagName)
			return run > 0 && valid
		}
		return comparison > 0
	}

	remoteRun, remoteTagSHA := parseDevVersion(release.displayVersion())
	localRun, localTagSHA := parseDevVersion(current)
	remoteCommit := firstNonEmpty(
		normalizeCommit(release.Commit),
		normalizeCommit(release.TargetCommitish),
		remoteTagSHA,
	)
	localCommit := firstNonEmpty(
		normalizeCommit(currentCommit()),
		localTagSHA,
	)

	// When both sides carry canonical dev-{run}-{yyyymmdd}-{sha} versions,
	// identical commits are already up to date and different commits must move
	// forward in run number so older dev builds cannot be replayed.
	if remoteRun > 0 && localRun > 0 {
		if remoteCommit != "" && localCommit != "" && remoteCommit == localCommit {
			return false
		}
		return remoteRun > localRun
	}

	// Compatibility when both sides use numeric release versions.
	if comparison, ok := compareReleaseVersions(release.Version, current); ok && comparison != 0 {
		return comparison > 0
	}

	// Transitions between versioned builds (such as 0.4.6.2) and dev builds
	// follow the commit SHA.
	if remoteCommit != "" && localCommit != "" {
		return remoteCommit != localCommit
	}

	// Refuse rather than risk an endless self-update loop.
	u.logger.Warn("self_update_versions_incomparable",
		zap.String("current", current),
		zap.String("remote", release.displayVersion()),
	)
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func normalizeCommit(commit string) string {
	commit = strings.ToLower(strings.TrimSpace(commit))
	if commit == "" || commit == "unknown" {
		return ""
	}
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}

func isHexString(value string) bool {
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return value != ""
}

func compareReleaseVersions(a, b string) (int, bool) {
	av, okA := parseReleaseVersion(a)
	bv, okB := parseReleaseVersion(b)
	if !okA || !okB {
		return 0, false
	}
	return slices.Compare(av[:], bv[:]), true
}

func parseReleaseVersion(version string) ([4]uint64, bool) {
	var result [4]uint64
	core := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if end := strings.IndexAny(core, "-+"); end >= 0 {
		core = core[:end]
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 && len(parts) != 4 {
		return result, false
	}
	for i, part := range parts {
		n, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return result, false
		}
		result[i] = n
	}
	return result, true
}

// parseDevVersion extracts the run number and normalized commit from dev-{run}-{yyyymmdd}-{sha}.
func parseDevVersion(version string) (runNumber int, sha string) {
	parts := strings.SplitN(strings.TrimSpace(version), "-", 4)
	if len(parts) != 4 || parts[0] != "dev" || len(parts[2]) != 8 {
		return 0, ""
	}
	n, err := strconv.Atoi(parts[1])
	if err != nil || n <= 0 {
		return 0, ""
	}
	for _, r := range parts[2] {
		if r < '0' || r > '9' {
			return 0, ""
		}
	}
	sha = normalizeCommit(parts[3])
	if len(sha) < 7 || !isHexString(sha) {
		return 0, ""
	}
	return n, sha
}

func targetName() string {
	name := assetPrefix + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

func (u *Updater) download(ctx context.Context, cfg Config, release *releaseInfo) (string, string, error) {
	u.mu.Lock()
	u.status.State = StateDownloading
	u.status.Progress = progressDownloadStart
	u.status.DownloadProgress = 0
	u.mu.Unlock()

	target := targetName()
	binaryAsset := release.asset(target)
	if binaryAsset == nil {
		return "", "", fmt.Errorf("no asset found for %s in release %s", target, release.TagName)
	}
	checksumsAsset := release.asset(checksumsName)
	if checksumsAsset == nil {
		return "", "", fmt.Errorf("release %s is missing %s, refusing unverified update", release.TagName, checksumsName)
	}
	var manifestDigest string
	if len(release.AssetDigests) > 0 {
		var ok bool
		manifestDigest, ok = release.AssetDigests[target]
		if !ok {
			return "", "", fmt.Errorf("version metadata does not contain asset %s", target)
		}
	}

	if err := os.MkdirAll(cfg.DownloadDir, 0o755); err != nil {
		return "", "", fmt.Errorf("create update dir: %w", err)
	}
	finalPath := filepath.Join(cfg.DownloadDir, assetPrefix+sanitizePathPart(release.TagName))
	tmpPath := finalPath + ".tmp"

	dlCtx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	if err := u.downloadFile(dlCtx, resolveDownloadURL(cfg, binaryAsset.BrowserDownloadURL), tmpPath, binaryAsset.Size); err != nil {
		_ = os.Remove(tmpPath)
		return "", "", fmt.Errorf("download binary: %w", err)
	}

	u.mu.Lock()
	u.status.Progress = progressVerifyStart
	u.mu.Unlock()

	verifiedHash, err := u.verifyChecksum(dlCtx, cfg, checksumsAsset, target, manifestDigest, tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return "", "", err
	}

	u.mu.Lock()
	u.status.Progress = progressVerifyDone
	u.mu.Unlock()

	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", "", err
	}
	u.logger.Info("self_update_downloaded", zap.String("version", release.displayVersion()), zap.String("path", finalPath))
	return finalPath, verifiedHash, nil
}

// verifyChecksum enforces the SHA256SUMS entry (and manifest asset binding
// when present) for target; any missing file, missing entry or mismatch fails.
func (u *Updater) verifyChecksum(ctx context.Context, cfg Config, checksumsAsset *assetInfo, target, manifestDigest, path string) (string, error) {
	sums, err := u.fetchAsset(ctx, cfg, checksumsAsset, maxChecksumsSize)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", checksumsName, err)
	}
	expected, err := sha256ForTarget(sums, target)
	if err != nil {
		return "", err
	}
	if manifestDigest != "" && !strings.EqualFold(expected, manifestDigest) {
		return "", fmt.Errorf("sha256 mismatch between %s and %s for %s", versionName, checksumsName, target)
	}
	actual, err := fileSHA256(path)
	if err != nil {
		return "", fmt.Errorf("compute sha256: %w", err)
	}
	if !strings.EqualFold(actual, expected) {
		return "", fmt.Errorf("sha256 mismatch: expected %s, got %s", expected, actual)
	}
	return strings.ToLower(expected), nil
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
// the status code. Network errors and oversized responses are returned; HTTP
// error statuses are not.
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if int64(len(body)) > limit {
		return nil, resp.StatusCode, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return body, resp.StatusCode, nil
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

// applyUpdate re-verifies newBinaryPath, swaps the running executable for it,
// and re-executes it in place (same PID, so service managers do not notice the restart).
func (u *Updater) applyUpdate(newBinaryPath, tag, expectedHash string) error {
	u.mu.Lock()
	u.status.State = StateApplying
	u.status.Progress = progressApplying
	u.mu.Unlock()

	if expectedHash == "" {
		return errors.New("missing expected binary sha256")
	}
	actualHash, err := fileSHA256(newBinaryPath)
	if err != nil {
		return fmt.Errorf("verify pending binary sha256: %w", err)
	}
	if !strings.EqualFold(actualHash, expectedHash) {
		return fmt.Errorf("pending binary sha256 mismatch: expected %s, got %s", expectedHash, actualHash)
	}

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
