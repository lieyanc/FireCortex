//go:build windows

package setup

import "errors"

// ReExecSelf 在 Windows 上不支持原地 exec；调用方应退出进程交由服务管理器重启。
func ReExecSelf() error {
	return errors.New("in-place process exec is not supported on windows")
}
