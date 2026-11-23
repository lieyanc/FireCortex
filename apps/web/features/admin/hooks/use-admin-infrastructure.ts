"use client";

import type {
  BackupRecordDTO,
  DangerRecreateContainerRequest,
  DangerRestoreBackupRequest,
  DangerSwitchDatabaseRequest,
  InfraConfigResponse,
  InfraJobEventDTO,
  InfraJobSnapshotResponse,
  InfraOverviewResponse,
  PatchInfraConfigRequest,
} from "@firecortex/api-contract";
import * as React from "react";
import { toast } from "sonner";
import { ApiError } from "@/shared/api/http-client";
import { useAuthSession } from "@/shared/auth/auth-session-context";
import {
  createAdminInfraBackup,
  dangerRecreateAdminContainer,
  dangerRestoreAdminBackup,
  dangerSwitchAdminDatabase,
  deleteAdminInfraBackup,
  downloadAdminInfraBackup,
  getAdminInfraComposePreview,
  getAdminInfraConfig,
  getAdminInfraOverview,
  getAdminInfraServiceLogs,
  listAdminInfraBackups,
  patchAdminInfraConfig,
  pollServerHealthReady,
  streamAdminInfraJobEvents,
  triggerAdminInfraServiceAction,
} from "../api/infrastructure";

function readErr(error: unknown): string {
  if (error instanceof ApiError) {
    return error.rawMessage || error.message;
  }
  if (error instanceof Error) {
    return error.message;
  }
  return "Request failed";
}

