import { authedRequest } from "@/shared/api/authed-client";
import type { AdminSystemUpdateStatus } from "@/features/admin/api/system-update.types";

export async function getAdminSystemUpdateStatus(
  accessToken: string,
  options: { signal?: AbortSignal } = {},
): Promise<AdminSystemUpdateStatus> {
  return authedRequest<AdminSystemUpdateStatus>(
    "/api/v1/admin/system/update",
    { accessToken, signal: options.signal },
    true,
  );
}

export async function checkAdminSystemUpdate(accessToken: string): Promise<AdminSystemUpdateStatus> {
  return authedRequest<AdminSystemUpdateStatus>(
    "/api/v1/admin/system/update/check",
    { method: "POST", accessToken },
    true,
  );
}

export async function applyAdminSystemUpdate(accessToken: string): Promise<AdminSystemUpdateStatus> {
  return authedRequest<AdminSystemUpdateStatus>(
    "/api/v1/admin/system/update/apply",
    { method: "POST", accessToken },
    true,
  );
}

export async function dismissAdminSystemUpdate(accessToken: string): Promise<AdminSystemUpdateStatus> {
  return authedRequest<AdminSystemUpdateStatus>(
    "/api/v1/admin/system/update/dismiss",
    { method: "POST", accessToken },
    true,
  );
}
