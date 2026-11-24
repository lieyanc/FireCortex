"use client";

// Desktop session bootstrap: connects the shared session snapshot and the API
// client to the Tauri shell. No-op in browsers.

import type { LoginData } from "@/shared/api/auth-types";
import { registerRuntimeApiBaseURLResolver } from "@/shared/api/http-client";
import { registerSessionClearedHandler, writeSessionSnapshot } from "@/shared/auth/session";
import { isDesktopApp } from "@/shared/platform";
import { clearSession, type ServerInfo, storeSession } from "@/shared/platform/desktop-shell";
import { loadServer, readServerOrigin } from "@/shared/platform/server-address";

let initialized: Promise<ServerInfo | null> | null = null;

/**
 * Install desktop hooks and resolve with the configured server (null on first
 * run). Idempotent; browsers resolve immediately with null.
 */
export function initializeDesktopSession(): Promise<ServerInfo | null> {
  if (!isDesktopApp()) {
    return Promise.resolve(null);
  }
  initialized ??= (async () => {
    registerRuntimeApiBaseURLResolver(readServerOrigin);
    registerSessionClearedHandler(async () => {
      await clearSession();
    });
    return loadServer();
  })();
  return initialized;
}

/**
 * Resolve once requests can be addressed: true right away in browsers; on
 * desktop true once this tab is bound to a server, false while it shows the
 * setup screen. Rejects when the bound server cannot be resolved.
 */
export async function waitForApiServer(): Promise<boolean> {
  if (!isDesktopApp()) {
    return true;
  }
  return (await initializeDesktopSession()) !== null;
}

/**
 * Record an authenticated session. Every sign-in path goes through here; on
 * desktop the refresh token is handed to the shell and never read back.
 */
export async function completeNativeSignIn(result: LoginData): Promise<void> {
  writeSessionSnapshot({ accessToken: result.accessToken, sessionID: result.sessionID });
  if (isDesktopApp() && result.refreshToken) {
    await storeSession(result.refreshToken);
  }
}
