package config

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/lieyanc/FireCortex/backend/internal/pkg/secretbox"
	sharedsecurity "github.com/lieyanc/FireCortex/backend/internal/shared/security"
)

const (
	// 未配置密钥时的开发占位值；prod 环境下 Validate 拒绝使用。
	// 首次释放 config.json 时会生成随机密钥，因此只有显式清空密钥才会落到这里。
	defaultJWTSecret         = "firecortex-dev-secret"
	defaultDataEncryptionKey = "firecortex-dev-data-encryption-key"
	defaultAdminUsername     = "admin"
	defaultAdminDisplayName  = "System Admin"

	// DefaultDesktopDownloadURL 是桌面端下载页的默认地址。
	DefaultDesktopDownloadURL = "https://github.com/lieyanc/FireCortex"
	// DefaultPersonalProvidersMaxPerUser 是每个用户可添加的个人模型服务默认上限。
	DefaultPersonalProvidersMaxPerUser = 10
	// MaxPersonalProvidersMaxPerUser 是管理员可配置的上限。
	MaxPersonalProvidersMaxPerUser = 50
	// DefaultFileFullContextMaxBytes 是全文注入的默认提取文本大小上限（2 MiB）。
	DefaultFileFullContextMaxBytes int64 = 2 * 1024 * 1024

	// DefaultContextWindowFallbackTokens 是无法从模型能力配置或内置目录识别窗口时的默认值。
	DefaultContextWindowFallbackTokens = 128_000
	// MinContextWindowFallbackTokens 与 MaxContextWindowFallbackTokens 限制管理员可配置的安全范围。
	MinContextWindowFallbackTokens = 16_384
	MaxContextWindowFallbackTokens = 16_000_000
	// DefaultContextCompactTriggerPercent 在有效输入预算的 80% 处主动压缩。
	DefaultContextCompactTriggerPercent = 80
	MinContextCompactTriggerPercent     = 10
	MaxContextCompactTriggerPercent     = 95
)


const (
	// DefaultTurnstileSiteverifyURL 是 Cloudflare Turnstile 默认校验端点。
	DefaultTurnstileSiteverifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

	// DefaultMCPMaxSelectedToolsPerMessage 是单次消息可选择 MCP 工具数量的默认值。
	DefaultMCPMaxSelectedToolsPerMessage = 32
	// MaxMCPSelectedToolsPerMessage 是运行时配置允许的安全上限，防止一次请求暴露过多工具 schema。
	MaxMCPSelectedToolsPerMessage = 128

	// EmbeddingDimensionsPolicySend 在 Embedding 请求中携带 dimensions 参数。
	EmbeddingDimensionsPolicySend = "send"
	// EmbeddingDimensionsPolicyOmit 省略 dimensions 参数，返回向量仍按 EmbeddingOutputDimensions 校验。
	EmbeddingDimensionsPolicyOmit = "omit"

	// EmbeddingProtocolOpenAI 为 OpenAI 兼容的 /embeddings 文本协议。
	EmbeddingProtocolOpenAI = "openai"
	// EmbeddingProtocolGemini 为 Google Generative Language 的 batchEmbedContents 协议，支持图片输入。
	EmbeddingProtocolGemini = "gemini"
	// EmbeddingProtocolVoyage 为 Voyage AI 的 /multimodalembeddings 协议，支持图片输入。
	EmbeddingProtocolVoyage = "voyage"
	// EmbeddingProtocolJina 为 Jina AI 的 /embeddings 协议（对象数组输入），支持图片输入。
	EmbeddingProtocolJina = "jina"
)

// DefaultModelOptionAllowedPathsJSON 返回用户可透传模型参数的默认白名单。
func DefaultModelOptionAllowedPathsJSON() string {
	return `{
  "default": [
    "temperature",
    "top_p",
    "max_tokens",
    "max_output_tokens",
    "max_completion_tokens",
    "stop",
    "response_format.type"
  ],
  "openai_chat_completions": [
    "service_tier",
    "presence_penalty",
    "frequency_penalty",
    "reasoning_effort",
    "verbosity",
    "thinking.type",
    "stream_options.include_usage"
  ],
  "openai_responses": [
    "service_tier",
    "reasoning.effort",
    "reasoning.summary",
    "text.verbosity"
  ],
  "openai_image_generations": [
    "background",
    "moderation",
    "n",
    "output_compression",
    "output_format",
    "partial_images",
    "quality",
    "response_format",
    "size",
    "style",
    "user"
  ],
  "openai_image_edits": [
    "background",
    "input_fidelity",
    "n",
    "output_compression",
    "output_format",
    "partial_images",
    "quality",
    "response_format",
    "size",
    "user"
  ],
  "anthropic_messages": [
    "speed",
    "top_k",
    "thinking.type",
    "thinking.budget_tokens"
  ],
  "gemini_generate_content": [
    "generationConfig.temperature",
    "generationConfig.topP",
    "generationConfig.maxOutputTokens",
    "generationConfig.responseMimeType",
    "generationConfig.thinkingConfig.includeThoughts",
    "generationConfig.thinkingConfig.thinkingLevel"
  ],
  "google_image_generation": [
    "generationConfig.responseModalities",
    "generationConfig.imageConfig.aspectRatio",
    "generationConfig.imageConfig.imageSize"
  ],
  "gemini_interactions": [
    "generation_config.temperature",
    "generation_config.top_p",
    "generation_config.max_output_tokens",
    "generation_config.thinking_level",
    "generation_config.thinking_summaries",
    "response_format.type",
    "response_format.aspect_ratio",
    "response_format.image_size",
    "response_format.mime_type",
    "response_format.schema",
    "generation_config.video_config.task"
  ],
  "xai_responses": [
    "reasoning.effort",
    "min_p",
    "parallel_tool_calls",
    "store",
    "top_k"
  ],
  "xai_image": [
    "aspect_ratio",
    "n",
    "resolution",
    "response_format"
  ],
  "xai_image_edits": [
    "aspect_ratio",
    "n",
    "resolution",
    "response_format"
  ],
  "xai_video": [
    "aspect_ratio",
    "duration",
    "resolution"
  ],
  "xai_video_extensions": [
    "duration"
  ],
  "openrouter_chat_completions": [
    "presence_penalty",
    "frequency_penalty",
    "reasoning_effort",
    "reasoning.effort",
    "reasoning.summary",
    "verbosity",
    "thinking.type",
    "stream_options.include_usage"
  ],
  "openrouter_responses": [
    "reasoning.effort",
    "reasoning.summary"
  ],
  "openrouter_images": [
    "aspect_ratio",
    "background",
    "n",
    "output_compression",
    "output_format",
    "provider.allow_fallbacks",
    "provider.ignore",
    "provider.only",
    "provider.order",
    "provider.sort",
    "quality",
    "resolution",
    "seed",
    "size",
    "user"
  ]
}`
}

// DefaultModelOptionDeniedPathsJSON 返回所有策略模式都会叠加拦截的默认黑名单。
func DefaultModelOptionDeniedPathsJSON() string {
	return `{
  "default": [
    "model",
    "messages",
    "input",
    "instructions",
    "prompt",
    "system",
    "systemInstruction",
    "headers",
    "api_key",
    "apiKey",
    "base_url",
    "baseURL",
    "stream",
    "previous_response_id"
  ]
}`
}

