package setup

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lieyanc/FireCortex/backend/internal/infra/config"
	"github.com/lieyanc/FireCortex/backend/internal/infra/dockermgr"
	managedports "github.com/lieyanc/FireCortex/backend/internal/ports/managedinfra"
)

type stubDockerClient struct {
	containers map[string]*managedports.ContainerInfo
	images     map[string]bool
}

func (s *stubDockerClient) Host() string { return "unix:///stub/docker.sock" }
func (s *stubDockerClient) Ping(context.Context) (managedports.DaemonInfo, error) {
	return managedports.DaemonInfo{Available: true, ServerVersion: "27.4.0"}, nil
}
func (s *stubDockerClient) EnsureNetwork(context.Context, string) error { return nil }
func (s *stubDockerClient) InspectImage(_ context.Context, img string) (bool, error) {
	return s.images[img], nil
}
func (s *stubDockerClient) PullImage(_ context.Context, img string, onProgress func(managedports.PullProgress)) error {
	if s.images == nil {
		s.images = map[string]bool{}
	}
	s.images[img] = true
	if onProgress != nil {
		onProgress(managedports.PullProgress{Status: "Pull complete", Percent: 100})
	}
	return nil
}
func (s *stubDockerClient) InspectContainer(_ context.Context, name string) (*managedports.ContainerInfo, error) {
	return s.containers[name], nil
}
func (s *stubDockerClient) CreateContainer(_ context.Context, spec managedports.ContainerSpec) (string, error) {
	if s.containers == nil {
		s.containers = map[string]*managedports.ContainerInfo{}
	}
	s.containers[spec.Name] = &managedports.ContainerInfo{ID: "id-" + spec.Name, Name: spec.Name, Image: spec.Image, Running: false}
	return "id-" + spec.Name, nil
}
func (s *stubDockerClient) StartContainer(_ context.Context, nameOrID string) error {
	for _, c := range s.containers {
		if c.ID == nameOrID || c.Name == nameOrID {
			c.Running = true
			c.Status = "running"
		}
	}
	return nil
}
func (s *stubDockerClient) StopContainer(context.Context, string, int) error    { return nil }
func (s *stubDockerClient) RestartContainer(context.Context, string, int) error { return nil }
func (s *stubDockerClient) RemoveContainer(context.Context, string, bool) error { return nil }
func (s *stubDockerClient) ContainerStats(context.Context, string) (managedports.ContainerStats, error) {
	return managedports.ContainerStats{}, nil
}
func (s *stubDockerClient) TailLogs(context.Context, string, int) (string, error) { return "", nil }
func (s *stubDockerClient) Exec(_ context.Context, _ string, cmd []string, _ []string) (managedports.ExecResult, error) {
	if len(cmd) > 0 && cmd[0] == "redis-cli" {
		return managedports.ExecResult{ExitCode: 0, Stdout: "PONG"}, nil
	}
	return managedports.ExecResult{ExitCode: 0, Stdout: "accepting connections"}, nil
}
func (s *stubDockerClient) ExecStreamStdout(_ context.Context, _ string, _ []string, _ []string, _ io.Writer) (string, int, error) {
	return "", 0, nil
}
func (s *stubDockerClient) ExecWithStdin(_ context.Context, _ string, _ []string, _ []string, _ io.Reader) (managedports.ExecResult, error) {
	return managedports.ExecResult{ExitCode: 0}, nil
}

type stubHostOps struct {
	*dockermgr.HostAdapter
	client managedports.DockerClient
}

func (s *stubHostOps) NewDockerClient(string) (managedports.DockerClient, error) {
	return s.client, nil
}

type stubSetupInfra struct{}

func (stubSetupInfra) ProbePostgres(context.Context, string) (managedports.PostgresProbeResult, error) {
	return managedports.PostgresProbeResult{ServerVersion: "16", VectorAvailable: true}, nil
}
func (stubSetupInfra) ProbeRedis(context.Context, managedports.RedisProbeInput) error { return nil }
func (stubSetupInfra) InitDBAndAdmin(context.Context, managedports.InitDBAdminInput) (bool, error) {
	return false, nil
}

func TestSetupServiceTokenAndApplyManagedFlow(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	managedDir := filepath.Join(dir, "managed")

	completedCh := make(chan struct{}, 1)
	stubClient := &stubDockerClient{containers: map[string]*managedports.ContainerInfo{}}
	svc, err := NewService(config.Config{
		HTTPPort:             "8080",
		ManagedNetwork:       "firecortex-network",
		ManagedDataDir:       managedDir,
		ManagedPostgresImage: "pgvector/pgvector:pg16",
		ManagedPostgresPort:  5432,
		ManagedRedisImage:    "redis:7-alpine",
		ManagedRedisPort:     6379,
	}, cfgPath, Dependencies{
		Host:  &stubHostOps{HostAdapter: dockermgr.NewHostOps(), client: stubClient},
		Infra: stubSetupInfra{},
	}, func() {
		completedCh <- struct{}{}
	})
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}
	defer svc.CleanupTokenFile()

	info, err := os.Stat(svc.TokenFilePath())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 token file, err=%v mode=%v", err, info)
	}

	if _, err := svc.VerifyToken("wrong-token"); err != ErrInvalidToken {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
	sessionID, err := svc.VerifyToken(svc.Token())
	if err != nil || sessionID == "" {
		t.Fatalf("VerifyToken failed: session=%q err=%v", sessionID, err)
	}
	if !svc.ValidateAuth(sessionID, "") || !svc.ValidateAuth("", svc.Token()) || svc.ValidateAuth("bad", "") {
		t.Fatal("ValidateAuth returned unexpected result")
	}

	if _, err := svc.StartApply(t.Context(), ApplyInput{
		DatabaseMode:  "managed",
		CacheMode:     "managed_redis",
		DataDir:       managedDir,
		Env:           "prod",
		PublicBaseURL: "http://127.0.0.1:8080",
		AdminUsername: "owner",
		AdminPassword: "password123",
	}); err == nil {
		t.Fatal("expected prod non-https URL to be rejected")
	}

	job, err := svc.StartApply(t.Context(), ApplyInput{
		DatabaseMode:  "managed",
		CacheMode:     "managed_redis",
		DataDir:       managedDir,
		ComposeSync:   true,
		Env:           "prod",
		PublicBaseURL: "https://chat.example.com",
		AdminUsername: "owner",
		AdminPassword: "password123",
	})
	if err != nil || job.JobID == "" {
		t.Fatalf("StartApply failed: job=%+v err=%v", job, err)
	}

	select {
	case <-completedCh:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for setup completion; current job=%+v", svc.CurrentJob())
	}

	finalJob := svc.CurrentJob()
	if finalJob.State != "done" || finalJob.ComposePath == "" {
		t.Fatalf("unexpected final job: %+v", finalJob)
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("expected config.json to be written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(managedDir, dockermgr.ComposeFileName)); err != nil {
		t.Fatalf("expected docker-compose.yml to be written: %v", err)
	}
	if _, err := os.Stat(svc.TokenFilePath()); !os.IsNotExist(err) {
		t.Fatalf("expected temporary setup token file to be cleaned up, err=%v", err)
	}
}
