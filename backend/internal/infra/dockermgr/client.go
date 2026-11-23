package dockermgr

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrNotFound 表示目标 Docker 资源（镜像、容器或网络）不存在。
var ErrNotFound = errors.New("docker resource not found")

// HTTPClient 通过标准库 net/http 直连 Docker Engine Unix Socket 或 TCP API（纯 Go，零 CGO）。
type HTTPClient struct {
	rawHost    string
	baseURL    string
	dialTarget func(ctx context.Context) (net.Conn, error)
	httpClient *http.Client
	streamHTTP *http.Client
}

// NewClient 根据 dockerHost（支持 unix:///var/run/docker.sock、/path/to.sock、http(s)://、tcp://）创建纯 Go 客户端。
func NewClient(dockerHost string) (*HTTPClient, error) {
	raw := strings.TrimSpace(dockerHost)
	if raw == "" {
		raw = "unix:///var/run/docker.sock"
	}

	transport := &http.Transport{
		DisableCompression:  true,
		IdleConnTimeout:     30 * time.Second,
		MaxIdleConnsPerHost: 4,
	}

	var baseURL string
	var dialTarget func(ctx context.Context) (net.Conn, error)
	switch {
	case strings.HasPrefix(raw, "unix://"):
		sockPath := strings.TrimPrefix(raw, "unix://")
		if sockPath == "" {
			return nil, errors.New("docker unix socket path is empty")
		}
		baseURL = "http://docker"
		dialTarget = func(ctx context.Context) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", sockPath)
		}
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialTarget(ctx)
		}
	case strings.HasPrefix(raw, "/"):
		sockPath := raw
		raw = "unix://" + sockPath
		baseURL = "http://docker"
		dialTarget = func(ctx context.Context) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", sockPath)
		}
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialTarget(ctx)
		}
	case strings.HasPrefix(raw, "tcp://"):
		addr := strings.TrimPrefix(raw, "tcp://")
		baseURL = "http://" + addr
		dialTarget = func(ctx context.Context) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "tcp", addr)
		}
	case strings.HasPrefix(raw, "http://"), strings.HasPrefix(raw, "https://"):
		baseURL = strings.TrimRight(raw, "/")
		parsed, err := url.Parse(baseURL)
		if err != nil || parsed.Hostname() == "" {
			return nil, errors.New("invalid Docker HTTP endpoint")
		}
		port := parsed.Port()
		if port == "" {
			port = parsed.Scheme
		}
		addr := net.JoinHostPort(parsed.Hostname(), port)
		dialTarget = func(ctx context.Context) (net.Conn, error) {
			if parsed.Scheme == "https" {
				dialer := tls.Dialer{Config: transport.TLSClientConfig}
				return dialer.DialContext(ctx, "tcp", addr)
			}
			var dialer net.Dialer
			return dialer.DialContext(ctx, "tcp", addr)
		}
	default:
		return nil, fmt.Errorf("unsupported docker host scheme %q", raw)
	}

	return &HTTPClient{
		rawHost:    raw,
		baseURL:    baseURL,
		dialTarget: dialTarget,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
		streamHTTP: &http.Client{
			Transport: transport,
			Timeout:   0, // 流式拉取/exec 由 ctx 控制超时
		},
	}, nil
}

// Host 返回客户端连接的 Docker Engine 地址。
func (c *HTTPClient) Host() string {
	return c.rawHost
}

