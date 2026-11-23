package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNeedsSetupRules(t *testing.T) {
	t.Run("fresh empty directory needs setup without writing config", func(t *testing.T) {
		cleanupConfigEnv(t)
		dir := t.TempDir()
		chdir(t, dir)

		needs, path, err := NeedsSetup()
		if err != nil {
			t.Fatalf("NeedsSetup failed: %v", err)
		}
		if !needs {
			t.Fatal("expected NeedsSetup=true on fresh directory")
		}
		if path != DefaultConfigFile {
			t.Fatalf("expected path %q, got %q", DefaultConfigFile, path)
		}
		if _, statErr := os.Stat(filepath.Join(dir, DefaultConfigFile)); !os.IsNotExist(statErr) {
			t.Fatalf("NeedsSetup must not create config.json, statErr=%v", statErr)
		}
	})

	t.Run("existing legacy config without bootstrap key skips setup", func(t *testing.T) {
		cleanupConfigEnv(t)
		useConfigFile(t, `{"server": {"http_port": "8080"}}`)

		needs, _, err := NeedsSetup()
		if err != nil {
			t.Fatalf("NeedsSetup failed: %v", err)
		}
		if needs {
			t.Fatal("expected existing config without bootstrap key to skip setup")
		}
	})

	t.Run("explicit bootstrap completed false enters setup", func(t *testing.T) {
		cleanupConfigEnv(t)
		useConfigFile(t, `{"bootstrap": {"completed": false}}`)

		needs, _, err := NeedsSetup()
		if err != nil {
			t.Fatalf("NeedsSetup failed: %v", err)
		}
		if !needs {
			t.Fatal("expected explicit bootstrap.completed=false to enter setup")
		}
	})

	t.Run("explicit POSTGRES_DSN env skips setup on fresh directory", func(t *testing.T) {
		cleanupConfigEnv(t)
		chdir(t, t.TempDir())
		t.Setenv("POSTGRES_DSN", "postgres://user:pass@127.0.0.1:5432/firecortex?sslmode=disable")

		needs, _, err := NeedsSetup()
		if err != nil {
			t.Fatalf("NeedsSetup failed: %v", err)
		}
		if needs {
			t.Fatal("expected POSTGRES_DSN env var to bypass setup mode")
		}
	})

	t.Run("legacy config.yaml skips setup for migration", func(t *testing.T) {
		cleanupConfigEnv(t)
		dir := t.TempDir()
		chdir(t, dir)
		writeFile(t, filepath.Join(dir, "config.yaml"), "server:\n  http_port: \"8080\"\n")

		needs, _, err := NeedsSetup()
		if err != nil {
			t.Fatalf("NeedsSetup failed: %v", err)
		}
		if needs {
			t.Fatal("expected legacy config.yaml to bypass setup mode")
		}
	})
}

func TestPrepareInitializedConfigValidatesAndPersists(t *testing.T) {
	cleanupConfigEnv(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")

	// prod mode requires https public URLs; invalid input must not touch disk.
	_, err := PrepareInitializedConfig(cfgPath, InitializedConfigInput{
		Env:              "prod",
		HTTPPort:         "8080",
		PublicAPIBaseURL: "http://example.com",
		PublicWebBaseURL: "http://example.com",
		CORSAllowOrigin:  "http://example.com",
		PostgresDSN:      "host=127.0.0.1 user=firecortex password=secret dbname=firecortex port=5432 sslmode=disable",
		CacheDriver:      "redis",
		RedisAddr:        "127.0.0.1:6379",
		RedisPassword:    "redis-secret",
	})
	if err == nil {
		t.Fatal("expected prod validation error for non-https public URL")
	}
	if _, statErr := os.Stat(cfgPath); !os.IsNotExist(statErr) {
		t.Fatalf("failed PrepareInitializedConfig must not create config.json, statErr=%v", statErr)
	}

	dataRoot := filepath.Join(dir, "managed-root")
	written, err := PrepareInitializedConfig(cfgPath, InitializedConfigInput{
		Env:                    "prod",
		HTTPPort:               "8080",
		PublicAPIBaseURL:       "https://chat.example.com",
		PublicWebBaseURL:       "https://chat.example.com",
		CORSAllowOrigin:        "https://chat.example.com",
		PostgresDSN:            "host=127.0.0.1 user=firecortex password=secret dbname=firecortex port=5432 sslmode=disable",
		CacheDriver:            "redis",
		RedisAddr:              "127.0.0.1:6379",
		RedisPassword:          "redis-secret",
		ManagedNetwork:         "firecortex-network",
		ManagedDataDir:         dataRoot,
		ManagedComposeSync:     true,
		ManagedPostgresEnabled: true,
		ManagedPostgresImage:   DefaultManagedPostgresImage,
		ManagedPostgresPort:    5432,
		ManagedRedisEnabled:    true,
		ManagedRedisImage:      DefaultManagedRedisImage,
		ManagedRedisPort:       6379,
		ManagedBackupEnabled:   true,
	})
	if err != nil {
		t.Fatalf("PrepareInitializedConfig failed: %v", err)
	}
	if err := written.Save(); err != nil {
		t.Fatal(err)
	}
	if !written.BootstrapCompleted || !written.HasManagedCoreServices() {
		t.Fatalf("unexpected written config flags: bootstrap=%v managedCore=%v", written.BootstrapCompleted, written.HasManagedCoreServices())
	}
	if got := written.EffectiveManagedPostgresDataDir(); got != filepath.Join(dataRoot, "postgres", "pgdata") {
		t.Fatalf("unexpected pgdata dir: %q", got)
	}
	if got := written.EffectiveManagedRedisDataDir(); got != filepath.Join(dataRoot, "redis") {
		t.Fatalf("unexpected redis dir: %q", got)
	}
	if got := written.EffectiveManagedBackupDir(); got != filepath.Join(dataRoot, "postgres", "backups") {
		t.Fatalf("unexpected backup dir: %q", got)
	}

	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("expected config.json to exist: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected config.json mode 0600, got %o", info.Mode().Perm())
	}

	t.Setenv("CONFIG_FILE", cfgPath)
	needs, _, err := NeedsSetup()
	if err != nil || needs {
		t.Fatalf("expected NeedsSetup=false after initialization, needs=%v err=%v", needs, err)
	}
}
