package managedinfra

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lieyanc/FireCortex/backend/internal/infra/config"
	managedports "github.com/lieyanc/FireCortex/backend/internal/ports/managedinfra"
)

const (
	// ConfirmTextRestore 是高危操作区恢复备份的强制确认词。
	ConfirmTextRestore = "RESTORE"
	// ConfirmTextSwitchDatabase 是高危操作区切换数据库的强制确认词。
	ConfirmTextSwitchDatabase = "SWITCH DATABASE"
	// ConfirmTextRecreate 是高危操作区重建托管容器的强制确认词。
	ConfirmTextRecreate = "RECREATE"
)

// InfraConfigView 返回可安全在管理后台展示和编辑的常用 config.json 基础设施视图（严格脱敏）。
type InfraConfigView struct {
	ConfigFile                   string
	Env                          string
	HTTPPort                     string
	PublicAPIBaseURL             string
	PublicWebBaseURL             string
	CORSAllowOrigin              string
	TrustedProxies               string
	SSRFProtectionEnabled        bool
	SSRFAllowedHosts             string
	SSRFAllowedCIDRs             string
	StorageBackend               string
	StorageRootDir               string
	StorageS3Endpoint            string
	StorageS3Region              string
	StorageS3Bucket              string
	StorageS3Prefix              string
	StorageS3AccessKeyID         string
	StorageS3SecretConfigured    bool
	StorageS3ForcePathStyle      bool
	ManagedDataDir               string
	ManagedRegistryMirror        string
	ManagedComposeSync           bool
	ManagedPostgresEnabled       bool
	ManagedPostgresMemoryLimitMB int
	ManagedRedisEnabled          bool
	ManagedRedisMemoryLimitMB    int
	ManagedBackupEnabled         bool
	ManagedBackupSchedule        string
	ManagedBackupRetentionCount  int
	UpdateEnabled                bool
	UpdateChannel                string
	UpdateCheckIntervalSeconds   int
	UpdateSource                 string
	UpdateProxyBaseURL           string
}

// DangerRestoreInput 描述高危备份恢复输入参数。
type DangerRestoreInput struct {
	BackupID        string
	CurrentPassword string
	ConfirmText     string
}

// DangerRestoreResult 描述恢复执行结果（含自动保护快照文件名）。
type DangerRestoreResult struct {
	RestoredBackupID string
	PreRestoreBackup string
	RestartScheduled bool
}

// DangerSwitchDatabaseInput 描述高危数据库/缓存模式切换参数。
type DangerSwitchDatabaseInput struct {
	CurrentPassword       string
	ConfirmText           string
	DatabaseMode          string // managed | external
	ExternalPostgresDSN   string
	CacheMode             string // managed_redis | external_redis | memory
	ExternalRedisAddr     string
	ExternalRedisUsername string
	ExternalRedisPassword string
	ExternalRedisDB       int
	ExternalRedisTLS      bool
	ExternalRedisSkipTLS  bool
}

// DangerRecreateInput 描述高危重建托管容器参数（严格保留宿主机数据目录）。
type DangerRecreateInput struct {
	ServiceID       string // postgres | redis
	PullLatest      bool
	CurrentPassword string
	ConfirmText     string
}

