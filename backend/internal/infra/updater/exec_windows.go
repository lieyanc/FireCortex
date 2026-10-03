//go:build windows

package updater

import "errors"

// Windows cannot replace a running executable in place and is not a release target.
const execSupported = false

func replaceProcess(string, []string, []string) error {
	return errors.New("self-update is not supported on windows")
}
