package infra

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lieyanc/FireCortex/backend/internal/application/managedinfra"
	domainuser "github.com/lieyanc/FireCortex/backend/internal/domain/user"
	"github.com/lieyanc/FireCortex/backend/internal/infra/config"
	"github.com/lieyanc/FireCortex/backend/internal/shared/response"
	"github.com/lieyanc/FireCortex/backend/internal/transport/http/middleware"
)

// Handler 处理 superadmin 专属「服务与基础设施」接口。
type Handler struct {
	manager *managedinfra.Manager
}

// NewHandler 创建基础设施管理处理器。
func NewHandler(mgr *managedinfra.Manager) *Handler {
	return &Handler{manager: mgr}
}

// GetOverview godoc
// @Summary 查询 Docker 宿主机与托管服务总览
// @Tags admin/infra
// @Produce json
// @Security BearerAuth
// @Success 200 {object} InfraOverviewResponseDoc
// @Router /admin/infra/overview [get]
func (h *Handler) GetOverview(c *gin.Context) {
	if !h.requireSuperAdmin(c) {
		return
	}
	response.Success(c, toOverviewResponse(h.manager.GetOverview(c.Request.Context())))
}

// TriggerServiceAction godoc
// @Summary 触发托管服务的启停、重启或拉取升级任务
// @Tags admin/infra
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "服务 ID (postgres/redis/tika/docling/tesseract/rapidocr)"
// @Param action path string true "操作 (start/stop/restart/pull)"
// @Param request body ServiceActionRequest false "可选受控参数"
// @Success 200 {object} InfraJobSnapshotResponseDoc
// @Router /admin/infra/services/{id}/{action} [post]
func (h *Handler) TriggerServiceAction(c *gin.Context) {
	if !h.requireSuperAdmin(c) {
		return
	}
	var req ServiceActionRequest
	_ = c.ShouldBindJSON(&req)

	actorID := c.GetUint(middleware.ContextKeyUserID)
	snap, err := h.manager.StartServiceAction(actorID, c.Param("id"), c.Param("action"), managedinfra.ServiceActionOptions{
		Port:          req.Port,
		MemoryLimitMB: req.MemoryLimitMB,
		TesseractLang: req.TesseractLang,
	})
	if err != nil {
		writeInfraError(c, http.StatusBadRequest, "infra.service_action_failed", err.Error())
		return
	}
	response.Success(c, toJobSnapshotResponse(snap))
}

// GetServiceLogs godoc
// @Summary 读取托管容器最近日志
// @Tags admin/infra
// @Produce json
// @Security BearerAuth
// @Param id path string true "服务 ID"
// @Param tail query int false "读取行数（默认 200）"
// @Success 200 {object} ServiceLogsResponseDoc
// @Router /admin/infra/services/{id}/logs [get]
func (h *Handler) GetServiceLogs(c *gin.Context) {
	if !h.requireSuperAdmin(c) {
		return
	}
	tail := 200
	if raw := c.Query("tail"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			tail = parsed
		}
	}
	serviceID := c.Param("id")
	logs, err := h.manager.TailServiceLogs(c.Request.Context(), serviceID, tail)
	if err != nil {
		writeInfraError(c, http.StatusBadRequest, "infra.logs_unavailable", err.Error())
		return
	}
	response.Success(c, ServiceLogsResponse{ServiceID: serviceID, Logs: logs})
}

// GetJob godoc
// @Summary 查询异步基础设施任务快照
// @Tags admin/infra
// @Produce json
// @Security BearerAuth
// @Param jobId path string true "任务 ID"
// @Success 200 {object} InfraJobSnapshotResponseDoc
// @Router /admin/infra/jobs/{jobId} [get]
func (h *Handler) GetJob(c *gin.Context) {
	if !h.requireSuperAdmin(c) {
		return
	}
	snap, ok := h.manager.GetJob(c.Param("jobId"))
	if !ok {
		writeInfraError(c, http.StatusNotFound, "infra.job_not_found", "job not found")
		return
	}
	response.Success(c, toJobSnapshotResponse(snap))
}

