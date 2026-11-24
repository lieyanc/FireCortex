"use client";

import * as React from "react";
import { useTranslations } from "next-intl";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { HeightTransition } from "@/components/ui/height-transition";
import { Input } from "@/components/ui/input";
import { SpinnerLabel } from "@/components/ui/spinner";
import { AppLogo } from "@/shared/components/app-logo";
import { CustomBrandAttribution } from "@/shared/components/powered-by-firecortex";
import { useDesktopDistribution } from "@/features/desktop/hooks/use-desktop-distribution";
import { type DistributionPolicy, isPolicyError } from "@/shared/platform/desktop-distribution";
import { isShellSessionError } from "@/shared/platform/desktop-shell";
import { commitRemoteServer, validateApiBaseUrl } from "@/shared/platform/server-address";
import { fetchWithHeaderTimeout, isTimeoutError } from "@/shared/lib/fetch-timeout";

// Desktop first-run screen, styled as a sibling of the login page:
// connects this tab to a self-hosted FireCortex server.
// Enterprise policy (enforced by the shell) can prefill or lock the address;
// this screen only reflects it.

const HEALTH_PATH = "/healthz";
// An unreachable host can leave the probe pending for minutes; fail fast instead.
const HEALTH_PROBE_TIMEOUT_MS = 10_000;

export function ServerSetup({ onConfigured }: { onConfigured: () => void }) {
  const t = useTranslations("desktopSetup");
  const { status, distribution } = useDesktopDistribution();
  const policy = distribution.policy;

  return (
    <main className="flex min-h-screen animate-in items-center justify-center px-4 py-8 text-foreground fade-in-0 duration-300">
      <div className="w-full max-w-[360px]">
        <AppLogo width={32} height={32} priority className="mx-auto h-9 w-auto" />

        <HeightTransition className="px-2">
          {status === "loading" ? (
            // Policy decides whether the address is prefilled or locked; render once known.
            <div key="loading" className="pt-7" />
          ) : (
            <div key="remote" className="animate-in pt-7 fade-in-0 slide-in-from-bottom-1 duration-300">
              <RemoteForm labels={t} policy={policy} onConfigured={onConfigured} />
            </div>
          )}
        </HeightTransition>
      </div>

      <CustomBrandAttribution className="fixed bottom-4 right-4" />
    </main>
  );
}

function RemoteForm({
  labels,
  policy,
  onConfigured,
}: {
  labels: ReturnType<typeof useTranslations<"desktopSetup">>;
  policy: DistributionPolicy;
  onConfigured: () => void;
}) {
  const locked = policy.serverUrlLocked;
  const [value, setValue] = React.useState(policy.defaultServerUrl ?? "");
  const [busy, setBusy] = React.useState(false);

  const submit = React.useCallback(async () => {
    const candidate = validateApiBaseUrl(value);
    if (!candidate) {
      toast.error(labels("toasts.invalidUrl"));
      return;
    }
    setBusy(true);
    try {
      // Probe before pinning: a wrong address must not survive a restart.
      let response: Response;
      try {
        response = await fetchWithHeaderTimeout(`${candidate}${HEALTH_PATH}`, {
          method: "GET",
          cache: "no-store",
          timeoutMs: HEALTH_PROBE_TIMEOUT_MS,
        });
      } catch (error) {
        toast.error(
          isTimeoutError(error)
            ? labels("toasts.timeout", { seconds: HEALTH_PROBE_TIMEOUT_MS / 1000 })
            : labels("toasts.networkError"),
        );
        return;
      }
      if (!response.ok) {
        toast.error(labels("toasts.unexpectedStatus", { status: response.status }));
        return;
      }
      await commitRemoteServer(candidate);
      onConfigured();
    } catch (error) {
      if (isAlreadyOpen(error)) {
        toast.error(labels("toasts.alreadyOpen"));
      } else if (isPolicyError(error)) {
        toast.error(labels("toasts.serverLocked"));
      } else {
        toast.error(labels("toasts.invalidUrl"), { description: describe(error) });
      }
    } finally {
      setBusy(false);
    }
  }, [labels, onConfigured, value]);

  return (
    <form
      className="space-y-4"
      onSubmit={(event) => {
        event.preventDefault();
        void submit();
      }}
    >
      <div className="space-y-2">
        <label className="text-sm font-medium leading-none text-foreground" htmlFor="server-url">
          {labels("remote.title")}
        </label>
        <Input
          id="server-url"
          name="server-url"
          autoFocus={!locked}
          readOnly={locked}
          aria-describedby={locked ? "server-url-policy" : undefined}
          inputMode="url"
          autoComplete="url"
          className="h-9 border-input/50"
          placeholder="https://chat.example.com"
          value={value}
          onChange={(event) => setValue(event.target.value)}
          required
        />
        {locked ? (
          <p id="server-url-policy" className="text-xs text-muted-foreground">
            {labels("policy.managed")}
          </p>
        ) : null}
      </div>
      <Button
        type="submit"
        className="mt-1 h-9 w-full rounded-md bg-foreground text-sm font-semibold text-background shadow-none hover:bg-foreground/90"
        disabled={busy}
      >
        {busy ? <SpinnerLabel>{labels("remote.connecting")}</SpinnerLabel> : labels("remote.connect")}
      </Button>
    </form>
  );
}

// Rust refuses to bind a server that another tab already shows.
function isAlreadyOpen(error: unknown): boolean {
  return isShellSessionError(error) && error.kind === "tabs";
}

function describe(error: unknown): string {
  if (typeof error === "object" && error !== null && "message" in error) {
    return String(error.message);
  }
  return String(error);
}
