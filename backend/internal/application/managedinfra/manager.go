package managedinfra

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lieyanc/FireCortex/backend/internal/infra/config"
	"github.com/lieyanc/FireCortex/backend/internal/pkg/conv"
	managedports "github.com/lieyanc/FireCortex/backend/internal/ports/managedinfra"
	"go.uber.org/zap"
)

// ManagerDependencies 聚合托管运维管理器所需的外部端口（由组合根注入）。
type ManagerDependencies struct {
	Runtime      *config.Runtime
	HostOps      managedports.HostOps
	Infra        managedports.SetupInfraPort
	Prober       managedports.ServiceProber
	Security     managedports.AdminSecurityPort
	SettingsSync managedports.SettingsSyncPort
	Restarter    managedports.RestartTriggerPort
	Logger       *zap.Logger
}

// ServiceView 描述单个核心或可选旁路服务的完整运行视图（领域对象，无 JSON 标签）。
type ServiceView struct {
	ID               string
	Category         string // core | optional
	Name             string
	Description      string
	Mode             string // managed | external | memory | disabled
	ContainerName    string
	Image            string
	Port             int
	EndpointURL      string
	DataDir          string
	Status           string // running | stopped | unhealthy | unavailable | external | memory
	Health           string
	Reachable        bool
	Message          string
	CPUPercent       float64
	MemoryUsageBytes uint64
	MemoryLimitBytes uint64
	CanStart         bool
	CanStop          bool
	CanRestart       bool
	CanPull          bool
}

// Overview 返回「服务与基础设施」页面顶部概览与所有白名单服务卡片。
type Overview struct {
	Docker          managedports.DaemonInfo
	DockerError     string
	DataDir         managedports.DirInspection
	RegistryMirror  string
	ComposeSync     bool
	BackupEnabled   bool
	BackupSchedule  string
	BackupRetention int
	Services        []ServiceView
}

// ServiceActionOptions 是启停或拉取托管服务时允许传入的受控白名单参数。
type ServiceActionOptions struct {
	Port          int
	MemoryLimitMB int
	TesseractLang string
}

// JobEvent 表示一次异步基础设施操作中的进度事件。
type JobEvent struct {
	JobID     string
	Action    string
	ServiceID string
	Step      string
	Message   string
	Percent   int
	Status    string // running | done | failed
	Timestamp time.Time
}

// JobSnapshot 保存异步基础设施任务的完整快照。
type JobSnapshot struct {
	JobID     string
	Action    string
	ServiceID string
	State     string // running | done | failed
	Error     string
	Events    []JobEvent
}

type sidecarTemplate struct {
	id               string
	name             string
	description      string
	containerName    string
	defaultImage     string
	defaultPort      int
	containerPort    int
	containerDataDir string
	endpointSuffix   string
	defaultEnv       []string
}

var sidecarTemplates = []sidecarTemplate{
	{
		id:             managedports.OptionalServiceTika,
		name:           "Apache Tika",
		description:    "Document text extraction for Office, PDF, and archive formats",
		containerName:  managedports.TikaContainerName,
		defaultImage:   managedports.DefaultTikaImage,
		defaultPort:    9998,
		containerPort:  9998,
		endpointSuffix: "",
	},
	{
		id:               managedports.OptionalServiceDocling,
		name:             "Docling",
		description:      "High-accuracy structured PDF/document parsing and OCR",
		containerName:    managedports.DoclingContainerName,
		defaultImage:     managedports.DefaultDoclingImage,
		defaultPort:      8005,
		containerPort:    8005,
		containerDataDir: "/root/.cache",
		endpointSuffix:   "/ocr",
	},
	{
		id:             managedports.OptionalServiceTesseract,
		name:           "Tesseract OCR",
		description:    "Lightweight multilingual OCR engine (chi_sim+eng)",
		containerName:  managedports.TesseractContainerName,
		defaultImage:   managedports.DefaultTesseractImage,
		defaultPort:    8004,
		containerPort:  8004,
		endpointSuffix: "/ocr",
		defaultEnv:     []string{"TESSERACT_LANG=chi_sim+eng", "TESSERACT_PSM=3", "TESSERACT_OEM=3"},
	},
	{
		id:             managedports.OptionalServiceRapidOCR,
		name:           "RapidOCR",
		description:    "Fast ONNX-based OCR optimized for Chinese and English text",
		containerName:  managedports.RapidOCRContainerName,
		defaultImage:   managedports.DefaultRapidOCRImage,
		defaultPort:    8002,
		containerPort:  8002,
		endpointSuffix: "/ocr",
	},
}

// Manager 统一编排运行时核心服务、可选旁路服务、备份调度、高危操作与常用基础设施配置。
type Manager struct {
	runtimeCfg   *config.Runtime
	hostOps      managedports.HostOps
	infra        managedports.SetupInfraPort
	prober       managedports.ServiceProber
	security     managedports.AdminSecurityPort
	settingsSync managedports.SettingsSyncPort
	restarter    managedports.RestartTriggerPort
	logger       *zap.Logger

	mu          sync.Mutex
	jobs        map[string]*JobSnapshot
	subscribers map[string]map[chan JobEvent]struct{}
	lastCronMin string
}

