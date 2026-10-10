package config

import (
	"bytes"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultConfigFile 是未设置 CONFIG_FILE 时读取的配置文件，相对进程工作目录。
const DefaultConfigFile = "config.json"

// configTemplate 是完整的默认配置：配置文件不存在时据此释放，已有文件缺少的键据此补全。
// 键顺序必须与 fileConfig 字段顺序一致（由测试保证）。
//
//go:embed template.json
var configTemplate []byte

// fileConfig 对应 config.json 的字段映射（仅保留基础设施与安全凭据）。
// yaml 标签仅用于把旧版 config.yaml 迁移为 config.json。
type fileConfig struct {
	App struct {
		Name string `json:"name" yaml:"name"`
		Env  string `json:"env" yaml:"env"`
	} `json:"app" yaml:"app"`
	Server struct {
		HTTPPort                 string `json:"http_port" yaml:"http_port"`
		CORSAllowOrigin          string `json:"cors_allow_origin" yaml:"cors_allow_origin"`
		TrustedProxies           string `json:"trusted_proxies" yaml:"trusted_proxies"`
		PublicAPIBaseURL         string `json:"public_api_base_url" yaml:"public_api_base_url"`
		PublicWebBaseURL         string `json:"public_web_base_url" yaml:"public_web_base_url"`
		FrontendDistDir          string `json:"frontend_dist_dir" yaml:"frontend_dist_dir"`
		ReadHeaderTimeoutSeconds int    `json:"read_header_timeout_seconds" yaml:"read_header_timeout_seconds"`
		ReadTimeoutSeconds       int    `json:"read_timeout_seconds" yaml:"read_timeout_seconds"`
		IdleTimeoutSeconds       int    `json:"idle_timeout_seconds" yaml:"idle_timeout_seconds"`
		MaxHeaderBytes           int    `json:"max_header_bytes" yaml:"max_header_bytes"`
		ShutdownTimeoutSeconds   int    `json:"shutdown_timeout_seconds" yaml:"shutdown_timeout_seconds"`
	} `json:"server" yaml:"server"`
	Security struct {
		JWTSecret              string `json:"jwt_secret" yaml:"jwt_secret"`
		MCPUserContextSecret   string `json:"mcp_user_context_secret" yaml:"mcp_user_context_secret"`
		DataEncryptionKey      string `json:"data_encryption_key" yaml:"data_encryption_key"`
		SSRFProtectionEnabled  bool   `json:"ssrf_protection_enabled" yaml:"ssrf_protection_enabled"`
		SSRFAllowedHosts       string `json:"ssrf_allowed_hosts" yaml:"ssrf_allowed_hosts"`
		SSRFAllowedCIDRs       string `json:"ssrf_allowed_cidrs" yaml:"ssrf_allowed_cidrs"`
		TurnstileSiteverifyURL string `json:"turnstile_siteverify_url" yaml:"turnstile_siteverify_url"`
	} `json:"security" yaml:"security"`
	Branding struct {
		Title                 string `json:"title" yaml:"title"`
		ShortName             string `json:"short_name" yaml:"short_name"`
		Description           string `json:"description" yaml:"description"`
		LogoURL               string `json:"logo_url" yaml:"logo_url"`
		FaviconURL            string `json:"favicon_url" yaml:"favicon_url"`
		PWAIcon192URL         string `json:"pwa_icon_192_url" yaml:"pwa_icon_192_url"`
		PWAIcon512URL         string `json:"pwa_icon_512_url" yaml:"pwa_icon_512_url"`
		PWAMaskableIcon512URL string `json:"pwa_maskable_icon_512_url" yaml:"pwa_maskable_icon_512_url"`
		AppleTouchIcon180URL  string `json:"apple_touch_icon_180_url" yaml:"apple_touch_icon_180_url"`
	} `json:"branding" yaml:"branding"`
	Database struct {
		Driver   string `json:"driver" yaml:"driver"`
		Postgres struct {
			DSN                string `json:"dsn" yaml:"dsn"`
			MaxOpenConns       int    `json:"max_open_conns" yaml:"max_open_conns"`
			MaxIdleConns       int    `json:"max_idle_conns" yaml:"max_idle_conns"`
			ConnMaxLifetimeMin int    `json:"conn_max_lifetime_minutes" yaml:"conn_max_lifetime_minutes"`
			ConnMaxIdleTimeMin int    `json:"conn_max_idle_time_minutes" yaml:"conn_max_idle_time_minutes"`
		} `json:"postgres" yaml:"postgres"`
		SQLite struct {
			Path          string `json:"path" yaml:"path"`
			DSN           string `json:"dsn" yaml:"dsn"`
			MaxOpenConns  int    `json:"max_open_conns" yaml:"max_open_conns"`
			BusyTimeoutMS int    `json:"busy_timeout_ms" yaml:"busy_timeout_ms"`
			CacheSizeKB   int    `json:"cache_size_kb" yaml:"cache_size_kb"`
			MmapSizeBytes int64  `json:"mmap_size_bytes" yaml:"mmap_size_bytes"`
			Synchronous   string `json:"synchronous" yaml:"synchronous"`
			TempStore     string `json:"temp_store" yaml:"temp_store"`
		} `json:"sqlite" yaml:"sqlite"`
		Redis struct {
			Addr                  string `json:"addr" yaml:"addr"`
			Username              string `json:"username" yaml:"username"`
			Password              string `json:"password" yaml:"password"`
			DB                    int    `json:"db" yaml:"db"`
			TLSEnabled            bool   `json:"tls_enabled" yaml:"tls_enabled"`
			TLSInsecureSkipVerify bool   `json:"tls_insecure_skip_verify" yaml:"tls_insecure_skip_verify"`
		} `json:"redis" yaml:"redis"`
	} `json:"database" yaml:"database"`
	Cache struct {
		Driver string `json:"driver" yaml:"driver"`
	} `json:"cache" yaml:"cache"`
	Storage struct {
		Backend string `json:"backend" yaml:"backend"`
		Local   struct {
			RootDir string `json:"root_dir" yaml:"root_dir"`
		} `json:"local" yaml:"local"`
		S3 struct {
			Endpoint        string `json:"endpoint" yaml:"endpoint"`
			Region          string `json:"region" yaml:"region"`
			Bucket          string `json:"bucket" yaml:"bucket"`
			Prefix          string `json:"prefix" yaml:"prefix"`
			AccessKeyID     string `json:"access_key_id" yaml:"access_key_id"`
			SecretAccessKey string `json:"secret_access_key" yaml:"secret_access_key"`
			ForcePathStyle  bool   `json:"force_path_style" yaml:"force_path_style"`
		} `json:"s3" yaml:"s3"`
	} `json:"storage" yaml:"storage"`
	GeoIP struct {
		Provider             string `json:"provider" yaml:"provider"`
		BaseURL              string `json:"base_url" yaml:"base_url"`
		Token                string `json:"token" yaml:"token"`
		TimeoutMS            int    `json:"timeout_ms" yaml:"timeout_ms"`
		DatabaseURL          string `json:"database_url" yaml:"database_url"`
		DatabasePath         string `json:"database_path" yaml:"database_path"`
		DatabaseMaxBytes     int64  `json:"database_max_bytes" yaml:"database_max_bytes"`
		RefreshIntervalHours int    `json:"refresh_interval_hours" yaml:"refresh_interval_hours"`
	} `json:"geoip" yaml:"geoip"`
	Observability struct {
		Tracing struct {
			// Enabled 为 null 时按 endpoint 是否配置自动决定。
			Enabled      *bool   `json:"enabled" yaml:"enabled"`
			Endpoint     string  `json:"endpoint" yaml:"endpoint"`
			Headers      string  `json:"headers" yaml:"headers"`
			Insecure     bool    `json:"insecure" yaml:"insecure"`
			Protocol     string  `json:"protocol" yaml:"protocol"`
			SamplingRate float64 `json:"sampling_rate" yaml:"sampling_rate"`
		} `json:"tracing" yaml:"tracing"`
	} `json:"observability" yaml:"observability"`
	Update struct {
		Enabled              bool   `json:"enabled" yaml:"enabled"`
		Channel              string `json:"channel" yaml:"channel"`
		CheckIntervalSeconds int    `json:"check_interval_seconds" yaml:"check_interval_seconds"`
		Source               string `json:"source" yaml:"source"`
		ProxyBaseURL         string `json:"proxy_base_url" yaml:"proxy_base_url"`
		Repo                 string `json:"repo" yaml:"repo"`
		DownloadDir          string `json:"download_dir" yaml:"download_dir"`
	} `json:"update" yaml:"update"`
}

// templateConfig 返回内置模板解析出的默认配置。
func templateConfig() fileConfig {
	var fc fileConfig
	if err := json.Unmarshal(configTemplate, &fc); err != nil {
		panic("config template: " + err.Error())
	}
	return fc
}

// resolveConfigFile 返回配置文件路径：CONFIG_FILE 优先，否则为工作目录下的 config.json。
// CONFIG_FILE 仍指向旧版 .yaml/.yml 时改用同目录同名的 .json，旧文件在首次启动时迁移。
func resolveConfigFile() string {
	path := strings.TrimSpace(os.Getenv("CONFIG_FILE"))
	if path == "" {
		return DefaultConfigFile
	}
	if isYAMLPath(path) {
		return strings.TrimSuffix(path, filepath.Ext(path)) + ".json"
	}
	return path
}

func isYAMLPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return true
	default:
		return false
	}
}