export function useAdminInfrastructure() {
  const { accessToken, user } = useAuthSession();
  const isSuperAdmin = user?.role === "superadmin";

  const [loading, setLoading] = React.useState(true);
  const [refreshing, setRefreshing] = React.useState(false);
  const [overview, setOverview] = React.useState<InfraOverviewResponse | null>(null);
  const [backups, setBackups] = React.useState<BackupRecordDTO[]>([]);
  const [infraConfig, setInfraConfig] = React.useState<InfraConfigResponse | null>(null);

  const [activeJob, setActiveJob] = React.useState<InfraJobSnapshotResponse | null>(null);
  const [jobEvents, setJobEvents] = React.useState<InfraJobEventDTO[]>([]);
  const jobAbortRef = React.useRef<AbortController | null>(null);

  const [logsTarget, setLogsTarget] = React.useState<{ serviceID: string; name: string } | null>(null);
  const [logsContent, setLogsContent] = React.useState("");
  const [logsLoading, setLogsLoading] = React.useState(false);

  const [composeOpen, setComposeOpen] = React.useState(false);
  const [composeContent, setComposeContent] = React.useState("");
  const [composeLoading, setComposeLoading] = React.useState(false);

  const [creatingBackup, setCreatingBackup] = React.useState(false);
  const [savingConfig, setSavingConfig] = React.useState(false);
  const [dangerBusy, setDangerBusy] = React.useState(false);
  const [restarting, setRestarting] = React.useState(false);

  const loadAll = React.useCallback(
    async (silent = false) => {
      if (!accessToken || !isSuperAdmin) {
        setLoading(false);
        return;
      }
      if (!silent) {
        setRefreshing(true);
      }
      try {
        const [ov, bk, cfg] = await Promise.all([
          getAdminInfraOverview(accessToken),
          listAdminInfraBackups(accessToken),
          getAdminInfraConfig(accessToken),
        ]);
        setOverview(ov);
        setBackups(bk.backups);
        setInfraConfig(cfg);
      } catch (err) {
        toast.error(readErr(err));
      } finally {
        setLoading(false);
        setRefreshing(false);
      }
    },
    [accessToken, isSuperAdmin],
  );

  React.useEffect(() => {
    void loadAll();
    return () => {
      jobAbortRef.current?.abort();
    };
  }, [loadAll]);

  const watchRestartAndReload = React.useCallback(() => {
    setRestarting(true);
    window.setTimeout(() => {
      const timer = window.setInterval(() => {
        void (async () => {
          const ok = await pollServerHealthReady();
          if (ok) {
            window.clearInterval(timer);
            window.location.reload();
          }
        })();
      }, 1500);
    }, 1500);
  }, []);

  const attachJobStream = React.useCallback(
    (snap: InfraJobSnapshotResponse) => {
      if (!accessToken) return;
      jobAbortRef.current?.abort();
      const ctrl = new AbortController();
      jobAbortRef.current = ctrl;
      setActiveJob(snap);
      setJobEvents(snap.events);

      void streamAdminInfraJobEvents(
        accessToken,
        snap.jobID,
        (evt) => {
          setJobEvents((prev) => {
            const exists = prev.some(
              (item) => item.timestamp === evt.timestamp && item.step === evt.step && item.message === evt.message,
            );
            return exists ? prev : [...prev, evt];
          });
          if (evt.status === "failed") {
            setActiveJob((prev) => (prev ? { ...prev, state: "failed", error: evt.message } : prev));
            toast.error(evt.message);
            void loadAll(true);
          } else if (evt.step === "complete" && evt.status === "done") {
            setActiveJob((prev) => (prev ? { ...prev, state: "done" } : prev));
            toast.success(evt.message);
            void loadAll(true);
          }
        },
        ctrl.signal,
      ).catch(() => {
        // Stream ended or aborted.
      });
    },
    [accessToken, loadAll],
  );

  const handleServiceAction = React.useCallback(
    async (serviceID: string, action: "start" | "stop" | "restart" | "pull") => {
      if (!accessToken) return;
      try {
        const snap = await triggerAdminInfraServiceAction(accessToken, serviceID, action);
        attachJobStream(snap);
      } catch (err) {
        toast.error(readErr(err));
      }
    },
    [accessToken, attachJobStream],
  );

  const handleOpenLogs = React.useCallback(
    async (serviceID: string, name: string) => {
      if (!accessToken) return;
      setLogsTarget({ serviceID, name });
      setLogsLoading(true);
      try {
        const res = await getAdminInfraServiceLogs(accessToken, serviceID, 300);
        setLogsContent(res.logs);
      } catch (err) {
        setLogsContent(readErr(err));
      } finally {
        setLogsLoading(false);
      }
    },
    [accessToken],
  );

  const handleOpenCompose = React.useCallback(async () => {
    if (!accessToken) return;
    setComposeOpen(true);
    setComposeLoading(true);
    try {
      const res = await getAdminInfraComposePreview(accessToken);
      setComposeContent(res.content);
    } catch (err) {
      toast.error(readErr(err));
      setComposeContent("");
    } finally {
      setComposeLoading(false);
    }
  }, [accessToken]);

  const handleCreateBackup = React.useCallback(async () => {
    if (!accessToken) return;
    setCreatingBackup(true);
    try {
      const rec = await createAdminInfraBackup(accessToken);
      toast.success(`Backup created: ${rec.fileName}`);
      await loadAll(true);
    } catch (err) {
      toast.error(readErr(err));
    } finally {
      setCreatingBackup(false);
    }
  }, [accessToken, loadAll]);

  const handleDownloadBackup = React.useCallback(
    async (backupID: string) => {
      if (!accessToken) return;
      try {
        const blob = await downloadAdminInfraBackup(accessToken, backupID);
        const url = URL.createObjectURL(blob);
        const anchor = document.createElement("a");
        anchor.href = url;
        anchor.download = backupID;
        anchor.click();
        URL.revokeObjectURL(url);
      } catch (err) {
        toast.error(readErr(err));
      }
    },
    [accessToken],
  );

  const handleDeleteBackup = React.useCallback(
    async (backupID: string) => {
      if (!accessToken) return;
      try {
        await deleteAdminInfraBackup(accessToken, backupID);
        toast.success(`Deleted ${backupID}`);
        await loadAll(true);
      } catch (err) {
        toast.error(readErr(err));
      }
    },
    [accessToken, loadAll],
  );

  const handleSaveConfig = React.useCallback(
    async (payload: PatchInfraConfigRequest) => {
      if (!accessToken) return;
      setSavingConfig(true);
      try {
        const res = await patchAdminInfraConfig(accessToken, payload);
        setInfraConfig(res.config);
        toast.success(res.restartScheduled ? "Config saved; restarting smoothly..." : "Config saved");
        if (res.restartScheduled) {
          watchRestartAndReload();
        } else {
          await loadAll(true);
        }
      } catch (err) {
        toast.error(readErr(err));
      } finally {
        setSavingConfig(false);
      }
    },
    [accessToken, loadAll, watchRestartAndReload],
  );

  const handleDangerRestore = React.useCallback(
    async (payload: DangerRestoreBackupRequest): Promise<boolean> => {
      if (!accessToken) return false;
      setDangerBusy(true);
      try {
        const res = await dangerRestoreAdminBackup(accessToken, payload);
        toast.success(`Restored ${res.restoredBackupID} (safety snapshot: ${res.preRestoreBackup})`);
        if (res.restartScheduled) {
          watchRestartAndReload();
        }
        return true;
      } catch (err) {
        toast.error(readErr(err));
        return false;
      } finally {
        setDangerBusy(false);
      }
    },
    [accessToken, watchRestartAndReload],
  );

  const handleDangerSwitchDatabase = React.useCallback(
    async (payload: DangerSwitchDatabaseRequest): Promise<boolean> => {
      if (!accessToken) return false;
      setDangerBusy(true);
      try {
        const res = await dangerSwitchAdminDatabase(accessToken, payload);
        toast.success("Database configuration switched");
        if (res.restartScheduled) {
          watchRestartAndReload();
        }
        return true;
      } catch (err) {
        toast.error(readErr(err));
        return false;
      } finally {
        setDangerBusy(false);
      }
    },
    [accessToken, watchRestartAndReload],
  );

  const handleDangerRecreate = React.useCallback(
    async (payload: DangerRecreateContainerRequest): Promise<boolean> => {
      if (!accessToken) return false;
      setDangerBusy(true);
      try {
        const snap = await dangerRecreateAdminContainer(accessToken, payload);
        attachJobStream(snap);
        return true;
      } catch (err) {
        toast.error(readErr(err));
        return false;
      } finally {
        setDangerBusy(false);
      }
    },
    [accessToken, attachJobStream],
  );

  return {
    isSuperAdmin,
    loading,
    refreshing,
    overview,
    backups,
    infraConfig,
    activeJob,
    jobEvents,
    logsTarget,
    setLogsTarget,
    logsContent,
    logsLoading,
    composeOpen,
    setComposeOpen,
    composeContent,
    composeLoading,
    creatingBackup,
    savingConfig,
    dangerBusy,
    restarting,
    loadAll,
    handleServiceAction,
    handleOpenLogs,
    handleOpenCompose,
    handleCreateBackup,
    handleDownloadBackup,
    handleDeleteBackup,
    handleSaveConfig,
    handleDangerRestore,
    handleDangerSwitchDatabase,
    handleDangerRecreate,
  };
}