// NewManager 创建基础设施运维管理器。
func NewManager(deps ManagerDependencies) *Manager {
	return &Manager{
		runtimeCfg:   deps.Runtime,
		hostOps:      deps.HostOps,
		infra:        deps.Infra,
		prober:       deps.Prober,
		security:     deps.Security,
		settingsSync: deps.SettingsSync,
		restarter:    deps.Restarter,
		logger:       deps.Logger,
		jobs:         map[string]*JobSnapshot{},
		subscribers:  map[string]map[chan JobEvent]struct{}{},
	}
}

func (m *Manager) snapshotConfig() config.Config {
	if m == nil || m.runtimeCfg == nil {
		return config.Config{}
	}
	return m.runtimeCfg.Snapshot()
}

// GetOverview 收集宿主机 Docker 信息、磁盘目录状态及全部核心与可选服务的运行状态。
func (m *Manager) GetOverview(ctx context.Context) Overview {
	cfg := m.snapshotConfig()
	host := cfg.EffectiveManagedDockerHost()
	dataRoot := cfg.EffectiveManagedDataDir()

	out := Overview{
		Docker:          managedports.DaemonInfo{Host: host},
		DataDir:         m.hostOps.InspectManagedRoot(dataRoot, cfg.ManagedPostgresDataDir, cfg.ManagedRedisDataDir, cfg.ManagedBackupDir),
		RegistryMirror:  cfg.ManagedRegistryMirror,
		ComposeSync:     cfg.ManagedComposeSync,
		BackupEnabled:   cfg.ManagedBackupEnabled,
		BackupSchedule:  cfg.ManagedBackupSchedule,
		BackupRetention: cfg.ManagedBackupRetentionCount,
	}

	var client managedports.DockerClient
	if m.hostOps != nil {
		c, err := m.hostOps.NewDockerClient(host)
		if err != nil {
			out.DockerError = err.Error()
		} else {
			info, pingErr := c.Ping(ctx)
			out.Docker = info
			if pingErr != nil {
				out.DockerError = pingErr.Error()
			} else {
				client = c
			}
		}
	}

	out.Services = append(out.Services,
		m.inspectPostgresView(ctx, cfg, client),
		m.inspectRedisView(ctx, cfg, client),
	)
	for _, tpl := range sidecarTemplates {
		out.Services = append(out.Services, m.inspectSidecarView(ctx, cfg, client, tpl))
	}
	return out
}

func (m *Manager) inspectPostgresView(ctx context.Context, cfg config.Config, client managedports.DockerClient) ServiceView {
	port := cfg.ManagedPostgresPort
	if port <= 0 {
		port = 5432
	}
	view := ServiceView{
		ID:            managedports.CoreServicePostgres,
		Category:      "core",
		Name:          "PostgreSQL + pgvector",
		Description:   "Primary relational and vector database",
		ContainerName: managedports.PostgresContainerName,
		Image:         cfg.ManagedPostgresImage,
		Port:          port,
		DataDir:       cfg.EffectiveManagedPostgresDataDir(),
	}
	if !cfg.ManagedPostgresEnabled {
		view.Mode = "external"
		view.Status = "external"
		view.Reachable = true
		view.Message = "Connected to external PostgreSQL instance"
		return view
	}
	view.Mode = "managed"
	if client == nil {
		view.Status = "unavailable"
		view.Message = "Docker Engine is not reachable"
		return view
	}
	view.CanRestart = true
	view.CanPull = true

	ctr, err := client.InspectContainer(ctx, managedports.PostgresContainerName)
	if err != nil {
		view.Status = "unhealthy"
		view.Message = err.Error()
		return view
	}
	if ctr == nil {
		view.Status = "stopped"
		view.CanStart = true
		view.Message = "Managed PostgreSQL container does not exist"
		return view
	}
	view.Image = ctr.Image
	view.Health = ctr.Health
	if ctr.Running {
		view.Status = "running"
		view.Reachable = ctr.Health == "" || ctr.Health == "healthy"
		if !view.Reachable {
			view.Status = "unhealthy"
		}
		if stats, sErr := client.ContainerStats(ctx, ctr.ID); sErr == nil {
			view.CPUPercent = stats.CPUPercent
			view.MemoryUsageBytes = stats.MemoryUsageBytes
			view.MemoryLimitBytes = stats.MemoryLimitBytes
		}
		view.Message = "Running with loopback-only bind mount"
	} else {
		view.Status = "stopped"
		view.CanStart = true
		view.Message = "Container is stopped (" + ctr.Status + ")"
	}
	return view
}