// Config 管理后端服务的运行参数。
// 静态字段由 config.json/ENV 加载；动态字段由 settings.RuntimeSettings.ApplyTo 从数据库覆盖。
type Config struct {
	// ── 静态配置（config.json/ENV） ──
	ConfigFile                 string // 实际读取的配置文件路径
	AppName                    string
	Env                        string
	BrandTitle                 string
	BrandShortName             string
	BrandDescription           string
	BrandLogoURL               string
	BrandFaviconURL            string
	BrandPWAIcon192URL         string
	BrandPWAIcon512URL         string
	BrandPWAMaskableIcon512URL string
	BrandAppleTouchIcon180URL  string
	HTTPPort                   string
	// HTTPListenAddr 非空时优先于 HTTPPort，形如 "127.0.0.1:0"（本地模式）。
	HTTPListenAddr string
	// LocalMode 表示作为桌面 sidecar 运行；LocalDataDir 是其数据目录。
	LocalMode                    bool
	LocalDataDir                 string
	CORSAllowOrigin              string
	TrustedProxies               string
	PublicAPIBaseURL             string
	PublicWebBaseURL             string
	FrontendDistDir              string
	HTTPReadHeaderTimeoutSeconds int
	HTTPReadTimeoutSeconds       int
	HTTPIdleTimeoutSeconds       int
	HTTPMaxHeaderBytes           int
	HTTPShutdownTimeoutSeconds   int
	JWTSecret                    string
	MCPUserContextSecret         string
	DataEncryptionKey            string
	DataEncryptionKeysPrevious   []string // 轮换前的旧主密钥，只用于解密
	SSRFProtectionEnabled        bool
	SSRFAllowedHosts             string
	SSRFAllowedCIDRs             string
	DatabaseDriver               string
	PostgresDSN                  string
	PostgresMaxOpenConns         int
	PostgresMaxIdleConns         int
	PostgresConnMaxLifetimeMin   int
	PostgresConnMaxIdleTimeMin   int
	SQLitePath                   string
	SQLiteDSN                    string
	SQLiteMaxOpenConns           int
	SQLiteBusyTimeoutMS          int
	SQLiteCacheSizeKB            int
	SQLiteMmapSizeBytes          int64
	SQLiteSynchronous            string
	SQLiteTempStore              string
	CacheDriver                  string
	RedisAddr                    string
	RedisUsername                string
	RedisPassword                string
	RedisDB                      int
	RedisTLSEnabled              bool
	RedisTLSInsecureSkipVerify   bool
	StorageBackend               string
	StorageRootDir               string
	StorageS3Endpoint            string
	StorageS3Region              string
	StorageS3Bucket              string
	StorageS3Prefix              string
	StorageS3AccessKeyID         string
	StorageS3SecretAccessKey     string
	StorageS3ForcePathStyle      bool
	AdminUsername                string
	AdminDisplayName             string
	GeoIPProvider                string
	GeoIPBaseURL                 string
	GeoIPToken                   string
	GeoIPTimeoutMS               int
	GeoIPDatabaseURL             string
	GeoIPDatabasePath            string
	GeoIPDatabaseMaxBytes        int64
	GeoIPRefreshIntervalHours    int
	SMTPHost                     string
	SMTPPort                     int
	SMTPUsername                 string
	SMTPPassword                 string
	SMTPFrom                     string
	TurnstileSiteverifyURL       string
	OTelEnabled                  *bool
	OTelExporterOTLPEndpoint     string
	OTelExporterOTLPHeaders      string
	OTelExporterOTLPInsecure     bool
	OTelExporterOTLPProtocol     string
	OTelSamplingRate             float64
	// 自更新（OTA）：后台检查开关、通道与下载源，手动检查不受 UpdateEnabled 限制
	UpdateEnabled              bool
	UpdateChannel              string
	UpdateCheckIntervalSeconds int
	UpdateSource               string
	UpdateProxyBaseURL         string
	UpdateRepo                 string
	UpdateDownloadDir          string

	// ── 动态配置（由 DB 种子初始化默认值，settings.RuntimeSettings.ApplyTo 覆盖） ──
	// 认证配置
	TokenTTLHours                int
	RefreshTokenTTLHours         int
	LoginMaxFailures             int
	LoginLockMinutes             int
	RateLimitEnabled             bool
	RateLimitRPM                 int
	PublicAuthRateLimitRPM       int
	UsernameLoginEnabled         bool
	EmailLoginEnabled            bool
	ThirdPartyLoginEnabled       bool
	EmailRegistrationEnabled     bool
	EmailVerificationEnabled     bool
	PasswordResetEnabled         bool
	EmailRegistrationDomains     string
	EmailRegistrationNoAlias     bool
	AutoLinkVerifiedEmail        bool
	TurnstileRegistrationEnabled bool
	TurnstileSiteKey             string
	TurnstileSecretKey           string
	// 对话配置
	MaxContextMessages           int
	ContextMaxTurns              int
	ContextCompactEnabled        bool
	UIComponentsEnabled          bool // 是否向模型注入交互式组件目录
	ContextWindowFallbackTokens  int
	ContextCompactTriggerPercent int
	ContextCompactPreserve       int
	ConversationDefaultModel     string
	ConversationTaskModel        string
	ConversationTitlePrompt      string
	ConversationLabelsPrompt     string
	DefaultSystemPrompt          string
	SkillsPrompt                 string
	ModelOptionPolicyMode        string
	ModelOptionAllowedPaths      string
	ModelOptionDeniedPaths       string
	// 知识库配置
	KnowledgeBaseEnabled bool
	// 用户自带 Key（个人模型服务）配置
	PersonalProvidersEnabled      bool   // 是否允许用户添加自己的模型服务
	PersonalProvidersMaxPerUser   int    // 每个用户最多可添加的服务数
	PersonalProvidersBlockedHosts string // 禁止接入的域名（逗号或换行分隔；精确匹配，*.example.com 匹配子域名）
	// 桌面端下载入口配置
	DesktopDownloadEnabled bool   // 是否在网页端用户菜单展示「下载桌面端」入口
	DesktopDownloadURL     string // 桌面端下载页地址
	// 存储配置
	UserStorageQuotaBytes int64
	MaxUploadFileBytes    int64
	MaxMessageFiles       int
	// 文件处理配置
	ImageMaxDimension                 int    // 图片缩放最大边长（像素），0 = 不缩放
	FileFullContextLimitEnabled       bool   // 是否启用全文注入阈值限制
	FileFullContextMaxBytes           int64  // 文本文件全文注入阈值（字节），超出不注入
	FileFullContextMaxTokens          int    // 文本文件全文注入阈值（token）
	FileImageMaxBytes                 int64  // 图片单文件上限（字节）
	FileDocMaxBytes                   int64  // 文档单文件上限（字节）
	FileFullContextPDFMaxPages        int    // PDF Full Context 页数上限，超出走 RAG
	FileAllowedMIMETypes              string // 白名单 MIME 类型（逗号分隔）
	ExtractEngine                     string // 提取主引擎枚举
	ExtractOCREngine                  string // OCR 引擎枚举
	ExtractImageOCREnabled            bool   // 是否对图片附件执行 OCR
	ExtractPDFOCRFallbackEnabled      bool   // PDF 原生文本提取失败时是否启用 OCR 回退
	ExtractTikaSource                 string // Tika 服务来源(external/managed)
	ExtractTikaBaseURL                string // Apache Tika 服务地址
	ExtractTikaTimeoutSeconds         int    // Apache Tika 请求超时(秒)
	ExtractTikaAuthToken              string // Apache Tika 鉴权 Token
	ExtractDoclingBaseURL             string // Docling 服务地址
	ExtractDoclingTimeoutSeconds      int    // Docling 请求超时(秒)
	ExtractDoclingAuthToken           string // Docling 鉴权 Token
	ExtractTesseractOCRBaseURL        string // Tesseract OCR 服务地址
	ExtractTesseractOCRTimeoutSeconds int    // Tesseract OCR 请求超时(秒)
	ExtractTesseractOCRAuthToken      string // Tesseract OCR 鉴权 Token
	ExtractRapidOCRSource             string // RapidOCR 服务来源(external/managed)
	ExtractRapidOCRBaseURL            string // RapidOCR 服务地址
	ExtractRapidOCRTimeoutSeconds     int    // RapidOCR 请求超时(秒)
	ExtractRapidOCRAuthToken          string // RapidOCR 鉴权 Token
	ExtractPaddleOCRBaseURL           string // Paddle OCR 服务地址
	ExtractPaddleOCRTimeoutSeconds    int    // Paddle OCR 请求超时(秒)
	ExtractPaddleOCRAuthToken         string // Paddle OCR 鉴权 Token
	ExtractTencentOCRSecretID         string // 腾讯云 OCR SecretId
	ExtractTencentOCRSecretKey        string // 腾讯云 OCR SecretKey
	ExtractTencentOCRRegion           string // 腾讯云 OCR 地域
	ExtractTencentOCREndpoint         string // 腾讯云 OCR 接入点
	ExtractTencentOCRTimeoutSeconds   int    // 腾讯云 OCR 请求超时(秒)
	ExtractAliyunOCRAccessKeyID       string // 阿里云 OCR AccessKey ID
	ExtractAliyunOCRAccessKeySecret   string // 阿里云 OCR AccessKey Secret
	ExtractAliyunOCRRegion            string // 阿里云 OCR 地域
	ExtractAliyunOCREndpoint          string // 阿里云 OCR 接入点
	ExtractAliyunOCRTimeoutSeconds    int    // 阿里云 OCR 请求超时(秒)
	ExtractMinerUSource               string // MinerU 服务类型(cloud/self_hosted)
	ExtractMinerUBaseURL              string // MinerU 服务地址
	ExtractMinerUFileTypes            string // MinerU 处理的文件类型（逗号分隔）
	ExtractMinerUTimeoutSeconds       int    // MinerU 请求超时(秒)
	ExtractMinerUAuthToken            string // MinerU 鉴权 Token
	ExtractMistralOCRBaseURL          string // Mistral OCR 服务地址
	ExtractMistralOCRModel            string // Mistral OCR 请求模型
	ExtractMistralOCRTimeoutSeconds   int    // Mistral OCR 请求超时(秒)
	ExtractMistralOCRAuthToken        string // Mistral OCR 鉴权 Token
	ExtractLLMOCRBaseURL              string // LLM OCR 服务地址
	ExtractLLMOCRModel                string // LLM OCR 请求模型
	ExtractLLMOCRTimeoutSeconds       int    // LLM OCR 请求超时(秒)
	ExtractLLMOCRAuthToken            string // LLM OCR 鉴权 Token
	ExtractLLMOCRPrompt               string // LLM OCR 提示词
	EmbeddingEnabled                  bool   // 是否启用 Embedding 服务
	EmbeddingHost                     string // Embedding HTTP 服务地址
	EmbeddingProtocol                 string // Embedding 请求协议：openai / gemini / voyage / jina
	EmbeddingKey                      string // Embedding HTTP 服务鉴权 Key，可选
	EmbeddingTimeoutSeconds           int    // Embedding 请求超时（秒）
	EmbeddingOutputDimensions         int    // 写库/检索统一输出维度
	EmbeddingDimensionsPolicy         string // Embedding 请求 dimensions 参数策略
	EmbeddingNormalize                bool   // 是否做归一化
	EmbeddingModelSignature           string // 当前生效的模型签名（派生值，由 settings 变更时自动更新）
	EmbedTriggerOnUpload              bool   // 上传后是否异步触发 embedding
	EmbedChunkSizeTokens              int    // RAG 分片大小（token 估算）
	EmbedChunkOverlapTokens           int    // 分片重叠 token 数
	EmbedBatchSize                    int    // embedding API 单批次数量
	RAGTopK                           int    // RAG 检索返回片段数
	RAGModel                          string // embedding 使用的模型名
	// chat（补充）
	RAGEnabled                      bool    // 全局开关：是否允许 RAG 功能
	RAGMinSimilarity                float64 // RAG 最低相似度阈值
	RAGTokenBudget                  int     // RAG 注入 token 预算
	RAGFetchMultiplier              int     // 检索阶段额外抓取倍数，用于过滤后留足候选
	RAGWaitReadyMS                  int     // 发送时等待 embedding 就绪的最长时间
	RAGQueryHistoryTurns            int     // 构建 RAG 查询时带入的最近用户轮次
	RAGRetrievalCacheTTL            int     // RAG 检索缓存 TTL(秒)
	RAGHybridEnabled                bool    // 是否启用混合检索（BM25+向量）
	ContextCompactHighlightsPerRole int     // 压缩摘要每个角色保留的亮点条数
	ContextCompactSnippetChars      int     // 压缩摘要单条片段最大字符数
	// 上下文压缩增强选项
	CompactLLMEnabled   bool // 是否启用 LLM 语义压缩（4级回退的 Level 2/3）
	CompactTaskModel    string
	CompactAsyncEnabled bool   // 是否将压缩移出响应关键路径（异步执行）
	CompactMaxFailures  int    // LLM 压缩熔断阈值：连续失败次数上限
	CompactSystemPrompt string // 全量摘要提示词（空串用内置默认值）；支持占位符 {{FROM_TURN}}、{{TO_TURN}}
	CompactLightPrompt  string // 轻量摘要提示词（空串用内置默认值）；支持占位符 {{FROM_TURN}}、{{TO_TURN}}
	// Token 预算感知上下文截断
	ContextTokenBudgetEnabled bool // 是否按 Token 预算截断上下文（替代消息数截断）
	// 消息历史 Embedding（语义上下文召回）
	MessageEmbeddingEnabled      bool // 是否对每轮消息异步生成向量嵌入
	SemanticContextEnabled       bool // 是否在上下文组装时加入语义召回片段
	ProcessTraceEnabled          bool // 是否启用消息处理轨迹
	ProcessTracePersistInflight  bool // 是否在流式阶段持久化轨迹
	ContextArtifactRetentionDays int  // 上下文证据保留天数，<=0 表示不自动过期
	// MCP 配置
	MCPEnable                     bool
	MCPToolTimeoutSeconds         int
	MCPToolRetryCount             int
	MCPMaxConcurrentCalls         int
	MCPMaxSelectedToolsPerMessage int
	MCPMaxLLMCallsPerRun          int
	MCPMaxToolCallsPerRun         int
	MCPToolPrompt                 string
}

