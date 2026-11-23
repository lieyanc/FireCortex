//go:build !windows

package setup

import (
	"os"
	"syscall"
)

// ReExecSelf 用当前可执行文件与参数原地替换进程（保持同 PID，systemd 无感重启）。
func ReExecSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return syscall.Exec(exe, os.Args, os.Environ())
}
