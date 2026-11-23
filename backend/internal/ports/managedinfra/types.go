// Package managedinfra 定义 FireCortex 自托管 Docker 基础设施的端口契约与值对象。
// 本包仅依赖 Go 标准库，供 application 与 infra/dockermgr 双向解耦实现。
package managedinfra

import (
	"context"
	"io"
	"time"
)

const (
	// ManagedLabelKey 标记所有由 FireCortex 托管的 Docker 资源（网络与容器）。
	ManagedLabelKey = "com.firecortex.managed"
	// ServiceLabelKey 标记托管容器对应的白名单服务 ID。
	ServiceLabelKey = "com.firecortex.service"

	// 核心服务 ID 与容器名。
	CoreServicePostgres   = "postgres"
	PostgresContainerName = "firecortex-postgres"
	CoreServiceRedis      = "redis"
	RedisContainerName    = "firecortex-redis"

	// 可选旁路服务 ID 与容器名。
	OptionalServiceTika      = "tika"
	TikaContainerName        = "firecortex-tika"
	OptionalServiceDocling   = "docling"
	DoclingContainerName     = "firecortex-docling"
	OptionalServiceTesseract = "tesseract"
	TesseractContainerName   = "firecortex-tesseract"
	OptionalServiceRapidOCR  = "rapidocr"
	RapidOCRContainerName    = "firecortex-rapidocr"

	// 默认可选旁路服务镜像与端口。
	DefaultTikaImage      = "apache/tika:3.2.3.0"
	DefaultDoclingImage   = "ghcr.io/lieyanc/firecortex-docling:latest"
	DefaultTesseractImage = "ghcr.io/lieyanc/firecortex-tesseract:latest"
	DefaultRapidOCRImage  = "ghcr.io/lieyanc/firecortex-rapidocr:latest"

	// DirStatusEmpty 等常量描述托管目录预检状态分类。
	DirStatusEmpty           = "empty"
	DirStatusAdoptablePGData = "adoptable_pgdata"
	DirStatusManagedExisting = "managed_existing"
	DirStatusInvalid         = "invalid"
)

// DaemonInfo 描述宿主机 Docker Engine 的基础信息。
type DaemonInfo struct {
	Available         bool
	Host              string
	ServerVersion     string
	APIVersion        string
	OSType            string
	Architecture      string
	MemTotalBytes     int64
	ContainersRunning int
}

// PullProgress 描述单次镜像拉取的流式进度事件。
type PullProgress struct {
	Status  string
	LayerID string
	Current int64
	Total   int64
	Percent int
}

// ContainerInfo 描述已存在容器的运行状态与元数据。
type ContainerInfo struct {
	ID          string
	Name        string
	Image       string
	Status      string
	Running     bool
	Health      string
	Env         map[string]string
	HostPorts   map[string]int
	BindMounts  map[string]string
	MemoryBytes int64
	Labels      map[string]string
}

// ContainerStats 描述容器实时 CPU 与内存占用。
type ContainerStats struct {
	CPUPercent       float64
	MemoryUsageBytes uint64
	MemoryLimitBytes uint64
}

// PortBinding 描述宿主机端口到容器端口的绑定。
type PortBinding struct {
	HostIP        string
	HostPort      int
	ContainerPort int
	Protocol      string
}

// HealthcheckSpec 描述容器内置健康检查参数。
type HealthcheckSpec struct {
	Test        []string
	Interval    time.Duration
	Timeout     time.Duration
	StartPeriod time.Duration
	Retries     int
}

// ContainerSpec 描述要创建的托管容器声明。
type ContainerSpec struct {
	Name          string
	Image         string
	Network       string
	NetworkAlias  string
	Env           []string
	Cmd           []string
	Binds         []string
	Ports         []PortBinding
	Labels        map[string]string
	RestartPolicy string
	MemoryBytes   int64
	Healthcheck   *HealthcheckSpec
}

// ExecResult 保存容器内执行命令的标准输出、标准错误和退出码。
type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// DirInspection 保存对用户指定托管数据目录的安全检查结果。
type DirInspection struct {
	ResolvedRoot       string
	PostgresDataDir    string
	RedisDataDir       string
	BackupDir          string
	Valid              bool
	Status             string
	Reason             string
	PGVersion          string
	HasManagedMarker   bool
	HasExistingCompose bool
	DiskFreeBytes      uint64
	DiskTotalBytes     uint64
	Warnings           []string
}

// PostgresContainerConfig 描述托管 PostgreSQL + pgvector 容器的声明参数。
type PostgresContainerConfig struct {
	Image          string
	RegistryMirror string
	Network        string
	Port           int
	DataDir        string
	User           string
	Password       string
	Database       string
	MemoryLimitMB  int
}

// RedisContainerConfig 描述托管 Redis 容器的声明参数。
type RedisContainerConfig struct {
	Image          string
	RegistryMirror string
	Network        string
	Port           int
	DataDir        string
	Password       string
	MemoryLimitMB  int
}

// SidecarContainerConfig 描述受控可选旁路服务（Tika / Docling / Tesseract / RapidOCR）的容器声明。
type SidecarContainerConfig struct {
	ServiceID        string
	ContainerName    string
	Image            string
	RegistryMirror   string
	Network          string
	HostPort         int
	ContainerPort    int
	DataDir          string
	ContainerDataDir string
	Env              []string
	MemoryLimitMB    int
}

