package cli

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
)

// systemdUnitOptions 描述生成的 firecortex.service 单元参数。
type systemdUnitOptions struct {
	ServiceName        string
	User               string
	Group              string
	InstallDir         string
	DataDir            string
	DockerSocketPath   string
	IncludeDockerGroup bool
}

func renderSystemdUnit(opts systemdUnitOptions) string {
	serviceName := strings.TrimSpace(opts.ServiceName)
	if serviceName == "" {
		serviceName = "firecortex"
	}
	userName := strings.TrimSpace(opts.User)
	if userName == "" {
		userName = "firecortex"
	}
	groupName := strings.TrimSpace(opts.Group)
	if groupName == "" {
		groupName = userName
	}
	installDir := filepath.Clean(opts.InstallDir)
	dataDir := filepath.Clean(opts.DataDir)

	var rwPaths []string
	addPath := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		for _, existing := range rwPaths {
			if existing == p {
				return
			}
		}
		rwPaths = append(rwPaths, p)
	}
	addPath(installDir)
	if dataDir != "" && dataDir != installDir && !strings.HasPrefix(dataDir, installDir+string(filepath.Separator)) {
		addPath(dataDir)
	}
	if opts.IncludeDockerGroup {
		sockPath := strings.TrimSpace(opts.DockerSocketPath)
		if sockPath == "" {
			sockPath = "/var/run/docker.sock"
		}
		addPath(sockPath)
	}

	protectHome := !isUnderHomeDir(installDir) && !isUnderHomeDir(dataDir)

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "# Managed by `firecortex install-service` (%s)\n", serviceName)
	buf.WriteString("[Unit]\n")
	buf.WriteString("Description=FireCortex AI Workspace Server\n")
	buf.WriteString("Documentation=https://github.com/lieyanc/FireCortex\n")
	if opts.IncludeDockerGroup {
		buf.WriteString("Wants=network-online.target docker.service\n")
		buf.WriteString("After=network-online.target docker.service\n")
	} else {
		buf.WriteString("Wants=network-online.target\n")
		buf.WriteString("After=network-online.target\n")
	}
	buf.WriteString("\n[Service]\n")
	buf.WriteString("Type=simple\n")
	fmt.Fprintf(&buf, "User=%s\n", userName)
	fmt.Fprintf(&buf, "Group=%s\n", groupName)
	if opts.IncludeDockerGroup {
		buf.WriteString("SupplementaryGroups=docker\n")
	}
	fmt.Fprintf(&buf, "WorkingDirectory=%s\n", installDir)
	fmt.Fprintf(&buf, "ExecStart=%s\n", filepath.Join(installDir, "firecortex"))
	buf.WriteString("Restart=always\n")
	buf.WriteString("RestartSec=5\n")
	buf.WriteString("LimitNOFILE=65535\n")
	buf.WriteString("\n# Security hardening with explicit write access for binary OTA updates, managed data, and Docker socket.\n")
	buf.WriteString("ProtectSystem=strict\n")
	fmt.Fprintf(&buf, "ReadWritePaths=%s\n", strings.Join(rwPaths, " "))
	if protectHome {
		buf.WriteString("ProtectHome=true\n")
	} else {
		buf.WriteString("ProtectHome=false\n")
	}
	buf.WriteString("PrivateTmp=true\n")
	buf.WriteString("NoNewPrivileges=true\n")
	buf.WriteString("ProtectKernelTunables=true\n")
	buf.WriteString("ProtectKernelModules=true\n")
	buf.WriteString("ProtectControlGroups=true\n")
	buf.WriteString("RestrictSUIDSGID=true\n")
	buf.WriteString("RestrictRealtime=true\n")
	buf.WriteString("LockPersonality=true\n")
	buf.WriteString("\n[Install]\n")
	buf.WriteString("WantedBy=multi-user.target\n")
	return buf.String()
}

func isUnderHomeDir(path string) bool {
	clean := filepath.Clean(path)
	return clean == "/home" || strings.HasPrefix(clean, "/home/") || clean == "/root" || strings.HasPrefix(clean, "/root/")
}
