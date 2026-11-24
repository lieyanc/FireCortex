package managedinfra

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lieyanc/FireCortex/backend/internal/infra/config"
	managedports "github.com/lieyanc/FireCortex/backend/internal/ports/managedinfra"
)

type stubManagerHostOps struct {
	client managedports.DockerClient
	synced int
}

func (s *stubManagerHostOps) NewDockerClient(string) (managedports.DockerClient, error) {
	return s.client, nil
}
func (s *stubManagerHostOps) InspectManagedRoot(rawRoot, _, _, _ string) managedports.DirInspection {
	return managedports.DirInspection{ResolvedRoot: rawRoot, Valid: true, Status: managedports.DirStatusManagedExisting}
}
func (s *stubManagerHostOps) PrepareManagedDirs(rootDir, pgDir, redisDir, backupDir, servicesDir string) error {
	for _, d := range []string{rootDir, pgDir, redisDir, backupDir, servicesDir} {
		if d != "" {
			if err := os.MkdirAll(d, 0o750); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *stubManagerHostOps) SyncComposeFiles(rootDir string, _ managedports.ComposeState) (string, string, error) {
	s.synced++
	return filepath.Join(rootDir, "docker-compose.yml"), filepath.Join(rootDir, ".env"), nil
}
func (s *stubManagerHostOps) ReadComposeFile(string) (string, error) {
	return "services:\n  postgres: {}\n", nil
}
func (s *stubManagerHostOps) ReadExistingComposeEnv(string) map[string]string {
	return map[string]string{}
}
func (s *stubManagerHostOps) PostgresSpec(cfg managedports.PostgresContainerConfig) managedports.ContainerSpec {
	return managedports.ContainerSpec{Name: managedports.PostgresContainerName, Image: cfg.Image, Network: cfg.Network}
}
func (s *stubManagerHostOps) RedisSpec(cfg managedports.RedisContainerConfig) managedports.ContainerSpec {
	return managedports.ContainerSpec{Name: managedports.RedisContainerName, Image: cfg.Image, Network: cfg.Network}
}
func (s *stubManagerHostOps) SidecarSpec(cfg managedports.SidecarContainerConfig) managedports.ContainerSpec {
	return managedports.ContainerSpec{Name: cfg.ContainerName, Image: cfg.Image, Network: cfg.Network}
}

type stubSecurity struct {
	validPass string
	audits    []string
}

func (s *stubSecurity) VerifySuperAdminPassword(_ context.Context, _ uint, pass string) error {
	if pass != s.validPass {
		return errors.New("invalid current password")
	}
	return nil
}
func (s *stubSecurity) RecordAudit(_ context.Context, _ uint, action, _, _ string, _ any) {
	s.audits = append(s.audits, action)
}

type stubSettingsSync struct {
	applied map[string]string
}

func (s *stubSettingsSync) ApplyExtractionManagedURL(_ context.Context, serviceID, baseURL string) error {
	if s.applied == nil {
		s.applied = map[string]string{}
	}
	s.applied[serviceID] = baseURL
	return nil
}

func TestManagerBackupCreateListRestoreAndCronMatch(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{
		Env:                         "dev",
		DatabaseDriver:              "postgres",
		PostgresDSN:                 "postgres://firecortex:secret@127.0.0.1:5432/firecortex?sslmode=disable",
		CacheDriver:                 "memory",
		ManagedDataDir:              root,
		ManagedNetwork:              "firecortex-network",
		ManagedPostgresEnabled:      true,
		ManagedPostgresImage:        "pgvector/pgvector:pg16",
		ManagedPostgresPort:         5432,
		ManagedBackupEnabled:        true,
		ManagedBackupSchedule:       "0 3 * * *",
		ManagedBackupRetentionCount: 2,
	}

	fakeDocker := &fakeDockerClient{containers: map[string]*managedports.ContainerInfo{
		managedports.PostgresContainerName: {ID: "pg-1", Name: managedports.PostgresContainerName, Running: true, Health: "healthy"},
	}}
	sec := &stubSecurity{validPass: "supersecret"}
	syncer := &stubSettingsSync{}
	mgr := NewManager(ManagerDependencies{
		Runtime:      config.NewRuntime(cfg),
		HostOps:      &stubManagerHostOps{client: fakeDocker},
		Security:     sec,
		SettingsSync: syncer,
	})

	rec, err := mgr.CreateBackup(context.Background(), 1, "manual")
	if err != nil {
		t.Fatalf("CreateBackup failed: %v", err)
	}
	if rec.SizeBytes == 0 || len(rec.SHA256) != 64 || rec.Trigger != "manual" {
		t.Fatalf("unexpected backup record: %+v", rec)
	}

	list, err := mgr.ListBackups()
	if err != nil || len(list) != 1 || list[0].ID != rec.ID {
		t.Fatalf("ListBackups unexpected: list=%+v err=%v", list, err)
	}

	// Reject wrong confirmation text or wrong password
	if _, err := mgr.DangerRestoreBackup(context.Background(), 1, DangerRestoreInput{
		BackupID:        rec.ID,
		CurrentPassword: "supersecret",
		ConfirmText:     "WRONG",
	}); err == nil {
		t.Fatal("expected confirmation mismatch error")
	}
	if _, err := mgr.DangerRestoreBackup(context.Background(), 1, DangerRestoreInput{
		BackupID:        rec.ID,
		CurrentPassword: "bad-password",
		ConfirmText:     ConfirmTextRestore,
	}); err == nil {
		t.Fatal("expected invalid password error")
	}

	// Valid restore creates a pre-restore safety snapshot first
	restoreRes, err := mgr.DangerRestoreBackup(context.Background(), 1, DangerRestoreInput{
		BackupID:        rec.ID,
		CurrentPassword: "supersecret",
		ConfirmText:     ConfirmTextRestore,
	})
	if err != nil {
		t.Fatalf("DangerRestoreBackup failed: %v", err)
	}
	if restoreRes.PreRestoreBackup == "" || restoreRes.RestoredBackupID != rec.ID {
		t.Fatalf("unexpected restore result: %+v", restoreRes)
	}

	fakeDocker.restoreResult = managedports.ExecResult{ExitCode: 1, Stderr: "errors ignored on restore: 1"}
	input := DangerRestoreInput{BackupID: rec.ID, CurrentPassword: "supersecret", ConfirmText: ConfirmTextRestore}
	if _, err := mgr.DangerRestoreBackup(context.Background(), 1, input); err == nil {
		t.Fatal("partial restore reported success")
	}
	archive, _, err := mgr.ResolveBackupPath(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(archive + ".sha256"); err != nil {
		t.Fatal(err)
	}
	if err := verifyFileSHA256(archive); err == nil {
		t.Fatal("missing checksum accepted")
	}

	// Core postgres cannot be stopped from UI
	if _, err := mgr.StartServiceAction(t.Context(), 1, managedports.CoreServicePostgres, "stop", ServiceActionOptions{}); err == nil {
		t.Fatal("expected stopping core postgres to be blocked")
	}

	if !matchesCronMinute("0 3 * * 1-5", time.Date(2026, 11, 23, 3, 0, 0, 0, time.UTC)) {
		t.Fatal("weekday range was not scheduled")
	}
	// Cron matching
	ts := time.Date(2026, 11, 23, 3, 0, 0, 0, time.UTC)
	if !matchesCronMinute("0 3 * * *", ts) || matchesCronMinute("15 3 * * *", ts) {
		t.Fatal("cron minute matching failed")
	}
	if !matchesCronMinute("*/15 * * * *", time.Date(2026, 11, 23, 14, 45, 0, 0, time.UTC)) {
		t.Fatal("cron step matching failed")
	}
}

func TestCoreCredentialsSurviveRetryWithoutCompose(t *testing.T) {
	root := t.TempDir()
	docker := &fakeDockerClient{}
	for _, service := range []string{managedports.CoreServicePostgres, managedports.CoreServiceRedis} {
		first, err := PrepareCoreCredentials(t.Context(), docker, root, service, CoreCredentials{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		retry, err := PrepareCoreCredentials(t.Context(), docker, root, service, CoreCredentials{}, nil)
		if err != nil || first.Password == "" || retry != first {
			t.Fatal("failed to reuse generated credentials", err)
		}
		info, err := os.Stat(filepath.Join(root, "."+service+"-credentials.json"))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("credential file must be private", err)
		}
	}
}

func TestSyncComposeRetainsSidecarSettings(t *testing.T) {
	ctr := &managedports.ContainerInfo{
		Image: "mirror.local/ocr:custom", HostPorts: map[string]int{"8004/tcp": 8099}, MemoryBytes: 128 << 20,
		Labels: map[string]string{managedports.ManagedLabelKey: "true", managedports.ServiceLabelKey: managedports.OptionalServiceTesseract},
		Env:    map[string]string{"TESSERACT_LANG": "fra"},
	}
	client := &fakeDockerClient{containers: map[string]*managedports.ContainerInfo{managedports.TesseractContainerName: ctr}}
	var state managedports.ComposeState
	if err := collectComposeSidecars(t.Context(), config.Config{}, client, &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Sidecars) != 1 {
		t.Fatal("sidecar omitted")
	}
	got := state.Sidecars[0]
	if got.HostPort != 8099 || got.MemoryLimitMB != 128 || got.Image != ctr.Image || got.Env[0] != "TESSERACT_LANG=fra" {
		t.Fatal("sidecar settings were not preserved")
	}
}

type switchProbe struct {
	managedports.SetupInfraPort
	target string
	err    error
}

func (p *switchProbe) ProbePostgres(_ context.Context, dsn string) (managedports.PostgresProbeResult, error) {
	p.target = dsn
	return managedports.PostgresProbeResult{}, p.err
}

func TestSwitchBackValidatesManagedCredentialsBeforeWriting(t *testing.T) {
	root := t.TempDir()
	cfgPath := filepath.Join(root, "config.json")
	original := []byte("{\"app\":{\"env\":\"dev\"}}")
	if err := os.WriteFile(cfgPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{ConfigFile: cfgPath, PostgresDSN: "postgres://external:external-pass@db/external", ManagedDataDir: root, ManagedPostgresPort: 5432}
	docker := &fakeDockerClient{containers: map[string]*managedports.ContainerInfo{
		managedports.PostgresContainerName: {Running: true, Env: map[string]string{"POSTGRES_PASSWORD": "managed-pass", "POSTGRES_USER": "local", "POSTGRES_DB": "local-db"}},
	}}
	probe := &switchProbe{err: errors.New("authentication failed")}
	mgr := NewManager(ManagerDependencies{Runtime: config.NewRuntime(cfg), HostOps: &stubManagerHostOps{client: docker}, Infra: probe, Security: &stubSecurity{validPass: "admin"}})
	input := DangerSwitchDatabaseInput{DatabaseMode: "managed", CacheMode: "memory", CurrentPassword: "admin", ConfirmText: ConfirmTextSwitchDatabase}
	if _, err := mgr.DangerSwitchDatabase(t.Context(), 1, input); err == nil {
		t.Fatal("switch accepted failed authentication")
	}
	if probe.target != BuildPostgresDSN("127.0.0.1", 5432, "local", "managed-pass", "local-db") {
		t.Fatal("tested the wrong credentials")
	}
	got, err := os.ReadFile(cfgPath)
	if err != nil || string(got) != string(original) {
		t.Fatal("configuration changed after failed probe")
	}
	probe.err = nil
	if _, err := mgr.DangerSwitchDatabase(t.Context(), 1, input); err != nil {
		t.Fatal(err)
	}
}