// loadFile 读取配置文件：不存在时从模板释放（有旧版 config.yaml 则迁移），
// 已存在时把模板中缺少的键补全写回。返回值以模板为底，缺省字段取模板默认值。
// 释放或补全时缺失的密钥键会生成随机值；显式写为空串的密钥保持原样。
func loadFile(path string) (fileConfig, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return releaseFile(path)
	}
	if err != nil {
		return fileConfig{}, fmt.Errorf("read config %s: %w", path, err)
	}

	want, err := encodeConfig(withGeneratedSecrets(templateConfig()))
	if err != nil {
		return fileConfig{}, err
	}
	completed, added, unknown, err := completeJSON(data, want)
	if err != nil {
		return fileConfig{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	fc := templateConfig()
	if err := json.Unmarshal(completed, &fc); err != nil {
		return fileConfig{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	for _, key := range unknown {
		fmt.Fprintf(os.Stderr, "warning: config %s: unknown key %q is ignored\n", path, key)
	}
	if len(added) > 0 {
		if err := writeConfigFile(path, completed); err != nil {
			return fileConfig{}, err
		}
		fmt.Fprintf(os.Stderr, "config: added missing keys to %s: %s\n", path, strings.Join(added, ", "))
	}
	return fc, nil
}

// releaseFile 在 path 写出完整配置并返回其内容。
// 同目录存在旧版 config.yaml/config.yml 时迁移其值（不生成密钥，以免旧数据无法解密）；
// 否则使用模板并生成随机密钥。
func releaseFile(path string) (fileConfig, error) {
	fc := templateConfig()
	legacy, err := readLegacyYAML(path, &fc)
	if err != nil {
		return fileConfig{}, err
	}
	if legacy == "" {
		fc = withGeneratedSecrets(fc)
	}

	data, err := encodeConfig(fc)
	if err != nil {
		return fileConfig{}, err
	}
	if err := writeConfigFile(path, data); err != nil {
		return fileConfig{}, err
	}
	if legacy != "" {
		fmt.Fprintf(os.Stderr, "config: migrated %s to %s; the YAML file is no longer read and can be removed\n", legacy, path)
	} else {
		fmt.Fprintf(os.Stderr, "config: created %s from the built-in template; review it before exposing the service\n", path)
	}
	return fc, nil
}

// withGeneratedSecrets 为空的 JWT 密钥与数据加密密钥填入随机值。
func withGeneratedSecrets(fc fileConfig) fileConfig {
	if fc.Security.JWTSecret == "" {
		fc.Security.JWTSecret = randomSecret()
	}
	if fc.Security.DataEncryptionKey == "" {
		fc.Security.DataEncryptionKey = randomSecret()
	}
	return fc
}

// encodeConfig 按模板格式（两空格缩进、不转义 HTML）序列化配置。
func encodeConfig(fc fileConfig) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(fc); err != nil {
		return nil, fmt.Errorf("encode config: %w", err)
	}
	return buf.Bytes(), nil
}

// readLegacyYAML 把与 path 同目录同名的旧版 YAML 配置解析到 fc，返回其路径；不存在时返回空串。
func readLegacyYAML(path string, fc *fileConfig) (string, error) {
	base := strings.TrimSuffix(path, filepath.Ext(path))
	for _, candidate := range []string{base + ".yaml", base + ".yml"} {
		data, err := os.ReadFile(candidate)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("read legacy config %s: %w", candidate, err)
		}
		if err := yaml.Unmarshal(data, fc); err != nil {
			return "", fmt.Errorf("parse legacy config %s: %w", candidate, err)
		}
		return candidate, nil
	}
	return "", nil
}

func randomSecret() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic("config: generate secret: " + err.Error())
	}
	return hex.EncodeToString(buf)
}

