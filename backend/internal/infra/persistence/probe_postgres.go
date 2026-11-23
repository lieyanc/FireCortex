//go:build !nopostgres

package persistence

import (
	"context"
	"fmt"
	"strings"
	"time"

	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// PostgresProbeResult 描述外部或托管 PostgreSQL 连通性与 pgvector 扩展探测结果。
type PostgresProbeResult struct {
	ServerVersion   string `json:"serverVersion"`
	VectorInstalled bool   `json:"vectorInstalled"`
	VectorAvailable bool   `json:"vectorAvailable"`
}

// ProbePostgres 在不修改表结构的前提下测试 PostgreSQL 连通性并探测 pgvector 扩展状态。
func ProbePostgres(ctx context.Context, dsn string) (PostgresProbeResult, error) {
	trimmed := strings.TrimSpace(dsn)
	if trimmed == "" {
		return PostgresProbeResult{}, fmt.Errorf("postgres DSN is empty")
	}

	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	db, err := gorm.Open(gormpostgres.Open(trimmed), &gorm.Config{
		Logger:               gormlogger.Discard,
		DisableAutomaticPing: true,
	})
	if err != nil {
		return PostgresProbeResult{}, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return PostgresProbeResult{}, err
	}
	defer sqlDB.Close()

	if err := sqlDB.PingContext(probeCtx); err != nil {
		return PostgresProbeResult{}, err
	}

	var res PostgresProbeResult
	if err := sqlDB.QueryRowContext(probeCtx, "SELECT version()").Scan(&res.ServerVersion); err != nil {
		return res, err
	}

	var installed string
	if scanErr := sqlDB.QueryRowContext(probeCtx, "SELECT extname FROM pg_extension WHERE extname = 'vector' LIMIT 1").Scan(&installed); scanErr == nil && installed == "vector" {
		res.VectorInstalled = true
		res.VectorAvailable = true
	} else {
		var avail string
		if availErr := sqlDB.QueryRowContext(probeCtx, "SELECT name FROM pg_available_extensions WHERE name = 'vector' LIMIT 1").Scan(&avail); availErr == nil && avail == "vector" {
			res.VectorAvailable = true
		}
	}
	return res, nil
}
