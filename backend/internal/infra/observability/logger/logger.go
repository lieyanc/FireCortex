package logger

import (
	"os"

	base "github.com/lieyanc/FireCortex/backend/internal/pkg/logger"
	"go.uber.org/zap"
)

// New 创建平台日志实例（stdout）。
func New(env string) (*zap.Logger, error) {
	return base.New(env, "firecortex")
}

// NewStderr 创建写到 stderr 的日志实例，供本地 sidecar 模式使用。
func NewStderr(env string) (*zap.Logger, error) {
	return base.NewWithOutput(env, os.Stderr)
}
