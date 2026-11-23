package infra

import "github.com/gin-gonic/gin"

// RegisterAdminRoutes 注册 superadmin 专属基础设施与托管服务路由。
func (m *Module) RegisterAdminRoutes(adminGroup *gin.RouterGroup) {
	g := adminGroup.Group("/infra")
	g.GET("/overview", m.Handler.GetOverview)
	g.POST("/services/:id/:action", m.Handler.TriggerServiceAction)
	g.GET("/services/:id/logs", m.Handler.GetServiceLogs)
	g.GET("/jobs/:jobId", m.Handler.GetJob)
	g.GET("/jobs/:jobId/events", m.Handler.StreamJobEvents)
	g.GET("/compose", m.Handler.GetComposePreview)
	g.GET("/backups", m.Handler.ListBackups)
	g.POST("/backups", m.Handler.CreateBackup)
	g.GET("/backups/:id/download", m.Handler.DownloadBackup)
	g.DELETE("/backups/:id", m.Handler.DeleteBackup)
	g.GET("/config", m.Handler.GetInfraConfig)
	g.PATCH("/config", m.Handler.PatchInfraConfig)
	g.POST("/danger/restore-backup", m.Handler.DangerRestoreBackup)
	g.POST("/danger/switch-database", m.Handler.DangerSwitchDatabase)
	g.POST("/danger/recreate-container", m.Handler.DangerRecreateContainer)
}