// Load 加载配置：读取 config.json（不存在时从内置模板释放，缺键时补全），再用环境变量覆盖。
// 优先级为环境变量 > config.json > 内置模板；环境变量不会写回配置文件。
// 动态业务配置使用硬编码默认值，启动后由 settings.RuntimeSettings 从 DB 覆盖。
func Load() (Config, error) {
	return loadPath(resolveConfigFile())
}

// LoadForDataDir keeps a desktop sidecar's configuration inside its writable data directory.
func LoadForDataDir(dir string) (Config, error) {
	if dir == "" {
		return Load()
	}
	if !filepath.IsAbs(dir) {
		return Config{}, errors.New("local mode: data directory must be an absolute path")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return Config{}, err
	}
	return loadPath(filepath.Join(dir, "config.json"))
}

func loadPath(path string) (Config, error) {
	fc, err := loadFile(path)
	if err != nil {
		return Config{}, err
	}
	def := templateConfig()
	sourceDir := ""
	if absolutePath, absErr := filepath.Abs(path); absErr == nil {
		sourceDir = filepath.Dir(absolutePath)
	}
	return Config{
		// 静态基础设施
		ConfigFile:                   path,
		AppName:                      envOr("APP_NAME", fc.App.Name, def.App.Name),
		Env:                          normalizeEnv(envOrNonEmpty("APP_ENV", fc.App.Env, def.App.Env)),
		BrandTitle:                   valueOrDefault(fc.Branding.Title, def.Branding.Title),
		BrandShortName:               valueOrDefault(fc.Branding.ShortName, def.Branding.ShortName),
		BrandDescription:             valueOrDefault(fc.Branding.Description, def.Branding.Description),
		BrandLogoURL:                 strings.TrimSpace(fc.Branding.LogoURL),
		BrandFaviconURL:              valueOrDefault(fc.Branding.FaviconURL, def.Branding.FaviconURL),
		BrandPWAIcon192URL:           valueOrDefault(fc.Branding.PWAIcon192URL, def.Branding.PWAIcon192URL),
		BrandPWAIcon512URL:           valueOrDefault(fc.Branding.PWAIcon512URL, def.Branding.PWAIcon512URL),
		BrandPWAMaskableIcon512URL:   valueOrDefault(fc.Branding.PWAMaskableIcon512URL, def.Branding.PWAMaskableIcon512URL),
		BrandAppleTouchIcon180URL:    valueOrDefault(fc.Branding.AppleTouchIcon180URL, def.Branding.AppleTouchIcon180URL),
		HTTPPort:                     envOr("HTTP_PORT", fc.Server.HTTPPort, def.Server.HTTPPort),
		CORSAllowOrigin:              envOr("CORS_ALLOW_ORIGIN", fc.Server.CORSAllowOrigin, def.Server.CORSAllowOrigin),
		TrustedProxies:               envOr("TRUSTED_PROXIES", fc.Server.TrustedProxies, def.Server.TrustedProxies),
		PublicAPIBaseURL:             envOr("PUBLIC_API_BASE_URL", fc.Server.PublicAPIBaseURL, def.Server.PublicAPIBaseURL),
		PublicWebBaseURL:             envOr("PUBLIC_WEB_BASE_URL", fc.Server.PublicWebBaseURL, def.Server.PublicWebBaseURL),
		FrontendDistDir:              envOrPath("FRONTEND_DIST_DIR", fc.Server.FrontendDistDir, def.Server.FrontendDistDir, sourceDir),
		HTTPReadHeaderTimeoutSeconds: envOrInt("HTTP_READ_HEADER_TIMEOUT_SECONDS", fc.Server.ReadHeaderTimeoutSeconds, def.Server.ReadHeaderTimeoutSeconds),
		HTTPReadTimeoutSeconds:       envOrInt("HTTP_READ_TIMEOUT_SECONDS", fc.Server.ReadTimeoutSeconds, def.Server.ReadTimeoutSeconds),
		HTTPIdleTimeoutSeconds:       envOrInt("HTTP_IDLE_TIMEOUT_SECONDS", fc.Server.IdleTimeoutSeconds, def.Server.IdleTimeoutSeconds),
		HTTPMaxHeaderBytes:           envOrInt("HTTP_MAX_HEADER_BYTES", fc.Server.MaxHeaderBytes, def.Server.MaxHeaderBytes),
		HTTPShutdownTimeoutSeconds:   envOrInt("HTTP_SHUTDOWN_TIMEOUT_SECONDS", fc.Server.ShutdownTimeoutSeconds, def.Server.ShutdownTimeoutSeconds),
		JWTSecret:                    envOr("JWT_SECRET", fc.Security.JWTSecret, defaultJWTSecret),
		MCPUserContextSecret:         envOr("MCP_USER_CONTEXT_SECRET", fc.Security.MCPUserContextSecret, def.Security.MCPUserContextSecret),
		DataEncryptionKeysPrevious:   envOrList("DATA_ENCRYPTION_KEYS_PREVIOUS", fc.Security.DataEncryptionKeysPrevious),
		DataEncryptionKey:            envOr("DATA_ENCRYPTION_KEY", fc.Security.DataEncryptionKey, defaultDataEncryptionKey),
		SSRFProtectionEnabled:        envOrBool("SSRF_PROTECTION_ENABLED", fc.Security.SSRFProtectionEnabled),
		SSRFAllowedHosts:             envOr("SSRF_ALLOWED_HOSTS", fc.Security.SSRFAllowedHosts, def.Security.SSRFAllowedHosts),
		SSRFAllowedCIDRs:             envOr("SSRF_ALLOWED_CIDRS", fc.Security.SSRFAllowedCIDRs, def.Security.SSRFAllowedCIDRs),
		DatabaseDriver:               normalizeDatabaseDriver(envOr("DATABASE_DRIVER", fc.Database.Driver, def.Database.Driver)),
		PostgresDSN:                  normalizePostgresDSN(envOr("POSTGRES_DSN", fc.Database.Postgres.DSN, def.Database.Postgres.DSN)),
		PostgresMaxOpenConns:         envOrInt("POSTGRES_MAX_OPEN_CONNS", fc.Database.Postgres.MaxOpenConns, def.Database.Postgres.MaxOpenConns),
		PostgresMaxIdleConns:         envOrInt("POSTGRES_MAX_IDLE_CONNS", fc.Database.Postgres.MaxIdleConns, def.Database.Postgres.MaxIdleConns),
		PostgresConnMaxLifetimeMin:   envOrInt("POSTGRES_CONN_MAX_LIFETIME_MINUTES", fc.Database.Postgres.ConnMaxLifetimeMin, def.Database.Postgres.ConnMaxLifetimeMin),
		PostgresConnMaxIdleTimeMin:   envOrInt("POSTGRES_CONN_MAX_IDLE_TIME_MINUTES", fc.Database.Postgres.ConnMaxIdleTimeMin, def.Database.Postgres.ConnMaxIdleTimeMin),
		SQLitePath:                   envOrPath("SQLITE_PATH", fc.Database.SQLite.Path, def.Database.SQLite.Path, sourceDir),
		SQLiteDSN:                    envOr("SQLITE_DSN", fc.Database.SQLite.DSN, def.Database.SQLite.DSN),
		SQLiteMaxOpenConns:           envOrInt("SQLITE_MAX_OPEN_CONNS", fc.Database.SQLite.MaxOpenConns, def.Database.SQLite.MaxOpenConns),
		SQLiteBusyTimeoutMS:          envOrInt("SQLITE_BUSY_TIMEOUT_MS", fc.Database.SQLite.BusyTimeoutMS, def.Database.SQLite.BusyTimeoutMS),
		SQLiteCacheSizeKB:            envOrInt("SQLITE_CACHE_SIZE_KB", fc.Database.SQLite.CacheSizeKB, def.Database.SQLite.CacheSizeKB),
		SQLiteMmapSizeBytes:          envOrInt64("SQLITE_MMAP_SIZE_BYTES", fc.Database.SQLite.MmapSizeBytes, def.Database.SQLite.MmapSizeBytes),
		SQLiteSynchronous:            normalizeSQLiteSynchronous(envOr("SQLITE_SYNCHRONOUS", fc.Database.SQLite.Synchronous, def.Database.SQLite.Synchronous)),
		SQLiteTempStore:              normalizeSQLiteTempStore(envOr("SQLITE_TEMP_STORE", fc.Database.SQLite.TempStore, def.Database.SQLite.TempStore)),
		CacheDriver:                  normalizeCacheDriver(envOr("CACHE_DRIVER", fc.Cache.Driver, def.Cache.Driver)),
		RedisAddr:                    envOr("REDIS_ADDR", fc.Database.Redis.Addr, def.Database.Redis.Addr),
		RedisUsername:                envOr("REDIS_USERNAME", fc.Database.Redis.Username, def.Database.Redis.Username),
		RedisPassword:                envOr("REDIS_PASSWORD", fc.Database.Redis.Password, def.Database.Redis.Password),
		RedisDB:                      envOrInt("REDIS_DB", fc.Database.Redis.DB, def.Database.Redis.DB),
		RedisTLSEnabled:              envOrBool("REDIS_TLS_ENABLED", fc.Database.Redis.TLSEnabled),
		RedisTLSInsecureSkipVerify:   envOrBool("REDIS_TLS_INSECURE_SKIP_VERIFY", fc.Database.Redis.TLSInsecureSkipVerify),
		StorageBackend:               envOr("STORAGE_BACKEND", fc.Storage.Backend, def.Storage.Backend),
		StorageRootDir:               envOrPath("STORAGE_ROOT_DIR", fc.Storage.Local.RootDir, def.Storage.Local.RootDir, sourceDir),
		StorageS3Endpoint:            envOr("STORAGE_S3_ENDPOINT", fc.Storage.S3.Endpoint, def.Storage.S3.Endpoint),
		StorageS3Region:              envOr("STORAGE_S3_REGION", fc.Storage.S3.Region, def.Storage.S3.Region),
		StorageS3Bucket:              envOr("STORAGE_S3_BUCKET", fc.Storage.S3.Bucket, def.Storage.S3.Bucket),
		StorageS3Prefix:              envOr("STORAGE_S3_PREFIX", fc.Storage.S3.Prefix, def.Storage.S3.Prefix),
		StorageS3AccessKeyID:         envOr("STORAGE_S3_ACCESS_KEY_ID", fc.Storage.S3.AccessKeyID, def.Storage.S3.AccessKeyID),
		StorageS3SecretAccessKey:     envOr("STORAGE_S3_SECRET_ACCESS_KEY", fc.Storage.S3.SecretAccessKey, def.Storage.S3.SecretAccessKey),
		StorageS3ForcePathStyle:      envOrBool("STORAGE_S3_FORCE_PATH_STYLE", fc.Storage.S3.ForcePathStyle),
		AdminUsername:                defaultAdminUsername,
		AdminDisplayName:             defaultAdminDisplayName,
		GeoIPProvider:                envOr("GEOIP_PROVIDER", fc.GeoIP.Provider, def.GeoIP.Provider),
		GeoIPBaseURL:                 envOr("GEOIP_BASE_URL", fc.GeoIP.BaseURL, def.GeoIP.BaseURL),
		GeoIPToken:                   envOr("GEOIP_TOKEN", fc.GeoIP.Token, def.GeoIP.Token),
		GeoIPTimeoutMS:               envOrInt("GEOIP_TIMEOUT_MS", fc.GeoIP.TimeoutMS, def.GeoIP.TimeoutMS),
		GeoIPDatabaseURL:             envOr("GEOIP_DATABASE_URL", fc.GeoIP.DatabaseURL, def.GeoIP.DatabaseURL),
		GeoIPDatabasePath:            envOrPath("GEOIP_DATABASE_PATH", fc.GeoIP.DatabasePath, def.GeoIP.DatabasePath, sourceDir),
		GeoIPDatabaseMaxBytes:        envOrInt64("GEOIP_DATABASE_MAX_BYTES", fc.GeoIP.DatabaseMaxBytes, def.GeoIP.DatabaseMaxBytes),
		GeoIPRefreshIntervalHours:    envOrInt("GEOIP_REFRESH_INTERVAL_HOURS", fc.GeoIP.RefreshIntervalHours, def.GeoIP.RefreshIntervalHours),
		SMTPHost:                     "",
		SMTPPort:                     587,
		SMTPUsername:                 "",
		SMTPPassword:                 "",
		SMTPFrom:                     "",
		TurnstileSiteverifyURL:       envOr("TURNSTILE_SITEVERIFY_URL", fc.Security.TurnstileSiteverifyURL, def.Security.TurnstileSiteverifyURL),
		OTelEnabled:                  envOrBoolOptional("OTEL_ENABLED", fc.Observability.Tracing.Enabled),
		OTelExporterOTLPEndpoint:     envOr("OTEL_EXPORTER_OTLP_ENDPOINT", fc.Observability.Tracing.Endpoint, def.Observability.Tracing.Endpoint),
		OTelExporterOTLPHeaders:      envOr("OTEL_EXPORTER_OTLP_HEADERS", fc.Observability.Tracing.Headers, def.Observability.Tracing.Headers),
		OTelExporterOTLPInsecure:     envOrBool("OTEL_EXPORTER_OTLP_INSECURE", fc.Observability.Tracing.Insecure),
		OTelExporterOTLPProtocol:     normalizeOTelExporterOTLPProtocol(envOr("OTEL_EXPORTER_OTLP_PROTOCOL", fc.Observability.Tracing.Protocol, def.Observability.Tracing.Protocol)),
		OTelSamplingRate:             envOrFloat("OTEL_TRACES_SAMPLER_ARG", envOrFloat("OTEL_SAMPLING_RATE", fc.Observability.Tracing.SamplingRate, def.Observability.Tracing.SamplingRate), def.Observability.Tracing.SamplingRate),
		UpdateEnabled:                envOrBool("UPDATE_ENABLED", fc.Update.Enabled),
		UpdateChannel:                envOr("UPDATE_CHANNEL", fc.Update.Channel, def.Update.Channel),
		UpdateCheckIntervalSeconds:   envOrInt("UPDATE_CHECK_INTERVAL_SECONDS", fc.Update.CheckIntervalSeconds, def.Update.CheckIntervalSeconds),
		UpdateSource:                 envOr("UPDATE_SOURCE", fc.Update.Source, def.Update.Source),
		UpdateProxyBaseURL:           envOr("UPDATE_PROXY_BASE_URL", fc.Update.ProxyBaseURL, def.Update.ProxyBaseURL),
		UpdateRepo:                   envOr("UPDATE_REPO", fc.Update.Repo, def.Update.Repo),
		UpdateDownloadDir:            envOrPath("UPDATE_DOWNLOAD_DIR", fc.Update.DownloadDir, def.Update.DownloadDir, sourceDir),

		// 动态配置默认值（会被 DB 覆盖）
		TokenTTLHours:                     24,
		RefreshTokenTTLHours:              720,
		LoginMaxFailures:                  5,
		LoginLockMinutes:                  15,
		RateLimitEnabled:                  false,
		RateLimitRPM:                      60,
		PublicAuthRateLimitRPM:            30,
		UsernameLoginEnabled:              true,
		EmailLoginEnabled:                 true,
		ThirdPartyLoginEnabled:            true,
		EmailRegistrationEnabled:          true,
		EmailVerificationEnabled:          false,
		PasswordResetEnabled:              false,
		EmailRegistrationDomains:          "",
		EmailRegistrationNoAlias:          false,
		AutoLinkVerifiedEmail:             true,
		TurnstileRegistrationEnabled:      false,
		TurnstileSiteKey:                  "",
		TurnstileSecretKey:                "",
		MaxContextMessages:                20,
		ContextMaxTurns:                   48,
		ContextCompactEnabled:             false,
		UIComponentsEnabled:               true,
		ContextWindowFallbackTokens:       DefaultContextWindowFallbackTokens,
		ContextCompactTriggerPercent:      DefaultContextCompactTriggerPercent,
		ContextCompactPreserve:            8,
		ConversationDefaultModel:          "",
		ConversationTaskModel:             "follow",
		ConversationTitlePrompt:           "",
		ConversationLabelsPrompt:          "",
		DefaultSystemPrompt:               "",
		SkillsPrompt:                      "",
		ModelOptionPolicyMode:             "allowlist",
		ModelOptionAllowedPaths:           DefaultModelOptionAllowedPathsJSON(),
		ModelOptionDeniedPaths:            DefaultModelOptionDeniedPathsJSON(),
		KnowledgeBaseEnabled:              true,
		PersonalProvidersMaxPerUser:       DefaultPersonalProvidersMaxPerUser,
		DesktopDownloadEnabled:            true,
		DesktopDownloadURL:                DefaultDesktopDownloadURL,
		UserStorageQuotaBytes:             104857600,
		MaxUploadFileBytes:                20971520,
		MaxMessageFiles:                   10,
		ImageMaxDimension:                 1024,
		FileFullContextLimitEnabled:       true,
		FileFullContextMaxBytes:           DefaultFileFullContextMaxBytes,
		FileFullContextMaxTokens:          65536,
		FileImageMaxBytes:                 0,
		FileDocMaxBytes:                   0,
		FileFullContextPDFMaxPages:        20,
		FileAllowedMIMETypes:              "image/jpeg,image/png,image/webp,image/gif,video/mp4,video/webm,video/quicktime,video/mpeg,audio/mpeg,audio/wav,audio/mp4,audio/aac,audio/ogg,audio/flac,audio/aiff,text/plain,text/markdown,text/csv,text/yaml,application/json,application/yaml,application/x-yaml,application/toml,application/pdf,application/msword,application/vnd.openxmlformats-officedocument.wordprocessingml.document,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,application/vnd.ms-excel",
		ExtractEngine:                     "builtin",
		ExtractOCREngine:                  "rapidocr",
		ExtractImageOCREnabled:            false,
		ExtractPDFOCRFallbackEnabled:      false,
		ExtractTikaSource:                 "external",
		ExtractTikaBaseURL:                "http://127.0.0.1:9998",
		ExtractTikaTimeoutSeconds:         60,
		ExtractTikaAuthToken:              "",
		ExtractDoclingBaseURL:             "http://127.0.0.1:8005/ocr",
		ExtractDoclingTimeoutSeconds:      60,
		ExtractDoclingAuthToken:           "",
		ExtractTesseractOCRBaseURL:        "http://127.0.0.1:8004/ocr",
		ExtractTesseractOCRTimeoutSeconds: 60,
		ExtractTesseractOCRAuthToken:      "",
		ExtractRapidOCRSource:             "external",
		ExtractRapidOCRBaseURL:            "http://127.0.0.1:8002/ocr",
		ExtractRapidOCRTimeoutSeconds:     60,
		ExtractRapidOCRAuthToken:          "",
		ExtractPaddleOCRBaseURL:           "",
		ExtractPaddleOCRTimeoutSeconds:    60,
		ExtractPaddleOCRAuthToken:         "",
		ExtractTencentOCRSecretID:         "",
		ExtractTencentOCRSecretKey:        "",
		ExtractTencentOCRRegion:           "ap-guangzhou",
		ExtractTencentOCREndpoint:         "ocr.tencentcloudapi.com",
		ExtractTencentOCRTimeoutSeconds:   60,
		ExtractAliyunOCRAccessKeyID:       "",
		ExtractAliyunOCRAccessKeySecret:   "",
		ExtractAliyunOCRRegion:            "cn-hangzhou",
		ExtractAliyunOCREndpoint:          "ocr-api.cn-hangzhou.aliyuncs.com",
		ExtractAliyunOCRTimeoutSeconds:    60,
		ExtractMinerUSource:               "cloud",
		ExtractMinerUBaseURL:              "https://mineru.net/api/v4",
		ExtractMinerUFileTypes:            "pdf,word,presentation",
		ExtractMinerUTimeoutSeconds:       180,
		ExtractMinerUAuthToken:            "",
		ExtractMistralOCRBaseURL:          "https://api.mistral.ai/v1/ocr",
		ExtractMistralOCRModel:            "mistral-ocr-latest",
		ExtractMistralOCRTimeoutSeconds:   60,
		ExtractMistralOCRAuthToken:        "",
		ExtractLLMOCRBaseURL:              "",
		ExtractLLMOCRModel:                "",
		ExtractLLMOCRTimeoutSeconds:       60,
		ExtractLLMOCRAuthToken:            "",
		ExtractLLMOCRPrompt:               "",
		EmbeddingEnabled:                  false,
		EmbeddingHost:                     "",
		EmbeddingKey:                      "",
		EmbeddingTimeoutSeconds:           60,
		EmbeddingOutputDimensions:         1536,
		EmbeddingDimensionsPolicy:         EmbeddingDimensionsPolicySend,
		EmbeddingProtocol:                 EmbeddingProtocolOpenAI,
		EmbeddingNormalize:                true,
		EmbedTriggerOnUpload:              true,
		EmbedChunkSizeTokens:              1024,
		EmbedChunkOverlapTokens:           64,
		EmbedBatchSize:                    20,
		RAGTopK:                           5,
		RAGModel:                          "sentence-transformers/all-MiniLM-L6-v2",
		RAGEnabled:                        false,
		RAGMinSimilarity:                  0.45,
		RAGTokenBudget:                    2000,
		RAGFetchMultiplier:                3,
		RAGWaitReadyMS:                    3000,
		RAGQueryHistoryTurns:              0,
		RAGRetrievalCacheTTL:              120,
		ContextCompactHighlightsPerRole:   6,
		ContextCompactSnippetChars:        140,
		CompactLLMEnabled:                 true,
		CompactTaskModel:                  "follow",
		CompactAsyncEnabled:               true,
		CompactMaxFailures:                3,
		ContextTokenBudgetEnabled:         true,
		MessageEmbeddingEnabled:           false, // 默认关闭，需要 embedding 服务就绪后开启
		SemanticContextEnabled:            false,
		ProcessTraceEnabled:               true,
		ProcessTracePersistInflight:       true,
		ContextArtifactRetentionDays:      90,
		MCPEnable:                         false,
		MCPToolTimeoutSeconds:             10,
		MCPToolRetryCount:                 0,
		MCPMaxConcurrentCalls:             8,
		MCPMaxSelectedToolsPerMessage:     DefaultMCPMaxSelectedToolsPerMessage,
		MCPMaxLLMCallsPerRun:              5,
		MCPMaxToolCallsPerRun:             8,
		MCPToolPrompt:                     "",
	}, nil
}

