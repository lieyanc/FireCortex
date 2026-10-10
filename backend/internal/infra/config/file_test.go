package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTemplateMatchesFileConfig(t *testing.T) {
	// 模板必须覆盖 fileConfig 的全部字段且键序一致，释放出的文件才与模板逐字相同。
	encoded, err := encodeConfig(templateConfig())
	if err != nil {
		t.Fatalf("encode template config: %v", err)
	}
	if string(encoded) != string(configTemplate) {
		t.Fatalf("template.json is out of sync with fileConfig; want:\n%s", encoded)
	}
}

func TestLoadReleasesTemplateWithGeneratedSecrets(t *testing.T) {
	cleanupConfigEnv(t)
	dir := t.TempDir()
	chdir(t, dir)

	cfg := mustLoad(t)
	path := filepath.Join(dir, DefaultConfigFile)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected config to be released: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected config mode 0600, got %o", perm)
	}
	if cfg.ConfigFile != DefaultConfigFile {
		t.Fatalf("expected ConfigFile %q, got %q", DefaultConfigFile, cfg.ConfigFile)
	}
	if len(cfg.JWTSecret) != 64 || len(cfg.DataEncryptionKey) != 64 || cfg.JWTSecret == cfg.DataEncryptionKey {
		t.Fatalf("expected distinct generated secrets, got jwt=%q key=%q", cfg.JWTSecret, cfg.DataEncryptionKey)
	}

	released := readFile(t, path)
	var fc fileConfig
	if err := json.Unmarshal(released, &fc); err != nil {
		t.Fatalf("parse released config: %v", err)
	}
	if fc.Security.JWTSecret != cfg.JWTSecret || fc.Security.DataEncryptionKey != cfg.DataEncryptionKey {
		t.Fatal("generated secrets were not persisted")
	}
	fc.Security.JWTSecret, fc.Security.DataEncryptionKey = "", ""
	if fc != templateConfig() {
		t.Fatalf("released config differs from template beyond secrets:\n%s", released)
	}

	again := mustLoad(t)
	if again.JWTSecret != cfg.JWTSecret {
		t.Fatal("second load regenerated secrets")
	}
	if !bytes.Equal(readFile(t, path), released) {
		t.Fatal("second load rewrote a complete config")
	}
}

func TestLoadCompletesMissingKeysAndKeepsExistingValues(t *testing.T) {
	cleanupConfigEnv(t)
	path := useConfigFile(t, `{
  "update": {"channel": "dev"},
  "custom": {"keep": true},
  "server": {"http_port": "9090", "trusted_proxies": null},
  "security": {"jwt_secret": "file-jwt-secret"}
}`)

	cfg := mustLoad(t)
	if cfg.HTTPPort != "9090" || cfg.UpdateChannel != "dev" || cfg.JWTSecret != "file-jwt-secret" {
		t.Fatalf("existing values not honored: port=%q channel=%q", cfg.HTTPPort, cfg.UpdateChannel)
	}
	if cfg.HTTPReadTimeoutSeconds != 120 || cfg.DatabaseDriver != "postgres" {
		t.Fatalf("missing values should fall back to template, got read timeout %d driver %q", cfg.HTTPReadTimeoutSeconds, cfg.DatabaseDriver)
	}
	if len(cfg.DataEncryptionKey) != 64 {
		t.Fatalf("completion should generate a missing secret, got %q", cfg.DataEncryptionKey)
	}

	completed := readFile(t, path)
	var raw map[string]any
	if err := json.Unmarshal(completed, &raw); err != nil {
		t.Fatalf("parse completed config: %v", err)
	}
	if _, ok := raw["custom"]; !ok {
		t.Fatal("unknown keys must be preserved")
	}
	server := raw["server"].(map[string]any)
	if server["http_port"] != "9090" || server["read_timeout_seconds"] != float64(120) || server["trusted_proxies"] != "" {
		t.Fatalf("unexpected completed server section: %v", server)
	}
	if raw["security"].(map[string]any)["data_encryption_key"] != cfg.DataEncryptionKey {
		t.Fatal("generated secret was not persisted")
	}
	if raw["update"].(map[string]any)["channel"] != "dev" {
		t.Fatal("existing nested value was overwritten")
	}
	text := string(completed)
	if strings.Index(text, `"update"`) > strings.Index(text, `"app"`) {
		t.Fatal("existing keys should keep their order, with new keys appended")
	}

	if _, added, _, err := completeJSON(completed, configTemplate); err != nil || len(added) != 0 {
		t.Fatalf("completed config should be complete, added=%v err=%v", added, err)
	}
}

