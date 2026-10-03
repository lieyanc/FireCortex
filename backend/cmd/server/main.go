package main

import (
	"log"

	"github.com/lieyanc/FireCortex/backend/docs"
	"github.com/lieyanc/FireCortex/backend/internal/cli"
	"github.com/lieyanc/FireCortex/backend/internal/shared/buildinfo"
)

// @title FireCortex API
// @version 0.4.3
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