// Validate 检查关键配置是否合法。
func (c Config) Validate() error {
	if err := c.validateDatabase(); err != nil {
		return err
	}
	if err := c.validateCache(); err != nil {
		return err
	}
	if err := c.validateStorage(); err != nil {
		return err
	}
	env := normalizeEnv(c.Env)
	if env != "dev" && env != "prod" {
		if env == "" {
			return errors.New("invalid config: APP_ENV/app.env must be dev, development, prod, or production (got empty)")
		}
		return fmt.Errorf("invalid config: APP_ENV/app.env must be dev, development, prod, or production (got %q)", c.Env)
	}
	if _, err := sharedsecurity.NewOutboundPolicy(c.ssrfProtectionEnforced(), splitCommaSeparated(c.SSRFAllowedHosts), splitCommaSeparated(c.SSRFAllowedCIDRs)); err != nil {
		return fmt.Errorf("invalid config: SSRF allowlist: %w", err)
	}
	if err := validateHTTPIntegrationURL(c.TurnstileSiteverifyURL, "TURNSTILE_SITEVERIFY_URL"); err != nil {
		return err
	}
	if _, err := c.Keyring(); err != nil {
		return fmt.Errorf("invalid config: DATA_ENCRYPTION_KEY: %w", err)
	}
	if env != "prod" {
		return nil
	}

	if strings.TrimSpace(c.JWTSecret) == "" || strings.TrimSpace(c.JWTSecret) == defaultJWTSecret {
		return errors.New("invalid production config: JWT_SECRET/security.jwt_secret must be explicitly set")
	}
	if len(strings.TrimSpace(c.JWTSecret)) < 16 {
		return errors.New("invalid production config: JWT_SECRET/security.jwt_secret is too short")
	}
	if strings.TrimSpace(c.DataEncryptionKey) == "" || strings.TrimSpace(c.DataEncryptionKey) == defaultDataEncryptionKey {
		return errors.New("invalid production config: DATA_ENCRYPTION_KEY/security.data_encryption_key must be explicitly set")
	}
	if len(strings.TrimSpace(c.DataEncryptionKey)) < 32 {
		return errors.New("invalid production config: DATA_ENCRYPTION_KEY/security.data_encryption_key is too short")
	}

	if strings.TrimSpace(c.CORSAllowOrigin) == "" || strings.TrimSpace(c.CORSAllowOrigin) == "*" {
		return errors.New("invalid production config: CORS_ALLOW_ORIGIN/server.cors_allow_origin must be explicitly set (wildcard * is not allowed)")
	}
	if c.LocalMode {
		// 本地 sidecar 只在回环地址上服务，公共 URL 在监听后由实际端口填入；
		// 其余生产级校验（密钥强度、CORS 白名单）对本地模式同样生效。
		return nil
	}
	if err := validatePublicURL(c.PublicAPIBaseURL, "PUBLIC_API_BASE_URL/server.public_api_base_url"); err != nil {
		return err
	}
	if err := validatePublicURL(c.PublicWebBaseURL, "PUBLIC_WEB_BASE_URL/server.public_web_base_url"); err != nil {
		return err
	}

	return nil
}

