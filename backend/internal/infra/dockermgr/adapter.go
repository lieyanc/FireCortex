package dockermgr

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	managedports "github.com/lieyanc/FireCortex/backend/internal/ports/managedinfra"
)

// 类型别名将 dockermgr 与 ports/managedinfra 保持完全零拷贝一致。
type (
	DaemonInfo              = managedports.DaemonInfo
	PullProgress            = managedports.PullProgress
	ContainerInfo           = managedports.ContainerInfo
	ContainerStats          = managedports.ContainerStats
	PortBinding             = managedports.PortBinding
	HealthcheckSpec         = managedports.HealthcheckSpec
	ContainerSpec           = managedports.ContainerSpec
	ExecResult              = managedports.ExecResult
	DirInspection           = managedports.DirInspection
	PostgresContainerConfig = managedports.PostgresContainerConfig
	RedisContainerConfig    = managedports.RedisContainerConfig
	SidecarContainerConfig  = managedports.SidecarContainerConfig
	ComposeState            = managedports.ComposeState
	Client                  = managedports.DockerClient
)

const (
	ManagedLabelKey          = managedports.ManagedLabelKey
	ServiceLabelKey          = managedports.ServiceLabelKey
	CoreServicePostgres      = managedports.CoreServicePostgres
	PostgresContainerName    = managedports.PostgresContainerName
	CoreServiceRedis         = managedports.CoreServiceRedis
	RedisContainerName       = managedports.RedisContainerName
	OptionalServiceTika      = managedports.OptionalServiceTika
	TikaContainerName        = managedports.TikaContainerName
	OptionalServiceDocling   = managedports.OptionalServiceDocling
	DoclingContainerName     = managedports.DoclingContainerName
	OptionalServiceTesseract = managedports.OptionalServiceTesseract
	TesseractContainerName   = managedports.TesseractContainerName
	OptionalServiceRapidOCR  = managedports.OptionalServiceRapidOCR
	RapidOCRContainerName    = managedports.RapidOCRContainerName
	DirStatusEmpty           = managedports.DirStatusEmpty
	DirStatusAdoptablePGData = managedports.DirStatusAdoptablePGData
	DirStatusManagedExisting = managedports.DirStatusManagedExisting
	DirStatusInvalid         = managedports.DirStatusInvalid
)

// HostAdapter 实现 ports/managedinfra.HostOps，供组合根（app）注入给 application 层。
type HostAdapter struct{}

// NewHostOps 创建默认宿主机 Docker 与落盘适配器。
func NewHostOps() *HostAdapter {
	return &HostAdapter{}
}

func (*HostAdapter) NewDockerClient(host string) (managedports.DockerClient, error) {
	return NewClient(host)
}

func (*HostAdapter) InspectManagedRoot(rawRoot, pgOverride, redisOverride, backupOverride string) managedports.DirInspection {
	return InspectManagedRoot(rawRoot, pgOverride, redisOverride, backupOverride)
}

func (*HostAdapter) PrepareManagedDirs(rootDir, pgDir, redisDir, backupDir, servicesDir string) error {
	return PrepareManagedDirs(rootDir, pgDir, redisDir, backupDir, servicesDir)
}

func (*HostAdapter) SyncComposeFiles(rootDir string, state managedports.ComposeState) (string, string, error) {
	return SyncComposeFiles(rootDir, state)
}

func (*HostAdapter) ReadComposeFile(rootDir string) (string, error) {
	return ReadComposeFile(rootDir)
}

func (*HostAdapter) ReadExistingComposeEnv(rootDir string) map[string]string {
	return ReadExistingComposeEnv(rootDir)
}

func (*HostAdapter) PostgresSpec(cfg managedports.PostgresContainerConfig) managedports.ContainerSpec {
	return PostgresSpec(cfg)
}

func (*HostAdapter) RedisSpec(cfg managedports.RedisContainerConfig) managedports.ContainerSpec {
	return RedisSpec(cfg)
}

func (*HostAdapter) SidecarSpec(cfg managedports.SidecarContainerConfig) managedports.ContainerSpec {
	return SidecarSpec(cfg)
}

// ReadExistingComposeEnv 读取托管目录下已存在的 .env 文件，便于接管已有数据目录时自动复用原凭据。
func ReadExistingComposeEnv(rootDir string) map[string]string {
	result := map[string]string{}
	file, err := os.Open(filepath.Join(rootDir, ComposeEnvFileName))
	if err != nil {
		return result
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && ((v[0] == '\'' && v[len(v)-1] == '\'') || (v[0] == '"' && v[len(v)-1] == '"')) {
			v = v[1 : len(v)-1]
			v = strings.ReplaceAll(v, `'\''`, `'`)
		}
		result[strings.TrimSpace(k)] = v
	}
	return result
}
