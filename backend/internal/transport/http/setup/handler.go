package setup

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	appsetup "github.com/lieyanc/FireCortex/backend/internal/application/setup"
	"github.com/lieyanc/FireCortex/backend/internal/shared/buildinfo"
	"github.com/lieyanc/FireCortex/backend/internal/shared/response"
)

// Handler 处理 Setup Mode 的全部引导接口。
type Handler struct {
	service *appsetup.Service
}

// NewHandler 创建 Setup 处理器。
func NewHandler(svc *appsetup.Service) *Handler {
	return &Handler{service: svc}
}

// GetStatus godoc
// @Summary 查询引导模式状态
// @Tags setup
// @Produce json
// @Success 200 {object} SetupStatusResponseDoc
// @Router /setup/status [get]
func (h *Handler) GetStatus(c *gin.Context) {
	authed := h.isAuthenticated(c)
	snap := h.service.CurrentJob()
	resp := SetupStatusResponse{
		NeedsSetup:    true,
		Authenticated: authed,
		Version:       buildinfo.ResolveVersion(),
		JobState:      snap.State,
	}
	if authed {
		defs := toSetupDefaultsDTO(h.service.Defaults())
		resp.Defaults = &defs
	}
	response.Success(c, resp)
}

// VerifyToken godoc
// @Summary 验证一次性 Setup Token 并签发引导会话
// @Tags setup
// @Accept json
// @Produce json
// @Param request body VerifyTokenRequest true "Setup Token"
// @Success 200 {object} VerifyTokenResponseDoc
// @Router /setup/verify [post]
func (h *Handler) VerifyToken(c *gin.Context) {
	var req VerifyTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeSetupError(c, http.StatusBadRequest, "setup.invalid_request", "Please enter the one-time Setup Token")
		return
	}
	sessionID, err := h.service.VerifyToken(req.Token)
	if err != nil {
		switch {
		case errors.Is(err, appsetup.ErrTooManyAttempts):
			writeSetupError(c, http.StatusTooManyRequests, "setup.too_many_attempts", err.Error())
		case errors.Is(err, appsetup.ErrAlreadyCompleted):
			writeSetupError(c, http.StatusConflict, "setup.already_completed", err.Error())
		default:
			writeSetupError(c, http.StatusUnauthorized, "setup.invalid_token", "Invalid Setup Token; check terminal or service logs")
		}
		return
	}

	secureCookie := c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(appsetup.SessionCookieName, sessionID, int((2 * time.Hour).Seconds()), "/", "", secureCookie, true)

	response.Success(c, VerifyTokenResponse{
		Authenticated: true,
		Defaults:      toSetupDefaultsDTO(h.service.Defaults()),
	})
}

// Preflight godoc
// @Summary 宿主机 Docker、目录与端口环境体检
// @Tags setup
// @Produce json
// @Param dockerHost query string false "可选 Docker Engine 地址"
// @Success 200 {object} PreflightResponseDoc
// @Router /setup/preflight [get]
func (h *Handler) Preflight(c *gin.Context) {
	if !h.requireAuth(c) {
		return
	}
	res := h.service.Preflight(c.Request.Context(), c.Query("dockerHost"))
	response.Success(c, toPreflightResponse(res))
}

// InspectDir godoc
// @Summary 校验托管数据目录安全性与已有 PostgreSQL 数据
// @Tags setup
// @Accept json
// @Produce json
// @Param request body InspectDirRequest true "目录参数"
// @Success 200 {object} InspectDirResponseDoc
// @Router /setup/inspect-dir [post]
func (h *Handler) InspectDir(c *gin.Context) {
	if !h.requireAuth(c) {
		return
	}
	var req InspectDirRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeSetupError(c, http.StatusBadRequest, "setup.invalid_request", "Please provide a directory path to inspect")
		return
	}
	res := h.service.InspectDirectory(appsetup.InspectDirInput{
		DataDir:         req.DataDir,
		PostgresDataDir: req.PostgresDataDir,
		RedisDataDir:    req.RedisDataDir,
		BackupDir:       req.BackupDir,
	})
	response.Success(c, toDirInspectionDTO(res))
}

// TestExternal godoc
// @Summary 只读测试外部 PostgreSQL（含 pgvector）与 Redis 连通性
// @Tags setup
// @Accept json
// @Produce json
// @Param request body TestExternalRequest true "外部连接参数"
// @Success 200 {object} TestExternalResponseDoc
// @Router /setup/test-external [post]
func (h *Handler) TestExternal(c *gin.Context) {
	if !h.requireAuth(c) {
		return
	}
	var req TestExternalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeSetupError(c, http.StatusBadRequest, "setup.invalid_request", "Please provide an external PostgreSQL DSN")
		return
	}
	res := h.service.TestExternal(c.Request.Context(), appsetup.TestExternalInput{
		PostgresDSN:          req.PostgresDSN,
		CacheDriver:          req.CacheDriver,
		RedisAddr:            req.RedisAddr,
		RedisUsername:        req.RedisUsername,
		RedisPassword:        req.RedisPassword,
		RedisDB:              req.RedisDB,
		RedisTLSEnabled:      req.RedisTLSEnabled,
		RedisTLSInsecureSkip: req.RedisTLSInsecureSkip,
	})
	response.Success(c, toTestExternalResponse(res))
}