func (c Config) validateDatabase() error {
	switch normalizeDatabaseDriver(c.DatabaseDriver) {
	case "postgres":
		return nil
	case "sqlite":
		if strings.TrimSpace(c.SQLiteDSN) == "" && strings.TrimSpace(c.SQLitePath) == "" {
			return errors.New("invalid database config: SQLITE_PATH or SQLITE_DSN must be set when DATABASE_DRIVER=sqlite")
		}
		if normalizeSQLiteSynchronous(c.SQLiteSynchronous) == "" {
			return fmt.Errorf("invalid database config: unsupported SQLITE_SYNCHRONOUS %q", c.SQLiteSynchronous)
		}
		if normalizeSQLiteTempStore(c.SQLiteTempStore) == "" {
			return fmt.Errorf("invalid database config: unsupported SQLITE_TEMP_STORE %q", c.SQLiteTempStore)
		}
		return nil
	default:
		return fmt.Errorf("invalid database config: unsupported DATABASE_DRIVER %q", c.DatabaseDriver)
	}
}

func (c Config) validateCache() error {
	switch normalizeCacheDriver(c.CacheDriver) {
	case "redis":
		return nil
	case "memory":
		return nil
	default:
		return fmt.Errorf("invalid cache config: unsupported CACHE_DRIVER %q", c.CacheDriver)
	}
}

