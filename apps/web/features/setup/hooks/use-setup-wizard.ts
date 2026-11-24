"use client";

import type {
  ApplyRequest,
  DirInspectionDTO,
  JobEventResponse,
  JobSnapshotResponse,
  PreflightResponse,
  SetupDefaultsDTO,
  TestExternalResponse,
} from "@firecortex/api-contract";
import * as React from "react";
import { ApiError } from "@/shared/api/http-client";
import {
  checkNormalServerReady,
  getSetupJob,
  getSetupStatus,
  inspectSetupDir,
  runSetupPreflight,
  startSetupApply,
  subscribeSetupEvents,
  testSetupExternal,
  verifySetupToken,
} from "../api/setup";

export type SetupStep = "unlock" | "environment" | "database" | "site" | "progress";
export type DatabaseMode = "managed" | "external";
export type CacheMode = "managed_redis" | "memory" | "external_redis";
export type RuntimeEnvMode = "prod" | "dev";

export type SetupFormState = {
  dockerHost: string;
  registryMirror: string;
  databaseMode: DatabaseMode;
  cacheMode: CacheMode;
  dataDir: string;
  postgresDataDir: string;
  redisDataDir: string;
  backupDir: string;
  postgresImage: string;
  postgresPort: number;
  postgresMemoryLimitMB: number;
  redisImage: string;
  redisPort: number;
  redisMemoryLimitMB: number;
  adoptExistingPGData: boolean;
  existingPGUser: string;
  existingPGPassword: string;
  existingDataEncryptionKey: string;
  existingPGDatabase: string;
  existingRedisPassword: string;
  composeSync: boolean;
  externalPostgresDSN: string;
  externalRedisAddr: string;
  externalRedisUser: string;
  externalRedisPass: string;
  externalRedisDB: number;
  externalRedisTLS: boolean;
  externalRedisSkipTLS: boolean;
  env: RuntimeEnvMode;
  publicBaseURL: string;
  corsAllowOrigin: string;
  adminUsername: string;
  adminDisplayName: string;
  adminPassword: string;
  adminPasswordConfirm: string;
};

const INITIAL_FORM: SetupFormState = {
  dockerHost: "",
  registryMirror: "",
  databaseMode: "managed",
  cacheMode: "managed_redis",
  dataDir: "./data/managed",
  postgresDataDir: "",
  redisDataDir: "",
  backupDir: "",
  postgresImage: "pgvector/pgvector:pg18-trixie",
  postgresPort: 5432,
  postgresMemoryLimitMB: 0,
  redisImage: "redis:7-alpine",
  redisPort: 6379,
  redisMemoryLimitMB: 0,
  adoptExistingPGData: false,
  existingPGUser: "",
  existingPGPassword: "",
  existingDataEncryptionKey: "",
  existingPGDatabase: "",
  existingRedisPassword: "",
  composeSync: true,
  externalPostgresDSN: "",
  externalRedisAddr: "127.0.0.1:6379",
  externalRedisUser: "",
  externalRedisPass: "",
  externalRedisDB: 0,
  externalRedisTLS: false,
  externalRedisSkipTLS: false,
  env: "dev",
  publicBaseURL: "",
  corsAllowOrigin: "",
  adminUsername: "admin",
  adminDisplayName: "System Admin",
  adminPassword: "",
  adminPasswordConfirm: "",
};

function readErrorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.rawMessage || error.message;
  }
  if (error instanceof Error) {
    return error.message;
  }
  return "Unexpected error";
}

function mergeDefaultsIntoForm(prev: SetupFormState, defaults: SetupDefaultsDTO): SetupFormState {
  return {
    ...prev,
    dockerHost: prev.dockerHost || defaults.dockerHost || "",
    registryMirror: prev.registryMirror || defaults.registryMirror || "",
    dataDir: defaults.dataDir || prev.dataDir,
    postgresImage: defaults.postgresImage || prev.postgresImage,
    postgresPort: defaults.postgresPort || prev.postgresPort,
    redisImage: defaults.redisImage || prev.redisImage,
    redisPort: defaults.redisPort || prev.redisPort,
  };
}

