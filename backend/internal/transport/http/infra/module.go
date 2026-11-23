package infra

// Module 封装「服务与基础设施」管理 HTTP 模块。
type Module struct {
	Handler *Handler
}

// NewModule 创建基础设施管理模块。
func NewModule(handler *Handler) *Module {
	return &Module{Handler: handler}
}
