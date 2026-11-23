package infra

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lieyanc/FireCortex/backend/internal/application/managedinfra"
	domainuser "github.com/lieyanc/FireCortex/backend/internal/domain/user"
	"github.com/lieyanc/FireCortex/backend/internal/infra/config"
	"github.com/lieyanc/FireCortex/backend/internal/infra/dockermgr"
	"github.com/lieyanc/FireCortex/backend/internal/transport/http/middleware"
)

func TestInfraRoutesRequireSuperAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mgr := managedinfra.NewManager(managedinfra.ManagerDependencies{
		Runtime: config.NewRuntime(config.Config{ManagedDataDir: t.TempDir()}),
		HostOps: dockermgr.NewHostOps(),
	})
	mod := NewModule(NewHandler(mgr))

	runWithRole := func(role string) int {
		engine := gin.New()
		g := engine.Group("/api/v1/admin", func(c *gin.Context) {
			c.Set(middleware.ContextKeyUserID, uint(1))
			c.Set(middleware.ContextKeyUserRole, role)
			c.Next()
		})
		mod.RegisterAdminRoutes(g)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/admin/infra/config", nil))
		return w.Code
	}

	if code := runWithRole(domainuser.RoleAdmin); code != http.StatusForbidden {
		t.Fatalf("expected non-superadmin admin to get 403, got %d", code)
	}
	if code := runWithRole(domainuser.RoleSuperAdmin); code != http.StatusOK {
		t.Fatalf("expected superadmin to get 200, got %d", code)
	}
}
