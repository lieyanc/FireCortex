package dockermgr

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRewriteImageWithMirror(t *testing.T) {
	tests := []struct {
		image  string
		mirror string
		want   string
	}{
		{"pgvector/pgvector:pg18-trixie", "", "pgvector/pgvector:pg18-trixie"},
		{"redis:7-alpine", "https://docker.m.daocloud.io/", "docker.m.daocloud.io/library/redis:7-alpine"},
		{"pgvector/pgvector:pg18-trixie", "docker.m.daocloud.io", "docker.m.daocloud.io/pgvector/pgvector:pg18-trixie"},
		{"docker.io/library/redis:7", "mirror.example.com", "mirror.example.com/library/redis:7"},
		{"docker.io/redis:7", "mirror.example.com", "mirror.example.com/library/redis:7"},
		{"ghcr.io/lieyanc/firecortex-rapidocr:latest", "mirror.example.com", "mirror.example.com/ghcr.io/lieyanc/firecortex-rapidocr:latest"},
		{"mirror.example.com/pgvector/pgvector:pg18-trixie", "mirror.example.com", "mirror.example.com/pgvector/pgvector:pg18-trixie"},
	}

	for _, tc := range tests {
		if got := RewriteImageWithMirror(tc.image, tc.mirror); got != tc.want {
			t.Errorf("RewriteImageWithMirror(%q, %q) = %q, want %q", tc.image, tc.mirror, got, tc.want)
		}
	}
}

func TestPostgresSpecLayoutByMajorVersion(t *testing.T) {
	hasEnv := func(env []string, want string) bool {
		for _, item := range env {
			if item == want {
				return true
			}
		}
		return false
	}
	hasEnvPrefix := func(env []string, prefix string) bool {
		for _, item := range env {
			if strings.HasPrefix(item, prefix) {
				return true
			}
		}
		return false
	}

	t.Run("default empty image uses pg18-trixie layout and applies mirror", func(t *testing.T) {
		spec := PostgresSpec(PostgresContainerConfig{
			RegistryMirror: "docker.m.daocloud.io",
			DataDir:        "/data/firecortex/postgres/pgdata",
			Password:       "secret",
		})
		if spec.Image != "docker.m.daocloud.io/pgvector/pgvector:pg18-trixie" {
			t.Fatalf("unexpected image %q", spec.Image)
		}
		if len(spec.Binds) != 1 || spec.Binds[0] != "/data/firecortex/postgres/pgdata:/var/lib/postgresql" {
			t.Fatalf("unexpected binds %+v", spec.Binds)
		}
		if hasEnvPrefix(spec.Env, "PGDATA=") {
			t.Fatalf("pg18 spec should not override PGDATA, got %+v", spec.Env)
		}
		if !hasEnv(spec.Env, "POSTGRES_INITDB_ARGS=--locale-provider=builtin --locale=C.UTF-8") {
			t.Fatalf("pg18 spec missing builtin initdb args: %+v", spec.Env)
		}
	})

	t.Run("pg17 keeps legacy mount and enables builtin initdb args", func(t *testing.T) {
		spec := PostgresSpec(PostgresContainerConfig{
			Image:    "pgvector/pgvector:pg17-bookworm",
			DataDir:  "/data/firecortex/postgres/pgdata",
			Password: "secret",
		})
		if len(spec.Binds) != 1 || spec.Binds[0] != "/data/firecortex/postgres/pgdata:/var/lib/postgresql/data" {
			t.Fatalf("unexpected binds %+v", spec.Binds)
		}
		if !hasEnv(spec.Env, "PGDATA=/var/lib/postgresql/data") {
			t.Fatalf("pg17 spec missing PGDATA: %+v", spec.Env)
		}
		if !hasEnv(spec.Env, "POSTGRES_INITDB_ARGS=--locale-provider=builtin --locale=C.UTF-8") {
			t.Fatalf("pg17 spec missing builtin initdb args: %+v", spec.Env)
		}
	})

	t.Run("pg16 keeps legacy mount without builtin initdb args", func(t *testing.T) {
		spec := PostgresSpec(PostgresContainerConfig{
			Image:    "pgvector/pgvector:pg16",
			DataDir:  "/data/firecortex/postgres/pgdata",
			Password: "secret",
		})
		if len(spec.Binds) != 1 || spec.Binds[0] != "/data/firecortex/postgres/pgdata:/var/lib/postgresql/data" {
			t.Fatalf("unexpected binds %+v", spec.Binds)
		}
		if !hasEnv(spec.Env, "PGDATA=/var/lib/postgresql/data") {
			t.Fatalf("pg16 spec missing PGDATA: %+v", spec.Env)
		}
		if hasEnvPrefix(spec.Env, "POSTGRES_INITDB_ARGS=") {
			t.Fatalf("pg16 spec must not set PG 17+ builtin initdb args: %+v", spec.Env)
		}
	})
}