func (m *Manager) inspectRedisView(ctx context.Context, cfg config.Config, client managedports.DockerClient) ServiceView {
	port := cfg.ManagedRedisPort
	if port <= 0 {
		port = 6379
	}
	view := ServiceView{
		ID:            managedports.CoreServiceRedis,
		Category:      "core",
		Name:          "Redis Cache",
		Description:   "Session, rate-limit, and runtime cache with AOF persistence",
		ContainerName: managedports.RedisContainerName,
		Image:         cfg.ManagedRedisImage,
		Port:          port,
		DataDir:       cfg.EffectiveManagedRedisDataDir(),
	}
	if strings.EqualFold(strings.TrimSpace(cfg.CacheDriver), "memory") {
		view.Mode = "memory"
		view.Status = "memory"
		view.Reachable = true
		view.Message = "Using in-process memory cache"
		return view
	}
	if !cfg.ManagedRedisEnabled {
		view.Mode = "external"
		view.Status = "external"
		view.Reachable = true
		view.Message = "Connected to external Redis (" + cfg.RedisAddr + ")"
		return view
	}
	view.Mode = "managed"
	if client == nil {
		view.Status = "unavailable"
		view.Message = "Docker Engine is not reachable"
		return view
	}
	view.CanRestart = true
	view.CanPull = true

	ctr, err := client.InspectContainer(ctx, managedports.RedisContainerName)
	if err != nil {
		view.Status = "unhealthy"
		view.Message = err.Error()
		return view
	}
	if ctr == nil {
		view.Status = "stopped"
		view.CanStart = true
		view.Message = "Managed Redis container does not exist"
		return view
	}
	view.Image = ctr.Image
	view.Health = ctr.Health
	if ctr.Running {
		view.Status = "running"
		view.Reachable = ctr.Health == "" || ctr.Health == "healthy"
		if !view.Reachable {
			view.Status = "unhealthy"
		}
		if stats, sErr := client.ContainerStats(ctx, ctr.ID); sErr == nil {
			view.CPUPercent = stats.CPUPercent
			view.MemoryUsageBytes = stats.MemoryUsageBytes
			view.MemoryLimitBytes = stats.MemoryLimitBytes
		}
		view.Message = "Running with AOF persistence and password authentication"
	} else {
		view.Status = "stopped"
		view.CanStart = true
		view.Message = "Container is stopped (" + ctr.Status + ")"
	}
	return view
}

func (m *Manager) inspectSidecarView(ctx context.Context, cfg config.Config, client managedports.DockerClient, tpl sidecarTemplate) ServiceView {
	endpoint := fmt.Sprintf("http://127.0.0.1:%d%s", tpl.defaultPort, tpl.endpointSuffix)
	dataDir := ""
	if tpl.containerDataDir != "" {
		dataDir = filepath.Join(cfg.EffectiveManagedServicesDir(), tpl.id)
	}
	view := ServiceView{
		ID:            tpl.id,
		Category:      "optional",
		Name:          tpl.name,
		Description:   tpl.description,
		ContainerName: tpl.containerName,
		Image:         tpl.defaultImage,
		Port:          tpl.defaultPort,
		EndpointURL:   endpoint,
		DataDir:       dataDir,
		Mode:          "disabled",
		Status:        "stopped",
	}

	if client == nil {
		view.Status = "unavailable"
		view.Message = "Docker Engine is not reachable"
		return view
	}
	view.CanStart = true
	view.CanPull = true

	ctr, err := client.InspectContainer(ctx, tpl.containerName)
	if err != nil {
		view.Status = "unhealthy"
		view.Message = err.Error()
		return view
	}
	if ctr == nil {
		view.Message = "Not started"
		return view
	}
	view.Mode = "managed"
	view.Image = ctr.Image
	view.Health = ctr.Health
	if ctr.Running {
		view.CanStart = false
		view.CanStop = true
		view.CanRestart = true
		reachable := m.probeSidecar(ctx, tpl.id, endpoint)
		view.Reachable = reachable
		if reachable {
			view.Status = "running"
			view.Message = "Running and reachable"
		} else {
			view.Status = "unhealthy"
			view.Message = "Container running, waiting for HTTP endpoint readiness"
		}
		if stats, sErr := client.ContainerStats(ctx, ctr.ID); sErr == nil {
			view.CPUPercent = stats.CPUPercent
			view.MemoryUsageBytes = stats.MemoryUsageBytes
			view.MemoryLimitBytes = stats.MemoryLimitBytes
		}
	} else {
		view.Status = "stopped"
		view.CanStop = true
		view.Message = "Container is stopped (" + ctr.Status + ")"
	}
	return view
}

func (m *Manager) probeSidecar(ctx context.Context, serviceID, endpoint string) bool {
	if m.prober == nil {
		return true
	}
	switch serviceID {
	case managedports.OptionalServiceTika:
		ok, _ := m.prober.ProbeTika(ctx, endpoint, "")
		return ok
	case managedports.OptionalServiceDocling:
		ok, _ := m.prober.ProbeDocling(ctx, endpoint, "")
		return ok
	case managedports.OptionalServiceTesseract:
		ok, _ := m.prober.ProbeOCR(ctx, endpoint, "")
		return ok
	case managedports.OptionalServiceRapidOCR:
		ok, _ := m.prober.ProbeRapidOCR(ctx, endpoint, "")
		return ok
	default:
		return false
	}
}