// GetInfraConfig 返回当前 config.json 中的常用基础设施脱敏视图。
func (m *Manager) GetInfraConfig() InfraConfigView {
	cfg := m.snapshotConfig()
	return InfraConfigView{
		ConfigFile:                   cfg.ConfigFile,
		Env:                          cfg.Env,
		HTTPPort:                     cfg.HTTPPort,
		PublicAPIBaseURL:             cfg.PublicAPIBaseURL,
		PublicWebBaseURL:             cfg.PublicWebBaseURL,
		CORSAllowOrigin:              cfg.CORSAllowOrigin,
		TrustedProxies:               cfg.TrustedProxies,
		SSRFProtectionEnabled:        cfg.SSRFProtectionEnabled,
		SSRFAllowedHosts:             cfg.SSRFAllowedHosts,
		SSRFAllowedCIDRs:             cfg.SSRFAllowedCIDRs,
		StorageBackend:               cfg.StorageBackend,
		StorageRootDir:               cfg.StorageRootDir,
		StorageS3Endpoint:            cfg.StorageS3Endpoint,
		StorageS3Region:              cfg.StorageS3Region,
		StorageS3Bucket:              cfg.StorageS3Bucket,
		StorageS3Prefix:              cfg.StorageS3Prefix,
		StorageS3AccessKeyID:         cfg.StorageS3AccessKeyID,
		StorageS3SecretConfigured:    strings.TrimSpace(cfg.StorageS3SecretAccessKey) != "",
		StorageS3ForcePathStyle:      cfg.StorageS3ForcePathStyle,
		ManagedDataDir:               cfg.EffectiveManagedDataDir(),
		ManagedRegistryMirror:        cfg.ManagedRegistryMirror,
		ManagedComposeSync:           cfg.ManagedComposeSync,
		ManagedPostgresEnabled:       cfg.ManagedPostgresEnabled,
		ManagedPostgresMemoryLimitMB: cfg.ManagedPostgresMemoryLimitMB,
		ManagedRedisEnabled:          cfg.ManagedRedisEnabled,
		ManagedRedisMemoryLimitMB:    cfg.ManagedRedisMemoryLimitMB,
		ManagedBackupEnabled:         cfg.ManagedBackupEnabled,
		ManagedBackupSchedule:        cfg.ManagedBackupSchedule,
		ManagedBackupRetentionCount:  cfg.ManagedBackupRetentionCount,
		UpdateEnabled:                cfg.UpdateEnabled,
		UpdateChannel:                cfg.UpdateChannel,
		UpdateCheckIntervalSeconds:   cfg.UpdateCheckIntervalSeconds,
		UpdateSource:                 cfg.UpdateSource,
		UpdateProxyBaseURL:           cfg.UpdateProxyBaseURL,
	}
}

// PatchInfraConfig 校验并原子保存常用基础设施配置到 config.json，可选触发原地平滑重启。
func (m *Manager) PatchInfraConfig(ctx context.Context, actorUserID uint, patch config.InfraConfigPatch, restartNow bool) (InfraConfigView, bool, error) {
	if patch.ManagedBackupSchedule != nil {
		if _, err := backupCron.Parse(*patch.ManagedBackupSchedule); err != nil {
			return InfraConfigView{}, false, fmt.Errorf("invalid backup schedule: %w", err)
		}
	}

	cur := m.snapshotConfig()
	updated, err := config.PatchInfraConfig(cur.ConfigFile, patch)
	if err != nil {
		return InfraConfigView{}, false, err
	}

	// 将静态变更同步进当前快照（动态业务设置保留原有值）
	nextSnap := cur
	nextSnap.Env = updated.Env
	nextSnap.PublicAPIBaseURL = updated.PublicAPIBaseURL
	nextSnap.PublicWebBaseURL = updated.PublicWebBaseURL
	nextSnap.CORSAllowOrigin = updated.CORSAllowOrigin
	nextSnap.TrustedProxies = updated.TrustedProxies
	nextSnap.SSRFProtectionEnabled = updated.SSRFProtectionEnabled
	nextSnap.SSRFAllowedHosts = updated.SSRFAllowedHosts
	nextSnap.SSRFAllowedCIDRs = updated.SSRFAllowedCIDRs
	nextSnap.StorageBackend = updated.StorageBackend
	nextSnap.StorageRootDir = updated.StorageRootDir
	nextSnap.StorageS3Endpoint = updated.StorageS3Endpoint
	nextSnap.StorageS3Region = updated.StorageS3Region
	nextSnap.StorageS3Bucket = updated.StorageS3Bucket
	nextSnap.StorageS3Prefix = updated.StorageS3Prefix
	nextSnap.StorageS3AccessKeyID = updated.StorageS3AccessKeyID
	nextSnap.StorageS3SecretAccessKey = updated.StorageS3SecretAccessKey
	nextSnap.StorageS3ForcePathStyle = updated.StorageS3ForcePathStyle
	nextSnap.ManagedRegistryMirror = updated.ManagedRegistryMirror
	nextSnap.ManagedComposeSync = updated.ManagedComposeSync
	nextSnap.ManagedPostgresMemoryLimitMB = updated.ManagedPostgresMemoryLimitMB
	nextSnap.ManagedRedisMemoryLimitMB = updated.ManagedRedisMemoryLimitMB
	nextSnap.ManagedBackupEnabled = updated.ManagedBackupEnabled
	nextSnap.ManagedBackupSchedule = updated.ManagedBackupSchedule
	nextSnap.ManagedBackupRetentionCount = updated.ManagedBackupRetentionCount
	nextSnap.UpdateEnabled = updated.UpdateEnabled
	nextSnap.UpdateChannel = updated.UpdateChannel
	nextSnap.UpdateCheckIntervalSeconds = updated.UpdateCheckIntervalSeconds
	nextSnap.UpdateSource = updated.UpdateSource
	nextSnap.UpdateProxyBaseURL = updated.UpdateProxyBaseURL
	if m.runtimeCfg != nil {
		m.runtimeCfg.Store(nextSnap)
	}

	if nextSnap.ManagedComposeSync && m.hostOps != nil {
		client, _ := m.hostOps.NewDockerClient(nextSnap.EffectiveManagedDockerHost())
		m.syncActiveCompose(ctx, nextSnap, client)
	}

	m.recordAudit(ctx, actorUserID, "infra.config.patch", "infra_config", nextSnap.ConfigFile, map[string]any{
		"restartNow": restartNow,
	})

	restarting := false
	if restartNow && m.restarter != nil {
		restarting = true
		go func() {
			time.Sleep(500 * time.Millisecond)
			m.restarter.TriggerRestart("infra_config_updated")
		}()
	}
	return m.GetInfraConfig(), restarting, nil
}

