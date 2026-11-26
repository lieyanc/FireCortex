package managedinfra

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	managedports "github.com/lieyanc/FireCortex/backend/internal/ports/managedinfra"
)

func TestBuildAndParsePostgresDSN(t *testing.T) {
	dsn := BuildPostgresDSN("127.0.0.1", 5433, "fc_user", "p@ss/word#1", "fc_db")
	user, pass, db := ParsePostgresDSN(dsn)
	if user != "fc_user" || pass != "p@ss/word#1" || db != "fc_db" {
		t.Fatalf("round-trip DSN mismatch: user=%q pass=%q db=%q (dsn=%q)", user, pass, db, dsn)
	}

	kvUser, kvPass, kvDB := ParsePostgresDSN("host=127.0.0.1 user=firecortex password='my secret' dbname=appdb port=5432 sslmode=disable")
	if kvUser != "firecortex" || kvPass != "my secret" || kvDB != "appdb" {
		t.Fatalf("kv DSN parse mismatch: user=%q pass=%q db=%q", kvUser, kvPass, kvDB)
	}
}

type fakeDockerClient struct {
	restoreResult  managedports.ExecResult
	networkEnsured bool
	imagePresent   bool
	pulledImages   []string
	containers     map[string]*managedports.ContainerInfo
	started        []string
	restarted      []string
	execErr        error
	tailLogs       string
}

func (f *fakeDockerClient) Host() string { return "unix:///fake/docker.sock" }
func (f *fakeDockerClient) Ping(context.Context) (managedports.DaemonInfo, error) {
	return managedports.DaemonInfo{Available: true, ServerVersion: "27.0.0"}, nil
}
func (f *fakeDockerClient) EnsureNetwork(context.Context, string) error {
	f.networkEnsured = true
	return nil
}
func (f *fakeDockerClient) InspectImage(context.Context, string) (bool, error) {
	return f.imagePresent, nil
}
func (f *fakeDockerClient) PullImage(_ context.Context, image string, onProgress func(managedports.PullProgress)) error {
	f.pulledImages = append(f.pulledImages, image)
	if onProgress != nil {
		onProgress(managedports.PullProgress{Status: "Pull complete", Percent: 100})
	}
	f.imagePresent = true
	return nil
}
func (f *fakeDockerClient) InspectContainer(_ context.Context, name string) (*managedports.ContainerInfo, error) {
	return f.containers[name], nil
}
func (f *fakeDockerClient) CreateContainer(_ context.Context, spec managedports.ContainerSpec) (string, error) {
	if f.containers == nil {
		f.containers = map[string]*managedports.ContainerInfo{}
	}
	f.containers[spec.Name] = &managedports.ContainerInfo{
		ID:      "id-" + spec.Name,
		Name:    spec.Name,
		Image:   spec.Image,
		Status:  "created",
		Running: false,
	}
	return "id-" + spec.Name, nil
}
func (f *fakeDockerClient) StartContainer(_ context.Context, nameOrID string) error {
	f.started = append(f.started, nameOrID)
	for _, c := range f.containers {
		if c.ID == nameOrID || c.Name == nameOrID {
			c.Running = true
			c.Status = "running"
		}
	}
	return nil
}
func (f *fakeDockerClient) StopContainer(context.Context, string, int) error { return nil }
func (f *fakeDockerClient) RestartContainer(_ context.Context, nameOrID string, _ int) error {
	f.restarted = append(f.restarted, nameOrID)
	for _, c := range f.containers {
		if c.ID == nameOrID || c.Name == nameOrID {
			c.Running = true
			c.Status = "running"
		}
	}
	return nil
}
func (f *fakeDockerClient) RemoveContainer(context.Context, string, bool) error { return nil }
func (f *fakeDockerClient) ContainerStats(context.Context, string) (managedports.ContainerStats, error) {
	return managedports.ContainerStats{}, nil
}
func (f *fakeDockerClient) TailLogs(context.Context, string, int) (string, error) {
	return f.tailLogs, nil
}
func (f *fakeDockerClient) Exec(_ context.Context, _ string, cmd []string, _ []string) (managedports.ExecResult, error) {
	if f.execErr != nil {
		return managedports.ExecResult{}, f.execErr
	}
	if len(cmd) > 0 && cmd[0] == "redis-cli" {
		return managedports.ExecResult{ExitCode: 0, Stdout: "PONG"}, nil
	}
	return managedports.ExecResult{ExitCode: 0, Stdout: "accepting connections"}, nil
}
func (f *fakeDockerClient) ExecStreamStdout(_ context.Context, _ string, _ []string, _ []string, stdout io.Writer) (string, int, error) {
	if stdout != nil {
		_, _ = stdout.Write([]byte("PGDMP-TEST-DATA"))
	}
	return "", 0, nil
}
func (f *fakeDockerClient) ExecWithStdin(_ context.Context, _ string, _ []string, _ []string, stdin io.Reader) (managedports.ExecResult, error) {
	if stdin != nil {
		_, _ = io.Copy(io.Discard, stdin)
	}
	return f.restoreResult, nil
}

