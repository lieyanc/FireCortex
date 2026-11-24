"use client";

// Server selection for the desktop shell. The pinned remote origin lives on the
// Rust side; this module keeps a synchronous copy of the live origin for the
// API client.

import { normalizeApiBaseUrl } from "@firecortex/core";

import { getServer, type ServerInfo, setRemoteServer } from "./desktop-shell";

let current: ServerInfo | null = null;
const originHandlers = new Set<(origin: string) => void>();

function setCurrent(next: ServerInfo | null): void {
  const origin = next?.origin ?? "";
  const changed = origin !== readServerOrigin();
  current = next;
  if (changed) {
    for (const handler of originHandlers) {
      handler(origin);
    }
  }
}

/**
 * Subscribe to origin changes (for example when a tab is bound to a remote
 * server). The handler receives the new origin, "" when the tab has none;
 * returns an unsubscribe function.
 */
export function onServerOriginChanged(handler: (origin: string) => void): () => void {
  originHandlers.add(handler);
  return () => {
    originHandlers.delete(handler);
  };
}

/** Synchronous read for the API client; empty until `loadServer` ran. */
export function readServerOrigin(): string {
  return current?.origin ?? "";
}

export async function loadServer(): Promise<ServerInfo | null> {
  const server = await getServer();
  setCurrent(server);
  return server;
}

/** Normalise user input; "" when it is not an absolute http(s) URL. */
export function validateApiBaseUrl(raw: string): string {
  return normalizeApiBaseUrl(raw);
}

export async function commitRemoteServer(origin: string): Promise<ServerInfo> {
  const server = await setRemoteServer(origin);
  setCurrent(server);
  return server;
}
