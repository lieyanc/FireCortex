package app

import (
	"context"
	"errors"
	"strings"

	"github.com/lieyanc/FireCortex/backend/internal/application/audit"
	"github.com/lieyanc/FireCortex/backend/internal/application/managedinfra"
	"github.com/lieyanc/FireCortex/backend/internal/application/settings"
	appsetup "github.com/lieyanc/FireCortex/backend/internal/application/setup"
	"github.com/lieyanc/FireCortex/backend/internal/infra/config"
	"github.com/lieyanc/FireCortex/backend/internal/infra/dockermgr"
	extractionprobe "github.com/lieyanc/FireCortex/backend/internal/infra/extraction/probe"
	userrepo "github.com/lieyanc/FireCortex/backend/internal/infra/persistence/postgres/user"
	managedports "github.com/lieyanc/FireCortex/backend/internal/ports/managedinfra"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

type adminSecurityAdapter struct {
	users *userrepo.Repo
	audit *audit.Service
}

func (a adminSecurityAdapter) VerifySuperAdminPassword(ctx context.Context, userID uint, currentPassword string) error {
	if userID == 0 || strings.TrimSpace(currentPassword) == "" {
		return errors.New("current superadmin password is required")
	}
	cred, err := a.users.GetCredentialByUserID(ctx, userID)
	if err != nil || cred == nil || !cred.PasswordEnabled || strings.TrimSpace(cred.PasswordHash) == "" {
		return errors.New("unable to verify current superadmin password")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(cred.PasswordHash), []byte(currentPassword)); err != nil {
		return errors.New("current superadmin password is incorrect")
	}
	return nil
}

func (a adminSecurityAdapter) RecordAudit(ctx context.Context, actorUserID uint, action, resource, resourceID string, detail any) {
	if a.audit == nil {
		return
	}
	a.audit.Write(ctx, audit.WriteInput{
		ActorUserID: actorUserID,
		Action:      action,
		Resource:    resource,
		ResourceID:  resourceID,
		Detail:      detail,
	})
}

type settingsSyncAdapter struct {
	settingsService *settings.Service
	runtimeSettings *settings.RuntimeSettings
	runtimeCfg      *config.Runtime
}

func (a settingsSyncAdapter) ApplyExtractionManagedURL(ctx context.Context, serviceID string, baseURL string) error {
	if a.settingsService == nil || strings.TrimSpace(baseURL) == "" {
		return nil
	}
	var patches []settings.PatchItem
	switch serviceID {
	case managedports.OptionalServiceTika:
		patches = []settings.PatchItem{
			{Namespace: "extract", Key: "tika_source", Value: "managed"},
			{Namespace: "extract", Key: "tika_base_url", Value: baseURL},
		}
	case managedports.OptionalServiceDocling:
		patches = []settings.PatchItem{
			{Namespace: "extract", Key: "docling_base_url", Value: baseURL},
		}
	case managedports.OptionalServiceTesseract:
		patches = []settings.PatchItem{
			{Namespace: "extract", Key: "tesseract_ocr_base_url", Value: baseURL},
		}
	case managedports.OptionalServiceRapidOCR:
		patches = []settings.PatchItem{
			{Namespace: "extract", Key: "rapidocr_source", Value: "managed"},
			{Namespace: "extract", Key: "rapidocr_base_url", Value: baseURL},
		}
	default:
		return nil
	}
	if _, err := a.settingsService.BatchUpdate(ctx, patches); err != nil {
		return err
	}
	if a.runtimeSettings != nil && a.runtimeCfg != nil {
		return a.runtimeSettings.ApplyTo(ctx, a.runtimeCfg)
	}
	return nil
}

type restartTriggerAdapter struct {
	coord *restartCoordinator
}

func (a restartTriggerAdapter) TriggerRestart(reason string) {
	if a.coord == nil {
		return
	}
	_ = a.coord.beforeExec(reason)
	if err := appsetup.ReExecSelf(); err != nil {
		a.coord.onExecFailure(err)
	}
}

func newInfraManager(
	runtimeCfg *config.Runtime,
	users *userrepo.Repo,
	auditService *audit.Service,
	settingsService *settings.Service,
	runtimeSettings *settings.RuntimeSettings,
	restart *restartCoordinator,
	log *zap.Logger,
) *managedinfra.Manager {
	return managedinfra.NewManager(managedinfra.ManagerDependencies{
		Runtime: runtimeCfg,
		HostOps: dockermgr.NewHostOps(),
		Infra:   setupInfraAdapter{},
		Prober:  extractionprobe.Prober{},
		Security: adminSecurityAdapter{
			users: users,
			audit: auditService,
		},
		SettingsSync: settingsSyncAdapter{
			settingsService: settingsService,
			runtimeSettings: runtimeSettings,
			runtimeCfg:      runtimeCfg,
		},
		Restarter: restartTriggerAdapter{coord: restart},
		Logger:    log.Named("managedinfra"),
	})
}
