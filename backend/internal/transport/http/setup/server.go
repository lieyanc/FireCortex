package setup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	appsetup "github.com/lieyanc/FireCortex/backend/internal/application/setup"
	"github.com/lieyanc/FireCortex/backend/internal/infra/config"
	"github.com/lieyanc/FireCortex/backend/internal/shared/buildinfo"
	"github.com/lieyanc/FireCortex/backend/internal/shared/response"
	"github.com/lieyanc/FireCortex/backend/internal/webui"
)

// NewEngine 构建 Setup Mode 专用的轻量 HTTP 引擎（无数据库依赖，复用内嵌 WebUI）。
func NewEngine(cfg config.Config, svc *appsetup.Service) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(setupCORSMiddleware())

	engine.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "setup", "needsSetup": true})
	})
	engine.GET("/readyz", func(c *gin.Context) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "setup_pending"})
	})

	api := engine.Group("/api/v1")
	api.GET("/version", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store, no-cache, must-revalidate")
		c.JSON(http.StatusOK, buildinfo.Snapshot())
	})
	api.GET("/capabilities", func(c *gin.Context) {
		response.Success(c, cfg.Capabilities())
	})
	api.GET("/branding", func(c *gin.Context) {
		response.Success(c, gin.H{
			"title":             cfg.BrandTitle,
			"shortName":         cfg.BrandShortName,
			"description":       cfg.BrandDescription,
			"logoURL":           cfg.BrandLogoURL,
			"faviconURL":        cfg.BrandFaviconURL,
			"pwaIcon192URL":     cfg.BrandPWAIcon192URL,
			"pwaIcon512URL":     cfg.BrandPWAIcon512URL,
			"pwaMaskable512URL": cfg.BrandPWAMaskableIcon512URL,
			"appleTouchIconURL": cfg.BrandAppleTouchIcon180URL,
		})
	})

	h := NewHandler(svc)
	setupGroup := api.Group("/setup")
	setupGroup.GET("/status", h.GetStatus)
	setupGroup.POST("/verify", h.VerifyToken)
	setupGroup.GET("/preflight", h.Preflight)
	setupGroup.POST("/inspect-dir", h.InspectDir)
	setupGroup.POST("/test-external", h.TestExternal)
	setupGroup.POST("/apply", h.Apply)
	setupGroup.GET("/job", h.GetJob)
	setupGroup.GET("/events", h.StreamEvents)

	registerSetupFrontend(engine, resolveSetupFrontendFS(cfg))
	return engine
}

// Run 启动 Setup Mode 引导服务器；当初始化完成触发重启时，优雅排空并调用 ReExecSelf 进入正式模式。
func Run(cfg config.Config, svc *appsetup.Service, restartCh <-chan struct{}) error {
	engine := NewEngine(cfg, svc)
	port := strings.TrimSpace(cfg.HTTPPort)
	if port == "" {
		port = "8080"
	}
	addr := ":" + port
	srv := &http.Server{
		Addr:              addr,
		Handler:           engine,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	printSetupBanner(port, svc.Token(), svc.TokenFilePath())

	errCh := make(chan error, 1)
	go func() {
		if serveErr := srv.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
		}
		close(errCh)
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	shouldRestart := false
	select {
	case serveErr := <-errCh:
		return serveErr
	case <-sigCh:
	case <-restartCh:
		shouldRestart = true
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	svc.CleanupTokenFile()

	if shouldRestart {
		fmt.Fprintf(os.Stderr, "\n[FireCortex] Setup complete. Restarting into normal server mode...\n")
		if execErr := appsetup.ReExecSelf(); execErr != nil {
			return fmt.Errorf("setup completed, please restart FireCortex manually: %w", execErr)
		}
	}
	return nil
}

func printSetupBanner(port, token, tokenFile string) {
	fmt.Fprintf(os.Stderr, "\n"+
		"====================================================================\n"+
		"  FireCortex Setup Mode (First-Run Initialization Wizard)\n"+
		"  Open:        http://<server-ip>:%s/setup\n"+
		"  Setup Token: %s\n"+
		"  Token File:  %s (mode 0600, deleted after setup)\n"+
		"====================================================================\n\n",
		port, token, tokenFile,
	)
}

func setupCORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		if origin != "" && (strings.HasPrefix(origin, "http://localhost:") || strings.HasPrefix(origin, "http://127.0.0.1:")) {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Headers", "Content-Type, X-Setup-Token, X-Client-Platform")
			c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusNoContent)
				return
			}
		}
		c.Next()
	}
}

func resolveSetupFrontendFS(cfg config.Config) fs.FS {
	if dir := strings.TrimSpace(cfg.FrontendDistDir); dir != "" {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return os.DirFS(dir)
		}
	}
	return webui.FS()
}

func registerSetupFrontend(engine *gin.Engine, fsys fs.FS) {
	engine.NoRoute(func(c *gin.Context) {
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Status(http.StatusNotFound)
			return
		}
		reqPath := path.Clean("/" + strings.TrimPrefix(c.Request.URL.Path, "/"))
		if strings.HasPrefix(reqPath, "/api/") || reqPath == "/api" {
			writeSetupError(c, http.StatusNotFound, "setup.not_found", "Setup Mode 仅开放 /api/v1/setup/* 接口")
			return
		}

		if fsys == nil {
			c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(fallbackSetupHTML))
			return
		}

		// 静态资源（JS/CSS/字体/图标）直接提供
		trimmed := strings.TrimPrefix(reqPath, "/")
		if trimmed != "" && isStaticFile(fsys, trimmed) {
			serveFSFile(c, fsys, trimmed)
			return
		}

		// 非 /setup 页面统一 302 跳转至 /setup
		if reqPath != "/setup" {
			c.Redirect(http.StatusFound, "/setup")
			return
		}

		for _, candidate := range []string{"setup.html", "setup/index.html", "index.html"} {
			if isStaticFile(fsys, candidate) {
				c.Header("Cache-Control", "no-cache")
				serveFSFile(c, fsys, candidate)
				return
			}
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(fallbackSetupHTML))
	})
}

func isStaticFile(fsys fs.FS, name string) bool {
	if !fs.ValidPath(name) {
		return false
	}
	info, err := fs.Stat(fsys, name)
	return err == nil && !info.IsDir()
}

func serveFSFile(c *gin.Context, fsys fs.FS, name string) {
	file, err := fsys.Open(name)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	content, ok := file.(io.ReadSeeker)
	if !ok {
		body, readErr := io.ReadAll(file)
		if readErr != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		content = bytes.NewReader(body)
	}
	http.ServeContent(c.Writer, c.Request, info.Name(), info.ModTime(), content)
}

const fallbackSetupHTML = `<!doctype html>
<html lang="zh-CN">
<head><meta charset="utf-8"><title>FireCortex Setup Mode</title></head>
<body style="font-family:system-ui,sans-serif;max-width:640px;margin:64px auto;padding:0 24px;line-height:1.6">
<h1>FireCortex Setup Mode</h1>
<p>当前二进制未嵌入静态前端资源（通常发生在开发模式下）。</p>
<p>请使用 <code>pnpm build:binary</code> 构建完整二进制，或在开发环境下访问前端开发服务的 <code>/setup</code> 页面。</p>
</body>
</html>`
