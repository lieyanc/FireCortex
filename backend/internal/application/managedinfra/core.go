package managedinfra

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/lieyanc/FireCortex/backend/internal/infra/config"
	managedports "github.com/lieyanc/FireCortex/backend/internal/ports/managedinfra"
	"go.uber.org/zap"
)

const (
	defaultPGUser     = "firecortex"
	defaultPGDatabase = "firecortex"
	coreReadyTimeout  = 90 * time.Second
)

// ProgressEvent 描述核心基础设施编排过程中的实时步骤事件（领域对象，无协议标签）。
type ProgressEvent struct {
	Step      string
	Service   string
	Message   string
	Percent   int
	Status    string // running | done | failed
	Timestamp time.Time
}

// ProgressFunc 是进度回调函数签名。
type ProgressFunc func(ProgressEvent)

// GenerateStrongPassword 生成适合 PostgreSQL / Redis 的高强度随机密码。
func GenerateStrongPassword() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random password: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// BuildPostgresDSN 构造带安全 URL 编码与默认时区的 PostgreSQL 连接串。
func BuildPostgresDSN(host string, port int, user, password, dbName string) string {
	if strings.TrimSpace(host) == "" {
		host = "127.0.0.1"
	}
	if port <= 0 {
		port = 5432
	}
	if strings.TrimSpace(user) == "" {
		user = defaultPGUser
	}
	if strings.TrimSpace(dbName) == "" {
		dbName = defaultPGDatabase
	}
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, password),
		Host:   fmt.Sprintf("%s:%d", host, port),
		Path:   "/" + dbName,
	}
	q := u.Query()
	q.Set("sslmode", "disable")
	q.Set("TimeZone", "Asia/Shanghai")
	u.RawQuery = q.Encode()
	return u.String()
}

// ParsePostgresDSN 从 URL 格式或 key=value 格式的 DSN 中解析 user、password 与 dbname。
func ParsePostgresDSN(dsn string) (user, password, dbName string) {
	trimmed := strings.TrimSpace(dsn)
	user = defaultPGUser
	dbName = defaultPGDatabase
	if trimmed == "" {
		return user, "", dbName
	}

	if strings.HasPrefix(trimmed, "postgres://") || strings.HasPrefix(trimmed, "postgresql://") {
		if parsed, err := url.Parse(trimmed); err == nil {
			if parsed.User != nil {
				if u := parsed.User.Username(); u != "" {
					user = u
				}
				if p, ok := parsed.User.Password(); ok {
					password = p
				}
			}
			if db := strings.TrimPrefix(parsed.Path, "/"); db != "" {
				dbName = db
			}
			return user, password, dbName
		}
	}

	for _, token := range splitKVTokens(trimmed) {
		k, v, ok := strings.Cut(token, "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, "'\"")
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "user":
			if v != "" {
				user = v
			}
		case "password":
			password = v
		case "dbname":
			if v != "" {
				dbName = v
			}
		}
	}
	return user, password, dbName
}

