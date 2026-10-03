//go:build !windows

package updater

import "syscall"

const execSupported = true

func replaceProcess(path string, args, env []string) error {
	return syscall.Exec(path, args, env)
}