// Ping 检测 Docker Engine 可达性并读取宿主机基础信息。
func (c *HTTPClient) Ping(ctx context.Context) (DaemonInfo, error) {
	info := DaemonInfo{Host: c.rawHost}

	pingResp, err := c.do(ctx, c.httpClient, http.MethodGet, "/_ping", nil)
	if err != nil {
		return info, fmt.Errorf("ping docker daemon (%s): %w", c.rawHost, err)
	}
	_, _ = io.Copy(io.Discard, pingResp.Body)
	pingResp.Body.Close()
	if pingResp.StatusCode >= 300 {
		return info, fmt.Errorf("docker ping returned status %d", pingResp.StatusCode)
	}
	info.APIVersion = strings.TrimSpace(pingResp.Header.Get("Api-Version"))
	info.Available = true

	infoResp, err := c.do(ctx, c.httpClient, http.MethodGet, "/info", nil)
	if err != nil {
		return info, nil
	}
	defer infoResp.Body.Close()
	if infoResp.StatusCode >= 300 {
		return info, nil
	}

	var payload struct {
		ServerVersion     string `json:"ServerVersion"`
		OSType            string `json:"OSType"`
		Architecture      string `json:"Architecture"`
		MemTotal          int64  `json:"MemTotal"`
		ContainersRunning int    `json:"ContainersRunning"`
	}
	if decodeErr := json.NewDecoder(infoResp.Body).Decode(&payload); decodeErr == nil {
		info.ServerVersion = payload.ServerVersion
		info.OSType = payload.OSType
		info.Architecture = payload.Architecture
		info.MemTotalBytes = payload.MemTotal
		info.ContainersRunning = payload.ContainersRunning
	}
	return info, nil
}

// EnsureNetwork 确保托管桥接网络存在。
func (c *HTTPClient) EnsureNetwork(ctx context.Context, name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return errors.New("docker network name is empty")
	}
	resp, err := c.do(ctx, c.httpClient, http.MethodGet, "/networks/"+url.PathEscape(trimmed), nil)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	if resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("inspect docker network %q: status %d", trimmed, resp.StatusCode)
	}

	body := map[string]any{
		"Name":           trimmed,
		"Driver":         "bridge",
		"CheckDuplicate": true,
		"Labels": map[string]string{
			ManagedLabelKey: "true",
		},
	}
	createResp, err := c.doJSON(ctx, c.httpClient, http.MethodPost, "/networks/create", body)
	if err != nil {
		return err
	}
	defer createResp.Body.Close()
	if createResp.StatusCode == http.StatusCreated || createResp.StatusCode == http.StatusOK || createResp.StatusCode == http.StatusConflict {
		return nil
	}
	return readDockerAPIError("create network", createResp)
}

// InspectImage 检查指定镜像是否已在本地存在。
func (c *HTTPClient) InspectImage(ctx context.Context, image string) (bool, error) {
	trimmed := strings.TrimSpace(image)
	if trimmed == "" {
		return false, errors.New("docker image is empty")
	}
	resp, err := c.do(ctx, c.httpClient, http.MethodGet, "/images/"+url.PathEscape(trimmed)+"/json", nil)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return true, nil
	}
	if resp.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, resp.Body)
		return false, nil
	}
	return false, readDockerAPIError("inspect image", resp)
}

