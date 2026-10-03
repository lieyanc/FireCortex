package system

// Module 聚合系统运维 HTTP 处理器。
type Module struct {
	Handler *Handler
}

// NewModule 创建系统运维 HTTP 模块。
func NewModule(handler *Handler) *Module {
	return &Module{Handler: handler}
}