func TestEnsurePostgresAndRedisLifecycle(t *testing.T) {
	fake := &fakeDockerClient{containers: map[string]*managedports.ContainerInfo{}}

	err := EnsurePostgres(context.Background(), fake, managedports.ContainerSpec{
		Name:    managedports.PostgresContainerName,
		Image:   "pgvector/pgvector:pg18-trixie",
		Network: "firecortex-network",
	}, "firecortex", "firecortex", nil)
	if err != nil {
		t.Fatalf("EnsurePostgres failed: %v", err)
	}
	if !fake.networkEnsured || len(fake.pulledImages) != 1 || len(fake.started) != 1 {
		t.Fatalf("unexpected docker calls for postgres: network=%v pulled=%v started=%v", fake.networkEnsured, fake.pulledImages, fake.started)
	}

	fake.imagePresent = false
	err = EnsureRedis(context.Background(), fake, managedports.ContainerSpec{
		Name:    managedports.RedisContainerName,
		Image:   "redis:7-alpine",
		Network: "firecortex-network",
	}, "secret", nil)
	if err != nil {
		t.Fatalf("EnsureRedis failed: %v", err)
	}
	if len(fake.pulledImages) != 2 || len(fake.started) != 2 {
		t.Fatalf("unexpected docker calls for redis: pulled=%v started=%v", fake.pulledImages, fake.started)
	}
}

func TestEnsurePostgresRestartsCrashLoopingContainerAndSurfacesLogs(t *testing.T) {
	origPoll := pgReadyPollInterval
	origGrace := containerCrashLoopGrace
	pgReadyPollInterval = 5 * time.Millisecond
	containerCrashLoopGrace = 15 * time.Millisecond
	defer func() {
		pgReadyPollInterval = origPoll
		containerCrashLoopGrace = origGrace
	}()

	t.Run("restarting existing container triggers immediate restart", func(t *testing.T) {
		fake := &fakeDockerClient{
			containers: map[string]*managedports.ContainerInfo{
				managedports.PostgresContainerName: {
					ID:      "pg-restarting",
					Name:    managedports.PostgresContainerName,
					Status:  "restarting",
					Running: true,
				},
			},
		}
		err := EnsurePostgres(context.Background(), fake, managedports.ContainerSpec{
			Name:    managedports.PostgresContainerName,
			Image:   "pgvector/pgvector:pg18-trixie",
			Network: "firecortex-network",
		}, "firecortex", "firecortex", nil)
		if err != nil {
			t.Fatalf("EnsurePostgres failed: %v", err)
		}
		if len(fake.restarted) != 1 || fake.restarted[0] != "pg-restarting" {
			t.Fatalf("expected RestartContainer to be called for restarting container, got %v", fake.restarted)
		}
	})

	t.Run("crash-looping container fails fast with deduplicated recent logs", func(t *testing.T) {
		fake := &fakeDockerClient{
			containers: map[string]*managedports.ContainerInfo{
				managedports.PostgresContainerName: {
					ID:      "pg-loop",
					Name:    managedports.PostgresContainerName,
					Status:  "restarting",
					Running: true,
				},
			},
			execErr: errors.New("Container pg-loop is restarting, wait until the container is running"),
			tailLogs: "2026-11-26T10:52:58.100Z mkdir: cannot create directory '/var/lib/postgresql': Permission denied\n" +
				"2026-11-26T10:52:59.200Z mkdir: cannot create directory '/var/lib/postgresql': Permission denied\n",
		}
		err := waitForPostgresReady(context.Background(), fake, managedports.PostgresContainerName, "firecortex", "firecortex", 5*time.Second)
		if err == nil {
			t.Fatal("expected crash-loop error, got nil")
		}
		if !strings.Contains(err.Error(), "crash-looping") {
			t.Fatalf("expected crash-looping reason, got %v", err)
		}
		wantLog := "mkdir: cannot create directory '/var/lib/postgresql': Permission denied"
		if strings.Count(err.Error(), wantLog) != 1 {
			t.Fatalf("expected deduplicated recent log line once, got %v", err)
		}
	})
}
