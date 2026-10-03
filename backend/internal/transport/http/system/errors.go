package system

import "github.com/lieyanc/FireCortex/backend/internal/shared/apperr"

// 错误码与文案是前端依赖的 API 契约，改动需同步 frontend/i18n/messages/*/errors.json。
var (
	errSuperAdminRequired = apperr.New("system_update.superadmin_required", "only a superadmin can manage updates")
	errUpdateInProgress   = apperr.New("system_update.in_progress", "an update is already in progress")
	errNoPendingUpdate    = apperr.New("system_update.no_pending", "no downloaded update is waiting to be applied")
	errUpdaterUnavailable = apperr.New("system_update.unavailable", "self-update is not available")
)
