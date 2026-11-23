//go:build nopostgres

package persistence

import "context"

// PostgresProbeResult 描述 PostgreSQL 连通性与 pgvector 扩展探测结果。
type PostgresProbeResult struct {
	ServerVersion   string `json:"serverVersion"`
	VectorInstalled bool   `json:"vectorInstalled"`
	VectorAvailable bool   `json:"vectorAvailable"`
}

// ProbePostgres 在 -tags nopostgres 构建下返回不可用错误。
func ProbePostgres(_ context.Context, _ string) (PostgresProbeResult, error) {
	return PostgresProbeResult{}, ErrPostgresUnavailable
}
