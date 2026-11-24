package managedinfra

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	// PostgresLegacyMountTarget 是 PostgreSQL 17 及更早版本官方镜像的默认 VOLUME 与 PGDATA 路径。
	PostgresLegacyMountTarget = "/var/lib/postgresql/data"
	// PostgresModernMountTarget 是 PostgreSQL 18+ 官方镜像的默认 VOLUME 路径（PGDATA 位于 <major>/docker）。
	PostgresModernMountTarget = "/var/lib/postgresql"
	// PostgresBuiltinInitDBArgs 在 PostgreSQL 17+ 初始化新库时启用内置 C.UTF-8 排序规则，免疫未来 glibc 升级。
	PostgresBuiltinInitDBArgs = "--locale-provider=builtin --locale=C.UTF-8"
)

// PostgresContainerLayout 描述不同 PostgreSQL 大版本容器对应的挂载目录、PGDATA 与 initdb 参数。
type PostgresContainerLayout struct {
	MajorVersion int
	MountTarget  string
	PGData       string
	InitDBArgs   string
}

var (
	pgTagPrefixRe = regexp.MustCompile(`(?:^|[-_.])pg(\d{1,2})(?:$|[-_.])`)
	pgTagLeadRe   = regexp.MustCompile(`^(\d{1,2})(?:$|[-_.])`)
)

// ParsePostgresMajorVersion 从 PostgreSQL / pgvector 镜像 tag 中解析主版本号（如 pg18-trixie、0.8.7-pg18、postgres:18.2）。
// 无法从 tag 识别（例如 :latest 或无 tag）时返回 0, false。
func ParsePostgresMajorVersion(image string) (int, bool) {
	ref := strings.TrimSpace(image)
	if ref == "" {
		return 0, false
	}
	if withoutDigest, _, hasDigest := strings.Cut(ref, "@"); hasDigest {
		ref = withoutDigest
	}
	lastSlash := strings.LastIndexByte(ref, '/')
	tail := ref[lastSlash+1:]
	_, tag, hasTag := strings.Cut(tail, ":")
	if !hasTag {
		return 0, false
	}
	tag = strings.ToLower(strings.TrimSpace(tag))
	if tag == "" {
		return 0, false
	}

	if m := pgTagPrefixRe.FindStringSubmatch(tag); len(m) == 2 {
		if major, ok := parseValidPGMajor(m[1]); ok {
			return major, true
		}
	}
	if m := pgTagLeadRe.FindStringSubmatch(tag); len(m) == 2 {
		if major, ok := parseValidPGMajor(m[1]); ok {
			return major, true
		}
	}
	return 0, false
}

// ResolvePostgresContainerLayout 根据目标镜像的 PostgreSQL 主版本返回容器挂载目录、PGDATA 与 initdb 参数。
//   - PostgreSQL 18+（及无法识别版本号的现代默认镜像）：挂载 /var/lib/postgresql，沿用镜像默认 PGDATA（/var/lib/postgresql/<major>/docker）；
//   - PostgreSQL 17 及更早版本：沿用 /var/lib/postgresql/data 与显式 PGDATA=/var/lib/postgresql/data；
//   - PostgreSQL 17+ 显式版本：初始化时附带 --locale-provider=builtin --locale=C.UTF-8。
func ResolvePostgresContainerLayout(image string) PostgresContainerLayout {
	trimmed := strings.TrimSpace(image)
	if trimmed == "" {
		trimmed = DefaultPostgresImage
	}
	major, ok := ParsePostgresMajorVersion(trimmed)
	if ok && major < 18 {
		layout := PostgresContainerLayout{
			MajorVersion: major,
			MountTarget:  PostgresLegacyMountTarget,
			PGData:       PostgresLegacyMountTarget,
		}
		if major >= 17 {
			layout.InitDBArgs = PostgresBuiltinInitDBArgs
		}
		return layout
	}

	layout := PostgresContainerLayout{
		MajorVersion: major,
		MountTarget:  PostgresModernMountTarget,
	}
	if ok && major >= 18 {
		layout.InitDBArgs = PostgresBuiltinInitDBArgs
	}
	return layout
}

// ValidatePostgresImageForDataVersion 校验目标 PostgreSQL 镜像主版本与宿主机已有数据目录的 PG_VERSION 是否兼容，
// 防止跨大版本直接挂载导致启动失败或在 PG 18+ 下静默初始化空集群。
func ValidatePostgresImageForDataVersion(image string, dataVersion string) error {
	dataMajor, okData := parseValidPGMajor(strings.TrimSpace(dataVersion))
	if !okData {
		return nil
	}
	trimmedImage := strings.TrimSpace(image)
	if trimmedImage == "" {
		trimmedImage = DefaultPostgresImage
	}
	imgMajor, okImg := ParsePostgresMajorVersion(trimmedImage)
	if !okImg || imgMajor == dataMajor {
		return nil
	}
	return fmt.Errorf(
		"existing PostgreSQL data directory is major version %d, which is incompatible with image %q (PostgreSQL %d); use a matching PostgreSQL %d image (for example pgvector/pgvector:pg%d) or choose a fresh directory",
		dataMajor, trimmedImage, imgMajor, dataMajor, dataMajor,
	)
}

func parseValidPGMajor(raw string) (int, bool) {
	if raw == "" || len(raw) > 2 || raw[0] == '0' {
		return 0, false
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] < '0' || raw[i] > '9' {
			return 0, false
		}
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 9 || v > 99 {
		return 0, false
	}
	return v, true
}