// DangerRestoreBackup 高危操作：校验 SHA-256 → 强制先拍 pre-restore 保护快照 → pg_restore 恢复 → 平滑重启。
func (m *Manager) DangerRestoreBackup(ctx context.Context, actorUserID uint, input DangerRestoreInput) (DangerRestoreResult, error) {
	if err := m.verifyDangerGate(ctx, actorUserID, input.CurrentPassword, input.ConfirmText, ConfirmTextRestore); err != nil {
		return DangerRestoreResult{}, err
	}
	cfg := m.snapshotConfig()
	if !cfg.ManagedPostgresEnabled {
		return DangerRestoreResult{}, errors.New("backup restore is only available for FireCortex-managed PostgreSQL")
	}

	backupPath, cleanID, err := m.ResolveBackupPath(input.BackupID)
	if err != nil {
		return DangerRestoreResult{}, err
	}
	if err := verifyFileSHA256(backupPath); err != nil {
		return DangerRestoreResult{}, err
	}

	// 恢复前强制自动留一份保护快照
	preSnap, err := m.CreateBackup(ctx, actorUserID, "pre-restore")
	if err != nil {
		return DangerRestoreResult{}, fmt.Errorf("failed to create safety pre-restore snapshot (restore aborted): %w", err)
	}

	client, err := m.hostOps.NewDockerClient(cfg.EffectiveManagedDockerHost())
	if err != nil {
		return DangerRestoreResult{}, err
	}
	user, password, dbName := ParsePostgresDSN(cfg.PostgresDSN)
	env := []string{"PGPASSWORD=" + password}

	// 终止其他占用连接，避免 pg_restore --clean 被锁死
	_, _ = client.Exec(ctx, managedports.PostgresContainerName, []string{
		"psql", "-U", user, "-d", "postgres", "-c",
		fmt.Sprintf("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s' AND pid <> pg_backend_pid();", strings.ReplaceAll(dbName, "'", "''")),
	}, env)

	f, err := os.Open(backupPath)
	if err != nil {
		return DangerRestoreResult{}, fmt.Errorf("open backup archive: %w", err)
	}
	defer f.Close()

	gzReader, err := gzip.NewReader(f)
	if err != nil {
		return DangerRestoreResult{}, fmt.Errorf("decompress backup archive: %w", err)
	}
	defer gzReader.Close()

	res, err := client.ExecWithStdin(
		ctx,
		managedports.PostgresContainerName,
		[]string{"pg_restore", "--single-transaction", "--clean", "--if-exists", "--no-owner", "--no-privileges", "-U", user, "-d", dbName},
		env,
		gzReader,
	)
	if err != nil {
		return DangerRestoreResult{}, fmt.Errorf("pg_restore failed (pre-restore backup kept at %s): %w", preSnap.FileName, err)
	}
	if res.ExitCode != 0 {
		return DangerRestoreResult{}, fmt.Errorf("pg_restore exited with code %d (pre-restore backup kept at %s): %s", res.ExitCode, preSnap.FileName, res.Stderr)
	}

	m.recordAudit(ctx, actorUserID, "infra.danger.restore_backup", "postgres_backup", cleanID, map[string]any{
		"preRestoreBackup": preSnap.FileName,
	})

	restarting := false
	if m.restarter != nil {
		restarting = true
		go func() {
			time.Sleep(600 * time.Millisecond)
			m.restarter.TriggerRestart("danger_restore_backup")
		}()
	}
	return DangerRestoreResult{
		RestoredBackupID: cleanID,
		PreRestoreBackup: preSnap.FileName,
		RestartScheduled: restarting,
	}, nil
}