// PullImage 流式拉取镜像并通过 onProgress 实时汇报每层进度。
func (c *HTTPClient) PullImage(ctx context.Context, image string, onProgress func(PullProgress)) error {
	trimmed := strings.TrimSpace(image)
	if trimmed == "" {
		return errors.New("docker image is empty")
	}
	endpoint := "/images/create?fromImage=" + url.QueryEscape(trimmed)
	resp, err := c.do(ctx, c.streamHTTP, http.MethodPost, endpoint, nil)
	if err != nil {
		return fmt.Errorf("pull image %q: %w", trimmed, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return readDockerAPIError("pull image "+trimmed, resp)
	}

	type layerState struct {
		current int64
		total   int64
		done    bool
	}
	layers := map[string]*layerState{}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var evt struct {
			Status         string `json:"status"`
			ID             string `json:"id"`
			Error          string `json:"error"`
			ProgressDetail struct {
				Current int64 `json:"current"`
				Total   int64 `json:"total"`
			} `json:"progressDetail"`
		}
		if err := json.Unmarshal(line, &evt); err != nil {
			continue
		}
		if strings.TrimSpace(evt.Error) != "" {
			return fmt.Errorf("pull image %q: %s", trimmed, strings.TrimSpace(evt.Error))
		}
		if evt.ID != "" {
			ls, ok := layers[evt.ID]
			if !ok {
				ls = &layerState{}
				layers[evt.ID] = ls
			}
			if evt.ProgressDetail.Total > 0 {
				ls.total = evt.ProgressDetail.Total
				ls.current = evt.ProgressDetail.Current
			}
			lower := strings.ToLower(evt.Status)
			if strings.Contains(lower, "pull complete") || strings.Contains(lower, "already exists") {
				ls.done = true
				if ls.total > 0 {
					ls.current = ls.total
				}
			}
		}
		if onProgress != nil {
			var sumCurrent, sumTotal int64
			doneCount := 0
			for _, ls := range layers {
				if ls.total > 0 {
					sumCurrent += ls.current
					sumTotal += ls.total
				}
				if ls.done {
					doneCount++
				}
			}
			pct := 0
			if sumTotal > 0 {
				pct = int((sumCurrent * 100) / sumTotal)
				if pct > 100 {
					pct = 100
				}
			} else if len(layers) > 0 && doneCount > 0 {
				pct = (doneCount * 100) / len(layers)
			}
			onProgress(PullProgress{
				Status:  evt.Status,
				LayerID: evt.ID,
				Current: sumCurrent,
				Total:   sumTotal,
				Percent: pct,
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read pull stream for %q: %w", trimmed, err)
	}
	return nil
}

// InspectContainer 查询指定容器状态；容器不存在时返回 nil, nil。
func (c *HTTPClient) InspectContainer(ctx context.Context, name string) (*ContainerInfo, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil, errors.New("container name is empty")
	}
	resp, err := c.do(ctx, c.httpClient, http.MethodGet, "/containers/"+url.PathEscape(trimmed)+"/json", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, nil
	}
	if resp.StatusCode >= 300 {
		return nil, readDockerAPIError("inspect container "+trimmed, resp)
	}

	var payload struct {
		ID    string `json:"Id"`
		Name  string `json:"Name"`
		State struct {
			Status  string `json:"Status"`
			Running bool   `json:"Running"`
			Health  *struct {
				Status string `json:"Status"`
			} `json:"Health"`
		} `json:"State"`
		Config struct {
			Env    []string
			Image  string
			Labels map[string]string `json:"Labels"`
		}
		HostConfig struct {
			Memory       int64
			PortBindings map[string][]struct{ HostPort string }
		}
		Mounts []struct{ Type, Source, Destination string }
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode container inspect %q: %w", trimmed, err)
	}
	health := ""
	if payload.State.Health != nil {
		health = payload.State.Health.Status
	}
	env := make(map[string]string)
	for _, entry := range payload.Config.Env {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			env[key] = value
		}
	}
	ports := make(map[string]int)
	for port, bindings := range payload.HostConfig.PortBindings {
		if len(bindings) > 0 {
			ports[port], _ = strconv.Atoi(bindings[0].HostPort)
		}
	}
	mounts := make(map[string]string)
	for _, mount := range payload.Mounts {
		if mount.Type == "bind" {
			mounts[mount.Destination] = mount.Source
		}
	}
	return &ContainerInfo{
		HostPorts: ports, BindMounts: mounts, MemoryBytes: payload.HostConfig.Memory, Env: env,
		ID:      payload.ID,
		Name:    strings.TrimPrefix(payload.Name, "/"),
		Image:   payload.Config.Image,
		Status:  payload.State.Status,
		Running: payload.State.Running,
		Health:  health,
		Labels:  payload.Config.Labels,
	}, nil
}

// CreateContainer 按声明创建托管容器并返回容器 ID。
func (c *HTTPClient) CreateContainer(ctx context.Context, spec ContainerSpec) (string, error) {
	if strings.TrimSpace(spec.Name) == "" || strings.TrimSpace(spec.Image) == "" {
		return "", errors.New("container name and image are required")
	}

	exposedPorts := map[string]struct{}{}
	portBindings := map[string][]map[string]string{}
	for _, p := range spec.Ports {
		proto := strings.TrimSpace(p.Protocol)
		if proto == "" {
			proto = "tcp"
		}
		key := fmt.Sprintf("%d/%s", p.ContainerPort, proto)
		exposedPorts[key] = struct{}{}
		hostIP := strings.TrimSpace(p.HostIP)
		if hostIP == "" {
			hostIP = "127.0.0.1"
		}
		portBindings[key] = append(portBindings[key], map[string]string{
			"HostIp":   hostIP,
			"HostPort": fmt.Sprintf("%d", p.HostPort),
		})
	}

	restartName := strings.TrimSpace(spec.RestartPolicy)
	if restartName == "" {
		restartName = "unless-stopped"
	}

	hostConfig := map[string]any{
		"Binds":        spec.Binds,
		"PortBindings": portBindings,
		"RestartPolicy": map[string]any{
			"Name": restartName,
		},
	}
	if spec.MemoryBytes > 0 {
		hostConfig["Memory"] = spec.MemoryBytes
	}

	payload := map[string]any{
		"Image":        spec.Image,
		"Env":          spec.Env,
		"Labels":       spec.Labels,
		"ExposedPorts": exposedPorts,
		"HostConfig":   hostConfig,
	}
	if len(spec.Cmd) > 0 {
		payload["Cmd"] = spec.Cmd
	}
	if spec.Healthcheck != nil && len(spec.Healthcheck.Test) > 0 {
		hc := map[string]any{
			"Test": spec.Healthcheck.Test,
		}
		if spec.Healthcheck.Interval > 0 {
			hc["Interval"] = spec.Healthcheck.Interval.Nanoseconds()
		}
		if spec.Healthcheck.Timeout > 0 {
			hc["Timeout"] = spec.Healthcheck.Timeout.Nanoseconds()
		}
		if spec.Healthcheck.StartPeriod > 0 {
			hc["StartPeriod"] = spec.Healthcheck.StartPeriod.Nanoseconds()
		}
		if spec.Healthcheck.Retries > 0 {
			hc["Retries"] = spec.Healthcheck.Retries
		}
		payload["Healthcheck"] = hc
	}
	if strings.TrimSpace(spec.Network) != "" {
		endpointCfg := map[string]any{}
		if alias := strings.TrimSpace(spec.NetworkAlias); alias != "" {
			endpointCfg["Aliases"] = []string{alias}
		}
		payload["NetworkingConfig"] = map[string]any{
			"EndpointsConfig": map[string]any{
				spec.Network: endpointCfg,
			},
		}
	}

	resp, err := c.doJSON(ctx, c.httpClient, http.MethodPost, "/containers/create?name="+url.QueryEscape(spec.Name), payload)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return "", readDockerAPIError("create container "+spec.Name, resp)
	}
	var out struct {
		ID string `json:"Id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode create container response: %w", err)
	}
	return out.ID, nil
}

// StartContainer 启动指定容器。
func (c *HTTPClient) StartContainer(ctx context.Context, nameOrID string) error {
	resp, err := c.do(ctx, c.httpClient, http.MethodPost, "/containers/"+url.PathEscape(nameOrID)+"/start", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotModified || resp.StatusCode == http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return readDockerAPIError("start container "+nameOrID, resp)
}

// StopContainer 优雅停止指定容器（不删除宿主机绑定目录）。
func (c *HTTPClient) StopContainer(ctx context.Context, nameOrID string, timeoutSeconds int) error {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 15
	}
	endpoint := fmt.Sprintf("/containers/%s/stop?t=%d", url.PathEscape(nameOrID), timeoutSeconds)
	resp, err := c.do(ctx, c.streamHTTP, http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotModified || resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return readDockerAPIError("stop container "+nameOrID, resp)
}

// RestartContainer 重启指定容器。
func (c *HTTPClient) RestartContainer(ctx context.Context, nameOrID string, timeoutSeconds int) error {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 15
	}
	endpoint := fmt.Sprintf("/containers/%s/restart?t=%d", url.PathEscape(nameOrID), timeoutSeconds)
	resp, err := c.do(ctx, c.streamHTTP, http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return readDockerAPIError("restart container "+nameOrID, resp)
}

// RemoveContainer 仅移除容器实例（固定 v=false，永不删除数据卷与宿主机绑定目录）。
func (c *HTTPClient) RemoveContainer(ctx context.Context, nameOrID string, force bool) error {
	endpoint := fmt.Sprintf("/containers/%s?v=false&force=%t", url.PathEscape(nameOrID), force)
	resp, err := c.do(ctx, c.httpClient, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return readDockerAPIError("remove container "+nameOrID, resp)
}

// ContainerStats 查询容器实时 CPU 与内存占用（单次快照，stream=false）。
func (c *HTTPClient) ContainerStats(ctx context.Context, nameOrID string) (ContainerStats, error) {
	endpoint := fmt.Sprintf("/containers/%s/stats?stream=false", url.PathEscape(nameOrID))
	resp, err := c.do(ctx, c.httpClient, http.MethodGet, endpoint, nil)
	if err != nil {
		return ContainerStats{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return ContainerStats{}, readDockerAPIError("stats container "+nameOrID, resp)
	}
	var payload struct {
		CPUStats struct {
			CPUUsage struct {
				TotalUsage uint64 `json:"total_usage"`
			} `json:"cpu_usage"`
			SystemCPUUsage uint64 `json:"system_cpu_usage"`
			OnlineCPUs     uint32 `json:"online_cpus"`
		} `json:"cpu_stats"`
		PreCPUStats struct {
			CPUUsage struct {
				TotalUsage uint64 `json:"total_usage"`
			} `json:"cpu_usage"`
			SystemCPUUsage uint64 `json:"system_cpu_usage"`
		} `json:"precpu_stats"`
		MemoryStats struct {
			Usage uint64            `json:"usage"`
			Limit uint64            `json:"limit"`
			Stats map[string]uint64 `json:"stats"`
		} `json:"memory_stats"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ContainerStats{}, fmt.Errorf("decode stats for %q: %w", nameOrID, err)
	}
	var cpuPct float64
	cpuDelta := float64(payload.CPUStats.CPUUsage.TotalUsage - payload.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(payload.CPUStats.SystemCPUUsage - payload.PreCPUStats.SystemCPUUsage)
	cpus := float64(payload.CPUStats.OnlineCPUs)
	if cpus <= 0 {
		cpus = 1
	}
	if sysDelta > 0 && cpuDelta > 0 {
		cpuPct = (cpuDelta / sysDelta) * cpus * 100.0
	}
	memUsage := payload.MemoryStats.Usage
	if cacheBytes := payload.MemoryStats.Stats["cache"]; cacheBytes > 0 && memUsage > cacheBytes {
		memUsage -= cacheBytes
	} else if inactiveFile := payload.MemoryStats.Stats["inactive_file"]; inactiveFile > 0 && memUsage > inactiveFile {
		memUsage -= inactiveFile
	}
	return ContainerStats{
		CPUPercent:       cpuPct,
		MemoryUsageBytes: memUsage,
		MemoryLimitBytes: payload.MemoryStats.Limit,
	}, nil
}

// TailLogs 读取指定容器最近 tail 行的合并日志（自动解复用 Docker 8 字节流帧）。
func (c *HTTPClient) TailLogs(ctx context.Context, nameOrID string, tail int) (string, error) {
	if tail <= 0 {
		tail = 200
	}
	if tail > 2000 {
		tail = 2000
	}
	endpoint := fmt.Sprintf("/containers/%s/logs?stdout=true&stderr=true&timestamps=true&tail=%d", url.PathEscape(nameOrID), tail)
	resp, err := c.do(ctx, c.httpClient, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", readDockerAPIError("tail logs "+nameOrID, resp)
	}
	stdout, stderr, err := demuxDockerStream(resp.Body)
	if err != nil {
		return "", err
	}
	if stderr == "" {
		return stdout, nil
	}
	if stdout == "" {
		return stderr, nil
	}
	return stdout + "\n" + stderr, nil
}

// ExecStreamStdout 在容器内执行命令并将 stdout 流式写入目标 writer（适合 pg_dump 等大体积流式导出）。
func (c *HTTPClient) ExecStreamStdout(ctx context.Context, nameOrID string, cmd []string, env []string, stdout io.Writer) (string, int, error) {
	execID, err := c.createExec(ctx, nameOrID, cmd, env, false)
	if err != nil {
		return "", -1, err
	}
	startResp, err := c.doJSON(ctx, c.streamHTTP, http.MethodPost, "/exec/"+url.PathEscape(execID)+"/start", map[string]any{
		"Detach": false,
		"Tty":    false,
	})
	if err != nil {
		return "", -1, err
	}
	defer startResp.Body.Close()
	if startResp.StatusCode >= 300 {
		return "", -1, readDockerAPIError("start stream exec on "+nameOrID, startResp)
	}
	var stderrBuf bytes.Buffer
	if err := demuxDockerStreamToWriters(startResp.Body, stdout, &stderrBuf); err != nil {
		return strings.TrimSpace(stderrBuf.String()), -1, err
	}
	code, err := c.inspectExecExitCode(ctx, execID)
	return strings.TrimSpace(stderrBuf.String()), code, err
}

// ExecWithStdin 通过 Docker HTTP Upgrade 劫持连接向容器内命令流式输入 stdin（用于 pg_restore 备份恢复）。
func (c *HTTPClient) ExecWithStdin(ctx context.Context, nameOrID string, cmd []string, env []string, stdin io.Reader) (ExecResult, error) {
	if c.dialTarget == nil {
		return ExecResult{}, errors.New("docker hijack stream is not supported for this docker host")
	}
	execID, err := c.createExec(ctx, nameOrID, cmd, env, true)
	if err != nil {
		return ExecResult{}, err
	}

	conn, err := c.dialTarget(ctx)
	if err != nil {
		return ExecResult{}, fmt.Errorf("dial docker host for exec stream: %w", err)
	}
	defer conn.Close()

	stopCancel := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopCancel()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	payload := `{"Detach":false,"Tty":false}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/exec/"+url.PathEscape(execID)+"/start", strings.NewReader(payload))
	if err != nil {
		return ExecResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "tcp")
	if err := req.Write(conn); err != nil {
		return ExecResult{}, fmt.Errorf("send exec hijack request: %w", err)
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodPost})
	if err != nil {
		return ExecResult{}, fmt.Errorf("read exec hijack response: %w", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols && resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return ExecResult{}, readDockerAPIError("exec hijack "+nameOrID, resp)
	}

	writeErrCh := make(chan error, 1)
	go func() {
		var copyErr error
		if stdin != nil {
			_, copyErr = io.Copy(conn, stdin)
		}
		if cw, ok := conn.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
		writeErrCh <- copyErr
	}()

	var stdoutBuf, stderrBuf bytes.Buffer
	readErr := demuxDockerStreamToWriters(br, &stdoutBuf, &stderrBuf)
	writeErr := <-writeErrCh
	if writeErr != nil && readErr == nil {
		readErr = writeErr
	}
	if readErr != nil {
		return ExecResult{Stdout: strings.TrimSpace(stdoutBuf.String()), Stderr: strings.TrimSpace(stderrBuf.String())}, readErr
	}

	code, err := c.inspectExecExitCode(ctx, execID)
	return ExecResult{
		ExitCode: code,
		Stdout:   strings.TrimSpace(stdoutBuf.String()),
		Stderr:   strings.TrimSpace(stderrBuf.String()),
	}, err
}

func (c *HTTPClient) createExec(ctx context.Context, nameOrID string, cmd []string, env []string, attachStdin bool) (string, error) {
	createBody := map[string]any{
		"AttachStdin":  attachStdin,
		"AttachStdout": true,
		"AttachStderr": true,
		"Tty":          false,
		"Cmd":          cmd,
	}
	if len(env) > 0 {
		createBody["Env"] = env
	}
	createResp, err := c.doJSON(ctx, c.httpClient, http.MethodPost, "/containers/"+url.PathEscape(nameOrID)+"/exec", createBody)
	if err != nil {
		return "", err
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated && createResp.StatusCode != http.StatusOK {
		return "", readDockerAPIError("create exec on "+nameOrID, createResp)
	}
	var created struct {
		ID string `json:"Id"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		return "", fmt.Errorf("decode exec create response: %w", err)
	}
	return created.ID, nil
}

