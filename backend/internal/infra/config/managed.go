package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// DefaultManagedNetwork 是托管容器使用的默认隔离 Docker 网络名。
	DefaultManagedNetwork = "firecortex-network"
	// DefaultManagedDataDir 是托管基础设施数据的默认根目录（相对 config.json 所在目录）。
	DefaultManagedDataDir = "./data/managed"
	// DefaultManagedPostgresImage 是默认托管的 PostgreSQL + pgvector 镜像。
	DefaultManagedPostgresImage = "pgvector/pgvector:pg16"
	// DefaultManagedRedisImage 是默认托管的 Redis 镜像。
	DefaultManagedRedisImage = "redis:7-alpine"
	// DefaultDockerSocketHost 是 Linux 宿主机默认的 Docker Engine Unix Socket 地址。
	DefaultDockerSocketHost = "unix:///var/run/docker.sock"
)

// ResolveConfigFilePath 返回当前进程将使用的配置文件路径。
func ResolveConfigFilePath() string {
	return resolveConfigFile()
}

// NeedsSetup 判断当前实例是否应当进入首次 Web 引导模式（Setup Mode）。
// 此检查只读磁盘，不会创建或改写 config.json。
//
// 判定规则：
//   - 若显式设置 BOOTSTRAP_COMPLETED=true，不进入 Setup Mode；
//   - 若 config.json 已存在：仅当其中显式存在 bootstrap.completed == false 时进入 Setup Mode；
//     老版本 config.json 缺少 bootstrap 键时视为已完成初始化，绝不误入向导；
//   - 若 config.json 不存在：
//   - 同目录存在旧版 config.yaml/config.yml（需走旧配置迁移）时不进入 Setup Mode；
//   - 显式设置了 POSTGRES_DSN 环境变量（自动化无交互部署）时不进入 Setup Mode；
//   - 否则进入 Setup Mode。
func NeedsSetup() (bool, string, error) {
	path := resolveConfigFile()
	if raw, ok := os.LookupEnv("BOOTSTRAP_COMPLETED"); ok {
		if strings.EqualFold(strings.TrimSpace(raw), "true") || strings.TrimSpace(raw) == "1" {
			return false, path, nil
		}
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if hasLegacyYAMLFile(path) {
			return false, path, nil
		}
		if strings.TrimSpace(os.Getenv("POSTGRES_DSN")) != "" {
			return false, path, nil
		}
		return true, path, nil
	}
	if err != nil {
		return false, path, fmt.Errorf("read config %s: %w", path, err)
	}

	var probe struct {
		Bootstrap *struct {
			Completed *bool `json:"completed"`
		} `json:"bootstrap"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return false, path, fmt.Errorf("parse config %s: %w", path, err)
	}
	if probe.Bootstrap != nil && probe.Bootstrap.Completed != nil && !*probe.Bootstrap.Completed {
		return true, path, nil
	}
	return false, path, nil
}

func hasLegacyYAMLFile(jsonPath string) bool {
	base := strings.TrimSuffix(jsonPath, filepath.Ext(jsonPath))
	for _, candidate := range []string{base + ".yaml", base + ".yml"} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

// LoadForSetup 在不创建或不改写 config.json 的前提下加载引导模式所需的配置默认值。
func LoadForSetup() (Config, error) {
	path := resolveConfigFile()
	fc := templateConfig()
	fc.Bootstrap.Completed = false

	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	if err == nil {
		if unmarshalErr := json.Unmarshal(data, &fc); unmarshalErr != nil {
			return Config{}, fmt.Errorf("parse config %s: %w", path, unmarshalErr)
		}
	}
	return buildConfig(path, fc), nil
}

// WritePreSetupConfig 写入未初始化状态的 config.json（供 install-service 在自定义参数时预填默认值）。
// 如果目标文件已存在则不做覆盖，返回 nil。
func WritePreSetupConfig(path string, httpPort string, managedDataDir string, dockerHost string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat config %s: %w", path, err)
	}

	fc := withGeneratedSecrets(templateConfig())
	fc.Bootstrap.Completed = false
	if port := strings.TrimSpace(httpPort); port != "" {
		fc.Server.HTTPPort = port
	}
	if dir := strings.TrimSpace(managedDataDir); dir != "" {
		fc.Managed.DataDir = dir
	}
	if host := strings.TrimSpace(dockerHost); host != "" {
		fc.Managed.DockerHost = host
	}
	data, err := encodeConfig(fc)
	if err != nil {
		return err
	}
	return writeConfigFile(path, data)
}

// InitializedConfigInput 是 Setup 向导完成后写入 config.json 的受控输入。
type InitializedConfigInput struct {
	DataEncryptionKey string
	Env               string
	HTTPPort          string
	PublicAPIBaseURL  string
	PublicWebBaseURL  string
	CORSAllowOrigin   string

	PostgresDSN string

	CacheDriver          string
	RedisAddr            string
	RedisUsername        string
	RedisPassword        string
	RedisDB              int
	RedisTLSEnabled      bool
	RedisTLSInsecureSkip bool

	ManagedDockerHost            string
	ManagedNetwork               string
	ManagedDataDir               string
	ManagedRegistryMirror        string
	ManagedComposeSync           bool
	ManagedPostgresEnabled       bool
	ManagedPostgresImage         string
	ManagedPostgresPort          int
	ManagedPostgresDataDir       string
	ManagedPostgresMemoryLimitMB int
	ManagedRedisEnabled          bool
	ManagedRedisImage            string
	ManagedRedisPort             int
	ManagedRedisDataDir          string
	ManagedRedisMemoryLimitMB    int
	ManagedBackupEnabled         bool
	ManagedBackupDir             string
	ManagedBackupSchedule        string
	ManagedBackupRetentionCount  int
}

// PreparedSetupConfig holds the exact configuration validated before adopting a database.
type PreparedSetupConfig struct {
	Config
	path    string
	encoded []byte
}

func (cfg PreparedSetupConfig) Save() error { return writeConfigFile(cfg.path, cfg.encoded) }

func PrepareInitializedConfig(path string, input InitializedConfigInput) (PreparedSetupConfig, error) {
	if strings.TrimSpace(path) == "" {
		path = resolveConfigFile()
	}

	fc := templateConfig()
	if existing, err := os.ReadFile(path); err == nil {
		if unmarshalErr := json.Unmarshal(existing, &fc); unmarshalErr != nil {
			return PreparedSetupConfig{}, fmt.Errorf("parse existing config %s: %w", path, unmarshalErr)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return PreparedSetupConfig{}, fmt.Errorf("read existing config %s: %w", path, err)
	}

	if input.DataEncryptionKey != "" {
		fc.Security.DataEncryptionKey = input.DataEncryptionKey
	}
	fc = withGeneratedSecrets(fc)
	if isPlaceholderSecret(fc.Security.JWTSecret) {
		fc.Security.JWTSecret = randomSecret()
	}
	if isPlaceholderSecret(fc.Security.DataEncryptionKey) {
		fc.Security.DataEncryptionKey = randomSecret()
	}

	if env := normalizeEnv(input.Env); env != "" {
		fc.App.Env = env
	}
	if port := strings.TrimSpace(input.HTTPPort); port != "" {
		fc.Server.HTTPPort = port
	}
	fc.Server.PublicAPIBaseURL = strings.TrimSpace(input.PublicAPIBaseURL)
	fc.Server.PublicWebBaseURL = strings.TrimSpace(input.PublicWebBaseURL)
	if cors := strings.TrimSpace(input.CORSAllowOrigin); cors != "" {
		fc.Server.CORSAllowOrigin = cors
	}

	fc.Database.Driver = "postgres"
	fc.Database.Postgres.DSN = normalizePostgresDSN(input.PostgresDSN)

	if cacheDriver := normalizeCacheDriver(input.CacheDriver); cacheDriver != "" {
		fc.Cache.Driver = cacheDriver
	}
	fc.Database.Redis.Addr = strings.TrimSpace(input.RedisAddr)
	fc.Database.Redis.Username = strings.TrimSpace(input.RedisUsername)
	fc.Database.Redis.Password = input.RedisPassword
	fc.Database.Redis.DB = input.RedisDB
	fc.Database.Redis.TLSEnabled = input.RedisTLSEnabled
	fc.Database.Redis.TLSInsecureSkipVerify = input.RedisTLSInsecureSkip

	fc.Bootstrap.Completed = true

	fc.Managed.DockerHost = strings.TrimSpace(input.ManagedDockerHost)
	if netName := strings.TrimSpace(input.ManagedNetwork); netName != "" {
		fc.Managed.Network = netName
	}
	if dataDir := strings.TrimSpace(input.ManagedDataDir); dataDir != "" {
		fc.Managed.DataDir = dataDir
	}
	fc.Managed.RegistryMirror = strings.TrimSpace(input.ManagedRegistryMirror)
	fc.Managed.ComposeSync = input.ManagedComposeSync

	fc.Managed.Postgres.Enabled = input.ManagedPostgresEnabled
	if img := strings.TrimSpace(input.ManagedPostgresImage); img != "" {
		fc.Managed.Postgres.Image = img
	}
	if input.ManagedPostgresPort > 0 {
		fc.Managed.Postgres.Port = input.ManagedPostgresPort
	}
	fc.Managed.Postgres.DataDir = strings.TrimSpace(input.ManagedPostgresDataDir)
	if input.ManagedPostgresMemoryLimitMB >= 0 {
		fc.Managed.Postgres.MemoryLimitMB = input.ManagedPostgresMemoryLimitMB
	}

	fc.Managed.Redis.Enabled = input.ManagedRedisEnabled
	if img := strings.TrimSpace(input.ManagedRedisImage); img != "" {
		fc.Managed.Redis.Image = img
	}
	if input.ManagedRedisPort > 0 {
		fc.Managed.Redis.Port = input.ManagedRedisPort
	}
	fc.Managed.Redis.DataDir = strings.TrimSpace(input.ManagedRedisDataDir)
	if input.ManagedRedisMemoryLimitMB >= 0 {
		fc.Managed.Redis.MemoryLimitMB = input.ManagedRedisMemoryLimitMB
	}

	fc.Managed.Backup.Enabled = input.ManagedBackupEnabled
	fc.Managed.Backup.Dir = strings.TrimSpace(input.ManagedBackupDir)
	if sched := strings.TrimSpace(input.ManagedBackupSchedule); sched != "" {
		fc.Managed.Backup.Schedule = sched
	}
	if input.ManagedBackupRetentionCount > 0 {
		fc.Managed.Backup.RetentionCount = input.ManagedBackupRetentionCount
	}

	candidate := buildConfig(path, fc)
	if err := candidate.Validate(); err != nil {
		return PreparedSetupConfig{}, err
	}

	encoded, err := encodeConfig(fc)
	if err != nil {
		return PreparedSetupConfig{}, err
	}
	return PreparedSetupConfig{Config: candidate, path: path, encoded: encoded}, nil
}

// HasManagedCoreServices 报告当前实例是否由 FireCortex 通过 Docker 托管核心基础设施（PostgreSQL 或 Redis）。
func (c Config) HasManagedCoreServices() bool {
	if c.LocalMode {
		return false
	}
	return c.ManagedPostgresEnabled || (c.ManagedRedisEnabled && normalizeCacheDriver(c.CacheDriver) == "redis")
}

// EffectiveManagedDockerHost 返回最终生效的 Docker Engine 地址（配置 > DOCKER_HOST > 默认 unix socket）。
func (c Config) EffectiveManagedDockerHost() string {
	if host := strings.TrimSpace(c.ManagedDockerHost); host != "" {
		return host
	}
	if envHost := strings.TrimSpace(os.Getenv("DOCKER_HOST")); envHost != "" {
		return envHost
	}
	return DefaultDockerSocketHost
}

// EffectiveManagedNetwork 返回托管容器使用的 Docker 网络名。
func (c Config) EffectiveManagedNetwork() string {
	if netName := strings.TrimSpace(c.ManagedNetwork); netName != "" {
		return netName
	}
	return DefaultManagedNetwork
}

// EffectiveManagedDataDir 返回托管基础设施数据根目录的绝对路径。
func (c Config) EffectiveManagedDataDir() string {
	dir := strings.TrimSpace(c.ManagedDataDir)
	if dir == "" {
		dir = DefaultManagedDataDir
	}
	if filepath.IsAbs(dir) {
		return filepath.Clean(dir)
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(dir)
}

// EffectiveManagedPostgresDataDir 返回托管 PostgreSQL 的宿主机 pgdata 目录。
func (c Config) EffectiveManagedPostgresDataDir() string {
	if dir := strings.TrimSpace(c.ManagedPostgresDataDir); dir != "" {
		if abs, err := filepath.Abs(dir); err == nil {
			return filepath.Clean(abs)
		}
		return filepath.Clean(dir)
	}
	return filepath.Join(c.EffectiveManagedDataDir(), "postgres", "pgdata")
}

// EffectiveManagedRedisDataDir 返回托管 Redis 的宿主机数据目录。
func (c Config) EffectiveManagedRedisDataDir() string {
	if dir := strings.TrimSpace(c.ManagedRedisDataDir); dir != "" {
		if abs, err := filepath.Abs(dir); err == nil {
			return filepath.Clean(abs)
		}
		return filepath.Clean(dir)
	}
	return filepath.Join(c.EffectiveManagedDataDir(), "redis")
}

// EffectiveManagedBackupDir 返回托管 PostgreSQL 的备份落盘目录。
func (c Config) EffectiveManagedBackupDir() string {
	if dir := strings.TrimSpace(c.ManagedBackupDir); dir != "" {
		if abs, err := filepath.Abs(dir); err == nil {
			return filepath.Clean(abs)
		}
		return filepath.Clean(dir)
	}
	return filepath.Join(c.EffectiveManagedDataDir(), "postgres", "backups")
}

// EffectiveManagedServicesDir 返回可选旁路服务的宿主机持久化/缓存目录。
func (c Config) EffectiveManagedServicesDir() string {
	return filepath.Join(c.EffectiveManagedDataDir(), "services")
}

// InfraConfigPatch 描述 superadmin 在「服务与基础设施」页面可安全修改的常用 config.json 基础设施字段。
// 明确不包含 jwt_secret 与 data_encryption_key，防止破坏已有加密数据或会话。
type InfraConfigPatch struct {
	Env                          *string
	PublicAPIBaseURL             *string
	PublicWebBaseURL             *string
	CORSAllowOrigin              *string
	TrustedProxies               *string
	SSRFProtectionEnabled        *bool
	SSRFAllowedHosts             *string
	SSRFAllowedCIDRs             *string
	StorageBackend               *string
	StorageRootDir               *string
	StorageS3Endpoint            *string
	StorageS3Region              *string
	StorageS3Bucket              *string
	StorageS3Prefix              *string
	StorageS3AccessKeyID         *string
	StorageS3SecretAccessKey     *string
	StorageS3ForcePathStyle      *bool
	ManagedRegistryMirror        *string
	ManagedComposeSync           *bool
	ManagedPostgresMemoryLimitMB *int
	ManagedRedisMemoryLimitMB    *int
	ManagedBackupEnabled         *bool
	ManagedBackupSchedule        *string
	ManagedBackupRetentionCount  *int
	UpdateEnabled                *bool
	UpdateChannel                *string
	UpdateCheckIntervalSeconds   *int
	UpdateSource                 *string
	UpdateProxyBaseURL           *string
}

// PatchInfraConfig 校验并原子更新 config.json 中的常用基础设施项（保持密钥与未知键不变）。
func PatchInfraConfig(path string, patch InfraConfigPatch) (Config, error) {
	if strings.TrimSpace(path) == "" {
		path = resolveConfigFile()
	}
	fc, err := readExistingOrTemplate(path)
	if err != nil {
		return Config{}, err
	}

	if patch.Env != nil {
		fc.App.Env = normalizeEnv(*patch.Env)
	}
	if patch.PublicAPIBaseURL != nil {
		fc.Server.PublicAPIBaseURL = strings.TrimSpace(*patch.PublicAPIBaseURL)
	}
	if patch.PublicWebBaseURL != nil {
		fc.Server.PublicWebBaseURL = strings.TrimSpace(*patch.PublicWebBaseURL)
	}
	if patch.CORSAllowOrigin != nil {
		fc.Server.CORSAllowOrigin = strings.TrimSpace(*patch.CORSAllowOrigin)
	}
	if patch.TrustedProxies != nil {
		fc.Server.TrustedProxies = strings.TrimSpace(*patch.TrustedProxies)
	}
	if patch.SSRFProtectionEnabled != nil {
		fc.Security.SSRFProtectionEnabled = *patch.SSRFProtectionEnabled
	}
	if patch.SSRFAllowedHosts != nil {
		fc.Security.SSRFAllowedHosts = strings.TrimSpace(*patch.SSRFAllowedHosts)
	}
	if patch.SSRFAllowedCIDRs != nil {
		fc.Security.SSRFAllowedCIDRs = strings.TrimSpace(*patch.SSRFAllowedCIDRs)
	}
	if patch.StorageBackend != nil {
		fc.Storage.Backend = strings.ToLower(strings.TrimSpace(*patch.StorageBackend))
	}
	if patch.StorageRootDir != nil {
		fc.Storage.Local.RootDir = strings.TrimSpace(*patch.StorageRootDir)
	}
	if patch.StorageS3Endpoint != nil {
		fc.Storage.S3.Endpoint = strings.TrimSpace(*patch.StorageS3Endpoint)
	}
	if patch.StorageS3Region != nil {
		fc.Storage.S3.Region = strings.TrimSpace(*patch.StorageS3Region)
	}
	if patch.StorageS3Bucket != nil {
		fc.Storage.S3.Bucket = strings.TrimSpace(*patch.StorageS3Bucket)
	}
	if patch.StorageS3Prefix != nil {
		fc.Storage.S3.Prefix = strings.TrimSpace(*patch.StorageS3Prefix)
	}
	if patch.StorageS3AccessKeyID != nil {
		fc.Storage.S3.AccessKeyID = strings.TrimSpace(*patch.StorageS3AccessKeyID)
	}
	if patch.StorageS3SecretAccessKey != nil && strings.TrimSpace(*patch.StorageS3SecretAccessKey) != "" {
		fc.Storage.S3.SecretAccessKey = strings.TrimSpace(*patch.StorageS3SecretAccessKey)
	}
	if patch.StorageS3ForcePathStyle != nil {
		fc.Storage.S3.ForcePathStyle = *patch.StorageS3ForcePathStyle
	}
	if patch.ManagedRegistryMirror != nil {
		fc.Managed.RegistryMirror = strings.TrimSpace(*patch.ManagedRegistryMirror)
	}
	if patch.ManagedComposeSync != nil {
		fc.Managed.ComposeSync = *patch.ManagedComposeSync
	}
	if patch.ManagedPostgresMemoryLimitMB != nil && *patch.ManagedPostgresMemoryLimitMB >= 0 {
		fc.Managed.Postgres.MemoryLimitMB = *patch.ManagedPostgresMemoryLimitMB
	}
	if patch.ManagedRedisMemoryLimitMB != nil && *patch.ManagedRedisMemoryLimitMB >= 0 {
		fc.Managed.Redis.MemoryLimitMB = *patch.ManagedRedisMemoryLimitMB
	}
	if patch.ManagedBackupEnabled != nil {
		fc.Managed.Backup.Enabled = *patch.ManagedBackupEnabled
	}
	if patch.ManagedBackupSchedule != nil && strings.TrimSpace(*patch.ManagedBackupSchedule) != "" {
		fc.Managed.Backup.Schedule = strings.TrimSpace(*patch.ManagedBackupSchedule)
	}
	if patch.ManagedBackupRetentionCount != nil && *patch.ManagedBackupRetentionCount > 0 {
		fc.Managed.Backup.RetentionCount = *patch.ManagedBackupRetentionCount
	}
	if patch.UpdateEnabled != nil {
		fc.Update.Enabled = *patch.UpdateEnabled
	}
	if patch.UpdateChannel != nil {
		ch := strings.ToLower(strings.TrimSpace(*patch.UpdateChannel))
		if ch == "stable" || ch == "dev" {
			fc.Update.Channel = ch
		}
	}
	if patch.UpdateCheckIntervalSeconds != nil && *patch.UpdateCheckIntervalSeconds >= 60 {
		fc.Update.CheckIntervalSeconds = *patch.UpdateCheckIntervalSeconds
	}
	if patch.UpdateSource != nil {
		src := strings.ToLower(strings.TrimSpace(*patch.UpdateSource))
		if src == "github" || src == "proxy" {
			fc.Update.Source = src
		}
	}
	if patch.UpdateProxyBaseURL != nil {
		fc.Update.ProxyBaseURL = strings.TrimSpace(*patch.UpdateProxyBaseURL)
	}

	candidate := buildConfig(path, fc)
	if err := candidate.Validate(); err != nil {
		return Config{}, err
	}
	encoded, err := encodeConfig(fc)
	if err != nil {
		return Config{}, err
	}
	if err := writeConfigFile(path, encoded); err != nil {
		return Config{}, err
	}
	return candidate, nil
}

// SwitchDatabasePatch 描述高危操作区中切换托管/外部数据库或缓存配置的参数。
type SwitchDatabasePatch struct {
	ManagedPostgresEnabled bool
	PostgresDSN            string
	PostgresImage          string
	PostgresPort           int
	ManagedRedisEnabled    bool
	CacheDriver            string
	RedisAddr              string
	RedisUsername          string
	RedisPassword          string
	RedisDB                int
	RedisTLSEnabled        bool
	RedisTLSInsecureSkip   bool
}

// PatchDatabaseConfig 校验并原子切换 config.json 中的数据库与缓存连接配置（不删旧数据目录）。
func PatchDatabaseConfig(path string, patch SwitchDatabasePatch) (Config, error) {
	if strings.TrimSpace(path) == "" {
		path = resolveConfigFile()
	}
	fc, err := readExistingOrTemplate(path)
	if err != nil {
		return Config{}, err
	}

	fc.Database.Driver = "postgres"
	fc.Database.Postgres.DSN = normalizePostgresDSN(patch.PostgresDSN)
	fc.Managed.Postgres.Enabled = patch.ManagedPostgresEnabled
	if img := strings.TrimSpace(patch.PostgresImage); img != "" {
		fc.Managed.Postgres.Image = img
	}
	if patch.PostgresPort > 0 {
		fc.Managed.Postgres.Port = patch.PostgresPort
	}

	if cd := normalizeCacheDriver(patch.CacheDriver); cd != "" {
		fc.Cache.Driver = cd
	}
	fc.Managed.Redis.Enabled = patch.ManagedRedisEnabled
	fc.Database.Redis.Addr = strings.TrimSpace(patch.RedisAddr)
	fc.Database.Redis.Username = strings.TrimSpace(patch.RedisUsername)
	fc.Database.Redis.Password = patch.RedisPassword
	fc.Database.Redis.DB = patch.RedisDB
	fc.Database.Redis.TLSEnabled = patch.RedisTLSEnabled
	fc.Database.Redis.TLSInsecureSkipVerify = patch.RedisTLSInsecureSkip

	candidate := buildConfig(path, fc)
	if err := candidate.Validate(); err != nil {
		return Config{}, err
	}
	encoded, err := encodeConfig(fc)
	if err != nil {
		return Config{}, err
	}
	if err := writeConfigFile(path, encoded); err != nil {
		return Config{}, err
	}
	return candidate, nil
}

func readExistingOrTemplate(path string) (fileConfig, error) {
	fc := templateConfig()
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fileConfig{}, fmt.Errorf("read config %s: %w", path, err)
	}
	if err == nil {
		if unmarshalErr := json.Unmarshal(data, &fc); unmarshalErr != nil {
			return fileConfig{}, fmt.Errorf("parse config %s: %w", path, unmarshalErr)
		}
	}
	return fc, nil
}