// StartServiceAction 异步执行白名单服务的启停、重启或拉取升级任务，并返回可订阅 SSE 进度的任务快照。
func (m *Manager) StartServiceAction(actorUserID uint, serviceID string, action string, opts ServiceActionOptions) (JobSnapshot, error) {
	serviceID = strings.ToLower(strings.TrimSpace(serviceID))
	action = strings.ToLower(strings.TrimSpace(action))

	isCore := serviceID == managedports.CoreServicePostgres || serviceID == managedports.CoreServiceRedis
	tpl, isSidecar := findSidecarTemplate(serviceID)
	if !isCore && !isSidecar {
		return JobSnapshot{}, fmt.Errorf("unknown managed service %q", serviceID)
	}
	if action != "start" && action != "stop" && action != "restart" && action != "pull" {
		return JobSnapshot{}, fmt.Errorf("unsupported service action %q", action)
	}
	if isCore && action == "stop" {
		return JobSnapshot{}, errors.New("core database and cache services cannot be stopped from the web UI")
	}

	cfg := m.snapshotConfig()
	if serviceID == managedports.CoreServicePostgres && !cfg.ManagedPostgresEnabled {
		return JobSnapshot{}, errors.New("PostgreSQL is not managed by FireCortex")
	}
	if serviceID == managedports.CoreServiceRedis && (!cfg.ManagedRedisEnabled || !strings.EqualFold(cfg.CacheDriver, "redis")) {
		return JobSnapshot{}, errors.New("Redis is not managed by FireCortex")
	}

	jobID := m.createJob(action, serviceID)
	go m.runServiceActionJob(jobID, actorUserID, serviceID, action, isCore, tpl, opts)
	snap, _ := m.GetJob(jobID)
	return snap, nil
}

func (m *Manager) runServiceActionJob(
	jobID string,
	actorUserID uint,
	serviceID string,
	action string,
	isCore bool,
	tpl sidecarTemplate,
	opts ServiceActionOptions,
) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	cfg := m.snapshotConfig()
	client, err := m.hostOps.NewDockerClient(cfg.EffectiveManagedDockerHost())
	if err != nil {
		m.failJob(jobID, action, serviceID, "connect", err)
		return
	}
	if _, err := client.Ping(ctx); err != nil {
		m.failJob(jobID, action, serviceID, "connect", fmt.Errorf("docker daemon unreachable: %w", err))
		return
	}

	if isCore {
		m.runCoreServiceAction(ctx, jobID, actorUserID, cfg, client, serviceID, action)
		return
	}
	m.runSidecarServiceAction(ctx, jobID, actorUserID, cfg, client, tpl, action, opts)
}

func (m *Manager) runCoreServiceAction(
	ctx context.Context,
	jobID string,
	actorUserID uint,
	cfg config.Config,
	client managedports.DockerClient,
	serviceID string,
	action string,
) {
	containerName := managedports.PostgresContainerName
	if serviceID == managedports.CoreServiceRedis {
		containerName = managedports.RedisContainerName
	}

	switch action {
	case "restart":
		m.emitJobEvent(jobID, action, serviceID, "restart", "Restarting container "+containerName+"...", 30, "running")
		if err := client.RestartContainer(ctx, containerName, 20); err != nil {
			m.failJob(jobID, action, serviceID, "restart", err)
			return
		}
		if err := m.waitCoreReady(ctx, cfg, client, serviceID); err != nil {
			m.failJob(jobID, action, serviceID, "health", err)
			return
		}
		m.recordAudit(ctx, actorUserID, "infra.service.restart", "managed_service", serviceID, map[string]any{"container": containerName})
		m.completeJob(jobID, action, serviceID, "Service restarted and healthy")
	case "start":
		m.emitJobEvent(jobID, action, serviceID, "start", "Ensuring core container "+containerName+"...", 25, "running")
		if err := m.ensureSingleCore(ctx, cfg, client, serviceID, jobID, action); err != nil {
			m.failJob(jobID, action, serviceID, "start", err)
			return
		}
		m.syncActiveCompose(ctx, cfg, client)
		m.recordAudit(ctx, actorUserID, "infra.service.start", "managed_service", serviceID, map[string]any{"container": containerName})
		m.completeJob(jobID, action, serviceID, "Core service started and healthy")
	case "pull":
		if serviceID == managedports.CoreServicePostgres {
			m.emitJobEvent(jobID, action, serviceID, "backup", "Creating pre-upgrade PostgreSQL backup snapshot...", 10, "running")
			if _, bErr := m.CreateBackup(ctx, actorUserID, "pre-upgrade"); bErr != nil {
				m.failJob(jobID, action, serviceID, "backup", fmt.Errorf("pre-upgrade backup failed (upgrade aborted for safety): %w", bErr))
				return
			}
			m.emitJobEvent(jobID, action, serviceID, "backup", "Pre-upgrade backup snapshot saved", 20, "done")
		}
		spec := m.buildCoreSpec(cfg, serviceID)
		m.emitJobEvent(jobID, action, serviceID, "pull", "Pulling image "+spec.Image+"...", 25, "running")
		if err := client.PullImage(ctx, spec.Image, func(p managedports.PullProgress) {
			pct := 25 + (p.Percent * 45 / 100)
			m.emitJobEvent(jobID, action, serviceID, "pull", fmt.Sprintf("Pulling %s (%s)", spec.Image, p.Status), pct, "running")
		}); err != nil {
			m.failJob(jobID, action, serviceID, "pull", err)
			return
		}
		m.emitJobEvent(jobID, action, serviceID, "recreate", "Recreating container while preserving host data directory...", 75, "running")
		_ = client.StopContainer(ctx, containerName, 20)
		_ = client.RemoveContainer(ctx, containerName, true)
		id, err := client.CreateContainer(ctx, spec)
		if err != nil {
			m.failJob(jobID, action, serviceID, "recreate", err)
			return
		}
		if err := client.StartContainer(ctx, id); err != nil {
			m.failJob(jobID, action, serviceID, "start", err)
			return
		}
		if err := m.waitCoreReady(ctx, cfg, client, serviceID); err != nil {
			m.failJob(jobID, action, serviceID, "health", err)
			return
		}
		m.syncActiveCompose(ctx, cfg, client)
		m.recordAudit(ctx, actorUserID, "infra.service.upgrade", "managed_service", serviceID, map[string]any{"image": spec.Image})
		m.completeJob(jobID, action, serviceID, "Image updated and container healthy")
	}
}

