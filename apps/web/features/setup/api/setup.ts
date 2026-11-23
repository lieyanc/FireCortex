import type {
  ApplyRequest,
  DirInspectionDTO,
  InspectDirRequest,
  JobEventResponse,
  JobSnapshotResponse,
  PreflightResponse,
  SetupStatusResponse,
  TestExternalRequest,
  TestExternalResponse,
  VerifyTokenResponse,
} from "@firecortex/api-contract";
import { apiRequest, resolveApiBaseURL } from "@/shared/api/http-client";
import { isRecord, parseJSON, readFiniteNumber, readString } from "@/shared/lib/type-guards";

export async function getSetupStatus(): Promise<SetupStatusResponse> {
  return apiRequest<SetupStatusResponse>("/api/v1/setup/status");
}

export async function verifySetupToken(token: string): Promise<VerifyTokenResponse> {
  return apiRequest<VerifyTokenResponse>("/api/v1/setup/verify", {
    method: "POST",
    body: { token },
  });
}

export async function runSetupPreflight(dockerHost?: string): Promise<PreflightResponse> {
  const query = dockerHost?.trim() ? `?dockerHost=${encodeURIComponent(dockerHost.trim())}` : "";
  return apiRequest<PreflightResponse>(`/api/v1/setup/preflight${query}`);
}

export async function inspectSetupDir(payload: InspectDirRequest): Promise<DirInspectionDTO> {
  return apiRequest<DirInspectionDTO>("/api/v1/setup/inspect-dir", {
    method: "POST",
    body: payload,
  });
}

export async function testSetupExternal(payload: TestExternalRequest): Promise<TestExternalResponse> {
  return apiRequest<TestExternalResponse>("/api/v1/setup/test-external", {
    method: "POST",
    body: payload,
  });
}

export async function startSetupApply(payload: ApplyRequest): Promise<JobSnapshotResponse> {
  return apiRequest<JobSnapshotResponse>("/api/v1/setup/apply", {
    method: "POST",
    body: payload,
  });
}

export async function getSetupJob(): Promise<JobSnapshotResponse> {
  return apiRequest<JobSnapshotResponse>("/api/v1/setup/job");
}

function parseJobEvent(raw: string): JobEventResponse | null {
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
    step,
    service: readString(parsed, "service"),
    message,
    percent: readFiniteNumber(parsed, "percent") ?? 0,
    status,
    timestamp: readString(parsed, "timestamp") ?? new Date().toISOString(),
  };
}

export function subscribeSetupEvents(
  onEvent: (event: JobEventResponse) => void,
  onError?: () => void,
): () => void {
  const endpoint = `${resolveApiBaseURL()}/api/v1/setup/events`;
  const source = new EventSource(endpoint, { withCredentials: true });
  const handleProgress = (evt: MessageEvent<string>) => {
    const parsed = parseJobEvent(evt.data);
    if (parsed) {
      onEvent(parsed);
    }
  };
  source.addEventListener("progress", handleProgress);
  source.onerror = () => {
    onError?.();
  };
  return () => {
    source.removeEventListener("progress", handleProgress);
    source.close();
  };
}

export async function checkNormalServerReady(): Promise<boolean> {
  try {
    const resp = await fetch(`${resolveApiBaseURL()}/healthz`, {
      cache: "no-store",
      credentials: "include",
    });
    if (!resp.ok) {
      return false;
    }
    const body = parseJSON(await resp.text());
    if (!isRecord(body)) {
      return false;
    }
    return body.needsSetup !== true && body.status !== "setup";
  } catch {
    return false;
  }
}
