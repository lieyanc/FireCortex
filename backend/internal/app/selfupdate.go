package app

import (
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/lieyanc/FireCortex/backend/internal/infra/config"
	"github.com/lieyanc/FireCortex/backend/internal/infra/updater"
	"github.com/lieyanc/FireCortex/backend/internal/shared/security"
	"go.uber.org/zap"
)

// restartGrace bounds how long the updater waits for Run to finish draining
// beyond the configured HTTP shutdown timeout before it re-execs anyway.
const restartGrace = time.Minute

// restartCoordinator hands control from the updater to App.Run for a graceful
// stop, then back to the updater, which replaces the process with exec.
type restartCoordinator struct {
	requested chan struct{}
	ready     chan struct{}
	once      sync.Once
	timeout   time.Duration
	logger    *zap.Logger
}

func newRestartCoordinator(drainTimeout time.Duration, logger *zap.Logger) *restartCoordinator {
	return &restartCoordinator{
		requested: make(chan struct{}),
		ready:     make(chan struct{}),
		timeout:   drainTimeout + restartGrace,
		logger:    logger,
	}
}

// beforeExec asks App.Run to drain and release resources, and blocks until it has.
func (r *restartCoordinator) beforeExec(tag string) error {
	r.logger.Info("self_update_stopping_server", zap.String("tag", tag))
	r.once.Do(func() { close(r.requested) })
	select {
	case <-r.ready:
	case <-time.After(r.timeout):
		r.logger.Warn("self_update_stop_timeout", zap.Duration("timeout", r.timeout))
	}
	return nil
}

// onExecFailure exits so a service manager can restart the process: the
// server has already stopped serving, and the previous binary was restored.
func (r *restartCoordinator) onExecFailure(err error) {
	r.logger.Error("self_update_exec_failed", zap.Error(err))
	_ = r.logger.Sync()
	os.Exit(1)
}

func newSelfUpdater(cfg config.Config, logger *zap.Logger, restart *restartCoordinator, isBusy func() bool) *updater.Updater {
	updateCfg := updater.Config{
		Enabled:       cfg.UpdateEnabled,
		Channel:       cfg.UpdateChannel,
		CheckInterval: cfg.UpdateCheckIntervalSeconds,
		Source:        cfg.UpdateSource,
		ProxyBaseURL:  cfg.UpdateProxyBaseURL,
		Repo:          cfg.UpdateRepo,
		DownloadDir:   cfg.UpdateDownloadDir,
	}
	return updater.New(func() updater.Config { return updateCfg }, updateHTTPClient(cfg, logger), logger.Named("updater"), updater.RestartHooks{
		BeforeExec:    restart.beforeExec,
		OnExecFailure: restart.onExecFailure,
		IsBusy:        isBusy,
	})
}

// updateHTTPClient follows the configured SSRF policy; the operator-configured
// proxy mirror is trusted like other explicitly configured integrations.
func updateHTTPClient(cfg config.Config, logger *zap.Logger) *http.Client {
	policy := cfg.TrustedOutboundPolicy()
	if proxyURL := strings.TrimSpace(cfg.UpdateProxyBaseURL); proxyURL != "" {
		trusted, err := policy.WithTrustedHTTPURLs(proxyURL)
		if err != nil {
			logger.Warn("self_update_proxy_url_invalid", zap.String("proxy_base_url", proxyURL), zap.Error(err))
		} else {
			policy = trusted
		}
	}
	return security.NewOutboundHTTPClient(policy, 0)
}
