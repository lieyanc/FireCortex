package managedinfra

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	managedports "github.com/lieyanc/FireCortex/backend/internal/ports/managedinfra"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

var backupFileNameRe = regexp.MustCompile(`^firecortex-(\d{8}-\d{6}(?:\.\d{9})?)-([a-z-]+)\.dump\.gz$`)

// BackupRecord 描述落盘在 postgres/backups/ 下的一份逻辑备份归档（领域对象，无 JSON 标签）。
type BackupRecord struct {
	ID        string
	FileName  string
	SizeBytes int64
	SHA256    string
	Trigger   string // scheduled | manual | pre-upgrade | pre-restore
	CreatedAt time.Time
	Verified  bool
}

// CreateBackup 在托管 PostgreSQL 容器内流式执行 pg_dump -Fc 并压缩校验落盘至 backups 目录。
func (m *Manager) CreateBackup(ctx context.Context, actorUserID uint, trigger string) (BackupRecord, error) {
	cfg := m.snapshotConfig()
	if !cfg.ManagedPostgresEnabled {
		return BackupRecord{}, errors.New("automated backups require FireCortex-managed PostgreSQL")
	}
	trigger = normalizeBackupTrigger(trigger)

	client, err := m.hostOps.NewDockerClient(cfg.EffectiveManagedDockerHost())
	if err != nil {
		return BackupRecord{}, err
	}
	backupDir := cfg.EffectiveManagedBackupDir()
	if err := m.hostOps.PrepareManagedDirs(cfg.EffectiveManagedDataDir(), "", "", backupDir, ""); err != nil {
		return BackupRecord{}, fmt.Errorf("prepare backup directory: %w", err)
	}

	now := time.Now().UTC()
	fileName := fmt.Sprintf("firecortex-%s-%s.dump.gz", now.Format("20060102-150405.000000000"), trigger)
	finalPath := filepath.Join(backupDir, fileName)
	shaPath := finalPath + ".sha256"

	tmpFile, err := os.CreateTemp(backupDir, ".backup-*.dump.gz.tmp")
	if err != nil {
		return BackupRecord{}, fmt.Errorf("create temp backup file: %w", err)
	}
	tmpName := tmpFile.Name()
	defer os.Remove(tmpName)

	hasher := sha256.New()
	gzWriter := gzip.NewWriter(io.MultiWriter(tmpFile, hasher))

	user, password, dbName := ParsePostgresDSN(cfg.PostgresDSN)
	stderr, exitCode, execErr := client.ExecStreamStdout(
		ctx,
		managedports.PostgresContainerName,
		[]string{"pg_dump", "-Fc", "-U", user, "-d", dbName},
		[]string{"PGPASSWORD=" + password},
		gzWriter,
	)
	closeGzErr := gzWriter.Close()
	syncErr := tmpFile.Sync()
	chmodErr := tmpFile.Chmod(0o600)
	closeFileErr := tmpFile.Close()

	if execErr != nil {
		return BackupRecord{}, fmt.Errorf("pg_dump stream failed: %w", execErr)
	}
	if exitCode != 0 {
		return BackupRecord{}, fmt.Errorf("pg_dump exited with code %d: %s", exitCode, stderr)
	}
	if closeGzErr != nil || syncErr != nil || chmodErr != nil || closeFileErr != nil {
		return BackupRecord{}, fmt.Errorf("finalize backup file: gz=%v sync=%v chmod=%v close=%v", closeGzErr, syncErr, chmodErr, closeFileErr)
	}

	if err := os.Rename(tmpName, finalPath); err != nil {
		return BackupRecord{}, fmt.Errorf("persist backup archive: %w", err)
	}

	sum := hex.EncodeToString(hasher.Sum(nil))
	if err := os.WriteFile(shaPath, []byte(sum+"  "+fileName+"\n"), 0o600); err != nil {
		_ = os.Remove(finalPath)
		_ = os.Remove(shaPath)
		return BackupRecord{}, fmt.Errorf("persist backup checksum: %w", err)
	}

	info, err := os.Stat(finalPath)
	if err != nil {
		return BackupRecord{}, err
	}

	if trigger == "scheduled" && cfg.ManagedBackupRetentionCount > 0 {
		m.pruneScheduledBackups(backupDir, cfg.ManagedBackupRetentionCount)
	}

	rec := BackupRecord{
		ID:        fileName,
		FileName:  fileName,
		SizeBytes: info.Size(),
		SHA256:    sum,
		Trigger:   trigger,
		CreatedAt: now,
		Verified:  true,
	}
	m.recordAudit(ctx, actorUserID, "infra.backup.create", "postgres_backup", fileName, map[string]any{
		"trigger":   trigger,
		"sizeBytes": rec.SizeBytes,
		"sha256":    sum,
	})
	return rec, nil
}