export function useSetupWizard() {
  const [loading, setLoading] = React.useState(true);
  const [alreadyInitialized, setAlreadyInitialized] = React.useState(false);
  const [step, setStep] = React.useState<SetupStep>("unlock");
  const [version, setVersion] = React.useState("");

  const [tokenInput, setTokenInput] = React.useState("");
  const [verifying, setVerifying] = React.useState(false);
  const [authError, setAuthError] = React.useState<string | null>(null);

  const [form, setForm] = React.useState<SetupFormState>(INITIAL_FORM);
  const [preflight, setPreflight] = React.useState<PreflightResponse | null>(null);
  const [preflighting, setPreflighting] = React.useState(false);

  const [dirInspection, setDirInspection] = React.useState<DirInspectionDTO | null>(null);
  const [inspectingDir, setInspectingDir] = React.useState(false);

  const [externalResult, setExternalResult] = React.useState<TestExternalResponse | null>(null);
  const [testingExternal, setTestingExternal] = React.useState(false);

  const [submitting, setSubmitting] = React.useState(false);
  const [submitError, setSubmitError] = React.useState<string | null>(null);
  const [job, setJob] = React.useState<JobSnapshotResponse | null>(null);
  const [events, setEvents] = React.useState<JobEventResponse[]>([]);
  const [serverReady, setServerReady] = React.useState(false);

  const sseCleanupRef = React.useRef<(() => void) | null>(null);

  const updateForm = React.useCallback(<K extends keyof SetupFormState>(key: K, value: SetupFormState[K]) => {
    setForm((prev) => ({ ...prev, [key]: value }));
  }, []);

  const loadPreflight = React.useCallback(async (dockerHost?: string) => {
    setPreflighting(true);
    try {
      const result = await runSetupPreflight(dockerHost);
      setPreflight(result);
      setDirInspection(result.defaultDir);
      setForm((prev) => {
        const next = mergeDefaultsIntoForm(prev, result.defaults);
        if (!result.docker.available && prev.databaseMode === "managed") {
          return { ...next, databaseMode: "external", cacheMode: "memory" };
        }
        return next;
      });
    } finally {
      setPreflighting(false);
    }
  }, []);

  const startEventStream = React.useCallback(() => {
    sseCleanupRef.current?.();
    sseCleanupRef.current = subscribeSetupEvents(
      (evt) => {
        setEvents((prev) => {
          const exists = prev.some(
            (item) => item.timestamp === evt.timestamp && item.step === evt.step && item.message === evt.message,
          );
          return exists ? prev : [...prev, evt];
        });
        if (evt.status === "failed") {
          void getSetupJob()
            .then((snap) => setJob(snap))
            .catch(() => undefined);
        }
        if (evt.step === "restart" && evt.status === "done") {
          void getSetupJob()
            .then((snap) => setJob(snap))
            .catch(() => {
              setJob((prev) => (prev ? { ...prev, state: "done" } : null));
            });
        }
      },
      () => {
        // SSE disconnects naturally when Setup server shuts down to re-exec into normal mode.
      },
    );
  }, []);

  React.useEffect(() => {
    if (typeof window !== "undefined") {
      const origin = window.location.origin;
      setForm((prev) => ({
        ...prev,
        publicBaseURL: prev.publicBaseURL || origin,
        env: origin.startsWith("https://") ? "prod" : "dev",
      }));
    }

    let cancelled = false;
    void (async () => {
      try {
        const status = await getSetupStatus();
        if (cancelled) return;
        if (!status.needsSetup) {
          setAlreadyInitialized(true);
          window.location.replace("/login");
          return;
        }
        setVersion(status.version);
        if (status.authenticated) {
          if (status.defaults) {
            const loadedDefaults = status.defaults;
            setForm((prev) => mergeDefaultsIntoForm(prev, loadedDefaults));
          }
          if (status.jobState === "running" || status.jobState === "done") {
            const snap = await getSetupJob();
            if (!cancelled) {
              setJob(snap);
              setEvents(snap.events);
              setStep("progress");
              if (snap.state === "running") {
                startEventStream();
              }
            }
          } else {
            await loadPreflight();
            if (!cancelled) {
              setStep("environment");
            }
          }
        } else {
          setStep("unlock");
        }
      } catch (err) {
        if (cancelled) return;
        if (err instanceof ApiError && err.status === 404) {
          setAlreadyInitialized(true);
          window.location.replace("/login");
          return;
        }
        setAuthError(readErrorMessage(err));
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    })();

    return () => {
      cancelled = true;
      sseCleanupRef.current?.();
    };
  }, [loadPreflight, startEventStream]);

  // Poll /healthz after setup completes so the browser redirects automatically once full server starts.
  const jobDone =
    job?.state === "done" || events.some((evt) => evt.step === "restart" && evt.status === "done");
  React.useEffect(() => {
    if (!jobDone || serverReady) {
      return;
    }
    let stopped = false;
    const timer = window.setInterval(() => {
      void (async () => {
        const ready = await checkNormalServerReady();
        if (ready && !stopped) {
          setServerReady(true);
          window.clearInterval(timer);
          window.setTimeout(() => {
            window.location.replace("/login");
          }, 900);
        }
      })();
    }, 1500);
    return () => {
      stopped = true;
      window.clearInterval(timer);
    };
  }, [jobDone, serverReady]);

  const handleVerifyToken = React.useCallback(async () => {
    setAuthError(null);
    setVerifying(true);
    try {
      const res = await verifySetupToken(tokenInput.trim());
      setForm((prev) => mergeDefaultsIntoForm(prev, res.defaults));
      await loadPreflight();
      setStep("environment");
    } catch (err) {
      setAuthError(readErrorMessage(err));
    } finally {
      setVerifying(false);
    }
  }, [loadPreflight, tokenInput]);

  const handleInspectDir = React.useCallback(async () => {
    setInspectingDir(true);
    try {
      const res = await inspectSetupDir({
        dataDir: form.dataDir,
        postgresDataDir: form.postgresDataDir,
        redisDataDir: form.redisDataDir,
        backupDir: form.backupDir,
      });
      setDirInspection(res);
      if (res.status === "empty") {
        setForm((prev) => ({ ...prev, adoptExistingPGData: false }));
      }
    } finally {
      setInspectingDir(false);
    }
  }, [form.backupDir, form.dataDir, form.postgresDataDir, form.redisDataDir]);

  const handleTestExternal = React.useCallback(async () => {
    setTestingExternal(true);
    try {
      const res = await testSetupExternal({
        postgresDSN: form.externalPostgresDSN,
        cacheDriver: form.cacheMode === "external_redis" ? "redis" : "memory",
        redisAddr: form.externalRedisAddr,
        redisUsername: form.externalRedisUser,
        redisPassword: form.externalRedisPass,
        redisDB: form.externalRedisDB,
        redisTLSEnabled: form.externalRedisTLS,
        redisTLSInsecureSkip: form.externalRedisSkipTLS,
      });
      setExternalResult(res);
    } finally {
      setTestingExternal(false);
    }
  }, [
    form.cacheMode,
    form.externalPostgresDSN,
    form.externalRedisAddr,
    form.externalRedisDB,
    form.externalRedisPass,
    form.externalRedisSkipTLS,
    form.externalRedisTLS,
    form.externalRedisUser,
  ]);

  const handleStartDeploy = React.useCallback(async () => {
    setSubmitError(null);
    if (form.adminPassword !== form.adminPasswordConfirm) {
      setSubmitError("passwordMismatch");
      return;
    }
    const payload: ApplyRequest = {
      databaseMode: form.databaseMode,
      cacheMode: form.cacheMode,
      dockerHost: form.dockerHost,
      registryMirror: form.registryMirror,
      dataDir: form.dataDir,
      postgresDataDir: form.postgresDataDir,
      redisDataDir: form.redisDataDir,
      backupDir: form.backupDir,
      postgresImage: form.postgresImage,
      postgresPort: form.postgresPort,
      postgresMemoryLimitMB: form.postgresMemoryLimitMB,
      redisImage: form.redisImage,
      redisPort: form.redisPort,
      redisMemoryLimitMB: form.redisMemoryLimitMB,
      adoptExistingPGData: form.adoptExistingPGData,
      existingPGUser: form.existingPGUser,
      existingPGPassword: form.existingPGPassword,
      existingDataEncryptionKey: form.databaseMode === "external" || form.adoptExistingPGData ? form.existingDataEncryptionKey : undefined,
      existingPGDatabase: form.existingPGDatabase,
      existingRedisPassword: form.existingRedisPassword,
      composeSync: form.composeSync,
      externalPostgresDSN: form.externalPostgresDSN,
      externalRedisAddr: form.externalRedisAddr,
      externalRedisUser: form.externalRedisUser,
      externalRedisPass: form.externalRedisPass,
      externalRedisDB: form.externalRedisDB,
      externalRedisTLS: form.externalRedisTLS,
      externalRedisSkipTLS: form.externalRedisSkipTLS,
      env: form.env,
      publicBaseURL: form.publicBaseURL,
      corsAllowOrigin: form.corsAllowOrigin,
      adminUsername: form.adminUsername,
      adminDisplayName: form.adminDisplayName,
      adminPassword: form.adminPassword,
    };

    setSubmitting(true);
    try {
      const snap = await startSetupApply(payload);
      setJob(snap);
      setEvents(snap.events);
      setStep("progress");
      startEventStream();
    } catch (err) {
      setSubmitError(readErrorMessage(err));
    } finally {
      setSubmitting(false);
    }
  }, [form, startEventStream]);

  const latestPercent = React.useMemo(() => {
    if (jobDone) return 100;
    let max = 0;
    for (const evt of events) {
      if (evt.percent > max) {
        max = evt.percent;
      }
    }
    return max;
  }, [events, jobDone]);

  const jobFailed = job?.state === "failed" || events.some((evt) => evt.status === "failed");

  return {
    loading,
    alreadyInitialized,
    step,
    setStep,
    version,
    tokenInput,
    setTokenInput,
    verifying,
    authError,
    form,
    updateForm,
    preflight,
    preflighting,
    loadPreflight,
    dirInspection,
    inspectingDir,
    handleInspectDir,
    externalResult,
    testingExternal,
    handleTestExternal,
    submitting,
    submitError,
    job,
    events,
    latestPercent,
    jobDone,
    jobFailed,
    serverReady,
    handleVerifyToken,
    handleStartDeploy,
  };
}
