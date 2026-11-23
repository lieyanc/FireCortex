package setup

import (
	"time"

	appsetup "github.com/lieyanc/FireCortex/backend/internal/application/setup"
	managedports "github.com/lieyanc/FireCortex/backend/internal/ports/managedinfra"
)

// DaemonInfoDTO 描述宿主机 Docker Engine 基础信息。
type DaemonInfoDTO struct {
	Available         bool   `json:"available"`
	Host              string `json:"host"`
	ServerVersion     string `json:"serverVersion"`
	APIVersion        string `json:"apiVersion"`
	OSType            string `json:"osType"`
	Architecture      string `json:"architecture"`
	MemTotalBytes     int64  `json:"memTotalBytes"`
	ContainersRunning int    `json:"containersRunning"`
}

// DirInspectionDTO 保存对用户指定托管数据目录的安全检查结果。
type DirInspectionDTO struct {
	ResolvedRoot       string   `json:"resolvedRoot"`
	PostgresDataDir    string   `json:"postgresDataDir"`
	RedisDataDir       string   `json:"redisDataDir"`
	BackupDir          string   `json:"backupDir"`
	Valid              bool     `json:"valid"`
	Status             string   `json:"status"`
	Reason             string   `json:"reason,omitempty"`
	PGVersion          string   `json:"pgVersion,omitempty"`
	HasManagedMarker   bool     `json:"hasManagedMarker"`
	HasExistingCompose bool     `json:"hasExistingCompose"`
	DiskFreeBytes      uint64   `json:"diskFreeBytes"`
	DiskTotalBytes     uint64   `json:"diskTotalBytes"`
	Warnings           []string `json:"warnings,omitempty"`
}

// SetupDefaultsDTO 描述向导表单的初始推荐值。
type SetupDefaultsDTO struct {
	HTTPPort       string `json:"httpPort"`
	DockerHost     string `json:"dockerHost"`
	Network        string `json:"network"`
	DataDir        string `json:"dataDir"`
	RegistryMirror string `json:"registryMirror"`
	PostgresImage  string `json:"postgresImage"`
	PostgresPort   int    `json:"postgresPort"`
	RedisImage     string `json:"redisImage"`
	RedisPort      int    `json:"redisPort"`
}

// SetupStatusResponse 描述当前实例是否处于首次引导模式及会话认证状态。
type SetupStatusResponse struct {
	NeedsSetup    bool              `json:"needsSetup"`
	Authenticated bool              `json:"authenticated"`
	Version       string            `json:"version"`
	JobState      string            `json:"jobState"`
	Defaults      *SetupDefaultsDTO `json:"defaults,omitempty"`
}

// SetupStatusResponseDoc Swagger 文档包装。
type SetupStatusResponseDoc struct {
	ErrorMsg string              `json:"errorMsg"`
	Data     SetupStatusResponse `json:"data"`
}

// VerifyTokenRequest 提交一次性 Setup Token。
type VerifyTokenRequest struct {
	Token string `json:"token" binding:"required"`
}

// VerifyTokenResponse 令牌验证成功响应。
type VerifyTokenResponse struct {
	Authenticated bool             `json:"authenticated"`
	Defaults      SetupDefaultsDTO `json:"defaults"`
}

// VerifyTokenResponseDoc Swagger 文档包装。
type VerifyTokenResponseDoc struct {
	ErrorMsg string              `json:"errorMsg"`
	Data     VerifyTokenResponse `json:"data"`
}

// PreflightResponse 描述宿主机环境体检结果。
type PreflightResponse struct {
	Docker                DaemonInfoDTO    `json:"docker"`
	DockerError           string           `json:"dockerError,omitempty"`
	DefaultDir            DirInspectionDTO `json:"defaultDir"`
	PostgresPortAvailable bool             `json:"postgresPortAvailable"`
	PostgresPortMessage   string           `json:"postgresPortMessage,omitempty"`
	RedisPortAvailable    bool             `json:"redisPortAvailable"`
	RedisPortMessage      string           `json:"redisPortMessage,omitempty"`
	Defaults              SetupDefaultsDTO `json:"defaults"`
}

// PreflightResponseDoc Swagger 文档包装。
type PreflightResponseDoc struct {
	ErrorMsg string            `json:"errorMsg"`
	Data     PreflightResponse `json:"data"`
}

// InspectDirRequest 提交待校验的宿主机托管目录路径。
type InspectDirRequest struct {
	DataDir         string `json:"dataDir" binding:"required"`
	PostgresDataDir string `json:"postgresDataDir"`
	RedisDataDir    string `json:"redisDataDir"`
	BackupDir       string `json:"backupDir"`
}

