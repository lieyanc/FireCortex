"use client";

import * as React from "react";

import {
  applyAdminSystemUpdate,
  checkAdminSystemUpdate,
  dismissAdminSystemUpdate,
  getAdminSystemUpdateStatus,
} from "@/features/admin/api/system-update";
import type {
  AdminSystemUpdateState,
  AdminSystemUpdateStatus,
} from "@/features/admin/api/system-update.types";
import { useAuthSession } from "@/shared/auth/auth-session-context";

// 管理端 OTA 更新状态：侧边栏与关于页共享同一份后端状态，忙碌时轮询，安装后等待新版本上线并刷新页面。

export type SystemUpdateAction = "check" | "apply" | "dismiss";

export type SystemUpdateSnapshot = {
  status: AdminSystemUpdateStatus | null;
  // 尚未拿到任何状态时最近一次加载失败的错误。
  loadError: unknown;
  pendingAction: SystemUpdateAction | null;
  // 已触发安装，正在跟随检查、下载、重启直至新版本上线。
  following: boolean;
  // 跟随重启期间服务端长时间不可达。
  restartTimedOut: boolean;
};

type FollowState = {
  startedAt: number;
  lastCheck: string;
  sawBusy: boolean;
  lastReachableAt: number;
};

const POLL_INTERVAL_MS = 1_500;
const POLL_REQUEST_TIMEOUT_MS = 10_000;
// apply 在后台协程中启动，紧随其后的状态可能仍是旧的 idle/failed。
const APPLY_START_GRACE_MS = 10_000;
const RESTART_TIMEOUT_MS = 3 * 60_000;
const BUSY_STATES = new Set<AdminSystemUpdateState>(["checking", "downloading", "applying"]);

const INITIAL_SNAPSHOT: SystemUpdateSnapshot = {
  status: null,
  loadError: null,
  pendingAction: null,
  following: false,
  restartTimedOut: false,
};

let snapshot = INITIAL_SNAPSHOT;
const listeners = new Set<() => void>();
let latestAccessToken = "";
let pendingLoad: Promise<void> | null = null;
let pollTimer: ReturnType<typeof setTimeout> | null = null;
let requestSequence = 0;
let appliedSequence = 0;
let baselineVersion = "";
let follow: FollowState | null = null;
let reloading = false;

export function isSystemUpdateBusy(state: AdminSystemUpdateState | undefined): boolean {
  return state !== undefined && BUSY_STATES.has(state);
}

export function hasPendingSystemUpdate(status: AdminSystemUpdateStatus | null): boolean {
  return Boolean(status && (status.hasUpdate || status.state === "ready"));
}

export function formatSystemVersion(version: string): string {
  const value = version.trim();
  return /^\d/.test(value) ? `v${value}` : value;
}

export function systemUpdateReleaseURL(status: AdminSystemUpdateStatus): string {
  const tag = status.channel === "dev" ? "dev-latest" : `v${status.latestVersion.replace(/^v/i, "")}`;
  return `https://github.com/${status.repo}/releases/tag/${encodeURIComponent(tag)}`;
}

function setSnapshot(next: Partial<SystemUpdateSnapshot>) {
  snapshot = { ...snapshot, ...next };
  for (const listener of listeners) listener();
}

function shouldPoll(): boolean {
  return follow !== null || isSystemUpdateBusy(snapshot.status?.state);
}

function schedulePoll() {
  if (pollTimer) {
    clearTimeout(pollTimer);
    pollTimer = null;
  }
  if (reloading || listeners.size === 0 || !shouldPoll()) return;
  pollTimer = setTimeout(() => {
    pollTimer = null;
    void refreshSystemUpdateStatus();
  }, POLL_INTERVAL_MS);
}

function reloadPage() {
  if (reloading) return;
  reloading = true;
  window.location.reload();
}

function startFollowing(status: AdminSystemUpdateStatus) {
  const now = Date.now();
  follow = {
    startedAt: now,
    lastCheck: status.lastCheck,
    sawBusy: isSystemUpdateBusy(status.state),
    lastReachableAt: now,
  };
}