// StreamJobEvents godoc
// @Summary 通过 SSE 实时订阅异步基础设施任务进度
// @Tags admin/infra
// @Produce text/event-stream
// @Security BearerAuth
// @Param jobId path string true "任务 ID"
// @Success 200 {string} string "SSE event stream"
// @Router /admin/infra/jobs/{jobId}/events [get]
func (h *Handler) StreamJobEvents(c *gin.Context) {
	if !h.requireSuperAdmin(c) {
		return
	}
	ctx := c.Request.Context()
	events, cancel, ok := h.manager.SubscribeJob(ctx, c.Param("jobId"))
	if !ok {
		writeInfraError(c, http.StatusNotFound, "infra.job_not_found", "job not found")
		return
	}
	defer cancel()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		writeInfraError(c, http.StatusInternalServerError, "infra.stream_unsupported", "streaming unsupported")
		return
	}

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			_, _ = fmt.Fprint(c.Writer, ": keepalive\n\n")
			flusher.Flush()
		case evt, open := <-events:
			if !open {
				return
			}
			payload, err := json.Marshal(toJobEventDTO(evt))
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(c.Writer, "event: progress\ndata: %s\n\n", payload)
			flusher.Flush()
			if (evt.Step == "complete" && evt.Status == "done") || evt.Status == "failed" {
				return
			}
		}
	}
}

// GetComposePreview godoc
// @Summary 查看落盘的 docker-compose.yml（不含明文密码）
// @Tags admin/infra
// @Produce json
// @Security BearerAuth
// @Success 200 {object} ComposePreviewResponseDoc
// @Router /admin/infra/compose [get]
func (h *Handler) GetComposePreview(c *gin.Context) {
	if !h.requireSuperAdmin(c) {
		return
	}
	content, err := h.manager.ReadComposeYAML()
	if err != nil {
		response.Success(c, ComposePreviewResponse{Content: ""})
		return
	}
	response.Success(c, ComposePreviewResponse{Content: content})
}

// ListBackups godoc
// @Summary 列出托管 PostgreSQL 历史逻辑备份
// @Tags admin/infra
// @Produce json
// @Security BearerAuth
// @Success 200 {object} BackupListResponseDoc
// @Router /admin/infra/backups [get]
func (h *Handler) ListBackups(c *gin.Context) {
	if !h.requireSuperAdmin(c) {
		return
	}
	records, err := h.manager.ListBackups()
	if err != nil {
		writeInfraError(c, http.StatusInternalServerError, "infra.backup_list_failed", err.Error())
		return
	}
	items := make([]BackupRecordDTO, 0, len(records))
	for _, r := range records {
		items = append(items, toBackupDTO(r))
	}
	response.Success(c, BackupListResponse{Backups: items})
}

// CreateBackup godoc
// @Summary 立即执行一次手动 PostgreSQL 逻辑备份
// @Tags admin/infra
// @Produce json
// @Security BearerAuth
// @Success 200 {object} BackupRecordResponseDoc
// @Router /admin/infra/backups [post]
func (h *Handler) CreateBackup(c *gin.Context) {
	if !h.requireSuperAdmin(c) {
		return
	}
	actorID := c.GetUint(middleware.ContextKeyUserID)
	rec, err := h.manager.CreateBackup(c.Request.Context(), actorID, "manual")
	if err != nil {
		writeInfraError(c, http.StatusBadRequest, "infra.backup_create_failed", err.Error())
		return
	}
	response.Success(c, toBackupDTO(rec))
}

// DownloadBackup godoc
// @Summary 流式下载单份 PostgreSQL 逻辑备份归档
// @Tags admin/infra
// @Produce application/gzip
// @Security BearerAuth
// @Param id path string true "备份文件名 ID"
// @Success 200 {file} binary
// @Router /admin/infra/backups/{id}/download [get]
func (h *Handler) DownloadBackup(c *gin.Context) {
	if !h.requireSuperAdmin(c) {
		return
	}
	fullPath, fileName, err := h.manager.ResolveBackupPath(c.Param("id"))
	if err != nil {
		writeInfraError(c, http.StatusNotFound, "infra.backup_not_found", err.Error())
		return
	}
	c.FileAttachment(fullPath, fileName)
}

