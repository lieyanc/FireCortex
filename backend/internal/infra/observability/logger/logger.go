package logger

import (
	base "github.com/lieyanc/FireCortex/backend/internal/pkg/logger"
	"go.uber.org/zap"
)

// New 创建平台日志实例。
func New(env string) (*zap.Logger, error) {
	return base.New(env, "firecortex")
}
