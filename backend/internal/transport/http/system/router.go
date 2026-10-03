package system

import "github.com/gin-gonic/gin"

// RegisterAdminRoutes 注册系统运维管理路由（由管理员中间件保护）。
func (m *Module) RegisterAdminRoutes(adminGroup *gin.RouterGroup) {
	g := adminGroup.Group("/system/update")
	g.GET("", m.Handler.GetUpdateStatus)
	g.POST("/check", m.Handler.CheckUpdate)
	g.POST("/apply", m.Handler.ApplyUpdate)
	g.POST("/dismiss", m.Handler.DismissUpdate)
}
