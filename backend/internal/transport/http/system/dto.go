package system

import "github.com/lieyanc/FireCortex/backend/internal/infra/updater"

// UpdateStatusResponse 是自更新状态快照。
type UpdateStatusResponse struct {
	// State 取值 idle | checking | downloading | ready | applying | failed。
	State          string `json:"state" enums:"idle,checking,downloading,ready,applying,failed"`
	CurrentVersion string `json:"currentVersion"`
	CurrentCommit  string `json:"currentCommit"`
	LatestVersion  string `json:"latestVersion"`
	HasUpdate      bool   `json:"hasUpdate"`
	IsPrerelease   bool   `json:"isPrerelease"`
	// Progress 是整体进度（0-100），DownloadProgress 是下载阶段进度（0-100）。
	Progress         float64 `json:"progress"`
	DownloadProgress float64 `json:"downloadProgress"`
	Error            string  `json:"error"`
	// LastCheck 是最近一次检查时间（RFC3339），从未检查时为空。
	LastCheck    string `json:"lastCheck"`
	ReleaseNotes string `json:"releaseNotes"`
	// Enabled 表示后台自动检查是否开启；手动检查不受影响。
	Enabled bool   `json:"enabled"`
	Channel string `json:"channel" enums:"stable,dev"`
	Source  string `json:"source" enums:"github,proxy"`
	Repo    string `json:"repo"`
}

type UpdateStatusResponseDoc struct {
	ErrorMsg string               `json:"errorMsg"`
	Data     UpdateStatusResponse `json:"data"`
}

func toUpdateStatusResponse(status updater.Status) UpdateStatusResponse {
	return UpdateStatusResponse{
		State:            status.State,
		CurrentVersion:   status.CurrentVersion,
		CurrentCommit:    status.CurrentCommit,
		LatestVersion:    status.LatestVersion,
		HasUpdate:        status.HasUpdate,
		IsPrerelease:     status.IsPrerelease,
		Progress:         status.Progress,
		DownloadProgress: status.DownloadProgress,
		Error:            status.Error,
		LastCheck:        status.LastCheck,
		ReleaseNotes:     status.ReleaseNotes,
		Enabled:          status.Enabled,
		Channel:          status.Channel,
		Source:           status.Source,
		Repo:             status.Repo,
	}
}
