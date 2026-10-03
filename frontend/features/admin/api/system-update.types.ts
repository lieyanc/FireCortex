import type { UpdateStatusResponse } from "@firecortex/api-contract";

export type AdminSystemUpdateStatus = UpdateStatusResponse;
export type AdminSystemUpdateState = UpdateStatusResponse["state"];
export type AdminSystemUpdateChannel = UpdateStatusResponse["channel"];