// ComposeState 描述当前需要同步落盘到 docker-compose.yml 与 .env 的托管服务声明。
type ComposeState struct {
	Network        string
	RegistryMirror string
	Postgres       *PostgresContainerConfig
	Redis          *RedisContainerConfig
	Sidecars       []SidecarContainerConfig
}

// PostgresProbeResult 描述外部或托管 PostgreSQL 连通性与 pgvector 扩展探测结果。
type PostgresProbeResult struct {
	ServerVersion   string
	VectorInstalled bool
	VectorAvailable bool
}

// RedisProbeInput 描述只读探测外部 Redis 所需的参数。
type RedisProbeInput struct {
	Addr               string
	Username           string
	Password           string
	DB                 int
	TLSEnabled         bool
	InsecureSkipVerify bool
}

// InitDBAdminInput 描述 Setup 完成后执行数据库迁移与首个超级管理员初始化所需的参数。
type InitDBAdminInput struct {
	DataEncryptionKey      string
	PreviousEncryptionKeys []string
	Env                    string
	PostgresDSN            string
	AdminUsername          string
	AdminDisplayName       string
	AdminPassword          string
}

// DockerClient 抽象纯 Go Docker Engine 操作。
type DockerClient interface {
	Host() string
	Ping(ctx context.Context) (DaemonInfo, error)
	EnsureNetwork(ctx context.Context, name string) error
	InspectImage(ctx context.Context, image string) (bool, error)
	PullImage(ctx context.Context, image string, onProgress func(PullProgress)) error
	InspectContainer(ctx context.Context, name string) (*ContainerInfo, error)
	CreateContainer(ctx context.Context, spec ContainerSpec) (string, error)
	StartContainer(ctx context.Context, nameOrID string) error
	StopContainer(ctx context.Context, nameOrID string, timeoutSeconds int) error
	RestartContainer(ctx context.Context, nameOrID string, timeoutSeconds int) error
	RemoveContainer(ctx context.Context, nameOrID string, force bool) error
	ContainerStats(ctx context.Context, nameOrID string) (ContainerStats, error)
	TailLogs(ctx context.Context, nameOrID string, tail int) (string, error)
	Exec(ctx context.Context, nameOrID string, cmd []string, env []string) (ExecResult, error)
	ExecStreamStdout(ctx context.Context, nameOrID string, cmd []string, env []string, stdout io.Writer) (stderr string, exitCode int, err error)
	ExecWithStdin(ctx context.Context, nameOrID string, cmd []string, env []string, stdin io.Reader) (ExecResult, error)
}

// HostOps 抽象宿主机 Docker 客户端构造、目录保护、容器模板与 Compose 落盘能力。
type HostOps interface {
	NewDockerClient(host string) (DockerClient, error)
	InspectManagedRoot(rawRoot, pgOverride, redisOverride, backupOverride string) DirInspection
	PrepareManagedDirs(rootDir, pgDir, redisDir, backupDir, servicesDir string) error
	SyncComposeFiles(rootDir string, state ComposeState) (composePath string, envPath string, err error)
	ReadComposeFile(rootDir string) (string, error)
	ReadExistingComposeEnv(rootDir string) map[string]string
	PostgresSpec(cfg PostgresContainerConfig) ContainerSpec
	RedisSpec(cfg RedisContainerConfig) ContainerSpec
	SidecarSpec(cfg SidecarContainerConfig) ContainerSpec
}

// SetupInfraPort 抽象引导向导与高危运维所需的数据库/缓存探测与初始化能力。
type SetupInfraPort interface {
	ProbePostgres(ctx context.Context, dsn string) (PostgresProbeResult, error)
	ProbeRedis(ctx context.Context, input RedisProbeInput) error
	InitDBAndAdmin(ctx context.Context, input InitDBAdminInput) (adoptedExistingAdmin bool, err error)
}

// ServiceProber 抽象可选旁路提取/OCR 服务的端点健康探测能力。
type ServiceProber interface {
	ProbeTika(ctx context.Context, baseURL, authToken string) (bool, string)
	ProbeDocling(ctx context.Context, baseURL, authToken string) (bool, string)
	ProbeOCR(ctx context.Context, baseURL, authToken string) (bool, string)
	ProbeRapidOCR(ctx context.Context, baseURL, authToken string) (bool, string)
}

// AdminSecurityPort 抽象高危操作所需的 superadmin 密码校验与审计记录能力。
type AdminSecurityPort interface {
	VerifySuperAdminPassword(ctx context.Context, userID uint, currentPassword string) error
	RecordAudit(ctx context.Context, actorUserID uint, action, resource, resourceID string, detail any)
}

// SettingsSyncPort 抽象托管旁路服务启停后自动同步 system_settings 的能力。
type SettingsSyncPort interface {
	ApplyExtractionManagedURL(ctx context.Context, serviceID string, baseURL string) error
}

// RestartTriggerPort 抽象保存配置或执行恢复后的平滑重启触发能力。
type RestartTriggerPort interface {
	TriggerRestart(reason string)
}