func TestPathGuardBlocksProtectedAndUnrelatedDirs(t *testing.T) {
	for _, blocked := range []string{"/", "/etc", "/etc/firecortex", "/usr/local/firecortex", "/var", "/var/lib", "/home", "/root", "/proc/self"} {
		if _, err := ValidateHostDir(blocked); err == nil {
			t.Errorf("expected ValidateHostDir(%q) to fail", blocked)
		}
	}

	t.Run("symlink to protected dir is blocked", func(t *testing.T) {
		tmp := t.TempDir()
		link := filepath.Join(tmp, "etc-link")
		if err := os.Symlink("/etc", link); err != nil {
			t.Skipf("symlink not supported: %v", err)
		}
		if _, err := ValidateHostDir(filepath.Join(link, "firecortex")); err == nil {
			t.Fatal("expected symlink pointing under /etc to be rejected")
		}
	})

	t.Run("empty dir is valid", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "managed")
		res := InspectManagedRoot(root, "", "", "")
		if !res.Valid || res.Status != DirStatusEmpty {
			t.Fatalf("expected valid empty dir, got %+v", res)
		}
	})

	t.Run("non-empty pgdata without PG_VERSION is rejected", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "managed")
		pgDir := filepath.Join(root, "postgres", "pgdata")
		if err := os.MkdirAll(pgDir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pgDir, "random.txt"), []byte("hello"), 0o600); err != nil {
			t.Fatal(err)
		}
		res := InspectManagedRoot(root, "", "", "")
		if res.Valid || res.Status != DirStatusInvalid {
			t.Fatalf("expected invalid status for unrelated non-empty pgdata, got %+v", res)
		}
	})

	t.Run("existing PG_VERSION is recognized as adoptable", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "managed")
		pgDir := filepath.Join(root, "postgres", "pgdata")
		if err := os.MkdirAll(pgDir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pgDir, "PG_VERSION"), []byte("16\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		res := InspectManagedRoot(root, "", "", "")
		if !res.Valid || res.Status != DirStatusAdoptablePGData || res.PGVersion != "16" {
			t.Fatalf("expected adoptable pgdata v16, got %+v", res)
		}

		if err := PrepareManagedDirs(root, pgDir, filepath.Join(root, "redis"), filepath.Join(root, "postgres", "backups"), filepath.Join(root, "services")); err != nil {
			t.Fatalf("PrepareManagedDirs failed: %v", err)
		}
		after := InspectManagedRoot(root, "", "", "")
		if !after.Valid || after.Status != DirStatusManagedExisting || !after.HasManagedMarker {
			t.Fatalf("expected managed_existing after marker creation, got %+v", after)
		}
	})

	t.Run("pg18 versioned layout is recognized as adoptable and managed", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "managed")
		pgDir := filepath.Join(root, "postgres", "pgdata")
		clusterDir := filepath.Join(pgDir, "18", "docker")
		if err := os.MkdirAll(clusterDir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(clusterDir, "PG_VERSION"), []byte("18\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pgDir, ".psql_history"), []byte("\\dt\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		res := InspectManagedRoot(root, "", "", "")
		if !res.Valid || res.Status != DirStatusAdoptablePGData || res.PGVersion != "18" {
			t.Fatalf("expected adoptable pgdata v18, got %+v", res)
		}

		if err := PrepareManagedDirs(root, pgDir, filepath.Join(root, "redis"), filepath.Join(root, "postgres", "backups"), filepath.Join(root, "services")); err != nil {
			t.Fatalf("PrepareManagedDirs failed: %v", err)
		}
		after := InspectManagedRoot(root, "", "", "")
		if !after.Valid || after.Status != DirStatusManagedExisting || !after.HasManagedMarker || after.PGVersion != "18" {
			t.Fatalf("expected managed_existing v18 after marker creation, got %+v", after)
		}

		// Adding an unrelated sibling file must fail safety inspection.
		if err := os.WriteFile(filepath.Join(pgDir, "random.txt"), []byte("nope"), 0o600); err != nil {
			t.Fatal(err)
		}
		invalid := InspectManagedRoot(root, "", "", "")
		if invalid.Valid || invalid.Status != DirStatusInvalid {
			t.Fatalf("expected invalid status when unrelated file exists alongside pg18 dir, got %+v", invalid)
		}
	})

	for _, tc := range []struct {
		name, cluster, version string
	}{
		{"non-Docker cluster", "18/main", "18"},
		{"cluster used as mount root", ".", "18"},
		{"missing version file", "18/docker", ""},
		{"version differs from directory", "19/docker", "18"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			cluster := filepath.Join(root, "postgres", "pgdata", tc.cluster)
			if err := os.MkdirAll(cluster, 0o750); err != nil {
				t.Fatal(err)
			}
			if tc.version != "" {
				if err := os.WriteFile(filepath.Join(cluster, "PG_VERSION"), []byte(tc.version+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			res := InspectManagedRoot(root, "", "", "")
			if res.Valid || res.Status != DirStatusInvalid {
				t.Fatalf("expected invalid PGDATA at %s, got %+v", tc.cluster, res)
			}
		})
	}

	t.Run("pg18 unreadable 0700 docker subdir is still recognized", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root bypasses chmod 0000 permission checks")
		}
		root := filepath.Join(t.TempDir(), "managed")
		pgDir := filepath.Join(root, "postgres", "pgdata")
		clusterDir := filepath.Join(pgDir, "18", "docker")
		if err := os.MkdirAll(clusterDir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(clusterDir, "PG_VERSION"), []byte("18\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(clusterDir, 0o000); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.Chmod(clusterDir, 0o750) }()

		res := InspectManagedRoot(root, "", "", "")
		if !res.Valid || res.Status != DirStatusAdoptablePGData || res.PGVersion != "18" {
			t.Fatalf("expected adoptable pgdata v18 on EACCES docker subdir, got %+v", res)
		}
	})

	t.Run("PrepareManagedDirs makes pg18 mount root traversable without widening legacy pgdata", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "managed")
		pgDir := filepath.Join(root, "postgres", "pgdata")
		redisDir := filepath.Join(root, "redis")
		backupDir := filepath.Join(root, "postgres", "backups")
		servicesDir := filepath.Join(root, "services")

		// Pre-create pgDir with restrictive 0750 mode to simulate a prior run or strict umask.
		if err := os.MkdirAll(pgDir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(pgDir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := PrepareManagedDirs(root, pgDir, redisDir, backupDir, servicesDir); err != nil {
			t.Fatalf("PrepareManagedDirs failed: %v", err)
		}
		pgInfo, err := os.Stat(pgDir)
		if err != nil {
			t.Fatal(err)
		}
		if pgInfo.Mode().Perm()&0o005 != 0o005 {
			t.Fatalf("expected pgDir to be world read/traversable (o+rx), got %04o", pgInfo.Mode().Perm())
		}

		// Legacy PG <= 17 directory with direct PG_VERSION at 0700 must keep 0700.
		legacyRoot := filepath.Join(t.TempDir(), "managed-legacy")
		legacyPG := filepath.Join(legacyRoot, "postgres", "pgdata")
		if err := os.MkdirAll(legacyPG, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(legacyPG, "PG_VERSION"), []byte("16\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(legacyPG, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := PrepareManagedDirs(legacyRoot, legacyPG, "", "", ""); err != nil {
			t.Fatalf("PrepareManagedDirs legacy failed: %v", err)
		}
		legacyInfo, err := os.Stat(legacyPG)
		if err != nil {
			t.Fatal(err)
		}
		if legacyInfo.Mode().Perm() != 0o700 {
			t.Fatalf("expected legacy PGDATA dir mode 0700 preserved, got %04o", legacyInfo.Mode().Perm())
		}
	})
}

func TestSyncComposeFilesPermissionsAndContent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "managed")
	composePath, envPath, err := SyncComposeFiles(root, ComposeState{
		Network:        "firecortex-network",
		RegistryMirror: "docker.m.daocloud.io",
		Postgres: &PostgresContainerConfig{
			Image:         "pgvector/pgvector:pg18-trixie",
			Port:          5432,
			User:          "firecortex",
			Password:      "pg$ecret",
			Database:      "firecortex",
			DataDir:       filepath.Join(root, "postgres", "pgdata"),
			MemoryLimitMB: 1024,
		},
		Redis: &RedisContainerConfig{
			Image:    "redis:7-alpine",
			Port:     6379,
			Password: "rd-password",
			DataDir:  filepath.Join(root, "redis"),
		},
	})
	if err != nil {
		t.Fatalf("SyncComposeFiles failed: %v", err)
	}

	envInfo, err := os.Stat(envPath)
	if err != nil {
		t.Fatalf("stat .env: %v", err)
	}
	if envInfo.Mode().Perm() != 0o600 {
		t.Fatalf("expected .env mode 0600, got %o", envInfo.Mode().Perm())
	}
	envContent, _ := os.ReadFile(envPath)
	if !strings.Contains(string(envContent), "FIRECORTEX_POSTGRES_IMAGE=docker.m.daocloud.io/pgvector/pgvector:pg18-trixie") {
		t.Fatalf("unexpected .env image line:\n%s", envContent)
	}
	if !strings.Contains(string(envContent), "FIRECORTEX_POSTGRES_PASSWORD='pg$ecret'") {
		t.Fatalf("expected shell-safe escaped password in .env:\n%s", envContent)
	}

	composeContent, _ := os.ReadFile(composePath)
	for _, want := range []string{
		"container_name: firecortex-postgres",
		"container_name: firecortex-redis",
		`POSTGRES_INITDB_ARGS: "--locale-provider=builtin --locale=C.UTF-8"`,
		`"${FIRECORTEX_POSTGRES_DATA_DIR}:/var/lib/postgresql"`,
		`"127.0.0.1:${FIRECORTEX_POSTGRES_PORT}:5432"`,
		`"127.0.0.1:${FIRECORTEX_REDIS_PORT}:6379"`,
		"mem_limit: 1024m",
		"--appendonly",
		`com.firecortex.managed: "true"`,
	} {
		if !strings.Contains(string(composeContent), want) {
			t.Fatalf("compose file missing %q:\n%s", want, composeContent)
		}
	}
	if strings.Contains(string(composeContent), "PGDATA: /var/lib/postgresql/data") {
		t.Fatalf("pg18 compose file must not force legacy PGDATA:\n%s", composeContent)
	}

	t.Run("legacy pg16 compose preserves /var/lib/postgresql/data layout", func(t *testing.T) {
		legacyRoot := filepath.Join(t.TempDir(), "managed-pg16")
		legacyComposePath, _, err := SyncComposeFiles(legacyRoot, ComposeState{
			Network: "firecortex-network",
			Postgres: &PostgresContainerConfig{
				Image:    "pgvector/pgvector:pg16",
				Port:     5432,
				User:     "firecortex",
				Password: "secret",
				Database: "firecortex",
				DataDir:  filepath.Join(legacyRoot, "postgres", "pgdata"),
			},
		})
		if err != nil {
			t.Fatalf("SyncComposeFiles legacy pg16 failed: %v", err)
		}
		legacyContent, _ := os.ReadFile(legacyComposePath)
		for _, want := range []string{
			"PGDATA: /var/lib/postgresql/data",
			`"${FIRECORTEX_POSTGRES_DATA_DIR}:/var/lib/postgresql/data"`,
		} {
			if !strings.Contains(string(legacyContent), want) {
				t.Fatalf("legacy compose file missing %q:\n%s", want, legacyContent)
			}
		}
		if strings.Contains(string(legacyContent), "POSTGRES_INITDB_ARGS") {
			t.Fatalf("pg16 compose file must not include PG 17+ builtin initdb args:\n%s", legacyContent)
		}
	})
}