// DeleteBackup godoc
// @Summary 删除单份过期备份文件（绝不触碰数据库目录）
// @Tags admin/infra
// @Produce json
// @Security BearerAuth
// @Param id path string true "备份文件名 ID"
// @Success 200 {object} response.SuccessDoc
// @Router /admin/infra/backups/{id} [delete]
func (h *Handler) DeleteBackup(c *gin.Context) {
	if !h.requireSuperAdmin(c) {
		return
	}
	actorID := c.GetUint(middleware.ContextKeyUserID)
	if err := h.manager.DeleteBackup(c.Request.Context(), actorID, c.Param("id")); err != nil {
		writeInfraError(c, http.StatusBadRequest, "infra.backup_delete_failed", err.Error())
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

// GetInfraConfig godoc
// @Summary 读取常用基础设施 config.json 脱敏视图
// @Tags admin/infra
// @Produce json
// @Security BearerAuth
// @Success 200 {object} InfraConfigResponseDoc
// @Router /admin/infra/config [get]
func (h *Handler) GetInfraConfig(c *gin.Context) {
	if !h.requireSuperAdmin(c) {
		return
	}
	response.Success(c, toInfraConfigResponse(h.manager.GetInfraConfig()))
}

// PatchInfraConfig godoc
// @Summary 校验并保存常用基础设施配置到 config.json，可选平滑重启
// @Tags admin/infra
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body PatchInfraConfigRequest true "基础设施配置补丁"
// @Success 200 {object} PatchInfraConfigResponseDoc
// @Router /admin/infra/config [patch]
func (h *Handler) PatchInfraConfig(c *gin.Context) {
	if !h.requireSuperAdmin(c) {
		return
	}
	var req PatchInfraConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeInfraError(c, http.StatusBadRequest, "infra.invalid_request", "invalid infrastructure config payload")
		return
	}
	actorID := c.GetUint(middleware.ContextKeyUserID)
	view, restarting, err := h.manager.PatchInfraConfig(c.Request.Context(), actorID, config.InfraConfigPatch{
		Env:                          req.Env,
		PublicAPIBaseURL:             req.PublicAPIBaseURL,
		PublicWebBaseURL:             req.PublicWebBaseURL,
		CORSAllowOrigin:              req.CORSAllowOrigin,
		TrustedProxies:               req.TrustedProxies,
		SSRFProtectionEnabled:        req.SSRFProtectionEnabled,
		SSRFAllowedHosts:             req.SSRFAllowedHosts,
		SSRFAllowedCIDRs:             req.SSRFAllowedCIDRs,
		StorageBackend:               req.StorageBackend,
		StorageRootDir:               req.StorageRootDir,
		StorageS3Endpoint:            req.StorageS3Endpoint,
		StorageS3Region:              req.StorageS3Region,
		StorageS3Bucket:              req.StorageS3Bucket,
		StorageS3Prefix:              req.StorageS3Prefix,
		StorageS3AccessKeyID:         req.StorageS3AccessKeyID,
		StorageS3SecretAccessKey:     req.StorageS3SecretAccessKey,
		StorageS3ForcePathStyle:      req.StorageS3ForcePathStyle,
		ManagedRegistryMirror:        req.ManagedRegistryMirror,
		ManagedComposeSync:           req.ManagedComposeSync,
		ManagedPostgresMemoryLimitMB: req.ManagedPostgresMemoryLimitMB,
		ManagedRedisMemoryLimitMB:    req.ManagedRedisMemoryLimitMB,
		ManagedBackupEnabled:         req.ManagedBackupEnabled,
		ManagedBackupSchedule:        req.ManagedBackupSchedule,
		ManagedBackupRetentionCount:  req.ManagedBackupRetentionCount,
		UpdateEnabled:                req.UpdateEnabled,
		UpdateChannel:                req.UpdateChannel,
		UpdateCheckIntervalSeconds:   req.UpdateCheckIntervalSeconds,
		UpdateSource:                 req.UpdateSource,
		UpdateProxyBaseURL:           req.UpdateProxyBaseURL,
	}, req.RestartNow)
	if err != nil {
		writeInfraError(c, http.StatusBadRequest, "infra.config_patch_failed", err.Error())
		return
	}
	response.Success(c, PatchInfraConfigResponse{
		Config:           toInfraConfigResponse(view),
		RestartScheduled: restarting,
	})
}

// DangerRestoreBackup godoc
// @Summary 高危操作：校验并从历史备份一键恢复 PostgreSQL（先自动留存保护快照）
// @Tags admin/infra
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body DangerRestoreBackupRequest true "高危恢复请求"
// @Success 200 {object} DangerRestoreBackupResponseDoc
// @Router /admin/infra/danger/restore-backup [post]
func (h *Handler) DangerRestoreBackup(c *gin.Context) {
	if !h.requireSuperAdmin(c) {
		return
	}
	var req DangerRestoreBackupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeInfraError(c, http.StatusBadRequest, "infra.invalid_request", "backupID, currentPassword, and confirmText are required")
		return
	}
	actorID := c.GetUint(middleware.ContextKeyUserID)
	res, err := h.manager.DangerRestoreBackup(c.Request.Context(), actorID, managedinfra.DangerRestoreInput{
		BackupID:        req.BackupID,
		CurrentPassword: req.CurrentPassword,
		ConfirmText:     req.ConfirmText,
	})
	if err != nil {
		writeInfraError(c, http.StatusBadRequest, "infra.danger_restore_failed", err.Error())
		return
	}
	response.Success(c, DangerRestoreBackupResponse{
		RestoredBackupID: res.RestoredBackupID,
		PreRestoreBackup: res.PreRestoreBackup,
		RestartScheduled: res.RestartScheduled,
	})
}

