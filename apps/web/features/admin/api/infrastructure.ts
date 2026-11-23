import type {
  BackupListResponse,
  BackupRecordDTO,
  ComposePreviewResponse,
  DangerRecreateContainerRequest,
  DangerRestoreBackupRequest,
  DangerRestoreBackupResponse,
  DangerSwitchDatabaseRequest,
  DangerSwitchDatabaseResponse,
  InfraConfigResponse,
  InfraJobEventDTO,
  InfraJobSnapshotResponse,
  InfraOverviewResponse,
  PatchInfraConfigRequest,
  PatchInfraConfigResponse,
  ServiceActionRequest,
  ServiceLogsResponse,
} from "@firecortex/api-contract";
import { authedFetch, authedRequest } from "@/shared/api/authed-client";
import { pathParam, resolveApiBaseURL } from "@/shared/api/http-client";
import { isRecord, parseJSON, readFiniteNumber, readString } from "@/shared/lib/type-guards";

export async function getAdminInfraOverview(accessToken: string): Promise<InfraOverviewResponse> {
  return authedRequest<InfraOverviewResponse>("/api/v1/admin/infra/overview", { accessToken }, true);
}

export async function triggerAdminInfraServiceAction(
  accessToken: string,
  serviceID: string,
  action: "start" | "stop" | "restart" | "pull",
  payload?: ServiceActionRequest,
): Promise<InfraJobSnapshotResponse> {
  return authedRequest<InfraJobSnapshotResponse>(
    `/api/v1/admin/infra/services/${pathParam(serviceID)}/${pathParam(action)}`,
    { method: "POST", accessToken, body: payload ?? {} },
    true,
  );
}

export async function getAdminInfraServiceLogs(
  accessToken: string,
  serviceID: string,
  tail = 200,
): Promise<ServiceLogsResponse> {
  return authedRequest<ServiceLogsResponse>(
    `/api/v1/admin/infra/services/${pathParam(serviceID)}/logs?tail=${tail}`,
    { accessToken },
    true,
  );
}

export async function getAdminInfraComposePreview(accessToken: string): Promise<ComposePreviewResponse> {
  return authedRequest<ComposePreviewResponse>("/api/v1/admin/infra/compose", { accessToken }, true);
}

export async function listAdminInfraBackups(accessToken: string): Promise<BackupListResponse> {
  return authedRequest<BackupListResponse>("/api/v1/admin/infra/backups", { accessToken }, true);
}

export async function createAdminInfraBackup(accessToken: string): Promise<BackupRecordDTO> {
  return authedRequest<BackupRecordDTO>("/api/v1/admin/infra/backups", { method: "POST", accessToken }, true);
}

export async function downloadAdminInfraBackup(accessToken: string, backupID: string): Promise<Blob> {
  const resp = await authedFetch(`/api/v1/admin/infra/backups/${pathParam(backupID)}/download`, { accessToken });
  if (!resp.ok) {
    throw new Error(`download failed: ${resp.status}`);
  }
  return resp.blob();
}

export async function deleteAdminInfraBackup(accessToken: string, backupID: string): Promise<void> {
  await authedRequest<unknown>(
    `/api/v1/admin/infra/backups/${pathParam(backupID)}`,
    { method: "DELETE", accessToken },
    true,
  );
}

export async function getAdminInfraConfig(accessToken: string): Promise<InfraConfigResponse> {
  return authedRequest<InfraConfigResponse>("/api/v1/admin/infra/config", { accessToken }, true);
}

export async function patchAdminInfraConfig(
  accessToken: string,
  payload: PatchInfraConfigRequest,
): Promise<PatchInfraConfigResponse> {
  return authedRequest<PatchInfraConfigResponse>(
    "/api/v1/admin/infra/config",
    { method: "PATCH", accessToken, body: payload },
    true,
  );
}

export async function dangerRestoreAdminBackup(
  accessToken: string,
  payload: DangerRestoreBackupRequest,
): Promise<DangerRestoreBackupResponse> {
  return authedRequest<DangerRestoreBackupResponse>(
    "/api/v1/admin/infra/danger/restore-backup",
    { method: "POST", accessToken, body: payload },
    true,
  );
}

export async function dangerSwitchAdminDatabase(
  accessToken: string,
  payload: DangerSwitchDatabaseRequest,
): Promise<DangerSwitchDatabaseResponse> {
  return authedRequest<DangerSwitchDatabaseResponse>(
    "/api/v1/admin/infra/danger/switch-database",
    { method: "POST", accessToken, body: payload },
    true,
  );
}

export async function dangerRecreateAdminContainer(
  accessToken: string,
  payload: DangerRecreateContainerRequest,
): Promise<InfraJobSnapshotResponse> {
  return authedRequest<InfraJobSnapshotResponse>(
    "/api/v1/admin/infra/danger/recreate-container",
    { method: "POST", accessToken, body: payload },
    true,
  );
}

function parseInfraJobEvent(raw: string): InfraJobEventDTO | null {
  const parsed = parseJSON(raw);
  if (!isRecord(parsed)) {
    return null;
  }
  const step = readString(parsed, "step");
  const message = readString(parsed, "message");
  const status = readString(parsed, "status");
  if (!step || !message || !status) {
    return null;
  }
  return {
    jobID: readString(parsed, "jobID") ?? "",
    action: readString(parsed, "action") ?? "",
    serviceID: readString(parsed, "serviceID") ?? "",
    step,
    message,
    percent: readFiniteNumber(parsed, "percent") ?? 0,
    status,
    timestamp: readString(parsed, "timestamp") ?? new Date().toISOString(),
  };
}

export async function streamAdminInfraJobEvents(
  accessToken: string,
  jobID: string,
  onEvent: (event: InfraJobEventDTO) => void,
  signal?: AbortSignal,
): Promise<void> {
  const resp = await authedFetch(`/api/v1/admin/infra/jobs/${pathParam(jobID)}/events`, {
    accessToken,
    signal,
  });
  if (!resp.ok || !resp.body) {
    throw new Error(`job stream failed: ${resp.status}`);
  }
  const reader = resp.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  try {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) {
        break;
      }
      buffer += decoder.decode(value, { stream: true });
      const chunks = buffer.split("\n\n");
      buffer = chunks.pop() ?? "";
      for (const block of chunks) {
        for (const line of block.split("\n")) {
          if (line.startsWith("data:")) {
            const evt = parseInfraJobEvent(line.slice(5).trim());
            if (evt) {
              onEvent(evt);
            }
          }
        }
      }
    }
  } finally {
    reader.releaseLock();
  }
}

export async function pollServerHealthReady(): Promise<boolean> {
  try {
    const resp = await fetch(`${resolveApiBaseURL()}/healthz`, {
      cache: "no-store",
      credentials: "include",
    });
    return resp.ok;
  } catch {
    return false;
  }
}