// DangerSwitchDatabase 高危操作：先测连通性 → 不删旧数据 → 原子切换 config.json → 平滑重启。
func (m *Manager) DangerSwitchDatabase(ctx context.Context, actorUserID uint, input DangerSwitchDatabaseInput) (bool, error) {
	if err := m.verifyDangerGate(ctx, actorUserID, input.CurrentPassword, input.ConfirmText, ConfirmTextSwitchDatabase); err != nil {
		return false, err
	}
	if m.infra == nil {
		return false, errors.New("database probe adapter is unavailable")
	}

	dbMode := strings.ToLower(strings.TrimSpace(input.DatabaseMode))
	if dbMode != "managed" && dbMode != "external" {
		return false, fmt.Errorf("invalid database mode %q", input.DatabaseMode)
	}
	cacheMode := strings.ToLower(strings.TrimSpace(input.CacheMode))
	if cacheMode != "managed_redis" && cacheMode != "external_redis" && cacheMode != "memory" {
		return false, fmt.Errorf("invalid cache mode %q", input.CacheMode)
	}

	cfg := m.snapshotConfig()
	patch := config.SwitchDatabasePatch{
		PostgresImage: cfg.ManagedPostgresImage,
		PostgresPort:  cfg.ManagedPostgresPort,
	}

	if dbMode == "external" {
		dsn := strings.TrimSpace(input.ExternalPostgresDSN)
		if dsn == "" {
			return false, errors.New("external PostgreSQL DSN is required")
		}
		if _, err := m.infra.ProbePostgres(ctx, dsn); err != nil {
			return false, fmt.Errorf("target PostgreSQL connectivity check failed: %w", err)
		}
		patch.ManagedPostgresEnabled = false
		patch.PostgresDSN = dsn
	} else {
		client, err := m.hostOps.NewDockerClient(cfg.EffectiveManagedDockerHost())
		if err != nil {
			return false, err
		}
		dirInfo := m.hostOps.InspectManagedRoot(cfg.EffectiveManagedDataDir(), cfg.ManagedPostgresDataDir, cfg.ManagedRedisDataDir, cfg.ManagedBackupDir)
		if err := managedports.ValidatePostgresImageForDataVersion(cfg.ManagedPostgresImage, dirInfo.PGVersion); err != nil {
			return false, err
		}
		if err := m.hostOps.PrepareManagedDirs(cfg.EffectiveManagedDataDir(), cfg.EffectiveManagedPostgresDataDir(), cfg.EffectiveManagedRedisDataDir(), cfg.EffectiveManagedBackupDir(), cfg.EffectiveManagedServicesDir()); err != nil {
			return false, err
		}
		root := cfg.EffectiveManagedDataDir()
		var current CoreCredentials
		if cfg.ManagedPostgresEnabled {
			current.User, current.Password, current.Database = ParsePostgresDSN(cfg.PostgresDSN)
		}
		creds, err := PrepareCoreCredentials(ctx, client, root, managedports.CoreServicePostgres, current, m.hostOps.ReadExistingComposeEnv(root))
		if err != nil {
			return false, err
		}
		user, password, dbName := creds.User, creds.Password, creds.Database
		pgPort := cfg.ManagedPostgresPort
		if pgPort <= 0 {
			pgPort = 5432
		}
		pgSpec := m.hostOps.PostgresSpec(managedports.PostgresContainerConfig{
			Image:          cfg.ManagedPostgresImage,
			RegistryMirror: cfg.ManagedRegistryMirror,
			Network:        cfg.EffectiveManagedNetwork(),
			Port:           pgPort,
			DataDir:        cfg.EffectiveManagedPostgresDataDir(),
			User:           user,
			Password:       password,
			Database:       dbName,
			MemoryLimitMB:  cfg.ManagedPostgresMemoryLimitMB,
		})
		if err := EnsurePostgres(ctx, client, pgSpec, user, dbName, nil); err != nil {
			return false, fmt.Errorf("prepare managed PostgreSQL container: %w", err)
		}
		patch.ManagedPostgresEnabled = true
		patch.PostgresPort = pgPort
		patch.PostgresDSN = BuildPostgresDSN("127.0.0.1", pgPort, user, password, dbName)
		if _, err := m.infra.ProbePostgres(ctx, patch.PostgresDSN); err != nil {
			return false, fmt.Errorf("verify managed PostgreSQL credentials: %w", err)
		}
	}

	switch cacheMode {
	case "memory":
		patch.CacheDriver = "memory"
		patch.ManagedRedisEnabled = false
	case "external_redis":
		addr := strings.TrimSpace(input.ExternalRedisAddr)
		if addr == "" {
			return false, errors.New("external Redis address is required")
		}
		if err := m.infra.ProbeRedis(ctx, managedports.RedisProbeInput{
			Addr:               addr,
			Username:           strings.TrimSpace(input.ExternalRedisUsername),
			Password:           input.ExternalRedisPassword,
			DB:                 input.ExternalRedisDB,
			TLSEnabled:         input.ExternalRedisTLS,
			InsecureSkipVerify: input.ExternalRedisSkipTLS,
		}); err != nil {
			return false, fmt.Errorf("target Redis connectivity check failed: %w", err)
		}
		patch.CacheDriver = "redis"
		patch.ManagedRedisEnabled = false
		patch.RedisAddr = addr
		patch.RedisUsername = strings.TrimSpace(input.ExternalRedisUsername)
		patch.RedisPassword = input.ExternalRedisPassword
		patch.RedisDB = input.ExternalRedisDB
		patch.RedisTLSEnabled = input.ExternalRedisTLS
		patch.RedisTLSInsecureSkip = input.ExternalRedisSkipTLS
	case "managed_redis":
		client, err := m.hostOps.NewDockerClient(cfg.EffectiveManagedDockerHost())
		if err != nil {
			return false, err
		}
		rdPort := cfg.ManagedRedisPort
		if rdPort <= 0 {
			rdPort = 6379
		}
		if err := m.hostOps.PrepareManagedDirs(cfg.EffectiveManagedDataDir(), "", cfg.EffectiveManagedRedisDataDir(), "", ""); err != nil {
			return false, err
		}
		root := cfg.EffectiveManagedDataDir()
		var current CoreCredentials
		if cfg.ManagedRedisEnabled {
			current.Password = cfg.RedisPassword
		}
		creds, err := PrepareCoreCredentials(ctx, client, root, managedports.CoreServiceRedis, current, m.hostOps.ReadExistingComposeEnv(root))
		if err != nil {
			return false, err
		}
		rdPass := creds.Password
		rdSpec := m.hostOps.RedisSpec(managedports.RedisContainerConfig{
			Image:          cfg.ManagedRedisImage,
			RegistryMirror: cfg.ManagedRegistryMirror,
			Network:        cfg.EffectiveManagedNetwork(),
			Port:           rdPort,
			DataDir:        cfg.EffectiveManagedRedisDataDir(),
			Password:       rdPass,
			MemoryLimitMB:  cfg.ManagedRedisMemoryLimitMB,
		})
		if err := EnsureRedis(ctx, client, rdSpec, rdPass, nil); err != nil {
			return false, fmt.Errorf("prepare managed Redis container: %w", err)
		}
		patch.CacheDriver = "redis"
		patch.ManagedRedisEnabled = true
		patch.RedisAddr = fmt.Sprintf("127.0.0.1:%d", rdPort)
		patch.RedisPassword = rdPass
	}

	if cfg.ManagedPostgresEnabled && !patch.ManagedPostgresEnabled {
		user, password, db := ParsePostgresDSN(cfg.PostgresDSN)
		if err := saveCoreCredentials(filepath.Join(cfg.EffectiveManagedDataDir(), ".postgres-credentials.json"), CoreCredentials{User: user, Password: password, Database: db}); err != nil {
			return false, fmt.Errorf("retain managed PostgreSQL credentials: %w", err)
		}
	}
	if cfg.ManagedRedisEnabled && !patch.ManagedRedisEnabled {
		if err := saveCoreCredentials(filepath.Join(cfg.EffectiveManagedDataDir(), ".redis-credentials.json"), CoreCredentials{Password: cfg.RedisPassword}); err != nil {
			return false, fmt.Errorf("retain managed Redis credentials: %w", err)
		}
	}
	if _, err := config.PatchDatabaseConfig(cfg.ConfigFile, patch); err != nil {
		return false, err
	}

	m.recordAudit(ctx, actorUserID, "infra.danger.switch_database", "infra_config", cfg.ConfigFile, map[string]any{
		"databaseMode": dbMode,
		"cacheMode":    cacheMode,
	})

	restarting := false
	if m.restarter != nil {
		restarting = true
		go func() {
			time.Sleep(600 * time.Millisecond)
			m.restarter.TriggerRestart("danger_switch_database")
		}()
	}
	return restarting, nil
}