func (c Config) validateStorage() error {
	switch strings.ToLower(strings.TrimSpace(c.StorageBackend)) {
	case "", "local":
		return nil
	case "s3":
		if strings.TrimSpace(c.StorageS3Bucket) == "" {
			return errors.New("invalid storage config: STORAGE_S3_BUCKET must be set when STORAGE_BACKEND=s3")
		}
		if strings.TrimSpace(c.StorageS3Region) == "" {
			return errors.New("invalid storage config: STORAGE_S3_REGION must be set when STORAGE_BACKEND=s3")
		}
		return nil
	default:
		return fmt.Errorf("invalid storage config: unsupported STORAGE_BACKEND %q", c.StorageBackend)
	}
}

func validateHTTPIntegrationURL(raw string, label string) error {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil
	}
	if err := sharedsecurity.ValidateTrustedOutboundHTTPURL(value); err != nil {
		return fmt.Errorf("invalid config: %s must be an http(s) URL without credentials; metadata and link-local targets are not allowed", label)
	}
	return nil
}

func validatePublicURL(raw string, label string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil || parsed.Host == "" || parsed.Scheme != "https" {
		return fmt.Errorf("invalid production config: %s must be an https URL", label)
	}
	return nil
}

// envOr 优先级：环境变量 > 配置文件值 > 模板默认值。
func envOr(envKey string, fileVal string, defaultVal string) string {
	if v, ok := os.LookupEnv(envKey); ok {
		return v
	}
	if fileVal != "" {
		return fileVal
	}
	return defaultVal
}

