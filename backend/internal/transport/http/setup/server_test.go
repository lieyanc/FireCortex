package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	appsetup "github.com/lieyanc/FireCortex/backend/internal/application/setup"
	"github.com/lieyanc/FireCortex/backend/internal/infra/config"
	"github.com/lieyanc/FireCortex/backend/internal/infra/dockermgr"
	managedports "github.com/lieyanc/FireCortex/backend/internal/ports/managedinfra"
)

type noopSetupInfra struct{}

func (noopSetupInfra) ProbePostgres(context.Context, string) (managedports.PostgresProbeResult, error) {
	return managedports.PostgresProbeResult{}, nil
}
func (noopSetupInfra) ProbeRedis(context.Context, managedports.RedisProbeInput) error { return nil }
func (noopSetupInfra) InitDBAndAdmin(context.Context, managedports.InitDBAdminInput) (bool, error) {
	return false, nil
}

func TestSetupHTTPAuthAndRoutes(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		HTTPPort:       "8080",
		BrandTitle:     "FireCortex",
		ManagedDataDir: filepath.Join(dir, "managed"),
	}
	svc, err := appsetup.NewService(cfg, filepath.Join(dir, "config.json"), appsetup.Dependencies{
		Host:  dockermgr.NewHostOps(),
		Infra: noopSetupInfra{},
	}, nil)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}
	defer svc.CleanupTokenFile()

	engine := NewEngine(cfg, svc)

	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/setup/status", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /status = %d, want 200", w.Code)
	}
	var statusResp struct {
		Data SetupStatusResponse `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &statusResp); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !statusResp.Data.NeedsSetup || statusResp.Data.Authenticated || statusResp.Data.Defaults != nil {
		t.Fatalf("unexpected unauthenticated status response: %+v", statusResp.Data)
	}

	w = httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/setup/preflight", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated /preflight = %d, want 401", w.Code)
	}

	body, _ := json.Marshal(VerifyTokenRequest{Token: svc.Token()})
	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/verify", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /verify = %d, want 200: %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != appsetup.SessionCookieName || !cookies[0].HttpOnly {
		t.Fatalf("expected HttpOnly setup session cookie, got %+v", cookies)
	}

	inspectBody, _ := json.Marshal(InspectDirRequest{DataDir: filepath.Join(dir, "managed")})
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/setup/inspect-dir", bytes.NewReader(inspectBody))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookies[0])
	engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /inspect-dir = %d, want 200: %s", w.Code, w.Body.String())
	}
}