// ListBackups 列出备份目录中的所有历史备份（按时间倒序）。
func (m *Manager) ListBackups() ([]BackupRecord, error) {
	cfg := m.snapshotConfig()
	backupDir := cfg.EffectiveManagedBackupDir()
	entries, err := os.ReadDir(backupDir)
	if errors.Is(err, os.ErrNotExist) {
		return []BackupRecord{}, nil
	}
	if err != nil {
		return nil, err
	}

	records := make([]BackupRecord, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		matches := backupFileNameRe.FindStringSubmatch(name)
		if len(matches) != 3 {
			continue
		}
		info, statErr := entry.Info()
		if statErr != nil {
			continue
		}
		createdAt, parseErr := time.Parse("20060102-150405", matches[1])
		if parseErr != nil {
			createdAt = info.ModTime().UTC()
		}
		sha, hasSha := readSidecarSHA256(filepath.Join(backupDir, name+".sha256"))
		records = append(records, BackupRecord{
			ID:        name,
			FileName:  name,
			SizeBytes: info.Size(),
			SHA256:    sha,
			Trigger:   matches[2],
			CreatedAt: createdAt.UTC(),
			Verified:  hasSha,
		})
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].CreatedAt.After(records[j].CreatedAt)
	})
	return records, nil
}

// ResolveBackupPath 严格校验备份 ID 并返回位于备份目录内的绝对路径（杜绝路径穿越）。
func (m *Manager) ResolveBackupPath(backupID string) (string, string, error) {
	cleanID := strings.TrimSpace(backupID)
	if !backupFileNameRe.MatchString(cleanID) {
		return "", "", errors.New("invalid backup identifier")
	}
	backupDir := m.snapshotConfig().EffectiveManagedBackupDir()
	fullPath := filepath.Join(backupDir, cleanID)
	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		return "", "", errors.New("backup archive not found")
	}
	return fullPath, cleanID, nil
}

// DeleteBackup 仅删除单条历史备份文件及其 .sha256 校验文件，绝不触碰数据库目录。
func (m *Manager) DeleteBackup(ctx context.Context, actorUserID uint, backupID string) error {
	fullPath, cleanID, err := m.ResolveBackupPath(backupID)
	if err != nil {
		return err
	}
	if err := os.Remove(fullPath); err != nil {
		return fmt.Errorf("delete backup file: %w", err)
	}
	_ = os.Remove(fullPath + ".sha256")
	m.recordAudit(ctx, actorUserID, "infra.backup.delete", "postgres_backup", cleanID, nil)
	return nil
}

// StartBackupScheduler 启动后台定时备份调度器（仅在启用了托管 PostgreSQL 且开启备份时运行）。
func (m *Manager) StartBackupScheduler(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				m.maybeRunScheduledBackup(ctx, now)
			}
		}
	}()
}

func (m *Manager) maybeRunScheduledBackup(ctx context.Context, now time.Time) {
	cfg := m.snapshotConfig()
	if !cfg.ManagedPostgresEnabled || !cfg.ManagedBackupEnabled {
		return
	}
	if !matchesCronMinute(cfg.ManagedBackupSchedule, now) {
		return
	}
	minuteKey := now.Format("20060102-1504")
	m.mu.Lock()
	if m.lastCronMin == minuteKey {
		m.mu.Unlock()
		return
	}
	m.lastCronMin = minuteKey
	m.mu.Unlock()

	backupCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	rec, err := m.CreateBackup(backupCtx, 0, "scheduled")
	if err != nil {
		if m.logger != nil {
			m.logger.Warn("scheduled_postgres_backup_failed", zap.Error(err))
		}
		return
	}
	if m.logger != nil {
		m.logger.Info("scheduled_postgres_backup_completed",
			zap.String("file", rec.FileName),
			zap.Int64("size_bytes", rec.SizeBytes),
		)
	}
}

func (m *Manager) pruneScheduledBackups(backupDir string, keepCount int) {
	records, err := m.ListBackups()
	if err != nil {
		return
	}
	kept := 0
	for _, rec := range records {
		if rec.Trigger != "scheduled" {
			continue
		}
		kept++
		if kept > keepCount {
			full := filepath.Join(backupDir, rec.FileName)
			_ = os.Remove(full)
			_ = os.Remove(full + ".sha256")
		}
	}
}

func verifyFileSHA256(filePath string) error {
	expected, ok := readSidecarSHA256(filePath + ".sha256")
	if !ok {
		return errors.New("backup checksum file is missing or invalid")
	}
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("backup checksum mismatch: expected %s, got %s", expected, actual)
	}
	return nil
}

func readSidecarSHA256(path string) (string, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	fields := strings.Fields(string(raw))
	if len(fields) == 0 || len(fields[0]) != 64 {
		return "", false
	}
	if _, err := hex.DecodeString(fields[0]); err != nil {
		return "", false
	}
	return strings.ToLower(fields[0]), true
}

func normalizeBackupTrigger(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "scheduled", "manual", "pre-upgrade", "pre-restore":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return "manual"
	}
}

var backupCron = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

func matchesCronMinute(expr string, t time.Time) bool {
	schedule, err := backupCron.Parse(expr)
	if err != nil {
		return false
	}
	minute := t.Truncate(time.Minute)
	return schedule.Next(minute.Add(-time.Minute)).Equal(minute)
}