func valueOrDefault(value string, defaultValue string) string {
	if normalized := strings.TrimSpace(value); normalized != "" {
		return normalized
	}
	return defaultValue
}

func envOrNonEmpty(envKey string, fileVal string, defaultVal string) string {
	if v, ok := os.LookupEnv(envKey); ok && strings.TrimSpace(v) != "" {
		return v
	}
	if strings.TrimSpace(fileVal) != "" {
		return fileVal
	}
	return defaultVal
}

func normalizeEnv(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "development":
		return "dev"
	case "production":
		return "prod"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

// IsProduction 判断配置是否使用生产环境语义。
func (c Config) IsProduction() bool {
	return normalizeEnv(c.Env) == "prod"
}

func normalizeDatabaseDriver(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "postgres", "postgresql", "pg":
		return "postgres"
	case "sqlite", "sqlite3":
		return "sqlite"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func normalizePostgresDSN(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return trimmed
	}
	if strings.Contains(trimmed, "://") {
		parsed, err := url.Parse(trimmed)
		if err != nil || parsed == nil || parsed.RawQuery == "" {
			return trimmed
		}
		parts := strings.Split(parsed.RawQuery, "&")
		changed := false
		for index, part := range parts {
			key, rawValue, ok := strings.Cut(part, "=")
			if !ok {
				continue
			}
			decodedKey, keyErr := url.QueryUnescape(key)
			if keyErr != nil || !strings.EqualFold(decodedKey, "timezone") || !strings.Contains(rawValue, "%") {
				continue
			}
			decodedValue, valueErr := url.QueryUnescape(rawValue)
			if valueErr != nil || strings.TrimSpace(decodedValue) == "" || decodedValue == rawValue {
				continue
			}
			parts[index] = key + "=" + decodedValue
			changed = true
		}
		if !changed {
			return trimmed
		}
		parsed.RawQuery = strings.Join(parts, "&")
		return parsed.String()
	}

	parts := strings.Fields(trimmed)
	changed := false
	for index, part := range parts {
		key, rawValue, ok := strings.Cut(part, "=")
		if !ok || !strings.EqualFold(key, "timezone") || !strings.Contains(rawValue, "%") {
			continue
		}
		decodedValue, err := url.QueryUnescape(rawValue)
		if err != nil || strings.TrimSpace(decodedValue) == "" || decodedValue == rawValue {
			continue
		}
		parts[index] = key + "=" + decodedValue
		changed = true
	}
	if !changed {
		return trimmed
	}
	return strings.Join(parts, " ")
}

func normalizeCacheDriver(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "redis":
		return "redis"
	case "memory", "mem", "inmemory", "in-memory":
		return "memory"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func normalizeOTelExporterOTLPProtocol(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "http", "http/protobuf":
		return "http"
	default:
		return "grpc"
	}
}

func normalizeSQLiteSynchronous(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "", "NORMAL":
		return "NORMAL"
	case "OFF", "FULL", "EXTRA":
		return strings.ToUpper(strings.TrimSpace(value))
	default:
		return ""
	}
}

