package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderSystemdUnit(t *testing.T) {
	unit := renderSystemdUnit(systemdUnitOptions{
		ServiceName:        "firecortex",
		User:               "firecortex",
		Group:              "firecortex",
		InstallDir:         "/opt/firecortex",
		DataDir:            "/data/firecortex",
		DockerSocketPath:   "/var/run/docker.sock",
		IncludeDockerGroup: true,
	})
	for _, want := range []string{
		"After=network-online.target docker.service",
		"SupplementaryGroups=docker",
		"ReadWritePaths=/opt/firecortex /data/firecortex /var/run/docker.sock",
		"ProtectHome=true",
		"ExecStart=/opt/firecortex/firecortex",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("expected systemd unit to contain %q:\n%s", want, unit)
		}
	}

	homeUnit := renderSystemdUnit(systemdUnitOptions{
		InstallDir:         "/opt/firecortex",
		DataDir:            "/home/ops/firecortex-data",
		IncludeDockerGroup: false,
	})
	if strings.Contains(homeUnit, "SupplementaryGroups=docker") {
		t.Fatalf("expected docker group to be omitted when disabled:\n%s", homeUnit)
	}
	if !strings.Contains(homeUnit, "ProtectHome=false") {
		t.Fatalf("expected ProtectHome=false when data dir is under /home:\n%s", homeUnit)
	}
}

func TestInstallServiceDryRun(t *testing.T) {
	var buf bytes.Buffer
	err := run([]string{"install-service", "--dry-run", "--dir", "/opt/firecortex", "--data-dir", "/var/lib/firecortex"}, &buf)
	if err != nil {
		t.Fatalf("install-service --dry-run failed: %v", err)
	}
	if !strings.Contains(buf.String(), "ReadWritePaths=/opt/firecortex /var/lib/firecortex /var/run/docker.sock") {
		t.Fatalf("unexpected dry-run output:\n%s", buf.String())
	}
}
