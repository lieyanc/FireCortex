package setup

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lieyanc/FireCortex/backend/internal/application/managedinfra"
	"github.com/lieyanc/FireCortex/backend/internal/infra/config"
	"github.com/lieyanc/FireCortex/backend/internal/pkg/conv"
	managedports "github.com/lieyanc/FireCortex/backend/internal/ports/managedinfra"
)

const (
	// TokenFileName 是 Setup Mode 运行期间写入的 0600 临时令牌文件，初始化完成后删除。
	TokenFileName = ".setup-token"
	// SessionCookieName 是 Setup Token 校验通过后签发的短时 HttpOnly Cookie 名称。
	SessionCookieName = "firecortex_setup_session"

	setupSessionTTL     = 2 * time.Hour
	maxVerifyFailures   = 10
	verifyLockoutWindow = time.Minute
	adminUsernameMinLen = 3
	adminUsernameMaxLen = 32
	adminPasswordMinLen = 8
)

var (
	// ErrInvalidToken 表示 Setup Token 不正确。
	ErrInvalidToken = errors.New("invalid setup token")
	// ErrTooManyAttempts 表示短时间内令牌尝试次数过多。
	ErrTooManyAttempts = errors.New("too many invalid setup token attempts; please wait a minute")
	// ErrApplyInProgress 表示已有正在执行的初始化任务。
	ErrApplyInProgress = errors.New("setup apply job is already running")
	// ErrAlreadyCompleted 表示引导初始化已完成。
	ErrAlreadyCompleted = errors.New("setup has already completed")

	validAdminUsernameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{2,31}$`)
)

// Dependencies 声明 Setup 服务所需的外部端口（由组合根注入，保持 application 层无 infra 适配器依赖）。
type Dependencies struct {
	Host  managedports.HostOps
	Infra managedports.SetupInfraPort
}

// PreflightResult 返回宿主机环境体检与默认参数，供引导向导预填。
type PreflightResult struct {
	Docker                managedports.DaemonInfo
	DockerError           string
	DefaultDir            managedports.DirInspection
	PostgresPortAvailable bool
	PostgresPortMessage   string
	RedisPortAvailable    bool
	RedisPortMessage      string
	Defaults              SetupDefaults
}

// SetupDefaults 描述向导表单的初始推荐值。
type SetupDefaults struct {
	HTTPPort       string
	DockerHost     string
	Network        string
	DataDir        string
	RegistryMirror string
	PostgresImage  string
	PostgresPort   int
	RedisImage     string
	RedisPort      int
}

// InspectDirInput 是目录预检请求参数。
type InspectDirInput struct {
	DataDir         string
	PostgresDataDir string
	RedisDataDir    string
	BackupDir       string
}

// TestExternalInput 是外部数据库/Redis 连通性探测参数。
type TestExternalInput struct {
	PostgresDSN          string
	CacheDriver          string // redis | memory
	RedisAddr            string
	RedisUsername        string
	RedisPassword        string
	RedisDB              int
	RedisTLSEnabled      bool
	RedisTLSInsecureSkip bool
}

// TestExternalResult 返回外部依赖的探测结果。
type TestExternalResult struct {
	PostgresOK              bool
	PostgresVersion         string
	PostgresVectorInstalled bool
	PostgresVectorAvailable bool
	PostgresError           string
	RedisChecked            bool
	RedisOK                 bool
	RedisError              string
}

// ApplyInput 是用户提交的初始化完整配置。
type ApplyInput struct {
	ExistingDataEncryptionKey string
	DatabaseMode              string // managed | external
	CacheMode                 string // managed_redis | external_redis | memory

	DockerHost            string
	RegistryMirror        string
	DataDir               string
	PostgresDataDir       string
	RedisDataDir          string
	BackupDir             string
	PostgresImage         string
	PostgresPort          int
	PostgresMemoryLimitMB int
	RedisImage            string
	RedisPort             int
	RedisMemoryLimitMB    int
	AdoptExistingPGData   bool
	ExistingPGUser        string
	ExistingPGPassword    string
	ExistingPGDatabase    string
	ExistingRedisPassword string
	ComposeSync           bool

	ExternalPostgresDSN  string
	ExternalRedisAddr    string
	ExternalRedisUser    string
	ExternalRedisPass    string
	ExternalRedisDB      int
	ExternalRedisTLS     bool
	ExternalRedisSkipTLS bool

	Env              string // prod | dev
	PublicBaseURL    string
	CORSAllowOrigin  string
	AdminUsername    string
	AdminDisplayName string
	AdminPassword    string
}

// JobEvent 表示一次异步初始化任务中的进度事件。
type JobEvent struct {
	JobID     string
	Step      string
	Service   string
	Message   string
	Percent   int
	Status    string // running | done | failed
	Timestamp time.Time
}

// JobSnapshot 保存当前初始化任务的完整状态快照与事件历史。
type JobSnapshot struct {
	JobID       string
	State       string // idle | running | done | failed
	Error       string
	ComposePath string
	ConfigPath  string
	AdoptedDB   bool
	Events      []JobEvent
}

// Service 管理 Setup Mode 的令牌鉴权、环境预检、异步编排与配置落盘。
type Service struct {
	cfg        config.Config
	configPath string
	token      string
	tokenFile  string
	onComplete func()
	hostOps    managedports.HostOps
	infra      managedports.SetupInfraPort

	mu           sync.Mutex
	sessions     map[string]time.Time
	failWindowAt time.Time
	failCount    int
	completed    bool
	job          JobSnapshot
	subscribers  map[chan JobEvent]struct{}
}

// NewService 创建 Setup Mode 服务并生成一次性 Setup Token。
func NewService(cfg config.Config, configPath string, deps Dependencies, onComplete func()) (*Service, error) {
	if deps.Host == nil {
		return nil, errors.New("setup service requires managed host ops")
	}
	if deps.Infra == nil {
		return nil, errors.New("setup service requires setup infra port")
	}
	if strings.TrimSpace(configPath) == "" {
		configPath = config.ResolveConfigFilePath()
	}
	token, err := generateRandomHex(16)
	if err != nil {
		return nil, fmt.Errorf("generate setup token: %w", err)
	}

	absCfg, err := filepath.Abs(configPath)
	if err != nil {
		absCfg = configPath
	}
	tokenFile := filepath.Join(filepath.Dir(absCfg), TokenFileName)
	_ = writeSecretFile(tokenFile, token+"\n")

	return &Service{
		cfg:         cfg,
		configPath:  configPath,
		token:       token,
		tokenFile:   tokenFile,
		onComplete:  onComplete,
		hostOps:     deps.Host,
		infra:       deps.Infra,
		sessions:    map[string]time.Time{},
		job:         JobSnapshot{State: "idle", Events: []JobEvent{}},
		subscribers: map[chan JobEvent]struct{}{},
	}, nil
}

// Token 返回本次启动生成的一次性 Setup Token。
func (s *Service) Token() string {
	return s.token
}

// TokenFilePath 返回临时 Setup Token 文件路径（0600）。
func (s *Service) TokenFilePath() string {
	return s.tokenFile
}

// CleanupTokenFile 删除磁盘上的临时 Setup Token 文件。
func (s *Service) CleanupTokenFile() {
	if s != nil && s.tokenFile != "" {
		_ = os.Remove(s.tokenFile)
	}
}

// VerifyToken 以恒定时间校验一次性 Setup Token，通过后签发短时会话令牌。
func (s *Service) VerifyToken(candidate string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.completed {
		return "", ErrAlreadyCompleted
	}
	now := time.Now()
	if now.Sub(s.failWindowAt) > verifyLockoutWindow {
		s.failWindowAt = now
		s.failCount = 0
	}
	if s.failCount >= maxVerifyFailures {
		return "", ErrTooManyAttempts
	}

	clean := strings.TrimSpace(candidate)
	if subtle.ConstantTimeCompare([]byte(clean), []byte(s.token)) != 1 {
		s.failCount++
		return "", ErrInvalidToken
	}

	sessionID, err := generateRandomHex(32)
	if err != nil {
		return "", err
	}
	s.sessions[sessionID] = now.Add(setupSessionTTL)
	s.failCount = 0
	return sessionID, nil
}

// ValidateAuth 校验请求携带的 Setup Session Cookie 或 X-Setup-Token 头。
func (s *Service) ValidateAuth(sessionCookie string, tokenHeader string) bool {
	if trimmed := strings.TrimSpace(tokenHeader); trimmed != "" {
		if subtle.ConstantTimeCompare([]byte(trimmed), []byte(s.token)) == 1 {
			return true
		}
	}
	if trimmed := strings.TrimSpace(sessionCookie); trimmed != "" {
		s.mu.Lock()
		defer s.mu.Unlock()
		exp, ok := s.sessions[trimmed]
		if ok && time.Now().Before(exp) {
			return true
		}
		if ok {
			delete(s.sessions, trimmed)
		}
	}
	return false
}

// Defaults 返回向导预填的推荐默认参数。
func (s *Service) Defaults() SetupDefaults {
	pgPort := s.cfg.ManagedPostgresPort
	if pgPort <= 0 {
		pgPort = 5432
	}
	rdPort := s.cfg.ManagedRedisPort
	if rdPort <= 0 {
		rdPort = 6379
	}
	pgImg := strings.TrimSpace(s.cfg.ManagedPostgresImage)
	if pgImg == "" {
		pgImg = config.DefaultManagedPostgresImage
	}
	rdImg := strings.TrimSpace(s.cfg.ManagedRedisImage)
	if rdImg == "" {
		rdImg = config.DefaultManagedRedisImage
	}
	return SetupDefaults{
		HTTPPort:       s.cfg.HTTPPort,
		DockerHost:     s.cfg.EffectiveManagedDockerHost(),
		Network:        s.cfg.EffectiveManagedNetwork(),
		DataDir:        s.cfg.EffectiveManagedDataDir(),
		RegistryMirror: s.cfg.ManagedRegistryMirror,
		PostgresImage:  pgImg,
		PostgresPort:   pgPort,
		RedisImage:     rdImg,
		RedisPort:      rdPort,
	}
}

// Preflight 探测宿主机 Docker、默认托管目录和常用端口可用性。
func (s *Service) Preflight(ctx context.Context, dockerHostOverride string) PreflightResult {
	defs := s.Defaults()
	host := strings.TrimSpace(dockerHostOverride)
	if host == "" {
		host = defs.DockerHost
	} else {
		defs.DockerHost = host
	}

	res := PreflightResult{
		Defaults:   defs,
		DefaultDir: s.hostOps.InspectManagedRoot(defs.DataDir, s.cfg.ManagedPostgresDataDir, s.cfg.ManagedRedisDataDir, s.cfg.ManagedBackupDir),
	}

	var pgExistingRunning, rdExistingRunning bool
	client, err := s.hostOps.NewDockerClient(host)
	if err != nil {
		res.Docker = managedports.DaemonInfo{Host: host}
		res.DockerError = err.Error()
	} else {
		info, pingErr := client.Ping(ctx)
		res.Docker = info
		if pingErr != nil {
			res.DockerError = pingErr.Error()
		} else {
			if ctr, cErr := client.InspectContainer(ctx, managedports.PostgresContainerName); cErr == nil && ctr != nil && ctr.Running {
				pgExistingRunning = true
			}
			if ctr, cErr := client.InspectContainer(ctx, managedports.RedisContainerName); cErr == nil && ctr != nil && ctr.Running {
				rdExistingRunning = true
			}
		}
	}

	if pgExistingRunning {
		res.PostgresPortAvailable = true
		res.PostgresPortMessage = "Existing running firecortex-postgres container detected and can be reused."
	} else if err := managedinfra.CheckPortAvailable(defs.PostgresPort); err != nil {
		res.PostgresPortAvailable = false
		res.PostgresPortMessage = err.Error()
	} else {
		res.PostgresPortAvailable = true
	}

	if rdExistingRunning {
		res.RedisPortAvailable = true
		res.RedisPortMessage = "Existing running firecortex-redis container detected and can be reused."
	} else if err := managedinfra.CheckPortAvailable(defs.RedisPort); err != nil {
		res.RedisPortAvailable = false
		res.RedisPortMessage = err.Error()
	} else {
		res.RedisPortAvailable = true
	}

	return res
}

// InspectDirectory 对前端指定的托管目录执行安全校验与已有数据检测。
func (s *Service) InspectDirectory(input InspectDirInput) managedports.DirInspection {
	return s.hostOps.InspectManagedRoot(input.DataDir, input.PostgresDataDir, input.RedisDataDir, input.BackupDir)
}

// TestExternal 对外部 PostgreSQL（含 pgvector 检查）和 Redis 进行只读连通性测试。
func (s *Service) TestExternal(ctx context.Context, input TestExternalInput) TestExternalResult {
	var res TestExternalResult
	probe, err := s.infra.ProbePostgres(ctx, input.PostgresDSN)
	if err != nil {
		res.PostgresError = err.Error()
	} else {
		res.PostgresOK = true
		res.PostgresVersion = probe.ServerVersion
		res.PostgresVectorInstalled = probe.VectorInstalled
		res.PostgresVectorAvailable = probe.VectorAvailable
	}

	if strings.EqualFold(strings.TrimSpace(input.CacheDriver), "redis") {
		res.RedisChecked = true
		if rErr := s.infra.ProbeRedis(ctx, managedports.RedisProbeInput{
			Addr:               strings.TrimSpace(input.RedisAddr),
			Username:           strings.TrimSpace(input.RedisUsername),
			Password:           input.RedisPassword,
			DB:                 input.RedisDB,
			TLSEnabled:         input.RedisTLSEnabled,
			InsecureSkipVerify: input.RedisTLSInsecureSkip,
		}); rErr != nil {
			res.RedisError = rErr.Error()
		} else {
			res.RedisOK = true
		}
	}
	return res
}

// CurrentJob 返回当前初始化任务的快照（供刷新重连的客户端查询）。
func (s *Service) CurrentJob() JobSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	copied := s.job
	copied.Events = append([]JobEvent(nil), s.job.Events...)
	return copied
}

// SubscribeJob 订阅实时初始化事件，先回放当前任务的历史事件再持续推送增量事件。
func (s *Service) SubscribeJob(ctx context.Context) (<-chan JobEvent, func()) {
	ch := make(chan JobEvent, 64)
	s.mu.Lock()
	history := append([]JobEvent(nil), s.job.Events...)
	s.subscribers[ch] = struct{}{}
	s.mu.Unlock()

	out := make(chan JobEvent, 64)
	go func() {
		defer close(out)
		for _, evt := range history {
			select {
			case out <- evt:
			case <-ctx.Done():
				return
			}
		}
		for {
			select {
			case evt, ok := <-ch:
				if !ok {
					return
				}
				select {
				case out <- evt:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	cancel := func() {
		s.mu.Lock()
		if _, exists := s.subscribers[ch]; exists {
			delete(s.subscribers, ch)
			close(ch)
		}
		s.mu.Unlock()
	}
	return out, cancel
}

// StartApply 校验初始化表单并在后台启动异步编排任务。
func (s *Service) StartApply(input ApplyInput) (JobSnapshot, error) {
	normalized, dirInspection, err := s.validateAndNormalizeApply(input)
	if err != nil {
		return JobSnapshot{}, err
	}

	s.mu.Lock()
	if s.completed {
		s.mu.Unlock()
		return JobSnapshot{}, ErrAlreadyCompleted
	}
	if s.job.State == "running" {
		s.mu.Unlock()
		return JobSnapshot{}, ErrApplyInProgress
	}
	jobID := conv.NormalizePublicID(uuid.NewString())
	s.job = JobSnapshot{
		JobID:  jobID,
		State:  "running",
		Events: []JobEvent{},
	}
	s.mu.Unlock()

	go s.runApplyJob(jobID, normalized, dirInspection)
	return s.CurrentJob(), nil
}

func (s *Service) validateAndNormalizeApply(input ApplyInput) (ApplyInput, managedports.DirInspection, error) {
	defs := s.Defaults()

	out := input
	out.DatabaseMode = strings.ToLower(strings.TrimSpace(out.DatabaseMode))
	if out.DatabaseMode == "" {
		out.DatabaseMode = "managed"
	}
	if out.DatabaseMode != "managed" && out.DatabaseMode != "external" {
		return out, managedports.DirInspection{}, fmt.Errorf("invalid database mode %q", out.DatabaseMode)
	}

	out.CacheMode = strings.ToLower(strings.TrimSpace(out.CacheMode))
	if out.CacheMode == "" {
		if out.DatabaseMode == "managed" {
			out.CacheMode = "managed_redis"
		} else {
			out.CacheMode = "memory"
		}
	}
	if out.CacheMode != "managed_redis" && out.CacheMode != "external_redis" && out.CacheMode != "memory" {
		return out, managedports.DirInspection{}, fmt.Errorf("invalid cache mode %q", out.CacheMode)
	}

	if strings.TrimSpace(out.DockerHost) == "" {
		out.DockerHost = defs.DockerHost
	}
	if strings.TrimSpace(out.DataDir) == "" {
		out.DataDir = defs.DataDir
	}
	if strings.TrimSpace(out.PostgresImage) == "" {
		out.PostgresImage = defs.PostgresImage
	}
	if out.PostgresPort <= 0 {
		out.PostgresPort = defs.PostgresPort
	}
	if strings.TrimSpace(out.RedisImage) == "" {
		out.RedisImage = defs.RedisImage
	}
	if out.RedisPort <= 0 {
		out.RedisPort = defs.RedisPort
	}

	var dirInfo managedports.DirInspection
	usesDocker := out.DatabaseMode == "managed" || out.CacheMode == "managed_redis"
	if usesDocker {
		dirInfo = s.hostOps.InspectManagedRoot(out.DataDir, out.PostgresDataDir, out.RedisDataDir, out.BackupDir)
		if !dirInfo.Valid {
			return out, dirInfo, fmt.Errorf("managed directory check failed: %s", dirInfo.Reason)
		}
		if out.DatabaseMode == "managed" && (dirInfo.Status == managedports.DirStatusAdoptablePGData || dirInfo.Status == managedports.DirStatusManagedExisting) {
			if !out.AdoptExistingPGData {
				return out, dirInfo, errors.New("existing PostgreSQL data detected in target directory; confirm safe adoption mode to continue")
			}
		}
	}

	if out.DatabaseMode == "external" && strings.TrimSpace(out.ExternalPostgresDSN) == "" {
		return out, dirInfo, errors.New("external database mode requires a PostgreSQL DSN")
	}
	if out.CacheMode == "external_redis" && strings.TrimSpace(out.ExternalRedisAddr) == "" {
		return out, dirInfo, errors.New("external Redis mode requires a Redis address")
	}

	out.PublicBaseURL = strings.TrimRight(strings.TrimSpace(out.PublicBaseURL), "/")
	out.Env = strings.ToLower(strings.TrimSpace(out.Env))
	if out.Env == "" {
		if strings.HasPrefix(strings.ToLower(out.PublicBaseURL), "https://") {
			out.Env = "prod"
		} else {
			out.Env = "dev"
		}
	}
	if out.Env != "prod" && out.Env != "dev" {
		return out, dirInfo, fmt.Errorf("app env must be prod or dev, got %q", out.Env)
	}
	if out.PublicBaseURL != "" {
		parsed, err := url.Parse(out.PublicBaseURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return out, dirInfo, errors.New("public base URL must be a valid http:// or https:// URL")
		}
		if out.Env == "prod" && parsed.Scheme != "https" {
			return out, dirInfo, errors.New("production mode requires an https:// public base URL; switch to dev mode for local HTTP access")
		}
	} else if out.Env == "prod" {
		return out, dirInfo, errors.New("production mode requires an https:// public base URL")
	}
	if strings.TrimSpace(out.CORSAllowOrigin) == "" {
		if out.PublicBaseURL != "" {
			out.CORSAllowOrigin = out.PublicBaseURL
		} else {
			out.CORSAllowOrigin = fmt.Sprintf("http://127.0.0.1:%s,http://localhost:%s", defs.HTTPPort, defs.HTTPPort)
		}
	}

	out.AdminUsername = strings.TrimSpace(out.AdminUsername)
	out.AdminDisplayName = strings.TrimSpace(out.AdminDisplayName)
	needAdminCreds := !(out.DatabaseMode == "managed" && out.AdoptExistingPGData) && out.DatabaseMode != "external"
	if needAdminCreds || out.AdminUsername != "" || out.AdminPassword != "" {
		if !validAdminUsernameRe.MatchString(out.AdminUsername) {
			return out, dirInfo, fmt.Errorf("admin username must be %d-%d characters (letters, digits, ., _, -) starting with a letter or digit", adminUsernameMinLen, adminUsernameMaxLen)
		}
		if len(out.AdminPassword) < adminPasswordMinLen {
			return out, dirInfo, fmt.Errorf("admin password must be at least %d characters", adminPasswordMinLen)
		}
	}
	return out, dirInfo, nil
}

func (s *Service) runApplyJob(jobID string, input ApplyInput, dirInfo managedports.DirInspection) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	emit := func(step, service, msg string, pct int, status string) {
		evt := JobEvent{
			JobID:     jobID,
			Step:      step,
			Service:   service,
			Message:   msg,
			Percent:   pct,
			Status:    status,
			Timestamp: time.Now().UTC(),
		}
		s.mu.Lock()
		s.job.Events = append(s.job.Events, evt)
		for ch := range s.subscribers {
			select {
			case ch <- evt:
			default:
			}
		}
		s.mu.Unlock()
	}

	fail := func(step, service string, err error) {
		msg := err.Error()
		emit(step, service, msg, 0, "failed")
		s.mu.Lock()
		s.job.State = "failed"
		s.job.Error = msg
		s.mu.Unlock()
	}

	defs := s.Defaults()
	usesDocker := input.DatabaseMode == "managed" || input.CacheMode == "managed_redis"
	var dockerClient managedports.DockerClient
	var existingEnv map[string]string

	if usesDocker {
		emit("preflight", "", "Checking Docker Engine and preparing managed directories...", 5, "running")
		client, err := s.hostOps.NewDockerClient(input.DockerHost)
		if err != nil {
			fail("preflight", "", fmt.Errorf("init docker client: %w", err))
			return
		}
		if _, err := client.Ping(ctx); err != nil {
			fail("preflight", "", fmt.Errorf("docker engine unreachable (%s): %w", input.DockerHost, err))
			return
		}
		dockerClient = client
		existingEnv = s.hostOps.ReadExistingComposeEnv(dirInfo.ResolvedRoot)

		servicesDir := filepath.Join(dirInfo.ResolvedRoot, "services")
		if err := s.hostOps.PrepareManagedDirs(dirInfo.ResolvedRoot, dirInfo.PostgresDataDir, dirInfo.RedisDataDir, dirInfo.BackupDir, servicesDir); err != nil {
			fail("preflight", "", fmt.Errorf("prepare managed directories: %w", err))
			return
		}
		emit("preflight", "", "Managed directories and marker file ready", 10, "done")
	}

	composeState := managedports.ComposeState{
		Network:        defs.Network,
		RegistryMirror: input.RegistryMirror,
	}

	// 1. PostgreSQL
	var pgDSN string
	if input.DatabaseMode == "managed" {
		creds, err := managedinfra.PrepareCoreCredentials(ctx, dockerClient, dirInfo.ResolvedRoot, managedports.CoreServicePostgres, managedinfra.CoreCredentials{
			User: input.ExistingPGUser, Database: input.ExistingPGDatabase, Password: input.ExistingPGPassword,
		}, existingEnv)
		if err != nil {
			fail("postgres", managedports.CoreServicePostgres, err)
			return
		}
		pgUser, pgDB, pgPass := creds.User, creds.Database, creds.Password

		pgCfg := managedports.PostgresContainerConfig{
			Image:          input.PostgresImage,
			RegistryMirror: input.RegistryMirror,
			Network:        defs.Network,
			Port:           input.PostgresPort,
			DataDir:        dirInfo.PostgresDataDir,
			User:           pgUser,
			Password:       pgPass,
			Database:       pgDB,
			MemoryLimitMB:  input.PostgresMemoryLimitMB,
		}
		if err := managedinfra.EnsurePostgres(ctx, dockerClient, s.hostOps.PostgresSpec(pgCfg), pgUser, pgDB, func(evt managedinfra.ProgressEvent) {
			mappedPct := 10 + (evt.Percent * 35 / 100)
			emit("postgres", managedports.CoreServicePostgres, evt.Message, mappedPct, evt.Status)
		}); err != nil {
			fail("postgres", managedports.CoreServicePostgres, err)
			return
		}
		composeState.Postgres = &pgCfg
		pgDSN = managedinfra.BuildPostgresDSN("127.0.0.1", input.PostgresPort, pgUser, pgPass, pgDB)
	} else {
		emit("postgres", managedports.CoreServicePostgres, "Verifying external PostgreSQL connection...", 25, "running")
		pgDSN = strings.TrimSpace(input.ExternalPostgresDSN)
		probe, err := s.infra.ProbePostgres(ctx, pgDSN)
		if err != nil {
			fail("postgres", managedports.CoreServicePostgres, fmt.Errorf("external postgres connection failed: %w", err))
			return
		}
		if !probe.VectorInstalled && !probe.VectorAvailable {
			emit("postgres", managedports.CoreServicePostgres, "Warning: pgvector extension not detected; semantic search will be limited", 40, "running")
		}
		emit("postgres", managedports.CoreServicePostgres, "External PostgreSQL verified", 45, "done")
	}

	// 2. Redis / Cache
	var cacheDriver, redisAddr, redisUser, redisPass string
	var redisDB int
	var redisTLS, redisSkipTLS bool
	switch input.CacheMode {
	case "managed_redis":
		cacheDriver = "redis"
		redisAddr = fmt.Sprintf("127.0.0.1:%d", input.RedisPort)
		creds, err := managedinfra.PrepareCoreCredentials(ctx, dockerClient, dirInfo.ResolvedRoot, managedports.CoreServiceRedis, managedinfra.CoreCredentials{Password: input.ExistingRedisPassword}, existingEnv)
		if err != nil {
			fail("redis", managedports.CoreServiceRedis, err)
			return
		}
		redisPass = creds.Password
		rdCfg := managedports.RedisContainerConfig{
			Image:          input.RedisImage,
			RegistryMirror: input.RegistryMirror,
			Network:        defs.Network,
			Port:           input.RedisPort,
			DataDir:        dirInfo.RedisDataDir,
			Password:       redisPass,
			MemoryLimitMB:  input.RedisMemoryLimitMB,
		}
		if err := managedinfra.EnsureRedis(ctx, dockerClient, s.hostOps.RedisSpec(rdCfg), redisPass, func(evt managedinfra.ProgressEvent) {
			mappedPct := 45 + (evt.Percent * 20 / 100)
			emit("redis", managedports.CoreServiceRedis, evt.Message, mappedPct, evt.Status)
		}); err != nil {
			fail("redis", managedports.CoreServiceRedis, err)
			return
		}
		composeState.Redis = &rdCfg
	case "external_redis":
		emit("redis", managedports.CoreServiceRedis, "Verifying external Redis connection...", 55, "running")
		cacheDriver = "redis"
		redisAddr = strings.TrimSpace(input.ExternalRedisAddr)
		redisUser = strings.TrimSpace(input.ExternalRedisUser)
		redisPass = input.ExternalRedisPass
		redisDB = input.ExternalRedisDB
		redisTLS = input.ExternalRedisTLS
		redisSkipTLS = input.ExternalRedisSkipTLS
		if err := s.infra.ProbeRedis(ctx, managedports.RedisProbeInput{
			Addr:               redisAddr,
			Username:           redisUser,
			Password:           redisPass,
			DB:                 redisDB,
			TLSEnabled:         redisTLS,
			InsecureSkipVerify: redisSkipTLS,
		}); err != nil {
			fail("redis", managedports.CoreServiceRedis, fmt.Errorf("external redis connection failed: %w", err))
			return
		}
		emit("redis", managedports.CoreServiceRedis, "External Redis verified", 65, "done")
	default:
		cacheDriver = "memory"
		emit("redis", "", "Using built-in in-process memory cache", 65, "done")
	}

	// 3. 落盘 docker-compose.yml & .env（0600）
	var composePath string
	if usesDocker && input.ComposeSync && (composeState.Postgres != nil || composeState.Redis != nil) {
		emit("compose", "", "Syncing docker-compose.yml and 0600 .env to disk...", 70, "running")
		path, _, err := s.hostOps.SyncComposeFiles(dirInfo.ResolvedRoot, composeState)
		if err != nil {
			fail("compose", "", fmt.Errorf("sync docker-compose.yml: %w", err))
			return
		}
		composePath = path
		emit("compose", "", "Persisted "+composePath, 75, "done")
	}

	dataDirToSave := input.DataDir
	if usesDocker && dirInfo.ResolvedRoot != "" {
		dataDirToSave = dirInfo.ResolvedRoot
	}
	prepared, err := config.PrepareInitializedConfig(s.configPath, config.InitializedConfigInput{
		DataEncryptionKey:            input.ExistingDataEncryptionKey,
		Env:                          input.Env,
		HTTPPort:                     defs.HTTPPort,
		PublicAPIBaseURL:             input.PublicBaseURL,
		PublicWebBaseURL:             input.PublicBaseURL,
		CORSAllowOrigin:              input.CORSAllowOrigin,
		PostgresDSN:                  pgDSN,
		CacheDriver:                  cacheDriver,
		RedisAddr:                    redisAddr,
		RedisUsername:                redisUser,
		RedisPassword:                redisPass,
		RedisDB:                      redisDB,
		RedisTLSEnabled:              redisTLS,
		RedisTLSInsecureSkip:         redisSkipTLS,
		ManagedDockerHost:            input.DockerHost,
		ManagedNetwork:               defs.Network,
		ManagedDataDir:               dataDirToSave,
		ManagedRegistryMirror:        input.RegistryMirror,
		ManagedComposeSync:           input.ComposeSync,
		ManagedPostgresEnabled:       input.DatabaseMode == "managed",
		ManagedPostgresImage:         input.PostgresImage,
		ManagedPostgresPort:          input.PostgresPort,
		ManagedPostgresDataDir:       strings.TrimSpace(input.PostgresDataDir),
		ManagedPostgresMemoryLimitMB: input.PostgresMemoryLimitMB,
		ManagedRedisEnabled:          input.CacheMode == "managed_redis",
		ManagedRedisImage:            input.RedisImage,
		ManagedRedisPort:             input.RedisPort,
		ManagedRedisDataDir:          strings.TrimSpace(input.RedisDataDir),
		ManagedRedisMemoryLimitMB:    input.RedisMemoryLimitMB,
		ManagedBackupEnabled:         input.DatabaseMode == "managed",
		ManagedBackupDir:             strings.TrimSpace(input.BackupDir),
	})
	if err != nil {
		fail("config", "", fmt.Errorf("write config.json: %w", err))
		return
	}
	// 4. 数据库迁移 + 初始化管理员账号
	emit("migrate", "", "Running database migrations and superadmin initialization...", 80, "running")
	adoptedAdmin, err := s.infra.InitDBAndAdmin(ctx, managedports.InitDBAdminInput{
		Env:                    input.Env,
		PostgresDSN:            prepared.PostgresDSN,
		DataEncryptionKey:      prepared.DataEncryptionKey,
		PreviousEncryptionKeys: prepared.DataEncryptionKeysPrevious,
		AdminUsername:          input.AdminUsername,
		AdminDisplayName:       input.AdminDisplayName,
		AdminPassword:          input.AdminPassword,
	})
	if err != nil {
		fail("migrate", "", fmt.Errorf("initialize database and superadmin: %w", err))
		return
	}
	if adoptedAdmin {
		emit("migrate", "", "Existing administrator account preserved from adopted database", 90, "done")
	} else {
		emit("migrate", "", "Database schema and initial superadmin ready", 90, "done")
	}

	emit("config", "", "Writing validated config.json...", 94, "running")
	if err := prepared.Save(); err != nil {
		fail("config", "", err)
		return
	}
	s.CleanupTokenFile()

	s.mu.Lock()
	s.completed = true
	s.job.State = "done"
	s.job.ComposePath = composePath
	s.job.ConfigPath = s.configPath
	s.job.AdoptedDB = adoptedAdmin
	s.mu.Unlock()

	emit("restart", "", "Initialization complete. Restarting into FireCortex...", 100, "done")

	if s.onComplete != nil {
		time.Sleep(800 * time.Millisecond)
		s.onComplete()
	}
}

func generateRandomHex(numBytes int) (string, error) {
	buf := make([]byte, numBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func writeSecretFile(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}