// Apply godoc
// @Summary 启动异步初始化与容器编排任务
// @Tags setup
// @Accept json
// @Produce json
// @Param request body ApplyRequest true "初始化配置"
// @Success 200 {object} JobSnapshotResponseDoc
// @Router /setup/apply [post]
func (h *Handler) Apply(c *gin.Context) {
	if !h.requireAuth(c) {
		return
	}
	var req ApplyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeSetupError(c, http.StatusBadRequest, "setup.invalid_request", "初始化请求格式不正确")
		return
	}
	composeSync := true
	if req.ComposeSync != nil {
		composeSync = *req.ComposeSync
	}

	snap, err := h.service.StartApply(c.Request.Context(), appsetup.ApplyInput{
		DatabaseMode:              req.DatabaseMode,
		CacheMode:                 req.CacheMode,
		DockerHost:                req.DockerHost,
		RegistryMirror:            req.RegistryMirror,
		DataDir:                   req.DataDir,
		PostgresDataDir:           req.PostgresDataDir,
		RedisDataDir:              req.RedisDataDir,
		BackupDir:                 req.BackupDir,
		PostgresImage:             req.PostgresImage,
		PostgresPort:              req.PostgresPort,
		PostgresMemoryLimitMB:     req.PostgresMemoryLimitMB,
		RedisImage:                req.RedisImage,
		RedisPort:                 req.RedisPort,
		RedisMemoryLimitMB:        req.RedisMemoryLimitMB,
		ExistingDataEncryptionKey: req.ExistingDataEncryptionKey,
		AdoptExistingPGData:       req.AdoptExistingPGData,
		ExistingPGUser:            req.ExistingPGUser,
		ExistingPGPassword:        req.ExistingPGPassword,
		ExistingPGDatabase:        req.ExistingPGDatabase,
		ExistingRedisPassword:     req.ExistingRedisPassword,
		ComposeSync:               composeSync,
		ExternalPostgresDSN:       req.ExternalPostgresDSN,
		ExternalRedisAddr:         req.ExternalRedisAddr,
		ExternalRedisUser:         req.ExternalRedisUser,
		ExternalRedisPass:         req.ExternalRedisPass,
		ExternalRedisDB:           req.ExternalRedisDB,
		ExternalRedisTLS:          req.ExternalRedisTLS,
		ExternalRedisSkipTLS:      req.ExternalRedisSkipTLS,
		Env:                       req.Env,
		PublicBaseURL:             req.PublicBaseURL,
		CORSAllowOrigin:           req.CORSAllowOrigin,
		AdminUsername:             req.AdminUsername,
		AdminDisplayName:          req.AdminDisplayName,
		AdminPassword:             req.AdminPassword,
	})
	if err != nil {
		switch {
		case errors.Is(err, appsetup.ErrApplyInProgress):
			writeSetupError(c, http.StatusConflict, "setup.apply_in_progress", err.Error())
		case errors.Is(err, appsetup.ErrAlreadyCompleted):
			writeSetupError(c, http.StatusConflict, "setup.already_completed", err.Error())
		default:
			writeSetupError(c, http.StatusBadRequest, "setup.validation_failed", err.Error())
		}
		return
	}
	response.Success(c, toJobSnapshotResponse(snap))
}

// GetJob godoc
// @Summary 查询当前初始化任务快照
// @Tags setup
// @Produce json
// @Success 200 {object} JobSnapshotResponseDoc
// @Router /setup/job [get]
func (h *Handler) GetJob(c *gin.Context) {
	if !h.requireAuth(c) {
		return
	}
	response.Success(c, toJobSnapshotResponse(h.service.CurrentJob()))
}

// StreamEvents godoc
// @Summary 通过 SSE 实时订阅初始化进度事件
// @Tags setup
// @Produce text/event-stream
// @Success 200 {string} string "SSE event stream"
// @Router /setup/events [get]
func (h *Handler) StreamEvents(c *gin.Context) {
	if !h.requireAuth(c) {
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	ctx := c.Request.Context()
	events, cancel := h.service.SubscribeJob(ctx)
	defer cancel()

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		writeSetupError(c, http.StatusInternalServerError, "setup.stream_unsupported", "streaming unsupported")
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
			payload, err := json.Marshal(toJobEventResponse(evt))
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(c.Writer, "event: progress\ndata: %s\n\n", payload)
			flusher.Flush()
			if evt.Step == "restart" && evt.Status == "done" {
				return
			}
		}
	}
}

func (h *Handler) isAuthenticated(c *gin.Context) bool {
	cookie, _ := c.Cookie(appsetup.SessionCookieName)
	return h.service.ValidateAuth(cookie, c.GetHeader("X-Setup-Token"))
}

func (h *Handler) requireAuth(c *gin.Context) bool {
	if !h.isAuthenticated(c) {
		writeSetupError(c, http.StatusUnauthorized, "setup.unauthorized", "请先输入一次性 Setup Token 完成验证")
		return false
	}
	return true
}

func writeSetupError(c *gin.Context, status int, code, message string) {
	response.ErrorDescribed(c, response.Description{
		Status:  status,
		Code:    code,
		Message: message,
	})
}