// DangerSwitchDatabase godoc
// @Summary 高危操作：连通测试后切换托管/外部数据库及缓存模式并平滑重启
// @Tags admin/infra
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body DangerSwitchDatabaseRequest true "高危切库请求"
// @Success 200 {object} DangerSwitchDatabaseResponseDoc
// @Router /admin/infra/danger/switch-database [post]
func (h *Handler) DangerSwitchDatabase(c *gin.Context) {
	if !h.requireSuperAdmin(c) {
		return
	}
	var req DangerSwitchDatabaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeInfraError(c, http.StatusBadRequest, "infra.invalid_request", "missing required switch database parameters")
		return
	}
	actorID := c.GetUint(middleware.ContextKeyUserID)
	restarting, err := h.manager.DangerSwitchDatabase(c.Request.Context(), actorID, managedinfra.DangerSwitchDatabaseInput{
		CurrentPassword:       req.CurrentPassword,
		ConfirmText:           req.ConfirmText,
		DatabaseMode:          req.DatabaseMode,
		ExternalPostgresDSN:   req.ExternalPostgresDSN,
		CacheMode:             req.CacheMode,
		ExternalRedisAddr:     req.ExternalRedisAddr,
		ExternalRedisUsername: req.ExternalRedisUsername,
		ExternalRedisPassword: req.ExternalRedisPassword,
		ExternalRedisDB:       req.ExternalRedisDB,
		ExternalRedisTLS:      req.ExternalRedisTLS,
		ExternalRedisSkipTLS:  req.ExternalRedisSkipTLS,
	})
	if err != nil {
		writeInfraError(c, http.StatusBadRequest, "infra.danger_switch_failed", err.Error())
		return
	}
	response.Success(c, DangerSwitchDatabaseResponse{RestartScheduled: restarting})
}

// DangerRecreateContainer godoc
// @Summary 高危操作：强制重建核心托管容器（严格保留宿主机数据目录）
// @Tags admin/infra
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body DangerRecreateContainerRequest true "高危重建请求"
// @Success 200 {object} InfraJobSnapshotResponseDoc
// @Router /admin/infra/danger/recreate-container [post]
func (h *Handler) DangerRecreateContainer(c *gin.Context) {
	if !h.requireSuperAdmin(c) {
		return
	}
	var req DangerRecreateContainerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeInfraError(c, http.StatusBadRequest, "infra.invalid_request", "serviceID, currentPassword, and confirmText are required")
		return
	}
	actorID := c.GetUint(middleware.ContextKeyUserID)
	snap, err := h.manager.DangerRecreateContainer(c.Request.Context(), actorID, managedinfra.DangerRecreateInput{
		ServiceID:       req.ServiceID,
		PullLatest:      true,
		CurrentPassword: req.CurrentPassword,
		ConfirmText:     req.ConfirmText,
	})
	if err != nil {
		writeInfraError(c, http.StatusBadRequest, "infra.danger_recreate_failed", err.Error())
		return
	}
	response.Success(c, toJobSnapshotResponse(snap))
}

func (h *Handler) requireSuperAdmin(c *gin.Context) bool {
	if h == nil || h.manager == nil {
		writeInfraError(c, http.StatusServiceUnavailable, "infra.unavailable", "infrastructure manager is unavailable")
		return false
	}
	if c.GetString(middleware.ContextKeyUserRole) != domainuser.RoleSuperAdmin {
		writeInfraError(c, http.StatusForbidden, "auth.superadmin_required", "superadmin role is required")
		return false
	}
	return true
}

func writeInfraError(c *gin.Context, status int, code, message string) {
	response.ErrorDescribed(c, response.Description{
		Status:  status,
		Code:    code,
		Message: message,
	})
}