// DangerRecreateContainer 高危操作：强制重建核心托管容器（严格保留宿主机数据目录，PostgreSQL 重建前先拍保护快照）。
func (m *Manager) DangerRecreateContainer(ctx context.Context, actorUserID uint, input DangerRecreateInput) (JobSnapshot, error) {
	if err := m.verifyDangerGate(ctx, actorUserID, input.CurrentPassword, input.ConfirmText, ConfirmTextRecreate); err != nil {
		return JobSnapshot{}, err
	}
	serviceID := strings.ToLower(strings.TrimSpace(input.ServiceID))
	if serviceID != managedports.CoreServicePostgres && serviceID != managedports.CoreServiceRedis {
		return JobSnapshot{}, errors.New("only managed postgres or redis containers can be recreated here")
	}
	return m.StartServiceAction(ctx, actorUserID, serviceID, "pull", ServiceActionOptions{})
}

func (m *Manager) verifyDangerGate(ctx context.Context, actorUserID uint, currentPassword, confirmText, expectedConfirm string) error {
	if strings.TrimSpace(confirmText) != expectedConfirm {
		return fmt.Errorf("confirmation text must exactly match %q", expectedConfirm)
	}
	if m.security == nil {
		return errors.New("admin security verifier is not configured")
	}
	if err := m.security.VerifySuperAdminPassword(ctx, actorUserID, currentPassword); err != nil {
		return err
	}
	return nil
}