// InspectDirResponseDoc Swagger 文档包装。
type InspectDirResponseDoc struct {
	ErrorMsg string           `json:"errorMsg"`
	Data     DirInspectionDTO `json:"data"`
}

// TestExternalRequest 测试外部 PostgreSQL / Redis 连通性。
type TestExternalRequest struct {
	PostgresDSN          string `json:"postgresDSN" binding:"required"`
	CacheDriver          string `json:"cacheDriver"`
	RedisAddr            string `json:"redisAddr"`
	RedisUsername        string `json:"redisUsername"`
	RedisPassword        string `json:"redisPassword"`
	RedisDB              int    `json:"redisDB"`
	RedisTLSEnabled      bool   `json:"redisTLSEnabled"`
	RedisTLSInsecureSkip bool   `json:"redisTLSInsecureSkip"`
}

// TestExternalResponse 描述外部依赖只读探测结果。
type TestExternalResponse struct {
	PostgresOK              bool   `json:"postgresOK"`
	PostgresVersion         string `json:"postgresVersion,omitempty"`
	PostgresVectorInstalled bool   `json:"postgresVectorInstalled"`
	PostgresVectorAvailable bool   `json:"postgresVectorAvailable"`
	PostgresError           string `json:"postgresError,omitempty"`
	RedisChecked            bool   `json:"redisChecked"`
	RedisOK                 bool   `json:"redisOK"`
	RedisError              string `json:"redisError,omitempty"`
}

// TestExternalResponseDoc Swagger 文档包装。
type TestExternalResponseDoc struct {
	ErrorMsg string               `json:"errorMsg"`
	Data     TestExternalResponse `json:"data"`
}

// ApplyRequest 提交完整的首次初始化配置。
type ApplyRequest struct {
	ExistingDataEncryptionKey string `json:"existingDataEncryptionKey,omitempty"`
	DatabaseMode              string `json:"databaseMode"` // managed | external
	CacheMode                 string `json:"cacheMode"`    // managed_redis | external_redis | memory
	DockerHost                string `json:"dockerHost"`
	RegistryMirror            string `json:"registryMirror"`
	DataDir                   string `json:"dataDir"`
	PostgresDataDir           string `json:"postgresDataDir"`
	RedisDataDir              string `json:"redisDataDir"`
	BackupDir                 string `json:"backupDir"`
	PostgresImage             string `json:"postgresImage"`
	PostgresPort              int    `json:"postgresPort"`
	PostgresMemoryLimitMB     int    `json:"postgresMemoryLimitMB"`
	RedisImage                string `json:"redisImage"`
	RedisPort                 int    `json:"redisPort"`
	RedisMemoryLimitMB        int    `json:"redisMemoryLimitMB"`
	AdoptExistingPGData       bool   `json:"adoptExistingPGData"`
	ExistingPGUser            string `json:"existingPGUser"`
	ExistingPGPassword        string `json:"existingPGPassword"`
	ExistingPGDatabase        string `json:"existingPGDatabase"`
	ExistingRedisPassword     string `json:"existingRedisPassword"`
	ComposeSync               *bool  `json:"composeSync"`

	ExternalPostgresDSN  string `json:"externalPostgresDSN"`
	ExternalRedisAddr    string `json:"externalRedisAddr"`
	ExternalRedisUser    string `json:"externalRedisUser"`
	ExternalRedisPass    string `json:"externalRedisPass"`
	ExternalRedisDB      int    `json:"externalRedisDB"`
	ExternalRedisTLS     bool   `json:"externalRedisTLS"`
	ExternalRedisSkipTLS bool   `json:"externalRedisSkipTLS"`

	Env              string `json:"env"`
	PublicBaseURL    string `json:"publicBaseURL"`
	CORSAllowOrigin  string `json:"corsAllowOrigin"`
	AdminUsername    string `json:"adminUsername"`
	AdminDisplayName string `json:"adminDisplayName"`
	AdminPassword    string `json:"adminPassword"`
}

// JobEventResponse 描述初始化异步进度事件。
type JobEventResponse struct {
	JobID     string    `json:"jobID"`
	Step      string    `json:"step"`
	Service   string    `json:"service,omitempty"`
	Message   string    `json:"message"`
	Percent   int       `json:"percent"`
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}