func (m *Manager) runSidecarServiceAction(
	ctx context.Context,
	jobID string,
	actorUserID uint,
	cfg config.Config,
	client managedports.DockerClient,
	tpl sidecarTemplate,
	action string,
	opts ServiceActionOptions,
) {
	port := opts.Port
	if port <= 0 {
		port = tpl.defaultPort
	}
	scCfg := buildSidecarConfig(cfg, tpl, port, opts)
	spec := m.hostOps.SidecarSpec(scCfg)
	endpoint := fmt.Sprintf("http://127.0.0.1:%d%s", port, tpl.endpointSuffix)

	switch action {
	case "stop":
		m.emitJobEvent(jobID, action, tpl.id, "stop", "Stopping "+tpl.name+"...", 40, "running")
		if err := client.StopContainer(ctx, tpl.containerName, 15); err != nil {
			m.failJob(jobID, action, tpl.id, "stop", err)
			return
		}
		if err := client.RemoveContainer(ctx, tpl.containerName, true); err != nil {
			m.failJob(jobID, action, tpl.id, "remove", err)
			return
		}
		m.syncActiveCompose(ctx, cfg, client)
		m.recordAudit(ctx, actorUserID, "infra.service.stop", "managed_service", tpl.id, nil)
		m.completeJob(jobID, action, tpl.id, tpl.name+" stopped")
	case "restart":
		m.emitJobEvent(jobID, action, tpl.id, "restart", "Restarting "+tpl.name+"...", 40, "running")
		if err := client.RestartContainer(ctx, tpl.containerName, 15); err != nil {
			m.failJob(jobID, action, tpl.id, "restart", err)
			return
		}
		m.waitSidecarProbe(ctx, jobID, action, tpl.id, endpoint)
		m.recordAudit(ctx, actorUserID, "infra.service.restart", "managed_service", tpl.id, nil)
		m.completeJob(jobID, action, tpl.id, tpl.name+" restarted")
	case "start", "pull":
		if err := client.EnsureNetwork(ctx, cfg.EffectiveManagedNetwork()); err != nil {
			m.failJob(jobID, action, tpl.id, "network", err)
			return
		}
		if scCfg.DataDir != "" {
			if err := m.hostOps.PrepareManagedDirs(cfg.EffectiveManagedDataDir(), "", "", "", scCfg.DataDir); err != nil {
				m.failJob(jobID, action, tpl.id, "prepare", err)
				return
			}
		}

		needPull := action == "pull"
		if !needPull {
			present, err := client.InspectImage(ctx, spec.Image)
			if err != nil {
				m.failJob(jobID, action, tpl.id, "pull", err)
				return
			}
			needPull = !present
		}
		if needPull {
			m.emitJobEvent(jobID, action, tpl.id, "pull", "Pulling "+spec.Image+"...", 10, "running")
			if err := client.PullImage(ctx, spec.Image, func(p managedports.PullProgress) {
				pct := 10 + (p.Percent * 60 / 100)
				m.emitJobEvent(jobID, action, tpl.id, "pull", fmt.Sprintf("Pulling %s (%s)", spec.Image, p.Status), pct, "running")
			}); err != nil {
				m.failJob(jobID, action, tpl.id, "pull", err)
				return
			}
			m.emitJobEvent(jobID, action, tpl.id, "pull", "Image ready", 70, "done")
		}

		existing, err := client.InspectContainer(ctx, tpl.containerName)
		if err != nil {
			m.failJob(jobID, action, tpl.id, "inspect", err)
			return
		}
		if existing != nil && action == "pull" {
			_ = client.StopContainer(ctx, tpl.containerName, 15)
			_ = client.RemoveContainer(ctx, tpl.containerName, true)
			existing = nil
		}

		if existing == nil {
			m.emitJobEvent(jobID, action, tpl.id, "create", "Creating and starting "+tpl.name+"...", 75, "running")
			id, err := client.CreateContainer(ctx, spec)
			if err != nil {
				m.failJob(jobID, action, tpl.id, "create", err)
				return
			}
			if err := client.StartContainer(ctx, id); err != nil {
				m.failJob(jobID, action, tpl.id, "start", err)
				return
			}
		} else if !existing.Running {
			m.emitJobEvent(jobID, action, tpl.id, "start", "Starting existing "+tpl.name+" container...", 75, "running")
			if err := client.StartContainer(ctx, existing.ID); err != nil {
				m.failJob(jobID, action, tpl.id, "start", err)
				return
			}
		}

		m.waitSidecarProbe(ctx, jobID, action, tpl.id, endpoint)

		// 自动回填 system_settings 中对应引擎的 URL 与托管来源
		if m.settingsSync != nil {
			if err := m.settingsSync.ApplyExtractionManagedURL(ctx, tpl.id, endpoint); err != nil && m.logger != nil {
				m.logger.Warn("managed_sidecar_settings_sync_failed", zap.String("service", tpl.id), zap.Error(err))
			}
		}
		m.syncActiveCompose(ctx, cfg, client)
		m.recordAudit(ctx, actorUserID, "infra.service."+action, "managed_service", tpl.id, map[string]any{"endpoint": endpoint, "image": spec.Image})
		m.completeJob(jobID, action, tpl.id, tpl.name+" is running at "+endpoint)
	}
}