func splitKVTokens(input string) []string {
	var tokens []string
	var cur strings.Builder
	inQuote := rune(0)
	escaped := false

	for _, r := range input {
		if escaped {
			cur.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' && inQuote != 0 {
			escaped = true
			continue
		}
		if inQuote != 0 {
			if r == inQuote {
				inQuote = 0
			} else {
				cur.WriteRune(r)
			}
			continue
		}
		if r == '\'' || r == '"' {
			inQuote = r
			continue
		}
		if r == ' ' || r == '\t' || r == '\n' {
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
			continue
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

// EnsurePostgres 确保托管的 PostgreSQL + pgvector 容器处于运行态并通过 pg_isready 探活。
func EnsurePostgres(
	ctx context.Context,
	client managedports.DockerClient,
	spec managedports.ContainerSpec,
	pgUser string,
	pgDatabase string,
	onProgress ProgressFunc,
) error {
	emit := func(step, msg string, pct int, status string) {
		if onProgress != nil {
			onProgress(ProgressEvent{
				Step:      step,
				Service:   managedports.CoreServicePostgres,
				Message:   msg,
				Percent:   pct,
				Status:    status,
				Timestamp: time.Now().UTC(),
			})
		}
	}

	if err := client.EnsureNetwork(ctx, spec.Network); err != nil {
		emit("network", "Failed to create Docker network: "+err.Error(), 0, "failed")
		return fmt.Errorf("ensure docker network %q: %w", spec.Network, err)
	}

	existing, err := client.InspectContainer(ctx, spec.Name)
	if err != nil {
		emit("inspect", "Failed to inspect PostgreSQL container: "+err.Error(), 0, "failed")
		return err
	}

	switch {
	case existing == nil:
		present, inspectErr := client.InspectImage(ctx, spec.Image)
		if inspectErr != nil {
			emit("pull", "Failed to inspect PostgreSQL image: "+inspectErr.Error(), 0, "failed")
			return inspectErr
		}
		if !present {
			emit("pull", fmt.Sprintf("Pulling image %s ...", spec.Image), 5, "running")
			if pullErr := client.PullImage(ctx, spec.Image, func(p managedports.PullProgress) {
				emit("pull", fmt.Sprintf("Pulling %s (%s)", spec.Image, p.Status), p.Percent, "running")
			}); pullErr != nil {
				emit("pull", "Failed to pull PostgreSQL image: "+pullErr.Error(), 0, "failed")
				return pullErr
			}
			emit("pull", fmt.Sprintf("Image %s ready", spec.Image), 100, "done")
		}
		emit("create", "Creating and starting PostgreSQL container...", 50, "running")
		id, createErr := client.CreateContainer(ctx, spec)
		if createErr != nil {
			emit("create", "Failed to create PostgreSQL container: "+createErr.Error(), 0, "failed")
			return createErr
		}
		if startErr := client.StartContainer(ctx, id); startErr != nil {
			emit("start", "Failed to start PostgreSQL container: "+startErr.Error(), 0, "failed")
			return startErr
		}
	case !existing.Running:
		emit("start", "Starting existing stopped PostgreSQL container...", 50, "running")
		if startErr := client.StartContainer(ctx, existing.ID); startErr != nil {
			emit("start", "Failed to start PostgreSQL container: "+startErr.Error(), 0, "failed")
			return startErr
		}
	default:
		emit("start", "PostgreSQL container is already running", 80, "running")
	}

	emit("health", "Waiting for PostgreSQL readiness...", 85, "running")
	if err := waitForPostgresReady(ctx, client, spec.Name, pgUser, pgDatabase, coreReadyTimeout); err != nil {
		emit("health", "PostgreSQL readiness timed out: "+err.Error(), 0, "failed")
		return err
	}
	emit("health", "PostgreSQL + pgvector is ready", 100, "done")
	return nil
}

// EnsureRedis 确保托管的 Redis 容器处于运行态并通过 PING 探活。
func EnsureRedis(
	ctx context.Context,
	client managedports.DockerClient,
	spec managedports.ContainerSpec,
	password string,
	onProgress ProgressFunc,
) error {
	emit := func(step, msg string, pct int, status string) {
		if onProgress != nil {
			onProgress(ProgressEvent{
				Step:      step,
				Service:   managedports.CoreServiceRedis,
				Message:   msg,
				Percent:   pct,
				Status:    status,
				Timestamp: time.Now().UTC(),
			})
		}
	}

	if err := client.EnsureNetwork(ctx, spec.Network); err != nil {
		emit("network", "Failed to create Docker network: "+err.Error(), 0, "failed")
		return fmt.Errorf("ensure docker network %q: %w", spec.Network, err)
	}

	existing, err := client.InspectContainer(ctx, spec.Name)
	if err != nil {
		emit("inspect", "Failed to inspect Redis container: "+err.Error(), 0, "failed")
		return err
	}

	switch {
	case existing == nil:
		present, inspectErr := client.InspectImage(ctx, spec.Image)
		if inspectErr != nil {
			emit("pull", "Failed to inspect Redis image: "+inspectErr.Error(), 0, "failed")
			return inspectErr
		}
		if !present {
			emit("pull", fmt.Sprintf("Pulling image %s ...", spec.Image), 5, "running")
			if pullErr := client.PullImage(ctx, spec.Image, func(p managedports.PullProgress) {
				emit("pull", fmt.Sprintf("Pulling %s (%s)", spec.Image, p.Status), p.Percent, "running")
			}); pullErr != nil {
				emit("pull", "Failed to pull Redis image: "+pullErr.Error(), 0, "failed")
				return pullErr
			}
			emit("pull", fmt.Sprintf("Image %s ready", spec.Image), 100, "done")
		}
		emit("create", "Creating and starting Redis container...", 50, "running")
		id, createErr := client.CreateContainer(ctx, spec)
		if createErr != nil {
			emit("create", "Failed to create Redis container: "+createErr.Error(), 0, "failed")
			return createErr
		}
		if startErr := client.StartContainer(ctx, id); startErr != nil {
			emit("start", "Failed to start Redis container: "+startErr.Error(), 0, "failed")
			return startErr
		}
	case !existing.Running:
		emit("start", "Starting existing stopped Redis container...", 50, "running")
		if startErr := client.StartContainer(ctx, existing.ID); startErr != nil {
			emit("start", "Failed to start Redis container: "+startErr.Error(), 0, "failed")
			return startErr
		}
	default:
		emit("start", "Redis container is already running", 80, "running")
	}

	emit("health", "Waiting for Redis readiness...", 85, "running")
	if err := waitForRedisReady(ctx, client, spec.Name, password, coreReadyTimeout); err != nil {
		emit("health", "Redis readiness timed out: "+err.Error(), 0, "failed")
		return err
	}
	emit("health", "Redis cache service is ready", 100, "done")
	return nil
}

func waitForPostgresReady(ctx context.Context, client managedports.DockerClient, containerName, user, dbName string, timeout time.Duration) error {
	if strings.TrimSpace(user) == "" {
		user = defaultPGUser
	}
	if strings.TrimSpace(dbName) == "" {
		dbName = defaultPGDatabase
	}
	deadline := time.Now().Add(timeout)
	var lastErr string
	for {
		res, err := client.Exec(ctx, containerName, []string{"pg_isready", "-U", user, "-d", dbName}, nil)
		if err == nil && res.ExitCode == 0 {
			return nil
		}
		if err != nil {
			lastErr = err.Error()
		} else {
			lastErr = strings.TrimSpace(res.Stdout + " " + res.Stderr)
		}
		if time.Now().After(deadline) {
			if lastErr == "" {
				lastErr = "pg_isready did not succeed in time"
			}
			return fmt.Errorf("postgres container %q not ready: %s", containerName, lastErr)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
}

func waitForRedisReady(ctx context.Context, client managedports.DockerClient, containerName, password string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var env []string
	if password != "" {
		env = []string{"REDISCLI_AUTH=" + password}
	}
	var lastErr string
	for {
		res, err := client.Exec(ctx, containerName, []string{"redis-cli", "ping"}, env)
		if err == nil && res.ExitCode == 0 && strings.Contains(strings.ToUpper(res.Stdout), "PONG") {
			return nil
		}
		if err != nil {
			lastErr = err.Error()
		} else {
			lastErr = strings.TrimSpace(res.Stdout + " " + res.Stderr)
		}
		if time.Now().After(deadline) {
			if lastErr == "" {
				lastErr = "redis ping did not return PONG"
			}
			return fmt.Errorf("redis container %q not ready: %s", containerName, lastErr)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// EnsureStartupCoreServices 在正式模式连接数据库/缓存前执行启动自愈：
// 若配置启用了托管 PostgreSQL 或 Redis，则自动确保容器运行、探活通过并同步 compose 文件。
func EnsureStartupCoreServices(ctx context.Context, cfg config.Config, hostOps managedports.HostOps, log *zap.Logger) error {
	if !cfg.HasManagedCoreServices() {
		return nil
	}
	if hostOps == nil {
		return errors.New("managed host ops adapter is nil")
	}

	client, err := hostOps.NewDockerClient(cfg.EffectiveManagedDockerHost())
	if err != nil {
		return fmt.Errorf("init docker client for managed services: %w", err)
	}
	if _, err := client.Ping(ctx); err != nil {
		return fmt.Errorf("managed services enabled, but docker daemon is unreachable: %w", err)
	}

	rootDir := cfg.EffectiveManagedDataDir()
	pgDir := cfg.EffectiveManagedPostgresDataDir()
	redisDir := cfg.EffectiveManagedRedisDataDir()
	backupDir := cfg.EffectiveManagedBackupDir()
	servicesDir := cfg.EffectiveManagedServicesDir()

	if err := hostOps.PrepareManagedDirs(rootDir, pgDir, redisDir, backupDir, servicesDir); err != nil {
		return fmt.Errorf("prepare managed directories: %w", err)
	}

	composeState := managedports.ComposeState{
		Network:        cfg.EffectiveManagedNetwork(),
		RegistryMirror: cfg.ManagedRegistryMirror,
	}

	onLog := func(evt ProgressEvent) {
		if log == nil {
			return
		}
		log.Info("managed_core_service",
			zap.String("service", evt.Service),
			zap.String("step", evt.Step),
			zap.String("status", evt.Status),
			zap.String("message", evt.Message),
		)
	}

	if cfg.ManagedPostgresEnabled {
		dirInfo := hostOps.InspectManagedRoot(rootDir, cfg.ManagedPostgresDataDir, cfg.ManagedRedisDataDir, cfg.ManagedBackupDir)
		if err := managedports.ValidatePostgresImageForDataVersion(cfg.ManagedPostgresImage, dirInfo.PGVersion); err != nil {
			return err
		}
		user, password, dbName := ParsePostgresDSN(cfg.PostgresDSN)
		pgCfg := managedports.PostgresContainerConfig{
			Image:          cfg.ManagedPostgresImage,
			RegistryMirror: cfg.ManagedRegistryMirror,
			Network:        cfg.EffectiveManagedNetwork(),
			Port:           cfg.ManagedPostgresPort,
			DataDir:        pgDir,
			User:           user,
			Password:       password,
			Database:       dbName,
			MemoryLimitMB:  cfg.ManagedPostgresMemoryLimitMB,
		}
		if err := EnsurePostgres(ctx, client, hostOps.PostgresSpec(pgCfg), user, dbName, onLog); err != nil {
			return err
		}
		composeState.Postgres = &pgCfg
	}

	if cfg.ManagedRedisEnabled && strings.EqualFold(strings.TrimSpace(cfg.CacheDriver), "redis") {
		rdCfg := managedports.RedisContainerConfig{
			Image:          cfg.ManagedRedisImage,
			RegistryMirror: cfg.ManagedRegistryMirror,
			Network:        cfg.EffectiveManagedNetwork(),
			Port:           cfg.ManagedRedisPort,
			DataDir:        redisDir,
			Password:       cfg.RedisPassword,
			MemoryLimitMB:  cfg.ManagedRedisMemoryLimitMB,
		}
		if err := EnsureRedis(ctx, client, hostOps.RedisSpec(rdCfg), cfg.RedisPassword, onLog); err != nil {
			return err
		}
		composeState.Redis = &rdCfg
	}

	if cfg.ManagedComposeSync && (composeState.Postgres != nil || composeState.Redis != nil) {
		err := collectComposeSidecars(ctx, cfg, client, &composeState)
		if err == nil {
			_, _, err = hostOps.SyncComposeFiles(rootDir, composeState)
		}
		if err != nil && log != nil {
			log.Warn("sync_compose_files_failed", zap.Error(err))
		}
	}
	return nil
}

// CheckPortAvailable 在本机回环地址探测指定端口是否被占用。
func CheckPortAvailable(port int) error {
	if port <= 0 || port > 65535 {
		return errors.New("port out of range (1-65535)")
	}
	return checkLoopbackPort(port)
}