function receiveStatus(status: AdminSystemUpdateStatus, sequence: number) {
  if (sequence < appliedSequence) {
    schedulePoll();
    return;
  }
  appliedSequence = sequence;

  if (!baselineVersion) {
    baselineVersion = status.currentVersion;
  } else if (status.currentVersion !== baselineVersion) {
    if (follow) {
      // 页面资源内嵌在二进制里，新版本上线后必须整页刷新。
      reloadPage();
      return;
    }
    baselineVersion = status.currentVersion;
  }

  let displayed = status;
  if (follow) {
    const now = Date.now();
    follow.lastReachableAt = now;
    if (isSystemUpdateBusy(status.state)) {
      follow.sawBusy = true;
    } else if (follow.sawBusy || status.lastCheck !== follow.lastCheck || now - follow.startedAt > APPLY_START_GRACE_MS) {
      follow = null;
    } else {
      displayed = { ...status, state: "checking", error: "" };
    }
  } else if (status.state === "applying") {
    // 由后台自动更新或其他管理员发起的安装，同样跟随到重启完成。
    startFollowing(status);
  }

  setSnapshot({ status: displayed, loadError: null, following: follow !== null, restartTimedOut: false });
  schedulePoll();
}

function receiveLoadFailure(error: unknown) {
  if (follow) {
    // 重启期间请求失败属于预期，继续轮询直到新版本上线或超时。
    if (Date.now() - follow.lastReachableAt > RESTART_TIMEOUT_MS) {
      follow = null;
      setSnapshot({ following: false, restartTimedOut: true });
    }
    schedulePoll();
    return;
  }
  if (!snapshot.status) {
    setSnapshot({ loadError: error });
  }
}

export function refreshSystemUpdateStatus(accessToken = latestAccessToken): Promise<void> {
  latestAccessToken = accessToken;
  if (pendingLoad) return pendingLoad;
  if (!accessToken) return Promise.resolve();

  const sequence = ++requestSequence;
  pendingLoad = getAdminSystemUpdateStatus(accessToken, { signal: AbortSignal.timeout(POLL_REQUEST_TIMEOUT_MS) })
    .then((status) => receiveStatus(status, sequence))
    .catch(receiveLoadFailure)
    .finally(() => {
      pendingLoad = null;
    });
  return pendingLoad;
}

async function runAction(
  action: SystemUpdateAction,
  accessToken: string,
  request: (accessToken: string) => Promise<AdminSystemUpdateStatus>,
): Promise<AdminSystemUpdateStatus> {
  latestAccessToken = accessToken;
  const sequence = ++requestSequence;
  setSnapshot({ pendingAction: action });
  try {
    const status = await request(accessToken);
    if (action === "apply") startFollowing(snapshot.status ?? status);
    receiveStatus(status, sequence);
    return status;
  } catch (error) {
    void refreshSystemUpdateStatus(accessToken);
    throw error;
  } finally {
    setSnapshot({ pendingAction: null });
  }
}

export function checkSystemUpdate(accessToken: string): Promise<AdminSystemUpdateStatus> {
  return runAction("check", accessToken, checkAdminSystemUpdate);
}

export function applySystemUpdate(accessToken: string): Promise<AdminSystemUpdateStatus> {
  return runAction("apply", accessToken, applyAdminSystemUpdate);
}

export function dismissSystemUpdate(accessToken: string): Promise<AdminSystemUpdateStatus> {
  return runAction("dismiss", accessToken, dismissAdminSystemUpdate);
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  if (listeners.size === 1) schedulePoll();
  return () => {
    listeners.delete(listener);
    if (listeners.size === 0 && pollTimer) {
      clearTimeout(pollTimer);
      pollTimer = null;
    }
  };
}

function getSnapshot(): SystemUpdateSnapshot {
  return snapshot;
}

function getServerSnapshot(): SystemUpdateSnapshot {
  return INITIAL_SNAPSHOT;
}

// 挂载时拉取一次状态；多个组件同时挂载时共享同一个请求。
export function useSystemUpdate(): SystemUpdateSnapshot {
  const { accessToken } = useAuthSession();

  React.useEffect(() => {
    if (accessToken) void refreshSystemUpdateStatus(accessToken);
  }, [accessToken]);

  return React.useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}