func (m *Manager) waitSidecarProbe(ctx context.Context, jobID, action, serviceID, endpoint string) {
	m.emitJobEvent(jobID, action, serviceID, "health", "Waiting for HTTP endpoint "+endpoint+"...", 85, "running")
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if m.probeSidecar(ctx, serviceID, endpoint) {
			m.emitJobEvent(jobID, action, serviceID, "health", "Endpoint healthy", 95, "done")
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(1 * time.Second):
		}
	}
	m.emitJobEvent(jobID, action, serviceID, "health", "Container started; endpoint still warming up", 95, "running")
}

func buildSidecarConfig(cfg config.Config, tpl sidecarTemplate, port int, opts ServiceActionOptions) managedports.SidecarContainerConfig {
	env := append([]string(nil), tpl.defaultEnv...)
	if tpl.id == managedports.OptionalServiceTesseract && strings.TrimSpace(opts.TesseractLang) != "" {
		env = []string{"TESSERACT_LANG=" + strings.TrimSpace(opts.TesseractLang), "TESSERACT_PSM=3", "TESSERACT_OEM=3"}
	}
	dataDir := ""
	if tpl.containerDataDir != "" {
		dataDir = filepath.Join(cfg.EffectiveManagedServicesDir(), tpl.id)
	}
	return managedports.SidecarContainerConfig{
		ServiceID:        tpl.id,
		ContainerName:    tpl.containerName,
		Image:            tpl.defaultImage,
		RegistryMirror:   cfg.ManagedRegistryMirror,
		Network:          cfg.EffectiveManagedNetwork(),
		HostPort:         port,
		ContainerPort:    tpl.containerPort,
		DataDir:          dataDir,
		ContainerDataDir: tpl.containerDataDir,
		Env:              env,
		MemoryLimitMB:    opts.MemoryLimitMB,
	}
}

func (m *Manager) buildCoreSpec(cfg config.Config, serviceID string) managedports.ContainerSpec {
	if serviceID == managedports.CoreServicePostgres {
		user, password, dbName := ParsePostgresDSN(cfg.PostgresDSN)
		return m.hostOps.PostgresSpec(managedports.PostgresContainerConfig{
			Image:          cfg.ManagedPostgresImage,
			RegistryMirror: cfg.ManagedRegistryMirror,
			Network:        cfg.EffectiveManagedNetwork(),
			Port:           cfg.ManagedPostgresPort,
			DataDir:        cfg.EffectiveManagedPostgresDataDir(),
			User:           user,
			Password:       password,
			Database:       dbName,
			MemoryLimitMB:  cfg.ManagedPostgresMemoryLimitMB,
		})
	}
	return m.hostOps.RedisSpec(managedports.RedisContainerConfig{
		Image:          cfg.ManagedRedisImage,
		RegistryMirror: cfg.ManagedRegistryMirror,
		Network:        cfg.EffectiveManagedNetwork(),
		Port:           cfg.ManagedRedisPort,
		DataDir:        cfg.EffectiveManagedRedisDataDir(),
		Password:       cfg.RedisPassword,
		MemoryLimitMB:  cfg.ManagedRedisMemoryLimitMB,
	})
}

