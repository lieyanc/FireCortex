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
		{"pgvector/pgvector:pg16", "", "pgvector/pgvector:pg16"},
		{"redis:7-alpine", "https://docker.m.daocloud.io/", "docker.m.daocloud.io/library/redis:7-alpine"},
		{"pgvector/pgvector:pg16", "docker.m.daocloud.io", "docker.m.daocloud.io/pgvector/pgvector:pg16"},
		{"docker.io/library/redis:7", "mirror.example.com", "mirror.example.com/library/redis:7"},
		{"docker.io/redis:7", "mirror.example.com", "mirror.example.com/library/redis:7"},
		{"ghcr.io/lieyanc/firecortex-rapidocr:latest", "mirror.example.com", "mirror.example.com/ghcr.io/lieyanc/firecortex-rapidocr:latest"},
		{"mirror.example.com/pgvector/pgvector:pg16", "mirror.example.com", "mirror.example.com/pgvector/pgvector:pg16"},
	}

	for _, tc := range tests {
		if got := RewriteImageWithMirror(tc.image, tc.mirror); got != tc.want {
			t.Errorf("RewriteImageWithMirror(%q, %q) = %q, want %q", tc.image, tc.mirror, got, tc.want)
		}
	}
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
}

func TestSyncComposeFilesPermissionsAndContent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "managed")
	composePath, envPath, err := SyncComposeFiles(root, ComposeState{
		Network:        "firecortex-network",
		RegistryMirror: "docker.m.daocloud.io",
		Postgres: &PostgresContainerConfig{
			Image:         "pgvector/pgvector:pg16",
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
	if !strings.Contains(string(envContent), "FIRECORTEX_POSTGRES_IMAGE=docker.m.daocloud.io/pgvector/pgvector:pg16") {
		t.Fatalf("unexpected .env image line:\n%s", envContent)
	}
	if !strings.Contains(string(envContent), "FIRECORTEX_POSTGRES_PASSWORD='pg$ecret'") {
		t.Fatalf("expected shell-safe escaped password in .env:\n%s", envContent)
	}

	composeContent, _ := os.ReadFile(composePath)
	for _, want := range []string{
		"container_name: firecortex-postgres",
		"container_name: firecortex-redis",
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
				"Image":  "pgvector/pgvector:pg16",
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
	if err := client.PullImage(ctx, "pgvector/pgvector:pg16", func(p PullProgress) { lastPct = p.Percent }); err != nil {
		t.Fatalf("PullImage failed: %v", err)
	}
	if lastPct != 100 {
		t.Fatalf("expected pull percent 100, got %d", lastPct)
	}

	id, err := client.CreateContainer(ctx, PostgresSpec(PostgresContainerConfig{
		Image:    "pgvector/pgvector:pg16",
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