// JobSnapshotResponse 描述异步初始化任务的完整快照。
type JobSnapshotResponse struct {
	JobID       string             `json:"jobID"`
	State       string             `json:"state"`
	Error       string             `json:"error,omitempty"`
	ComposePath string             `json:"composePath,omitempty"`
	ConfigPath  string             `json:"configPath,omitempty"`
	AdoptedDB   bool               `json:"adoptedDB"`
	Events      []JobEventResponse `json:"events"`
}

// JobSnapshotResponseDoc Swagger 文档包装。
type JobSnapshotResponseDoc struct {
	ErrorMsg string              `json:"errorMsg"`
	Data     JobSnapshotResponse `json:"data"`
}

func toDaemonInfoDTO(in managedports.DaemonInfo) DaemonInfoDTO {
	return DaemonInfoDTO{
		Available:         in.Available,
		Host:              in.Host,
		ServerVersion:     in.ServerVersion,
		APIVersion:        in.APIVersion,
		OSType:            in.OSType,
		Architecture:      in.Architecture,
		MemTotalBytes:     in.MemTotalBytes,
		ContainersRunning: in.ContainersRunning,
	}
}

func toDirInspectionDTO(in managedports.DirInspection) DirInspectionDTO {
	return DirInspectionDTO{
		ResolvedRoot:       in.ResolvedRoot,
		PostgresDataDir:    in.PostgresDataDir,
		RedisDataDir:       in.RedisDataDir,
		BackupDir:          in.BackupDir,
		Valid:              in.Valid,
		Status:             in.Status,
		Reason:             in.Reason,
		PGVersion:          in.PGVersion,
		HasManagedMarker:   in.HasManagedMarker,
		HasExistingCompose: in.HasExistingCompose,
		DiskFreeBytes:      in.DiskFreeBytes,
		DiskTotalBytes:     in.DiskTotalBytes,
		Warnings:           in.Warnings,
	}
}

func toSetupDefaultsDTO(in appsetup.SetupDefaults) SetupDefaultsDTO {
	return SetupDefaultsDTO{
		HTTPPort:       in.HTTPPort,
		DockerHost:     in.DockerHost,
		Network:        in.Network,
		DataDir:        in.DataDir,
		RegistryMirror: in.RegistryMirror,
		PostgresImage:  in.PostgresImage,
		PostgresPort:   in.PostgresPort,
		RedisImage:     in.RedisImage,
		RedisPort:      in.RedisPort,
	}
}

func toPreflightResponse(in appsetup.PreflightResult) PreflightResponse {
	return PreflightResponse{
		Docker:                toDaemonInfoDTO(in.Docker),
		DockerError:           in.DockerError,
		DefaultDir:            toDirInspectionDTO(in.DefaultDir),
		PostgresPortAvailable: in.PostgresPortAvailable,
		PostgresPortMessage:   in.PostgresPortMessage,
		RedisPortAvailable:    in.RedisPortAvailable,
		RedisPortMessage:      in.RedisPortMessage,
		Defaults:              toSetupDefaultsDTO(in.Defaults),
	}
}

func toTestExternalResponse(in appsetup.TestExternalResult) TestExternalResponse {
	return TestExternalResponse{
		PostgresOK:              in.PostgresOK,
		PostgresVersion:         in.PostgresVersion,
		PostgresVectorInstalled: in.PostgresVectorInstalled,
		PostgresVectorAvailable: in.PostgresVectorAvailable,
		PostgresError:           in.PostgresError,
		RedisChecked:            in.RedisChecked,
		RedisOK:                 in.RedisOK,
		RedisError:              in.RedisError,
	}
}

func toJobEventResponse(evt appsetup.JobEvent) JobEventResponse {
	return JobEventResponse{
		JobID:     evt.JobID,
		Step:      evt.Step,
		Service:   evt.Service,
		Message:   evt.Message,
		Percent:   evt.Percent,
		Status:    evt.Status,
		Timestamp: evt.Timestamp,
	}
}

func toJobSnapshotResponse(snap appsetup.JobSnapshot) JobSnapshotResponse {
	events := make([]JobEventResponse, 0, len(snap.Events))
	for _, evt := range snap.Events {
		events = append(events, toJobEventResponse(evt))
	}
	return JobSnapshotResponse{
		JobID:       snap.JobID,
		State:       snap.State,
		Error:       snap.Error,
		ComposePath: snap.ComposePath,
		ConfigPath:  snap.ConfigPath,
		AdoptedDB:   snap.AdoptedDB,
		Events:      events,
	}
}
