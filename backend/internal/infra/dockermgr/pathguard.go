package dockermgr

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// ManagedMarkerFile 标记该目录由 FireCortex 托管，防止误挂载或冲突。
	ManagedMarkerFile = ".firecortex-managed.json"
)

type managedMarker struct {
	SchemaVersion int       `json:"schemaVersion"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// forbiddenExactPaths 禁止直接作为托管根目录或数据目录挂载的顶级目录。
var forbiddenExactPaths = map[string]struct{}{
	"/":             {},
	"/bin":          {},
	"/boot":         {},
	"/dev":          {},
	"/etc":          {},
	"/home":         {},
	"/lib":          {},
	"/lib32":        {},
	"/lib64":        {},
	"/libx32":       {},
	"/lost+found":   {},
	"/media":        {},
	"/mnt":          {},
	"/opt":          {},
	"/proc":         {},
	"/root":         {},
	"/run":          {},
	"/sbin":         {},
	"/srv":          {},
	"/sys":          {},
	"/tmp":          {},
	"/usr":          {},
	"/var":          {},
	"/var/lib":      {},
	"/var/log":      {},
	"/System":       {},
	"/Library":      {},
	"/Applications": {},
}

// forbiddenPrefixes 禁止挂载其本身及任何子目录的系统关键路径前缀。
var forbiddenPrefixes = []string{
	"/bin",
	"/boot",
	"/dev",
	"/etc",
	"/lib",
	"/lib32",
	"/lib64",
	"/libx32",
	"/proc",
	"/sbin",
	"/sys",
	"/usr",
	"/System",
}

// ValidateHostDir 校验宿主机路径安全性（解析祖先软链，拦截系统敏感目录与路径穿越）。
func ValidateHostDir(rawPath string) (string, error) {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" {
		return "", errors.New("data directory path is required")
	}
	if strings.ContainsRune(trimmed, 0) {
		return "", errors.New("data directory path contains invalid null byte")
	}

	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path %q: %w", trimmed, err)
	}
	clean := filepath.Clean(abs)
	resolved, err := resolveWithExistingAncestor(clean)
	if err != nil {
		return "", err
	}

	if _, blocked := forbiddenExactPaths[resolved]; blocked {
		return "", fmt.Errorf("directory %q is a protected system directory and cannot be used", resolved)
	}
	for _, prefix := range forbiddenPrefixes {
		if resolved == prefix || strings.HasPrefix(resolved, prefix+string(filepath.Separator)) {
			return "", fmt.Errorf("directory %q is inside protected system path %q", resolved, prefix)
		}
	}
	if info, statErr := os.Stat(resolved); statErr == nil && !info.IsDir() {
		return "", fmt.Errorf("path %q already exists and is not a directory", resolved)
	}
	return resolved, nil
}

func resolveWithExistingAncestor(clean string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		return filepath.Clean(resolved), nil
	}
	var tail []string
	cur := clean
	for {
		parent := filepath.Dir(cur)
		if parent == cur {
			return clean, nil
		}
		tail = append([]string{filepath.Base(cur)}, tail...)
		cur = parent
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			parts := append([]string{resolved}, tail...)
			return filepath.Clean(filepath.Join(parts...)), nil
		}
	}
}

// InspectManagedRoot 对目标托管数据目录（以及可选的子目录覆盖）执行完整的安全与接管预检。
func InspectManagedRoot(rawRoot, pgOverride, redisOverride, backupOverride string) DirInspection {
	result := DirInspection{}

	resolvedRoot, err := ValidateHostDir(rawRoot)
	if err != nil {
		result.Status = DirStatusInvalid
		result.Reason = err.Error()
		return result
	}
	result.ResolvedRoot = resolvedRoot

	pgDir := filepath.Join(resolvedRoot, "postgres", "pgdata")
	if strings.TrimSpace(pgOverride) != "" {
		pgDir, err = ValidateHostDir(pgOverride)
		if err != nil {
			result.Status = DirStatusInvalid
			result.Reason = "postgres data_dir: " + err.Error()
			return result
		}
	}
	redisDir := filepath.Join(resolvedRoot, "redis")
	if strings.TrimSpace(redisOverride) != "" {
		redisDir, err = ValidateHostDir(redisOverride)
		if err != nil {
			result.Status = DirStatusInvalid
			result.Reason = "redis data_dir: " + err.Error()
			return result
		}
	}
	backupDir := filepath.Join(resolvedRoot, "postgres", "backups")
	if strings.TrimSpace(backupOverride) != "" {
		backupDir, err = ValidateHostDir(backupOverride)
		if err != nil {
			result.Status = DirStatusInvalid
			result.Reason = "backup dir: " + err.Error()
			return result
		}
	}

	result.PostgresDataDir = pgDir
	result.RedisDataDir = redisDir
	result.BackupDir = backupDir

	if statTarget := nearestExistingDir(resolvedRoot); statTarget != "" {
		result.DiskFreeBytes, result.DiskTotalBytes = diskUsage(statTarget)
	}
	if strings.HasPrefix(resolvedRoot, "/tmp/") {
		result.Warnings = append(result.Warnings, "目标目录位于 /tmp 下，宿主机重启后可能丢失数据，生产环境建议改为持久磁盘路径（如 /var/lib/firecortex 或 /data/firecortex）。")
	}

	if _, err := os.Stat(filepath.Join(resolvedRoot, ManagedMarkerFile)); err == nil {
		result.HasManagedMarker = true
	}
	if _, err := os.Stat(filepath.Join(resolvedRoot, ComposeFileName)); err == nil {
		result.HasExistingCompose = true
	}

	pgState, pgVersion, pgReason := inspectPGDataDir(pgDir)
	switch pgState {
	case DirStatusInvalid:
		result.Valid = false
		result.Status = DirStatusInvalid
		result.Reason = pgReason
		return result
	case DirStatusAdoptablePGData:
		result.Valid = true
		result.PGVersion = pgVersion
		if result.HasManagedMarker {
			result.Status = DirStatusManagedExisting
		} else {
			result.Status = DirStatusAdoptablePGData
		}
		return result
	default:
		result.Valid = true
		result.Status = DirStatusEmpty
		return result
	}
}

func inspectPGDataDir(pgDir string) (status string, pgVersion string, reason string) {
	info, err := os.Stat(pgDir)
	if errors.Is(err, os.ErrNotExist) {
		return DirStatusEmpty, "", ""
	}
	if err != nil {
		return DirStatusInvalid, "", fmt.Sprintf("cannot access postgres data dir %q: %v", pgDir, err)
	}
	if !info.IsDir() {
		return DirStatusInvalid, "", fmt.Sprintf("postgres data path %q is not a directory", pgDir)
	}

	entries, err := os.ReadDir(pgDir)
	if err != nil {
		return DirStatusInvalid, "", fmt.Sprintf("cannot read postgres data dir %q: %v", pgDir, err)
	}
	if len(entries) == 0 {
		return DirStatusEmpty, "", ""
	}

	verBytes, verErr := os.ReadFile(filepath.Join(pgDir, "PG_VERSION"))
	if verErr == nil {
		version := strings.TrimSpace(string(verBytes))
		major, ok := parsePGMajorDirName(version)
		if ok && major >= 18 {
			return DirStatusInvalid, "", fmt.Sprintf("PostgreSQL %s data in %q must be mounted from a root containing %s/docker/PG_VERSION", version, pgDir, version)
		}
		if !ok {
			return DirStatusInvalid, "", fmt.Sprintf("invalid PG_VERSION in %q", pgDir)
		}
		return DirStatusAdoptablePGData, version, ""
	}

	// 允许目录里只含 lost+found（独立挂载的空 ext4 分区根目录）。
	if len(entries) == 1 && entries[0].Name() == "lost+found" && entries[0].IsDir() {
		return DirStatusEmpty, "", ""
	}

	// 支持 PostgreSQL 18+ 官方镜像的版本化目录结构（如 <pgdata>/18/docker/PG_VERSION）。
	if detectedVersion, ok := inspectVersionedPGDataRoot(pgDir, entries); ok {
		return DirStatusAdoptablePGData, detectedVersion, ""
	}
	return DirStatusInvalid, "", fmt.Sprintf("postgres data dir %q has an unrecognized layout; PostgreSQL 18+ requires <major>/docker/PG_VERSION under the mount root", pgDir)
}

func inspectVersionedPGDataRoot(pgDir string, entries []os.DirEntry) (string, bool) {
	bestMajor := 0
	bestVersion := ""

	for _, entry := range entries {
		name := entry.Name()
		if name == "lost+found" && entry.IsDir() {
			continue
		}
		if !entry.IsDir() {
			if isAllowedPGHomeDotfile(name) {
				continue
			}
			return "", false
		}
		major, ok := parsePGMajorDirName(name)
		if !ok || major < 18 || !validDockerPGData(filepath.Join(pgDir, name), name) {
			return "", false
		}
		if major > bestMajor {
			bestMajor = major
			bestVersion = name
		}
	}
	return bestVersion, bestVersion != ""
}

func validDockerPGData(verDir, major string) bool {
	entries, err := os.ReadDir(verDir)
	if err != nil {
		return false
	}
	foundDocker := false
	for _, entry := range entries {
		if entry.Name() == "lost+found" && entry.IsDir() {
			continue
		}
		if entry.Name() != "docker" || !entry.IsDir() {
			return false
		}
		foundDocker = true
	}
	if !foundDocker {
		return false
	}
	version, err := os.ReadFile(filepath.Join(verDir, "docker", "PG_VERSION"))
	// The container owns PGDATA (0700). Its parent still identifies the exact mount layout.
	if errors.Is(err, os.ErrPermission) {
		return true
	}
	return err == nil && strings.TrimSpace(string(version)) == major
}

func parsePGMajorDirName(name string) (int, bool) {
	if name == "" || len(name) > 2 || name[0] == '0' {
		return 0, false
	}
	v := 0
	for i := 0; i < len(name); i++ {
		if name[i] < '0' || name[i] > '9' {
			return 0, false
		}
		v = v*10 + int(name[i]-'0')
	}
	if v < 9 || v > 99 {
		return 0, false
	}
	return v, true
}

func isAllowedPGHomeDotfile(name string) bool {
	switch name {
	case ".psql_history", ".pgpass", ".bash_history", ".profile", ".bashrc":
		return true
	default:
		return false
	}
}

func nearestExistingDir(path string) string {
	cur := path
	for {
		if info, err := os.Stat(cur); err == nil && info.IsDir() {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		cur = parent
	}
}

// PrepareManagedDirs 创建托管目录结构并写入归属标记文件（绝不清空或覆盖已有数据）。
func PrepareManagedDirs(rootDir, pgDir, redisDir, backupDir, servicesDir string) error {
	for _, dir := range []string{rootDir, pgDir, redisDir, backupDir, servicesDir} {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		if _, err := ValidateHostDir(dir); err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create managed dir %q: %w", dir, err)
		}
	}
	if err := ensurePostgresMountDirPerm(pgDir); err != nil {
		return err
	}
	return writeManagedMarker(rootDir)
}

// ensurePostgresMountDirPerm 确保 PG 18+ 挂载到 /var/lib/postgresql 的宿主机目录对容器内
// postgres 用户（UID 999）至少具备读取与穿越权限（o+rx，例如 0755）；对 PG <= 17 直接作为
// PGDATA（0700）的旧目录不做权限放宽。
func ensurePostgresMountDirPerm(pgDir string) error {
	trimmed := strings.TrimSpace(pgDir)
	if trimmed == "" {
		return nil
	}
	// Legacy PG <= 17 mounts pgDir directly as PGDATA (0700 owned by container UID 999);
	// never widen an existing PGDATA directory.
	if _, err := os.Stat(filepath.Join(trimmed, "PG_VERSION")); err == nil || errors.Is(err, os.ErrPermission) {
		return nil
	}
	info, err := os.Stat(trimmed)
	if err != nil {
		return fmt.Errorf("stat postgres data dir %q: %w", trimmed, err)
	}
	const otherRX = os.FileMode(0o005)
	if info.Mode().Perm()&otherRX == otherRX {
		return nil
	}
	mode := (info.Mode() & (os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky)) | 0o055
	if err := os.Chmod(trimmed, mode); err != nil {
		return fmt.Errorf("ensure postgres mount dir %q is traversable: %w", trimmed, err)
	}
	return nil
}

func writeManagedMarker(rootDir string) error {
	markerPath := filepath.Join(rootDir, ManagedMarkerFile)
	now := time.Now().UTC()
	marker := managedMarker{
		SchemaVersion: 1,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if existing, err := os.ReadFile(markerPath); err == nil {
		var prev managedMarker
		if json.Unmarshal(existing, &prev) == nil && !prev.CreatedAt.IsZero() {
			marker.CreatedAt = prev.CreatedAt
		}
	}
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileAtomic(markerPath, data, 0o600)
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".fc-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
