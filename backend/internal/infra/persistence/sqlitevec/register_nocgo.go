//go:build !cgo

package sqlitevec

// Register 在未启用 CGO 时为空操作：无 SQLite 驱动可加载，Migrate 会报告扩展不可用。
func Register() {}
