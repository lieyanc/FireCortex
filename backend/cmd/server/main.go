package main

import (
	"log"
	// Windows 没有系统时区库，用户机器上也没有 Go 自带的 zoneinfo.zip；
	// 内嵌时区数据后 time.LoadLocation 才能在桌面端本地服务中解析 Etc/UTC 等时区。
	// 其他平台仍优先读取系统时区库。
	_ "time/tzdata"

	"github.com/lieyanc/FireCortex/backend/docs"
	"github.com/lieyanc/FireCortex/backend/internal/cli"
	"github.com/lieyanc/FireCortex/backend/internal/shared/buildinfo"
)

// @title FireCortex API
// @version 0.4.7
// @description FireCortex 后端 API 文档
// @BasePath /api/v1
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
func main() {
	docs.SwaggerInfo.Version = buildinfo.ResolveVersion()
	if err := cli.Run(); err != nil {
		log.Fatalf("failed to run server: %v", err)
	}
}