func TestLoadKeepsExplicitlyEmptySecrets(t *testing.T) {
	cleanupConfigEnv(t)
	useConfigFile(t, `{"security": {"jwt_secret": "", "data_encryption_key": ""}}`)

	cfg := mustLoad(t)
	if cfg.JWTSecret != defaultJWTSecret || cfg.DataEncryptionKey != defaultDataEncryptionKey {
		t.Fatalf("explicitly empty secrets should fall back to dev placeholders, got jwt=%q key=%q", cfg.JWTSecret, cfg.DataEncryptionKey)
	}
}

func TestLoadRejectsInvalidJSON(t *testing.T) {
	cleanupConfigEnv(t)
	path := useConfigFile(t, `{"server": `)
	if _, err := Load(); err == nil {
		t.Fatal("expected parse error")
	}
	if got := string(readFile(t, path)); got != `{"server": ` {
		t.Fatalf("invalid config must not be rewritten, got %q", got)
	}
}

func TestLoadMigratesLegacyYAML(t *testing.T) {
	cleanupConfigEnv(t)
	dir := t.TempDir()
	chdir(t, dir)
	writeFile(t, filepath.Join(dir, "config.yaml"), `
app:
  env: dev
server:
  http_port: "7070"
security:
  jwt_secret: legacy-jwt-secret
  ssrf_protection_enabled: true
storage:
  s3:
    force_path_style: false
`)

	cfg := mustLoad(t)
	if cfg.Env != "dev" || cfg.HTTPPort != "7070" || cfg.JWTSecret != "legacy-jwt-secret" || !cfg.SSRFProtectionEnabled {
		t.Fatalf("legacy values not migrated: %+v", cfg)
	}
	if cfg.StorageS3ForcePathStyle {
		t.Fatal("legacy false boolean should override the template default")
	}
	if cfg.DataEncryptionKey != defaultDataEncryptionKey {
		t.Fatal("migration must keep an unset legacy secret unset instead of generating one")
	}
	var fc fileConfig
	if err := json.Unmarshal(readFile(t, filepath.Join(dir, "config.json")), &fc); err != nil {
		t.Fatalf("parse migrated config: %v", err)
	}
	if fc.Server.HTTPPort != "7070" || fc.Server.ReadTimeoutSeconds != 120 {
		t.Fatalf("migrated file should merge legacy values over the template: %+v", fc.Server)
	}
}

func TestLoadMapsLegacyYAMLConfigFileEnv(t *testing.T) {
	cleanupConfigEnv(t)
	dir := t.TempDir()
	chdir(t, t.TempDir())
	legacy := filepath.Join(dir, "firecortex.yml")
	writeFile(t, legacy, "server:\n  http_port: \"6060\"\n")
	t.Setenv("CONFIG_FILE", legacy)

	cfg := mustLoad(t)
	want := filepath.Join(dir, "firecortex.json")
	if cfg.ConfigFile != want || cfg.HTTPPort != "6060" {
		t.Fatalf("expected %s with migrated port, got %s port %q", want, cfg.ConfigFile, cfg.HTTPPort)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected migrated config: %v", err)
	}
}

func TestRepositoryConfigExamplesLoad(t *testing.T) {
	examples, err := filepath.Glob("../../../../config*.example.json")
	if err != nil || len(examples) == 0 {
		t.Fatalf("expected config examples at the repository root, err=%v", err)
	}
	for _, example := range examples {
		t.Run(filepath.Base(example), func(t *testing.T) {
			cleanupConfigEnv(t)
			data := readFile(t, example)
			if _, _, unknown, err := completeJSON(data, configTemplate); err != nil || len(unknown) > 0 {
				t.Fatalf("example must only use template keys, unknown=%v err=%v", unknown, err)
			}
			useConfigFile(t, string(data))
			cfg := mustLoad(t)
			if err := cfg.Validate(); err != nil {
				t.Fatalf("example config should validate: %v", err)
			}
		})
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}