func (m *Manager) ensureSingleCore(ctx context.Context, cfg config.Config, client managedports.DockerClient, serviceID, jobID, action string) error {
	spec := m.buildCoreSpec(cfg, serviceID)
	onProgress := func(evt ProgressEvent) {
		m.emitJobEvent(jobID, action, serviceID, evt.Step, evt.Message, evt.Percent, evt.Status)
	}
	if serviceID == managedports.CoreServicePostgres {
		user, _, dbName := ParsePostgresDSN(cfg.PostgresDSN)
		return EnsurePostgres(ctx, client, spec, user, dbName, onProgress)
	}
	return EnsureRedis(ctx, client, spec, cfg.RedisPassword, onProgress)
}

func (m *Manager) waitCoreReady(ctx context.Context, cfg config.Config, client managedports.DockerClient, serviceID string) error {
	if serviceID == managedports.CoreServicePostgres {
		user, _, dbName := ParsePostgresDSN(cfg.PostgresDSN)
		return waitForPostgresReady(ctx, client, managedports.PostgresContainerName, user, dbName, coreReadyTimeout)
	}
	return waitForRedisReady(ctx, client, managedports.RedisContainerName, cfg.RedisPassword, coreReadyTimeout)
}

func (m *Manager) syncActiveCompose(ctx context.Context, cfg config.Config, client managedports.DockerClient) {
	if !cfg.ManagedComposeSync || m.hostOps == nil {
		return
	}
	state := managedports.ComposeState{
		Network:        cfg.EffectiveManagedNetwork(),
		RegistryMirror: cfg.ManagedRegistryMirror,
	}
	if cfg.ManagedPostgresEnabled {
		user, password, dbName := ParsePostgresDSN(cfg.PostgresDSN)
		pgCfg := managedports.PostgresContainerConfig{
			Image:          cfg.ManagedPostgresImage,
			RegistryMirror: cfg.ManagedRegistryMirror,
			Network:        cfg.EffectiveManagedNetwork(),
			Port:           cfg.ManagedPostgresPort,
			DataDir:        cfg.EffectiveManagedPostgresDataDir(),
			User:           user,
			Password:       password,
			Database:       dbName,
			MemoryLimitMB:  cfg.ManagedPostgresMemoryLimitMB,
		}
		state.Postgres = &pgCfg
	}
	if cfg.ManagedRedisEnabled && strings.EqualFold(strings.TrimSpace(cfg.CacheDriver), "redis") {
		rdCfg := managedports.RedisContainerConfig{
			Image:          cfg.ManagedRedisImage,
			RegistryMirror: cfg.ManagedRegistryMirror,
			Network:        cfg.EffectiveManagedNetwork(),
			Port:           cfg.ManagedRedisPort,
			DataDir:        cfg.EffectiveManagedRedisDataDir(),
			Password:       cfg.RedisPassword,
			MemoryLimitMB:  cfg.ManagedRedisMemoryLimitMB,
		}
		state.Redis = &rdCfg
	}
	if client == nil {
		return
	}
	if err := collectComposeSidecars(ctx, cfg, client, &state); err != nil {
		if m.logger != nil {
			m.logger.Warn("inspect_compose_sidecars_failed", zap.Error(err))
		}
		return
	}
	if state.Postgres == nil && state.Redis == nil && len(state.Sidecars) == 0 {
		return
	}
	if _, _, err := m.hostOps.SyncComposeFiles(cfg.EffectiveManagedDataDir(), state); err != nil && m.logger != nil {
		m.logger.Warn("sync_compose_files_failed", zap.Error(err))
	}
}

func collectComposeSidecars(ctx context.Context, cfg config.Config, client managedports.DockerClient, state *managedports.ComposeState) error {
	for _, tpl := range sidecarTemplates {
		ctr, err := client.InspectContainer(ctx, tpl.containerName)
		if err != nil {
			return err
		}
		if ctr == nil || ctr.Labels[managedports.ManagedLabelKey] != "true" || ctr.Labels[managedports.ServiceLabelKey] != tpl.id {
			continue
		}
		port := ctr.HostPorts[fmt.Sprintf("%d/tcp", tpl.containerPort)]
		if port <= 0 {
			return fmt.Errorf("missing port binding for %s", tpl.containerName)
		}
		sc := buildSidecarConfig(cfg, tpl, port, ServiceActionOptions{MemoryLimitMB: int(ctr.MemoryBytes / (1024 * 1024)), TesseractLang: ctr.Env["TESSERACT_LANG"]})
		sc.Image = ctr.Image
		sc.RegistryMirror = ""
		sc.DataDir = ctr.BindMounts[tpl.containerDataDir]
		state.Sidecars = append(state.Sidecars, sc)
	}
	return nil
}

