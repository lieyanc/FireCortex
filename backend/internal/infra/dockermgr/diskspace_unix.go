//go:build !windows

package dockermgr

import "syscall"

func diskUsage(path string) (freeBytes uint64, totalBytes uint64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0
	}
	blockSize := uint64(stat.Bsize)
	return stat.Bavail * blockSize, stat.Blocks * blockSize
}