func normalizeSQLiteTempStore(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "", "MEMORY":
		return "MEMORY"
	case "DEFAULT", "FILE":
		return strings.ToUpper(strings.TrimSpace(value))
	default:
		return ""
	}
}

func envOrPath(envKey string, fileVal string, defaultVal string, sourceDir string) string {
	if v, ok := os.LookupEnv(envKey); ok {
		return v
	}
	if fileVal != "" {
		return resolveConfigPath(fileVal, sourceDir)
	}
	return resolveConfigPath(defaultVal, sourceDir)
}

func resolveConfigPath(value string, sourceDir string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || filepath.IsAbs(trimmed) || strings.TrimSpace(sourceDir) == "" {
		return trimmed
	}
	return filepath.Clean(filepath.Join(sourceDir, trimmed))
}

func envOrInt(envKey string, fileVal int, defaultVal int) int {
	if v, ok := os.LookupEnv(envKey); ok {
		if parsed, err := strconv.Atoi(v); err == nil {
			return parsed
		}
	}
	if fileVal != 0 {
		return fileVal
	}
	return defaultVal
}

func envOrInt64(envKey string, fileVal int64, defaultVal int64) int64 {
	if v, ok := os.LookupEnv(envKey); ok {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			return parsed
		}
	}
	if fileVal != 0 {
		return fileVal
	}
	return defaultVal
}

func envOrFloat(envKey string, fileVal float64, defaultVal float64) float64 {
	if v, ok := os.LookupEnv(envKey); ok {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			return parsed
		}
	}
	if fileVal != 0 {
		return fileVal
	}
	return defaultVal
}

func envOrBoolOptional(envKey string, fileVal *bool) *bool {
	if v, ok := os.LookupEnv(envKey); ok {
		if parsed, err := strconv.ParseBool(v); err == nil {
			return &parsed
		}
	}
	return fileVal
}

func envOrBool(envKey string, fileVal bool) bool {
	if v, ok := os.LookupEnv(envKey); ok {
		if parsed, err := strconv.ParseBool(v); err == nil {
			return parsed
		}
	}
	return fileVal
}

// TrustedProxyList 返回受信代理列表，支持逗号分隔。
func (c Config) TrustedProxyList() []string {
	return splitCommaSeparated(c.TrustedProxies)
}

// TrustedOutboundPolicy 返回部署级集成和可信私网重定向使用的全局 SSRF 白名单策略。
// 管理员保存的模型、MCP、Embedding 和身份源 endpoint 由对应适配器按精确 origin 局部授权；
// 只有跨 origin 的私网重定向目标需要显式进入全局白名单。
// 非法白名单在 Config.Validate 阶段阻止启动；此处保守回退为无白名单严格策略。
func (c Config) TrustedOutboundPolicy() sharedsecurity.OutboundPolicy {
	policy, err := sharedsecurity.NewOutboundPolicy(
		c.ssrfProtectionEnforced(),
		splitCommaSeparated(c.SSRFAllowedHosts),
		splitCommaSeparated(c.SSRFAllowedCIDRs),
	)
	if err != nil {
		return sharedsecurity.NewStrictOutboundPolicy(c.ssrfProtectionEnforced())
	}
	return policy
}

// StrictOutboundPolicy 返回不继承私网白名单的 SSRF 策略，用于外部内容和固定公网请求。
func (c Config) StrictOutboundPolicy() sharedsecurity.OutboundPolicy {
	return sharedsecurity.NewStrictOutboundPolicy(c.ssrfProtectionEnforced())
}

func (c Config) ssrfProtectionEnforced() bool {
	return c.IsProduction() && c.SSRFProtectionEnabled
}

// envOrList 读取逗号分隔的环境变量；未设置时使用 YAML 列表。
func envOrList(envKey string, yamlVal []string) []string {
	if raw, ok := os.LookupEnv(envKey); ok {
		return splitCommaSeparated(raw)
	}
	result := make([]string, 0, len(yamlVal))
	for _, item := range yamlVal {
		if value := strings.TrimSpace(item); value != "" {
			result = append(result, value)
		}
	}
	return result
}

// isPlaceholderSecret 判断密钥是否为内置默认值或示例配置里的占位值（change-me-…）。
// 这些值随源码公开，用它们加密等于没有加密。
func isPlaceholderSecret(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || value == defaultDataEncryptionKey || value == defaultJWTSecret || strings.HasPrefix(strings.ToLower(value), "change-me")
}

var keyrings sync.Map // 主密钥组合的 SHA-256 -> *secretbox.Keyring

// Keyring 返回由当前主密钥与旧主密钥构建的密钥环。同一组密钥只构建一次。
func (c Config) Keyring() (*secretbox.Keyring, error) {
	digest := sha256.New()
	for _, key := range append([]string{c.DataEncryptionKey}, c.DataEncryptionKeysPrevious...) {
		digest.Write([]byte(strings.TrimSpace(key)))
		digest.Write([]byte{0})
	}
	cacheKey := string(digest.Sum(nil))
	if cached, ok := keyrings.Load(cacheKey); ok {
		return cached.(*secretbox.Keyring), nil
	}
	ring, err := secretbox.NewKeyring(c.DataEncryptionKey, c.DataEncryptionKeysPrevious...)
	if err != nil {
		return nil, err
	}
	actual, _ := keyrings.LoadOrStore(cacheKey, ring)
	return actual.(*secretbox.Keyring), nil
}

// SecurityWarnings 返回不阻止启动、但应在日志里提醒运维的配置问题。
// 示例配置里的 change-me-… 占位值长度合规，生产校验不会拦下；已用它加密的数据只能先把它
// 移到 DATA_ENCRYPTION_KEYS_PREVIOUS 再换新密钥，所以这里只提醒、不阻止启动。
func (c Config) SecurityWarnings() []string {
	var warnings []string
	if isPlaceholderSecret(c.DataEncryptionKey) {
		warnings = append(warnings, "DATA_ENCRYPTION_KEY uses a built-in or example placeholder value, so anyone with the source code can decrypt stored credentials. Set DATA_ENCRYPTION_KEY to a random value of at least 32 characters and move the current value to DATA_ENCRYPTION_KEYS_PREVIOUS; existing data is re-encrypted automatically at startup.")
	}
	for _, previous := range c.DataEncryptionKeysPrevious {
		if isPlaceholderSecret(previous) {
			warnings = append(warnings, "DATA_ENCRYPTION_KEYS_PREVIOUS contains a built-in or example placeholder value. Data encrypted with it stays readable to anyone with the source code until it is re-encrypted; remove the value once key rotation reports completion.")
			break
		}
	}
	if isPlaceholderSecret(c.JWTSecret) {
		warnings = append(warnings, "JWT_SECRET uses a built-in or example placeholder value, so anyone with the source code can forge session tokens. Set it to a random value of at least 32 characters; existing sessions will be signed out.")
	}
	return warnings
}

func splitCommaSeparated(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		result = append(result, value)
	}
	return result
}

// Runtime 提供线程安全的运行时配置快照读取。
type Runtime struct {
	value atomic.Value
}

// NewRuntime 创建运行时配置容器。
func NewRuntime(cfg Config) *Runtime {
	runtime := &Runtime{}
	runtime.value.Store(cfg)
	return runtime
}

// Snapshot 返回当前配置快照。
func (r *Runtime) Snapshot() Config {
	if r == nil {
		return Config{}
	}
	value := r.value.Load()
	if value == nil {
		return Config{}
	}
	item, ok := value.(Config)
	if !ok {
		return Config{}
	}
	return item
}

// Store 覆盖当前配置快照。
func (r *Runtime) Store(cfg Config) {
	if r == nil {
		return
	}
	r.value.Store(cfg)
}
