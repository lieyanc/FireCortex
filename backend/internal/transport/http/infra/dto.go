package infra

import (
	"time"

	"github.com/lieyanc/FireCortex/backend/internal/application/managedinfra"
	managedports "github.com/lieyanc/FireCortex/backend/internal/ports/managedinfra"
)

// InfraDaemonInfoDTO 描述宿主机 Docker Engine 基础状态。
type InfraDaemonInfoDTO struct {
	Available         bool   `json:"available"`
	Host              string `json:"host"`
	ServerVersion     string `json:"serverVersion"`
	APIVersion        string `json:"apiVersion"`
	OSType            string `json:"osType"`
	Architecture      string `json:"architecture"`
	MemTotalBytes     int64  `json:"memTotalBytes"`
	ContainersRunning int    `json:"containersRunning"`
}

// InfraDirInfoDTO 描述托管目录与磁盘空间状态。
type InfraDirInfoDTO struct {
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

// ManagedServiceDTO 描述单个核心或可选旁路服务的运行视图。
type ManagedServiceDTO struct {
	ID               string  `json:"id"`
	Category         string  `json:"category"` // core | optional
	Name             string  `json:"name"`
	Description      string  `json:"description"`
	Mode             string  `json:"mode"` // managed | external | memory | disabled
	ContainerName    string  `json:"containerName"`
	Image            string  `json:"image"`
	Port             int     `json:"port"`
	EndpointURL      string  `json:"endpointURL,omitempty"`
	DataDir          string  `json:"dataDir,omitempty"`
	Status           string  `json:"status"`
	Health           string  `json:"health,omitempty"`
	Reachable        bool    `json:"reachable"`
	Message          string  `json:"message,omitempty"`
	CPUPercent       float64 `json:"cpuPercent"`
	MemoryUsageBytes uint64  `json:"memoryUsageBytes"`
	MemoryLimitBytes uint64  `json:"memoryLimitBytes"`
	CanStart         bool    `json:"canStart"`
	CanStop          bool    `json:"canStop"`
	CanRestart       bool    `json:"canRestart"`
	CanPull          bool    `json:"canPull"`
}

// InfraOverviewResponse 返回「服务与基础设施」页面概览。
type InfraOverviewResponse struct {
	Docker          InfraDaemonInfoDTO  `json:"docker"`
	DockerError     string              `json:"dockerError,omitempty"`
	DataDir         InfraDirInfoDTO     `json:"dataDir"`
	RegistryMirror  string              `json:"registryMirror"`
	ComposeSync     bool                `json:"composeSync"`
	BackupEnabled   bool                `json:"backupEnabled"`
	BackupSchedule  string              `json:"backupSchedule"`
	BackupRetention int                 `json:"backupRetention"`
	Services        []ManagedServiceDTO `json:"services"`
}

// InfraOverviewResponseDoc Swagger 包装。
type InfraOverviewResponseDoc struct {
	ErrorMsg string                `json:"errorMsg"`
	Data     InfraOverviewResponse `json:"data"`
}

// ServiceActionRequest 描述启停/拉取托管服务时允许的受控参数。
type ServiceActionRequest struct {
	Port          int    `json:"port"`
	MemoryLimitMB int    `json:"memoryLimitMB"`
	TesseractLang string `json:"tesseractLang"`
}

// InfraJobEventDTO 描述异步任务单条 SSE 进度事件。
type InfraJobEventDTO struct {
	JobID     string    `json:"jobID"`
	Action    string    `json:"action"`
	ServiceID string    `json:"serviceID"`
	Step      string    `json:"step"`
	Message   string    `json:"message"`
	Percent   int       `json:"percent"`
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}

// InfraJobSnapshotResponse 描述异步任务快照。
type InfraJobSnapshotResponse struct {
	JobID     string             `json:"jobID"`
	Action    string             `json:"action"`
	ServiceID string             `json:"serviceID"`
	State     string             `json:"state"`
	Error     string             `json:"error,omitempty"`
	Events    []InfraJobEventDTO `json:"events"`
}

// InfraJobSnapshotResponseDoc Swagger 包装。
type InfraJobSnapshotResponseDoc struct {
	ErrorMsg string                   `json:"errorMsg"`
	Data     InfraJobSnapshotResponse `json:"data"`
}

// ServiceLogsResponse 返回容器日志文本。
type ServiceLogsResponse struct {
	ServiceID string `json:"serviceID"`
	Logs      string `json:"logs"`
}

// ServiceLogsResponseDoc Swagger 包装。
type ServiceLogsResponseDoc struct {
	ErrorMsg string              `json:"errorMsg"`
	Data     ServiceLogsResponse `json:"data"`
}

// ComposePreviewResponse 返回只读 docker-compose.yml 预览内容。
type ComposePreviewResponse struct {
	Content string `json:"content"`
}

// ComposePreviewResponseDoc Swagger 包装。
type ComposePreviewResponseDoc struct {
	ErrorMsg string                 `json:"errorMsg"`
	Data     ComposePreviewResponse `json:"data"`
}

// BackupRecordDTO 描述一条 PostgreSQL 逻辑备份归档。
type BackupRecordDTO struct {
	ID        string    `json:"id"`
	FileName  string    `json:"fileName"`
	SizeBytes int64     `json:"sizeBytes"`
	SHA256    string    `json:"sha256"`
	Trigger   string    `json:"trigger"`
	CreatedAt time.Time `json:"createdAt"`
	Verified  bool      `json:"verified"`
}

// BackupRecordResponseDoc Swagger 包装。
type BackupRecordResponseDoc struct {
	ErrorMsg string          `json:"errorMsg"`
	Data     BackupRecordDTO `json:"data"`
}

// BackupListResponse 返回历史备份列表。
type BackupListResponse struct {
	Backups []BackupRecordDTO `json:"backups"`
}

// BackupListResponseDoc Swagger 包装。
type BackupListResponseDoc struct {
	ErrorMsg string             `json:"errorMsg"`
	Data     BackupListResponse `json:"data"`
}

// InfraConfigResponse 返回可安全在网页编辑的 config.json 脱敏视图。
type InfraConfigResponse struct {
	ConfigFile                   string `json:"configFile"`
	Env                          string `json:"env"`
	HTTPPort                     string `json:"httpPort"`
	PublicAPIBaseURL             string `json:"publicAPIBaseURL"`
	PublicWebBaseURL             string `json:"publicWebBaseURL"`
	CORSAllowOrigin              string `json:"corsAllowOrigin"`
	TrustedProxies               string `json:"trustedProxies"`
	SSRFProtectionEnabled        bool   `json:"ssrfProtectionEnabled"`
	SSRFAllowedHosts             string `json:"ssrfAllowedHosts"`
	SSRFAllowedCIDRs             string `json:"ssrfAllowedCIDRs"`
	StorageBackend               string `json:"storageBackend"`
	StorageRootDir               string `json:"storageRootDir"`
	StorageS3Endpoint            string `json:"storageS3Endpoint"`
	StorageS3Region              string `json:"storageS3Region"`
	StorageS3Bucket              string `json:"storageS3Bucket"`
	StorageS3Prefix              string `json:"storageS3Prefix"`
	StorageS3AccessKeyID         string `json:"storageS3AccessKeyID"`
	StorageS3SecretConfigured    bool   `json:"storageS3SecretConfigured"`
	StorageS3ForcePathStyle      bool   `json:"storageS3ForcePathStyle"`
	ManagedDataDir               string `json:"managedDataDir"`
	ManagedRegistryMirror        string `json:"managedRegistryMirror"`
	ManagedComposeSync           bool   `json:"managedComposeSync"`
	ManagedPostgresEnabled       bool   `json:"managedPostgresEnabled"`
	ManagedPostgresMemoryLimitMB int    `json:"managedPostgresMemoryLimitMB"`
	ManagedRedisEnabled          bool   `json:"managedRedisEnabled"`
	ManagedRedisMemoryLimitMB    int    `json:"managedRedisMemoryLimitMB"`
	ManagedBackupEnabled         bool   `json:"managedBackupEnabled"`
	ManagedBackupSchedule        string `json:"managedBackupSchedule"`
	ManagedBackupRetentionCount  int    `json:"managedBackupRetentionCount"`
	UpdateEnabled                bool   `json:"updateEnabled"`
	UpdateChannel                string `json:"updateChannel"`
	UpdateCheckIntervalSeconds   int    `json:"updateCheckIntervalSeconds"`
	UpdateSource                 string `json:"updateSource"`
	UpdateProxyBaseURL           string `json:"updateProxyBaseURL"`
}

// InfraConfigResponseDoc Swagger 包装。
type InfraConfigResponseDoc struct {
	ErrorMsg string              `json:"errorMsg"`
	Data     InfraConfigResponse `json:"data"`
}

// PatchInfraConfigRequest 提交受控基础设施配置变更。
type PatchInfraConfigRequest struct {
	Env                          *string `json:"env"`
	PublicAPIBaseURL             *string `json:"publicAPIBaseURL"`
	PublicWebBaseURL             *string `json:"publicWebBaseURL"`
	CORSAllowOrigin              *string `json:"corsAllowOrigin"`
	TrustedProxies               *string `json:"trustedProxies"`
	SSRFProtectionEnabled        *bool   `json:"ssrfProtectionEnabled"`
	SSRFAllowedHosts             *string `json:"ssrfAllowedHosts"`
	SSRFAllowedCIDRs             *string `json:"ssrfAllowedCIDRs"`
	StorageBackend               *string `json:"storageBackend"`
	StorageRootDir               *string `json:"storageRootDir"`
	StorageS3Endpoint            *string `json:"storageS3Endpoint"`
	StorageS3Region              *string `json:"storageS3Region"`
	StorageS3Bucket              *string `json:"storageS3Bucket"`
	StorageS3Prefix              *string `json:"storageS3Prefix"`
	StorageS3AccessKeyID         *string `json:"storageS3AccessKeyID"`
	StorageS3SecretAccessKey     *string `json:"storageS3SecretAccessKey"`
	StorageS3ForcePathStyle      *bool   `json:"storageS3ForcePathStyle"`
	ManagedRegistryMirror        *string `json:"managedRegistryMirror"`
	ManagedComposeSync           *bool   `json:"managedComposeSync"`
	ManagedPostgresMemoryLimitMB *int    `json:"managedPostgresMemoryLimitMB"`
	ManagedRedisMemoryLimitMB    *int    `json:"managedRedisMemoryLimitMB"`
	ManagedBackupEnabled         *bool   `json:"managedBackupEnabled"`
	ManagedBackupSchedule        *string `json:"managedBackupSchedule"`
	ManagedBackupRetentionCount  *int    `json:"managedBackupRetentionCount"`
	UpdateEnabled                *bool   `json:"updateEnabled"`
	UpdateChannel                *string `json:"updateChannel"`
	UpdateCheckIntervalSeconds   *int    `json:"updateCheckIntervalSeconds"`
	UpdateSource                 *string `json:"updateSource"`
	UpdateProxyBaseURL           *string `json:"updateProxyBaseURL"`
	RestartNow                   bool    `json:"restartNow"`
}

// PatchInfraConfigResponse 返回保存后的配置视图及是否已触发重启。
type PatchInfraConfigResponse struct {
	Config           InfraConfigResponse `json:"config"`
	RestartScheduled bool                `json:"restartScheduled"`
}

// PatchInfraConfigResponseDoc Swagger 包装。
type PatchInfraConfigResponseDoc struct {
	ErrorMsg string                   `json:"errorMsg"`
	Data     PatchInfraConfigResponse `json:"data"`
}

// DangerRestoreBackupRequest 高危备份恢复请求。
type DangerRestoreBackupRequest struct {
	BackupID        string `json:"backupID" binding:"required"`
	CurrentPassword string `json:"currentPassword" binding:"required"`
	ConfirmText     string `json:"confirmText" binding:"required"`
}

// DangerRestoreBackupResponse 高危备份恢复响应。
type DangerRestoreBackupResponse struct {
	RestoredBackupID string `json:"restoredBackupID"`
	PreRestoreBackup string `json:"preRestoreBackup"`
	RestartScheduled bool   `json:"restartScheduled"`
}

// DangerRestoreBackupResponseDoc Swagger 包装。
type DangerRestoreBackupResponseDoc struct {
	ErrorMsg string                      `json:"errorMsg"`
	Data     DangerRestoreBackupResponse `json:"data"`
}

// DangerSwitchDatabaseRequest 高危切换数据库/缓存模式请求。
type DangerSwitchDatabaseRequest struct {
	CurrentPassword       string `json:"currentPassword" binding:"required"`
	ConfirmText           string `json:"confirmText" binding:"required"`
	DatabaseMode          string `json:"databaseMode" binding:"required"`
	ExternalPostgresDSN   string `json:"externalPostgresDSN"`
	CacheMode             string `json:"cacheMode" binding:"required"`
	ExternalRedisAddr     string `json:"externalRedisAddr"`
	ExternalRedisUsername string `json:"externalRedisUsername"`
	ExternalRedisPassword string `json:"externalRedisPassword"`
	ExternalRedisDB       int    `json:"externalRedisDB"`
	ExternalRedisTLS      bool   `json:"externalRedisTLS"`
	ExternalRedisSkipTLS  bool   `json:"externalRedisSkipTLS"`
}

// DangerSwitchDatabaseResponse 高危切库响应。
type DangerSwitchDatabaseResponse struct {
	RestartScheduled bool `json:"restartScheduled"`
}

// DangerSwitchDatabaseResponseDoc Swagger 包装。
type DangerSwitchDatabaseResponseDoc struct {
	ErrorMsg string                       `json:"errorMsg"`
	Data     DangerSwitchDatabaseResponse `json:"data"`
}

// DangerRecreateContainerRequest 高危强制重建核心容器请求（保留宿主机数据目录）。
type DangerRecreateContainerRequest struct {
	ServiceID       string `json:"serviceID" binding:"required"`
	CurrentPassword string `json:"currentPassword" binding:"required"`
	ConfirmText     string `json:"confirmText" binding:"required"`
}

func toDaemonDTO(in managedports.DaemonInfo) InfraDaemonInfoDTO {
	return InfraDaemonInfoDTO{
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

func toDirDTO(in managedports.DirInspection) InfraDirInfoDTO {
	return InfraDirInfoDTO{
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

func toOverviewResponse(in managedinfra.Overview) InfraOverviewResponse {
	services := make([]ManagedServiceDTO, 0, len(in.Services))
	for _, s := range in.Services {
		services = append(services, ManagedServiceDTO{
			ID:               s.ID,
			Category:         s.Category,
			Name:             s.Name,
			Description:      s.Description,
			Mode:             s.Mode,
			ContainerName:    s.ContainerName,
			Image:            s.Image,
			Port:             s.Port,
			EndpointURL:      s.EndpointURL,
			DataDir:          s.DataDir,
			Status:           s.Status,
			Health:           s.Health,
			Reachable:        s.Reachable,
			Message:          s.Message,
			CPUPercent:       s.CPUPercent,
			MemoryUsageBytes: s.MemoryUsageBytes,
			MemoryLimitBytes: s.MemoryLimitBytes,
			CanStart:         s.CanStart,
			CanStop:          s.CanStop,
			CanRestart:       s.CanRestart,
			CanPull:          s.CanPull,
		})
	}
	return InfraOverviewResponse{
		Docker:          toDaemonDTO(in.Docker),
		DockerError:     in.DockerError,
		DataDir:         toDirDTO(in.DataDir),
		RegistryMirror:  in.RegistryMirror,
		ComposeSync:     in.ComposeSync,
		BackupEnabled:   in.BackupEnabled,
		BackupSchedule:  in.BackupSchedule,
		BackupRetention: in.BackupRetention,
		Services:        services,
	}
}

func toJobEventDTO(evt managedinfra.JobEvent) InfraJobEventDTO {
	return InfraJobEventDTO{
		JobID:     evt.JobID,
		Action:    evt.Action,
		ServiceID: evt.ServiceID,
		Step:      evt.Step,
		Message:   evt.Message,
		Percent:   evt.Percent,
		Status:    evt.Status,
		Timestamp: evt.Timestamp,
	}
}

func toJobSnapshotResponse(snap managedinfra.JobSnapshot) InfraJobSnapshotResponse {
	events := make([]InfraJobEventDTO, 0, len(snap.Events))
	for _, evt := range snap.Events {
		events = append(events, toJobEventDTO(evt))
	}
	return InfraJobSnapshotResponse{
		JobID:     snap.JobID,
		Action:    snap.Action,
		ServiceID: snap.ServiceID,
		State:     snap.State,
		Error:     snap.Error,
		Events:    events,
	}
}

func toBackupDTO(rec managedinfra.BackupRecord) BackupRecordDTO {
	return BackupRecordDTO{
		ID:        rec.ID,
		FileName:  rec.FileName,
		SizeBytes: rec.SizeBytes,
		SHA256:    rec.SHA256,
		Trigger:   rec.Trigger,
		CreatedAt: rec.CreatedAt,
		Verified:  rec.Verified,
	}
}

func toInfraConfigResponse(view managedinfra.InfraConfigView) InfraConfigResponse {
	return InfraConfigResponse{
		ConfigFile:                   view.ConfigFile,
		Env:                          view.Env,
		HTTPPort:                     view.HTTPPort,
		PublicAPIBaseURL:             view.PublicAPIBaseURL,
		PublicWebBaseURL:             view.PublicWebBaseURL,
		CORSAllowOrigin:              view.CORSAllowOrigin,
		TrustedProxies:               view.TrustedProxies,
		SSRFProtectionEnabled:        view.SSRFProtectionEnabled,
		SSRFAllowedHosts:             view.SSRFAllowedHosts,
		SSRFAllowedCIDRs:             view.SSRFAllowedCIDRs,
		StorageBackend:               view.StorageBackend,
		StorageRootDir:               view.StorageRootDir,
		StorageS3Endpoint:            view.StorageS3Endpoint,
		StorageS3Region:              view.StorageS3Region,
		StorageS3Bucket:              view.StorageS3Bucket,
		StorageS3Prefix:              view.StorageS3Prefix,
		StorageS3AccessKeyID:         view.StorageS3AccessKeyID,
		StorageS3SecretConfigured:    view.StorageS3SecretConfigured,
		StorageS3ForcePathStyle:      view.StorageS3ForcePathStyle,
		ManagedDataDir:               view.ManagedDataDir,
		ManagedRegistryMirror:        view.ManagedRegistryMirror,
		ManagedComposeSync:           view.ManagedComposeSync,
		ManagedPostgresEnabled:       view.ManagedPostgresEnabled,
		ManagedPostgresMemoryLimitMB: view.ManagedPostgresMemoryLimitMB,
		ManagedRedisEnabled:          view.ManagedRedisEnabled,
		ManagedRedisMemoryLimitMB:    view.ManagedRedisMemoryLimitMB,
		ManagedBackupEnabled:         view.ManagedBackupEnabled,
		ManagedBackupSchedule:        view.ManagedBackupSchedule,
		ManagedBackupRetentionCount:  view.ManagedBackupRetentionCount,
		UpdateEnabled:                view.UpdateEnabled,
		UpdateChannel:                view.UpdateChannel,
		UpdateCheckIntervalSeconds:   view.UpdateCheckIntervalSeconds,
		UpdateSource:                 view.UpdateSource,
		UpdateProxyBaseURL:           view.UpdateProxyBaseURL,
	}
}
