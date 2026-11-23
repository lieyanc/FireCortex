//go:build windows

package dockermgr

func diskUsage(_ string) (freeBytes uint64, totalBytes uint64) {
	return 0, 0
}