func (c *HTTPClient) inspectExecExitCode(ctx context.Context, execID string) (int, error) {
	inspectResp, err := c.do(ctx, c.httpClient, http.MethodGet, "/exec/"+url.PathEscape(execID)+"/json", nil)
	if err != nil {
		return -1, err
	}
	defer inspectResp.Body.Close()
	if inspectResp.StatusCode >= 300 {
		return -1, readDockerAPIError("inspect exec", inspectResp)
	}
	var inspected struct {
		ExitCode int `json:"ExitCode"`
	}
	if err := json.NewDecoder(inspectResp.Body).Decode(&inspected); err != nil {
		return -1, err
	}
	return inspected.ExitCode, nil
}

// Exec 在指定容器内执行非交互命令并返回分离后的 stdout、stderr 与退出码。
func (c *HTTPClient) Exec(ctx context.Context, nameOrID string, cmd []string, env []string) (ExecResult, error) {
	createBody := map[string]any{
		"AttachStdout": true,
		"AttachStderr": true,
		"Tty":          false,
		"Cmd":          cmd,
	}
	if len(env) > 0 {
		createBody["Env"] = env
	}
	createResp, err := c.doJSON(ctx, c.httpClient, http.MethodPost, "/containers/"+url.PathEscape(nameOrID)+"/exec", createBody)
	if err != nil {
		return ExecResult{}, err
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated && createResp.StatusCode != http.StatusOK {
		return ExecResult{}, readDockerAPIError("create exec on "+nameOrID, createResp)
	}
	var created struct {
		ID string `json:"Id"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		return ExecResult{}, fmt.Errorf("decode exec create response: %w", err)
	}

	startResp, err := c.doJSON(ctx, c.streamHTTP, http.MethodPost, "/exec/"+url.PathEscape(created.ID)+"/start", map[string]any{
		"Detach": false,
		"Tty":    false,
	})
	if err != nil {
		return ExecResult{}, err
	}
	defer startResp.Body.Close()
	if startResp.StatusCode >= 300 {
		return ExecResult{}, readDockerAPIError("start exec on "+nameOrID, startResp)
	}
	stdout, stderr, err := demuxDockerStream(startResp.Body)
	if err != nil {
		return ExecResult{}, fmt.Errorf("read exec output: %w", err)
	}

	inspectResp, err := c.do(ctx, c.httpClient, http.MethodGet, "/exec/"+url.PathEscape(created.ID)+"/json", nil)
	if err != nil {
		return ExecResult{Stdout: stdout, Stderr: stderr}, err
	}
	defer inspectResp.Body.Close()
	if inspectResp.StatusCode >= 300 {
		return ExecResult{Stdout: stdout, Stderr: stderr}, readDockerAPIError("inspect exec", inspectResp)
	}
	var inspected struct {
		ExitCode int `json:"ExitCode"`
	}
	if err := json.NewDecoder(inspectResp.Body).Decode(&inspected); err != nil {
		return ExecResult{Stdout: stdout, Stderr: stderr}, err
	}
	return ExecResult{
		ExitCode: inspected.ExitCode,
		Stdout:   strings.TrimSpace(stdout),
		Stderr:   strings.TrimSpace(stderr),
	}, nil
}

func (c *HTTPClient) do(ctx context.Context, client *http.Client, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}

func (c *HTTPClient) doJSON(ctx context.Context, client *http.Client, method, path string, payload any) (*http.Response, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return client.Do(req)
}

func readDockerAPIError(op string, resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	var apiErr struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &apiErr); err == nil && strings.TrimSpace(apiErr.Message) != "" {
		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("%s: %w: %s", op, ErrNotFound, strings.TrimSpace(apiErr.Message))
		}
		return fmt.Errorf("%s: %s", op, strings.TrimSpace(apiErr.Message))
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		text = resp.Status
	}
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%s: %w: %s", op, ErrNotFound, text)
	}
	return fmt.Errorf("%s: %s", op, text)
}

// demuxDockerStream 解复用 Docker Engine API 的 8 字节帧头流（stdout=1, stderr=2）。
// 若响应不是标准多路复用帧头，则安全回退为原始 stdout 读取。
func demuxDockerStream(r io.Reader) (string, string, error) {
	raw, err := io.ReadAll(io.LimitReader(r, 4*1024*1024))
	if err != nil {
		return "", "", err
	}
	if len(raw) == 0 {
		return "", "", nil
	}
	if len(raw) < 8 || (raw[0] != 0 && raw[0] != 1 && raw[0] != 2) || raw[1] != 0 || raw[2] != 0 || raw[3] != 0 {
		return string(raw), "", nil
	}

	var stdout, stderr bytes.Buffer
	offset := 0
	for offset+8 <= len(raw) {
		streamType := raw[offset]
		if (streamType != 0 && streamType != 1 && streamType != 2) || raw[offset+1] != 0 || raw[offset+2] != 0 || raw[offset+3] != 0 {
			return string(raw), "", nil
		}
		size := int(binary.BigEndian.Uint32(raw[offset+4 : offset+8]))
		offset += 8
		if size < 0 || offset+size > len(raw) {
			return string(raw), "", nil
		}
		chunk := raw[offset : offset+size]
		offset += size
		if streamType == 2 {
			stderr.Write(chunk)
		} else {
			stdout.Write(chunk)
		}
	}
	return stdout.String(), stderr.String(), nil
}

// demuxDockerStreamToWriters 流式解复用 Docker 8 字节帧头，直接拷贝至目标 stdout/stderr writer（零全量缓冲）。
func demuxDockerStreamToWriters(r io.Reader, stdout io.Writer, stderr io.Writer) error {
	header := make([]byte, 8)
	for {
		_, err := io.ReadFull(r, header)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return nil
		}
		if err != nil {
			return err
		}
		streamType := header[0]
		if (streamType != 0 && streamType != 1 && streamType != 2) || header[1] != 0 || header[2] != 0 || header[3] != 0 {
			// 非多路复用流（例如单路纯文本响应）：写出已读 8 字节并透传剩余内容
			if stdout != nil {
				if _, wErr := stdout.Write(header); wErr != nil {
					return wErr
				}
				_, cErr := io.Copy(stdout, r)
				return cErr
			}
			_, cErr := io.Copy(io.Discard, r)
			return cErr
		}
		size := int64(binary.BigEndian.Uint32(header[4:8]))
		dst := stdout
		if streamType == 2 {
			dst = stderr
		}
		if dst == nil {
			dst = io.Discard
		}
		if _, err := io.CopyN(dst, r, size); err != nil {
			return err
		}
	}
}