func TestHTTPClientCoreDockerOperations(t *testing.T) {
	networkCreated := false
	containerCreated := false
	containerStarted := false

	mux := http.NewServeMux()
	mux.HandleFunc("/_ping", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Api-Version", "1.47")
		_, _ = w.Write([]byte("OK"))
	})
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ServerVersion":     "27.3.1",
			"OSType":            "linux",
			"Architecture":      "x86_64",
			"MemTotal":          16 * 1024 * 1024 * 1024,
			"ContainersRunning": 2,
		})
	})
	mux.HandleFunc("/networks/firecortex-network", func(w http.ResponseWriter, r *http.Request) {
		if !networkCreated {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"Name": "firecortex-network"})
	})
	mux.HandleFunc("/networks/create", func(w http.ResponseWriter, r *http.Request) {
		networkCreated = true
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"Id": "net-1"})
	})
	mux.HandleFunc("/images/create", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"Downloading","id":"abc","progressDetail":{"current":50,"total":100}}` + "\n"))
		_, _ = w.Write([]byte(`{"status":"Pull complete","id":"abc"}` + "\n"))
	})
	mux.HandleFunc("/containers/create", func(w http.ResponseWriter, r *http.Request) {
		containerCreated = true
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"Id": "ctr-123"})
	})
	mux.HandleFunc("/containers/ctr-123/start", func(w http.ResponseWriter, r *http.Request) {
		containerStarted = true
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/containers/firecortex-postgres/json", func(w http.ResponseWriter, r *http.Request) {
		if !containerCreated {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Id":   "ctr-123",
			"Name": "/firecortex-postgres",
			"State": map[string]any{
				"Status":  "running",
				"Running": true,
				"Health":  map[string]any{"Status": "healthy"},
			},
			"Config": map[string]any{
				"Image":  "pgvector/pgvector:pg18-trixie",
				"Labels": map[string]string{ManagedLabelKey: "true", ServiceLabelKey: CoreServicePostgres},
			},
		})
	})
	mux.HandleFunc("/containers/firecortex-postgres/exec", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"Id": "exec-1"})
	})
	mux.HandleFunc("/exec/exec-1/start", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(makeDockerFrame(1, []byte("accepting connections\n")))
		_, _ = w.Write(makeDockerFrame(2, []byte("warn\n")))
	})
	mux.HandleFunc("/exec/exec-1/json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ExitCode": 0})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	ctx := context.Background()

	info, err := client.Ping(ctx)
	if err != nil || !info.Available || info.ServerVersion != "27.3.1" {
		t.Fatalf("Ping unexpected: info=%+v err=%v", info, err)
	}
	if err := client.EnsureNetwork(ctx, "firecortex-network"); err != nil || !networkCreated {
		t.Fatalf("EnsureNetwork failed: created=%v err=%v", networkCreated, err)
	}

	var lastPct int
	if err := client.PullImage(ctx, "pgvector/pgvector:pg18-trixie", func(p PullProgress) { lastPct = p.Percent }); err != nil {
		t.Fatalf("PullImage failed: %v", err)
	}
	if lastPct != 100 {
		t.Fatalf("expected pull percent 100, got %d", lastPct)
	}

	id, err := client.CreateContainer(ctx, PostgresSpec(PostgresContainerConfig{
		Image:    "pgvector/pgvector:pg18-trixie",
		Network:  "firecortex-network",
		Port:     5432,
		DataDir:  "/data/firecortex/postgres/pgdata",
		User:     "firecortex",
		Password: "secret",
		Database: "firecortex",
	}))
	if err != nil || id != "ctr-123" {
		t.Fatalf("CreateContainer failed: id=%q err=%v", id, err)
	}
	if err := client.StartContainer(ctx, id); err != nil || !containerStarted {
		t.Fatalf("StartContainer failed: started=%v err=%v", containerStarted, err)
	}
	inspected, err := client.InspectContainer(ctx, PostgresContainerName)
	if err != nil || inspected == nil || !inspected.Running || inspected.Health != "healthy" {
		t.Fatalf("InspectContainer unexpected: %+v err=%v", inspected, err)
	}
	execRes, err := client.Exec(ctx, PostgresContainerName, []string{"pg_isready"}, nil)
	if err != nil || execRes.ExitCode != 0 || execRes.Stdout != "accepting connections" || execRes.Stderr != "warn" {
		t.Fatalf("Exec unexpected: %+v err=%v", execRes, err)
	}
}

func makeDockerFrame(streamType byte, payload []byte) []byte {
	var buf bytes.Buffer
	header := make([]byte, 8)
	header[0] = streamType
	binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))
	buf.Write(header)
	buf.Write(payload)
	return buf.Bytes()
}

func TestExecWithStdinOverTLS(t *testing.T) {
	input := make(chan string, 1)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/proxy/containers/pg/exec":
			_, _ = io.WriteString(w, "{\"Id\":\"e\"}")
		case "/proxy/exec/e/start":
			if _, err := io.Copy(io.Discard, r.Body); err != nil {
				t.Error(err)
				return
			}
			conn, rw, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n")
			if err := rw.Flush(); err != nil {
				t.Error(err)
				return
			}
			body, err := io.ReadAll(rw)
			if err != nil {
				t.Error(err)
			}
			input <- string(body)
		case "/proxy/exec/e/json":
			_, _ = io.WriteString(w, "{\"ExitCode\":0}")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c, err := NewClient(srv.URL + "/proxy")
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	c.httpClient.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: pool}
	res, err := c.ExecWithStdin(t.Context(), "pg", []string{"pg_restore"}, nil, strings.NewReader("archive"))
	if err != nil || res.ExitCode != 0 {
		t.Fatal("TLS restore stream failed", err)
	}
	if got := <-input; got != "archive" {
		t.Fatalf("unexpected input %q", got)
	}
}
