package system

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	domainuser "github.com/lieyanc/FireCortex/backend/internal/domain/user"
	"github.com/lieyanc/FireCortex/backend/internal/infra/updater"
	"github.com/lieyanc/FireCortex/backend/internal/shared/response"
	"github.com/lieyanc/FireCortex/backend/internal/transport/http/middleware"
)

// Updater 是 HTTP 层依赖的自更新能力。
type Updater interface {
	Status() updater.Status
	CheckOnly(ctx context.Context) error
	StartUpdate()
	ApplyPending() error
	DismissPending()
}

// Handler 处理系统运维请求。
type Handler struct {
	updater Updater
}

// NewHandler 创建系统运维处理器。
func NewHandler(u Updater) *Handler {
	return &Handler{updater: u}
}

// GetUpdateStatus godoc
// @Summary 查询自更新状态
// @Tags admin/system
// @Produce json
// @Security BearerAuth
// @Success 200 {object} UpdateStatusResponseDoc
// @Router /admin/system/update [get]
func (h *Handler) GetUpdateStatus(c *gin.Context) {
	if !h.available(c) {
		return
	}
	response.Success(c, toUpdateStatusResponse(h.updater.Status()))
}

// CheckUpdate godoc
// @Summary 检查新版本（只检查不下载）
// @Description 检查失败不返回错误响应，而是体现在返回状态的 state=failed 与 error 字段中。
// @Tags admin/system
// @Produce json
// @Security BearerAuth
// @Success 200 {object} UpdateStatusResponseDoc
// @Router /admin/system/update/check [post]
func (h *Handler) CheckUpdate(c *gin.Context) {
	if !h.available(c) || !requireSuperAdmin(c) {
		return
	}
	// The outcome, including failures, is recorded in the status snapshot.
	_ = h.updater.CheckOnly(c.Request.Context())
	response.Success(c, toUpdateStatusResponse(h.updater.Status()))
}

// ApplyUpdate godoc
// @Summary 下载并应用更新
// @Description ready 状态下安装已下载的版本并重启；否则在后台检查、下载，stable 通道下载校验后自动重启，dev 通道停在 ready 等待再次确认。
// @Tags admin/system
// @Produce json
// @Security BearerAuth
// @Success 200 {object} UpdateStatusResponseDoc
// @Router /admin/system/update/apply [post]
func (h *Handler) ApplyUpdate(c *gin.Context) {
	if !h.available(c) || !requireSuperAdmin(c) {
		return
	}
	switch h.updater.Status().State {
	case updater.StateReady:
		if err := h.updater.ApplyPending(); err != nil {
			if errors.Is(err, updater.ErrNoPendingUpdate) {
				response.ErrorFrom(c, http.StatusConflict, errNoPendingUpdate)
				return
			}
			response.InternalError(c)
			return
		}
	case updater.StateChecking, updater.StateDownloading, updater.StateApplying:
		response.ErrorFrom(c, http.StatusConflict, errUpdateInProgress)
		return
	default:
		h.updater.StartUpdate()
	}
	response.Success(c, toUpdateStatusResponse(h.updater.Status()))
}

// DismissUpdate godoc
// @Summary 放弃已下载的待安装版本
// @Tags admin/system
// @Produce json
// @Security BearerAuth
// @Success 200 {object} UpdateStatusResponseDoc
// @Router /admin/system/update/dismiss [post]
func (h *Handler) DismissUpdate(c *gin.Context) {
	if !h.available(c) || !requireSuperAdmin(c) {
		return
	}
	h.updater.DismissPending()
	response.Success(c, toUpdateStatusResponse(h.updater.Status()))
}

func (h *Handler) available(c *gin.Context) bool {
	if h == nil || h.updater == nil {
		response.ErrorFrom(c, http.StatusServiceUnavailable, errUpdaterUnavailable)
		return false
	}
	return true
}

// requireSuperAdmin 限制替换服务端二进制这类高危操作只能由超级管理员发起。
func requireSuperAdmin(c *gin.Context) bool {
	if c.GetString(middleware.ContextKeyUserRole) != domainuser.RoleSuperAdmin {
		response.ErrorFrom(c, http.StatusForbidden, errSuperAdminRequired)
		return false
	}
	return true
}
