//go:build !linux

package cli

import (
	"errors"
	"io"
)

func runInstallService(_ []string, _ io.Writer) error {
	return errors.New("install-service is only supported on Linux with systemd")
}

func runUninstallService(_ []string, _ io.Writer) error {
	return errors.New("uninstall-service is only supported on Linux with systemd")
}
