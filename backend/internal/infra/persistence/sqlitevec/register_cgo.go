//go:build cgo

package sqlitevec

import (
	"sync"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

var registerOnce sync.Once

// Register 将 sqlite-vec 加载到此调用之后打开的所有 SQLite 连接中。
func Register() {
	registerOnce.Do(func() {
		sqlite_vec.Auto()
	})
}