// TailServiceLogs 读取白名单托管容器最近 tail 行日志。
func (m *Manager) TailServiceLogs(ctx context.Context, serviceID string, tail int) (string, error) {
	containerName, ok := resolveContainerName(serviceID)
	if !ok {
		return "", fmt.Errorf("unknown managed service %q", serviceID)
	}
	cfg := m.snapshotConfig()
	client, err := m.hostOps.NewDockerClient(cfg.EffectiveManagedDockerHost())
	if err != nil {
		return "", err
	}
	return client.TailLogs(ctx, containerName, tail)
}

// ReadComposeYAML 返回托管根目录下同步落盘的 docker-compose.yml 内容（不含明文密码）。
func (m *Manager) ReadComposeYAML() (string, error) {
	cfg := m.snapshotConfig()
	return m.hostOps.ReadComposeFile(cfg.EffectiveManagedDataDir())
}

// GetJob 返回指定异步任务的最新快照。
func (m *Manager) GetJob(jobID string) (JobSnapshot, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	snap, ok := m.jobs[jobID]
	if !ok {
		return JobSnapshot{}, false
	}
	copied := *snap
	copied.Events = append([]JobEvent(nil), snap.Events...)
	return copied, true
}

// SubscribeJob 订阅指定任务的 SSE 进度事件（先回放历史再持续推送）。
func (m *Manager) SubscribeJob(ctx context.Context, jobID string) (<-chan JobEvent, func(), bool) {
	m.mu.Lock()
	snap, ok := m.jobs[jobID]
	if !ok {
		m.mu.Unlock()
		return nil, nil, false
	}
	history := append([]JobEvent(nil), snap.Events...)
	ch := make(chan JobEvent, 64)
	if m.subscribers[jobID] == nil {
		m.subscribers[jobID] = map[chan JobEvent]struct{}{}
	}
	m.subscribers[jobID][ch] = struct{}{}
	m.mu.Unlock()

	out := make(chan JobEvent, 64)
	go func() {
		defer close(out)
		for _, evt := range history {
			select {
			case out <- evt:
			case <-ctx.Done():
				return
			}
		}
		for {
			select {
			case evt, ok := <-ch:
				if !ok {
					return
				}
				select {
				case out <- evt:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	cancel := func() {
		m.mu.Lock()
		if subs := m.subscribers[jobID]; subs != nil {
			if _, exists := subs[ch]; exists {
				delete(subs, ch)
				close(ch)
			}
		}
		m.mu.Unlock()
	}
	return out, cancel, true
}

func (m *Manager) createJob(action, serviceID string) string {
	jobID := conv.NormalizePublicID(uuid.NewString())
	m.mu.Lock()
	m.jobs[jobID] = &JobSnapshot{
		JobID:     jobID,
		Action:    action,
		ServiceID: serviceID,
		State:     "running",
		Events:    []JobEvent{},
	}
	m.mu.Unlock()
	return jobID
}

func (m *Manager) emitJobEvent(jobID, action, serviceID, step, msg string, pct int, status string) {
	evt := JobEvent{
		JobID:     jobID,
		Action:    action,
		ServiceID: serviceID,
		Step:      step,
		Message:   msg,
		Percent:   pct,
		Status:    status,
		Timestamp: time.Now().UTC(),
	}
	m.mu.Lock()
	if snap, ok := m.jobs[jobID]; ok {
		snap.Events = append(snap.Events, evt)
	}
	for ch := range m.subscribers[jobID] {
		select {
		case ch <- evt:
		default:
		}
	}
	m.mu.Unlock()
}

func (m *Manager) completeJob(jobID, action, serviceID, msg string) {
	m.mu.Lock()
	if snap, ok := m.jobs[jobID]; ok {
		snap.State = "done"
	}
	m.mu.Unlock()
	m.emitJobEvent(jobID, action, serviceID, "complete", msg, 100, "done")
}

func (m *Manager) failJob(jobID, action, serviceID, step string, err error) {
	msg := err.Error()
	m.mu.Lock()
	if snap, ok := m.jobs[jobID]; ok {
		snap.State = "failed"
		snap.Error = msg
	}
	m.mu.Unlock()
	m.emitJobEvent(jobID, action, serviceID, step, msg, 0, "failed")
}

func (m *Manager) recordAudit(ctx context.Context, actorUserID uint, action, resource, resourceID string, detail any) {
	if m.security != nil {
		m.security.RecordAudit(ctx, actorUserID, action, resource, resourceID, detail)
	}
}

func findSidecarTemplate(id string) (sidecarTemplate, bool) {
	for _, tpl := range sidecarTemplates {
		if tpl.id == id {
			return tpl, true
		}
	}
	return sidecarTemplate{}, false
}

func resolveContainerName(serviceID string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(serviceID)) {
	case managedports.CoreServicePostgres:
		return managedports.PostgresContainerName, true
	case managedports.CoreServiceRedis:
		return managedports.RedisContainerName, true
	default:
		if tpl, ok := findSidecarTemplate(strings.ToLower(strings.TrimSpace(serviceID))); ok {
			return tpl.containerName, true
		}
		return "", false
	}
}
