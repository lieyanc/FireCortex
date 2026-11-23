package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lieyanc/FireCortex/backend/internal/application/contentmoderation"
	"github.com/lieyanc/FireCortex/backend/internal/application/secretrotation"
	"github.com/lieyanc/FireCortex/backend/internal/application/settings"
	appsetup "github.com/lieyanc/FireCortex/backend/internal/application/setup"
	domainuser "github.com/lieyanc/FireCortex/backend/internal/domain/user"
	"github.com/lieyanc/FireCortex/backend/internal/infra/cache"
	"github.com/lieyanc/FireCortex/backend/internal/infra/config"
	"github.com/lieyanc/FireCortex/backend/internal/infra/dockermgr"
	"github.com/lieyanc/FireCortex/backend/internal/infra/persistence"
	secretrotationrepo "github.com/lieyanc/FireCortex/backend/internal/infra/persistence/postgres/secretrotation"
	userrepo "github.com/lieyanc/FireCortex/backend/internal/infra/persistence/postgres/user"
	"github.com/lieyanc/FireCortex/backend/internal/pkg/conv"
	"github.com/lieyanc/FireCortex/backend/internal/pkg/secretbox"
	managedports "github.com/lieyanc/FireCortex/backend/internal/ports/managedinfra"
	"github.com/lieyanc/FireCortex/backend/internal/repository"
	setuphttp "github.com/lieyanc/FireCortex/backend/internal/transport/http/setup"
	"golang.org/x/crypto/bcrypt"
)

const (
	setupBcryptCost     = 12
	setupMinPasswordLen = 8
)

type setupInfraAdapter struct{}

func (setupInfraAdapter) ProbePostgres(ctx context.Context, dsn string) (managedports.PostgresProbeResult, error) {
	res, err := persistence.ProbePostgres(ctx, dsn)
	if err != nil {
		return managedports.PostgresProbeResult{}, err
	}
	return managedports.PostgresProbeResult{
		ServerVersion:   res.ServerVersion,
		VectorInstalled: res.VectorInstalled,
		VectorAvailable: res.VectorAvailable,
	}, nil
}

func (setupInfraAdapter) ProbeRedis(ctx context.Context, input managedports.RedisProbeInput) error {
	backend, err := cache.Open(config.Config{
		CacheDriver:                "redis",
		RedisAddr:                  input.Addr,
		RedisUsername:              input.Username,
		RedisPassword:              input.Password,
		RedisDB:                    input.DB,
		RedisTLSEnabled:            input.TLSEnabled,
		RedisTLSInsecureSkipVerify: input.InsecureSkipVerify,
	})
	if err != nil {
		return err
	}
	defer backend.Close()
	pingCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	return backend.Ping(pingCtx)
}

func (setupInfraAdapter) InitDBAndAdmin(ctx context.Context, input managedports.InitDBAdminInput) (bool, error) {
	db, err := persistence.Open(config.Config{
		Env:            input.Env,
		DatabaseDriver: "postgres",
		PostgresDSN:    input.PostgresDSN,
	})
	if err != nil {
		return false, err
	}
	if sqlDB, dbErr := db.DB(); dbErr == nil {
		defer sqlDB.Close()
	}

	keyring, err := secretbox.NewKeyring(input.DataEncryptionKey, input.PreviousEncryptionKeys...)
	if err != nil {
		return false, err
	}
	verifier := secretrotation.NewService(secretrotationrepo.NewRepo(db), keyring, nil)
	verifier.SetSensitiveSettings(append(settings.SensitiveSettingKeys(), contentmoderation.SensitiveSettingKeys()...), nil)
	if err := verifier.VerifyStoredKeys(ctx); err != nil {
		return false, err
	}

	repo := userrepo.NewRepo(db)
	count, err := repo.CountSuperAdmins(ctx)
	if err != nil {
		return false, err
	}
	if count > 0 {
		return true, nil
	}

	username := strings.TrimSpace(input.AdminUsername)
	if username == "" || len(input.AdminPassword) < setupMinPasswordLen {
		return false, errors.New("no existing superadmin found in database; provide an initial admin username and password")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(input.AdminPassword), setupBcryptCost)
	if err != nil {
		return false, fmt.Errorf("hash admin password: %w", err)
	}
	now := time.Now()
	displayName := strings.TrimSpace(input.AdminDisplayName)
	if displayName == "" {
		displayName = username
	}
	item := &domainuser.User{
		PublicID:              conv.NormalizePublicID(uuid.NewString()),
		Username:              username,
		DisplayName:           displayName,
		Role:                  domainuser.RoleSuperAdmin,
		Status:                domainuser.StatusActive,
		Timezone:              "Etc/UTC",
		Locale:                "zh-CN",
		UsernameChangedAt:     &now,
		OnboardingCompletedAt: &now,
	}
	if err := repo.CreateWithCredential(ctx, repository.CreateWithCredentialInput{
		User: item,
		Credential: domainuser.Credential{
			PasswordHash:      string(hash),
			PasswordAlgo:      "bcrypt",
			PasswordEnabled:   true,
			PasswordUpdatedAt: &now,
			PasswordSetAt:     &now,
			PasswordOrigin:    domainuser.PasswordOriginUserSet,
			MustResetPassword: false,
		},
	}); err != nil {
		return false, err
	}
	return false, nil
}

// RunSetupMode 在组合根装配 Setup Mode 所需适配器并启动引导服务器。
func RunSetupMode(cfg config.Config, configPath string) error {
	restartCh := make(chan struct{}, 1)
	svc, err := appsetup.NewService(cfg, configPath, appsetup.Dependencies{
		Host:  dockermgr.NewHostOps(),
		Infra: setupInfraAdapter{},
	}, func() {
		select {
		case restartCh <- struct{}{}:
		default:
		}
	})
	if err != nil {
		return err
	}
	defer svc.CleanupTokenFile()
	return setuphttp.Run(cfg, svc, restartCh)
}
