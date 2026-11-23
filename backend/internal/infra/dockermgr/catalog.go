package dockermgr

import (
	"fmt"
	"strings"
	"time"
)

// RewriteImageWithMirror 根据可选的镜像加速前缀（Registry Mirror）改写待拉取的镜像地址。
// 规则：
//   - mirror 为空时原样返回；
//   - Docker Hub 官方镜像（如 redis:7-alpine）改写为 <mirror>/library/redis:7-alpine；
//   - Docker Hub 两段镜像（如 pgvector/pgvector:pg16）改写为 <mirror>/pgvector/pgvector:pg16；
//   - 带显式域名（含 '.' 或 ':'，如 ghcr.io/...）的镜像若非 docker.io 且与 mirror 不同，
//     则去掉 docker.io/ 前缀或保留其原始路径以便兼容多源代理。
func RewriteImageWithMirror(image, mirror string) string {
	img := strings.TrimSpace(image)
	m := normalizeRegistryMirror(mirror)
	if img == "" || m == "" {
		return img
	}
	if strings.HasPrefix(img, m+"/") {
		return img
	}

	firstSlash := strings.IndexByte(img, '/')
	if firstSlash < 0 {
		return m + "/library/" + img
	}
	firstSegment := img[:firstSlash]
	rest := img[firstSlash+1:]
	if firstSegment == "docker.io" || firstSegment == "index.docker.io" || firstSegment == "registry-1.docker.io" {
		if !strings.Contains(rest, "/") {
			return m + "/library/" + rest
		}
		return m + "/" + rest
	}
	if strings.ContainsAny(firstSegment, ".:") || firstSegment == "localhost" {
		return m + "/" + img
	}
	return m + "/" + img
}

func normalizeRegistryMirror(mirror string) string {
	m := strings.TrimSpace(mirror)
	m = strings.TrimPrefix(m, "https://")
	m = strings.TrimPrefix(m, "http://")
	return strings.TrimRight(m, "/")
}

// PostgresSpec 生成白名单受控的 PostgreSQL + pgvector 容器声明。
func PostgresSpec(cfg PostgresContainerConfig) ContainerSpec {
	port := cfg.Port
	if port <= 0 {
		port = 5432
	}
	user := strings.TrimSpace(cfg.User)
	if user == "" {
		user = "firecortex"
	}
	dbName := strings.TrimSpace(cfg.Database)
	if dbName == "" {
		dbName = "firecortex"
	}
	image := RewriteImageWithMirror(cfg.Image, cfg.RegistryMirror)
	if image == "" {
		image = "pgvector/pgvector:pg16"
	}

	return ContainerSpec{
		Name:         PostgresContainerName,
		Image:        image,
		Network:      cfg.Network,
		NetworkAlias: "postgres",
		Env: []string{
			"POSTGRES_USER=" + user,
			"POSTGRES_PASSWORD=" + cfg.Password,
			"POSTGRES_DB=" + dbName,
			"PGDATA=/var/lib/postgresql/data",
		},
		Binds: []string{
			cfg.DataDir + ":/var/lib/postgresql/data",
		},
		Ports: []PortBinding{
			{
				HostIP:        "127.0.0.1",
				HostPort:      port,
				ContainerPort: 5432,
				Protocol:      "tcp",
			},
		},
		Labels: map[string]string{
			ManagedLabelKey: "true",
			ServiceLabelKey: CoreServicePostgres,
		},
		RestartPolicy: "unless-stopped",
		MemoryBytes:   int64(cfg.MemoryLimitMB) * 1024 * 1024,
		Healthcheck: &HealthcheckSpec{
			Test:        []string{"CMD-SHELL", fmt.Sprintf("pg_isready -U %s -d %s", shellSingleQuote(user), shellSingleQuote(dbName))},
			Interval:    5 * time.Second,
			Timeout:     5 * time.Second,
			StartPeriod: 15 * time.Second,
			Retries:     10,
		},
	}
}

// RedisSpec 生成白名单受控的 Redis 容器声明（强制启用密码与 AOF 持久化）。
func RedisSpec(cfg RedisContainerConfig) ContainerSpec {
	port := cfg.Port
	if port <= 0 {
		port = 6379
	}
	image := RewriteImageWithMirror(cfg.Image, cfg.RegistryMirror)
	if image == "" {
		image = "redis:7-alpine"
	}

	cmd := []string{
		"redis-server",
		"--appendonly", "yes",
		"--appendfsync", "everysec",
		"--save", "900", "1",
		"--save", "300", "10",
		"--dir", "/data",
	}
	var env []string
	hcTest := []string{"CMD", "redis-cli", "ping"}
	if cfg.Password != "" {
		cmd = append(cmd, "--requirepass", cfg.Password)
		env = append(env, "REDISCLI_AUTH="+cfg.Password)
		hcTest = []string{"CMD-SHELL", "redis-cli ping | grep -q PONG"}
	}

	return ContainerSpec{
		Name:         RedisContainerName,
		Image:        image,
		Network:      cfg.Network,
		NetworkAlias: "redis",
		Env:          env,
		Cmd:          cmd,
		Binds: []string{
			cfg.DataDir + ":/data",
		},
		Ports: []PortBinding{
			{
				HostIP:        "127.0.0.1",
				HostPort:      port,
				ContainerPort: 6379,
				Protocol:      "tcp",
			},
		},
		Labels: map[string]string{
			ManagedLabelKey: "true",
			ServiceLabelKey: CoreServiceRedis,
		},
		RestartPolicy: "unless-stopped",
		MemoryBytes:   int64(cfg.MemoryLimitMB) * 1024 * 1024,
		Healthcheck: &HealthcheckSpec{
			Test:        hcTest,
			Interval:    5 * time.Second,
			Timeout:     5 * time.Second,
			StartPeriod: 5 * time.Second,
			Retries:     10,
		},
	}
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// SidecarSpec 生成白名单受控的可选旁路服务（Tika / Docling / Tesseract / RapidOCR）容器声明。
func SidecarSpec(cfg SidecarContainerConfig) ContainerSpec {
	image := RewriteImageWithMirror(cfg.Image, cfg.RegistryMirror)
	var binds []string
	if strings.TrimSpace(cfg.DataDir) != "" && strings.TrimSpace(cfg.ContainerDataDir) != "" {
		binds = append(binds, strings.TrimSpace(cfg.DataDir)+":"+strings.TrimSpace(cfg.ContainerDataDir))
	}
	return ContainerSpec{
		Name:         cfg.ContainerName,
		Image:        image,
		Network:      cfg.Network,
		NetworkAlias: cfg.ServiceID,
		Env:          cfg.Env,
		Binds:        binds,
		Ports: []PortBinding{
			{
				HostIP:        "127.0.0.1",
				HostPort:      cfg.HostPort,
				ContainerPort: cfg.ContainerPort,
				Protocol:      "tcp",
			},
		},
		Labels: map[string]string{
			ManagedLabelKey: "true",
			ServiceLabelKey: cfg.ServiceID,
		},
		RestartPolicy: "unless-stopped",
		MemoryBytes:   int64(cfg.MemoryLimitMB) * 1024 * 1024,
	}
}
