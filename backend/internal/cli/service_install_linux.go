//go:build linux

package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	appsetup "github.com/lieyanc/FireCortex/backend/internal/application/setup"
	"github.com/lieyanc/FireCortex/backend/internal/infra/config"
	"github.com/lieyanc/FireCortex/backend/internal/infra/dockermgr"
)

func runInstallService(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("firecortex install-service", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	installDir := fs.String("dir", "/opt/firecortex", "installation directory for the FireCortex binary and config.json")
	dataDir := fs.String("data-dir", "", "managed Docker data directory (default: <dir>/data/managed)")
	port := fs.String("port", "8080", "HTTP listen port")
	userName := fs.String("user", "firecortex", "Linux system user for the service")
	serviceName := fs.String("service-name", "firecortex", "systemd unit name (without .service)")
	dockerHost := fs.String("docker-host", "", "optional Docker Engine socket/host override")
	noDockerGroup := fs.Bool("no-docker-group", false, "do not add the service user to the docker supplementary group")
	dryRun := fs.Bool("dry-run", false, "print the generated systemd unit without modifying the host")
	if err := fs.Parse(args); err != nil {
		return err
	}

	resolvedDir, err := dockermgr.ValidateHostDir(*installDir)
	if err != nil {
		return fmt.Errorf("invalid --dir: %w", err)
	}
	targetDataDir := strings.TrimSpace(*dataDir)
	if targetDataDir == "" {
		targetDataDir = filepath.Join(resolvedDir, "data", "managed")
	}
	resolvedDataDir, err := dockermgr.ValidateHostDir(targetDataDir)
	if err != nil {
		return fmt.Errorf("invalid --data-dir: %w", err)
	}

	dockerSock := "/var/run/docker.sock"
	if strings.HasPrefix(strings.TrimSpace(*dockerHost), "unix://") {
		dockerSock = strings.TrimPrefix(strings.TrimSpace(*dockerHost), "unix://")
	} else if strings.HasPrefix(strings.TrimSpace(*dockerHost), "/") {
		dockerSock = strings.TrimSpace(*dockerHost)
	}

	includeDockerGroup := !*noDockerGroup
	if includeDockerGroup && !*dryRun {
		if _, err := user.LookupGroup("docker"); err != nil {
			fmt.Fprintln(os.Stderr, "warning: host 'docker' group not found; installing without SupplementaryGroups=docker")
			includeDockerGroup = false
		}
	}

	unitText := renderSystemdUnit(systemdUnitOptions{
		ServiceName:        *serviceName,
		User:               *userName,
		Group:              *userName,
		InstallDir:         resolvedDir,
		DataDir:            resolvedDataDir,
		DockerSocketPath:   dockerSock,
		IncludeDockerGroup: includeDockerGroup,
	})

	if *dryRun {
		_, err := io.WriteString(out, unitText)
		return err
	}

	if os.Geteuid() != 0 {
		return errors.New("install-service must be run as root (for example: sudo ./firecortex install-service)")
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return errors.New("systemctl not found; install-service requires systemd on Linux")
	}

	uid, gid, err := ensureSystemUser(*userName, resolvedDir)
	if err != nil {
		return err
	}

	ownedDirs := []string{resolvedDir}
	if !strings.HasPrefix(resolvedDataDir, resolvedDir+string(filepath.Separator)) && resolvedDataDir != resolvedDir {
		ownedDirs = append(ownedDirs, resolvedDataDir)
	}
	for _, dir := range ownedDirs {
		if err := makeServiceDir(dir, uid, gid); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}

	targetBinary := filepath.Join(resolvedDir, "firecortex")
	if err := installBinaryToPath(targetBinary, uid, gid); err != nil {
		return err
	}

	cfgPath := filepath.Join(resolvedDir, config.DefaultConfigFile)
	if err := config.WritePreSetupConfig(cfgPath, *port, resolvedDataDir, *dockerHost); err != nil {
		return fmt.Errorf("prepare pre-setup config: %w", err)
	}
	_ = os.Chown(cfgPath, uid, gid)

	unitPath := filepath.Join("/etc/systemd/system", *serviceName+".service")
	if err := os.WriteFile(unitPath, []byte(unitText), 0o644); err != nil {
		return fmt.Errorf("write systemd unit %s: %w", unitPath, err)
	}

	if err := runHostCmd("systemctl", "daemon-reload"); err != nil {
		return err
	}
	if err := runHostCmd("systemctl", "enable", "--now", *serviceName); err != nil {
		return err
	}

	fmt.Fprintf(out, "\nFireCortex systemd service installed and started (%s).\n", unitPath)
	fmt.Fprintf(out, "  Install Dir:  %s\n", resolvedDir)
	fmt.Fprintf(out, "  Managed Dir:  %s\n", resolvedDataDir)

	tokenPath := filepath.Join(resolvedDir, appsetup.TokenFileName)
	if token := waitForSetupToken(tokenPath, 10*time.Second); token != "" {
		fmt.Fprintf(out, "\nFirst-run Setup Wizard is ready:\n")
		fmt.Fprintf(out, "  Open:        http://<server-ip>:%s/setup\n", strings.TrimSpace(*port))
		fmt.Fprintf(out, "  Setup Token: %s\n\n", token)
	} else {
		fmt.Fprintf(out, "\nCheck logs with: journalctl -u %s -f\n\n", *serviceName)
	}
	return nil
}

func runUninstallService(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("firecortex uninstall-service", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	serviceName := fs.String("service-name", "firecortex", "systemd unit name (without .service)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if os.Geteuid() != 0 {
		return errors.New("uninstall-service must be run as root (for example: sudo ./firecortex uninstall-service)")
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return errors.New("systemctl not found")
	}

	_ = runHostCmd("systemctl", "disable", "--now", *serviceName)
	unitPath := filepath.Join("/etc/systemd/system", *serviceName+".service")
	if err := os.Remove(unitPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", unitPath, err)
	}
	if err := runHostCmd("systemctl", "daemon-reload"); err != nil {
		return err
	}
	fmt.Fprintf(out, "Removed systemd unit %s.\n", unitPath)
	fmt.Fprintln(out, "Your config.json, managed data directory, backups, and Docker containers were NOT deleted.")
	return nil
}

// Intermediate directories must be traversable by the service user. Only the
// requested leaf is private and owned by the service.
func makeServiceDir(dir string, uid, gid int) error {
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0o750); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return os.Chown(dir, uid, gid)
}

func ensureSystemUser(name, homeDir string) (int, int, error) {
	u, err := user.Lookup(name)
	if err != nil {
		if cmdErr := runHostCmd("useradd", "--system", "--home-dir", homeDir, "--shell", "/usr/sbin/nologin", name); cmdErr != nil {
			return 0, 0, fmt.Errorf("create system user %q: %w", name, cmdErr)
		}
		u, err = user.Lookup(name)
		if err != nil {
			return 0, 0, fmt.Errorf("lookup newly created user %q: %w", name, err)
		}
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, err
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return 0, 0, err
	}
	return uid, gid, nil
}

func installBinaryToPath(dst string, uid, gid int) error {
	src, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve current binary: %w", err)
	}
	srcReal, _ := filepath.EvalSymlinks(src)
	dstReal, _ := filepath.EvalSymlinks(dst)
	if srcReal != "" && srcReal == dstReal {
		_ = os.Chown(dst, uid, gid)
		return os.Chmod(dst, 0o755)
	}

	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source binary %s: %w", src, err)
	}
	defer in.Close()

	tmp, err := os.CreateTemp(filepath.Dir(dst), ".firecortex-bin-*")
	if err != nil {
		return fmt.Errorf("create temp binary in %s: %w", filepath.Dir(dst), err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return fmt.Errorf("copy binary to %s: %w", dst, err)
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chown(tmpName, uid, gid); err != nil {
		return err
	}
	return os.Rename(tmpName, dst)
}

func runHostCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w (%s)", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func waitForSetupToken(path string, timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil {
			if tok := strings.TrimSpace(string(data)); tok != "" {
				return tok
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return ""
}