// writeConfigFile 原子写入配置文件；文件含密钥，权限为 0600。
func writeConfigFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".config-*.json")
	if err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write config %s: %w", path, err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("write config %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	return nil
}

// completeJSON 把 want 中 have 缺少（或为 null）的键补入 have，保留 have 原有键序与取值，
// 新键追加在所属对象末尾。返回缩进后的结果、补入的键路径与 want 中不存在的未知键路径。
func completeJSON(have, want []byte) (out []byte, added, unknown []string, err error) {
	merged, err := completeValue(have, want, "", &added, &unknown)
	if err != nil {
		return nil, nil, nil, err
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, merged, "", "  "); err != nil {
		return nil, nil, nil, err
	}
	buf.WriteByte('\n')
	return buf.Bytes(), added, unknown, nil
}

func completeValue(have, want json.RawMessage, prefix string, added, unknown *[]string) (json.RawMessage, error) {
	if isJSONNull(have) && !isJSONNull(want) {
		*added = append(*added, prefix)
		return compactJSON(want)
	}
	haveObject, err := parseJSONObject(have)
	if err != nil {
		return nil, err
	}
	wantObject, err := parseJSONObject(want)
	if err != nil {
		return nil, err
	}
	if haveObject == nil || wantObject == nil {
		return compactJSON(have)
	}
	for _, key := range haveObject.keys {
		if _, ok := wantObject.values[key]; !ok {
			*unknown = append(*unknown, joinKey(prefix, key))
		}
	}
	for _, key := range wantObject.keys {
		path := joinKey(prefix, key)
		current, ok := haveObject.values[key]
		if !ok {
			haveObject.keys = append(haveObject.keys, key)
			haveObject.values[key] = wantObject.values[key]
			*added = append(*added, path)
			continue
		}
		merged, err := completeValue(current, wantObject.values[key], path, added, unknown)
		if err != nil {
			return nil, err
		}
		haveObject.values[key] = merged
	}
	return haveObject.marshal()
}

func joinKey(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func compactJSON(raw json.RawMessage) (json.RawMessage, error) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// jsonObject 是保留键序的 JSON 对象。
type jsonObject struct {
	keys   []string
	values map[string]json.RawMessage
}

// parseJSONObject 解析 JSON 对象；raw 不是对象时返回 nil。
func parseJSONObject(raw json.RawMessage) (*jsonObject, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	object := &jsonObject{values: map[string]json.RawMessage{}}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("unexpected object key %v", token)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		if _, exists := object.values[key]; !exists {
			object.keys = append(object.keys, key)
		}
		object.values[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("unexpected data after top-level object")
	}
	return object, nil
}

func (o *jsonObject) marshal() (json.RawMessage, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, key := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		encodedKey, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		buf.Write(encodedKey)
		buf.WriteByte(':')
		value, err := compactJSON(o.values[key])
		if err != nil {
			return nil, err
		}
		buf.Write(value)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
