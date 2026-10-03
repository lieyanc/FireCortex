"use client";

import { useState } from "react";
import { useLocale, useTranslations } from "next-intl";
import { CircleArrowUp, RefreshCw } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogHeightTransition,
  DialogTitle,
} from "@/components/ui/dialog";
import { SpinnerLabel } from "@/components/ui/spinner";
import type { AdminSystemUpdateStatus } from "@/features/admin/api/system-update.types";
import { AdminUpdateTooltipContent } from "@/features/admin/components/admin-update-tooltip-content";
import { formatDateTime } from "@/features/admin/model/log-display";
import {
  applySystemUpdate,
  checkSystemUpdate,
  dismissSystemUpdate,
  formatSystemVersion,
  hasPendingSystemUpdate,
  systemUpdateReleaseURL,
  type SystemUpdateSnapshot,
  useSystemUpdate,
} from "@/features/admin/model/system-update-store";
import { AboutSettingsContent } from "@/shared/components/about-settings-content";
import { UPSTREAM_PROJECT } from "@/shared/config/upstream";
import { useAuthSession } from "@/shared/auth/auth-session-context";
import { useLocalizedErrorMessage } from "@/i18n/use-localized-error";
import { cn } from "@/lib/utils";

type UpdateView =
  | "loading"
  | "unavailable"
  | "current"
  | "available"
  | "checking"
  | "downloading"
  | "ready"
  | "applying"
  | "restartTimeout"
  | "failed";

function resolveUpdateView(update: SystemUpdateSnapshot): UpdateView {
  const { status } = update;
  if (update.restartTimedOut) return "restartTimeout";
  if (!status) return update.loadError ? "unavailable" : "loading";
  if (status.state === "applying") return "applying";
  if (status.state === "checking" || update.pendingAction === "check") return "checking";
  if (status.state === "downloading") return "downloading";
  if (status.state === "ready") return "ready";
  if (status.state === "failed") return "failed";
  return status.hasUpdate ? "available" : "current";
}

function useUpdateActions() {
  const t = useTranslations("adminUsers.aboutPage");
  const { accessToken } = useAuthSession();
  const localizedError = useLocalizedErrorMessage();

  async function run(action: (accessToken: string) => Promise<unknown>): Promise<boolean> {
    try {
      await action(accessToken);
      return true;
    } catch (error) {
      toast.error(t("updateDialog.actionFailed"), { description: localizedError(error) });
      return false;
    }
  }

  return {
    check: () => run(checkSystemUpdate),
    apply: () => run(applySystemUpdate),
    dismiss: () => run(dismissSystemUpdate),
  };
}

function AdminUpdateActions({ update, canManage }: { update: SystemUpdateSnapshot; canManage: boolean }) {
  const t = useTranslations("adminUsers.aboutPage");
  const actions = useUpdateActions();
  const [dialogOpen, setDialogOpen] = useState(false);
  const { status } = update;
  const view = resolveUpdateView(update);
  const checking = view === "checking";

  async function handleCheckUpdate() {
    if (update.pendingAction) return;
    if (await actions.check()) setDialogOpen(true);
  }

  let statusLabel = "";
  if (view === "available" && status) {
    statusLabel = t("updateStatus.available", { latest: formatSystemVersion(status.latestVersion) });
  } else if (view === "downloading" && status) {
    statusLabel = t("updateStatus.downloading", { progress: Math.round(status.downloadProgress) });
  } else if (view === "ready") {
    statusLabel = t("updateStatus.ready");
  } else if (view === "applying" || update.following) {
    statusLabel = t("updateStatus.restarting");
  } else if (view === "failed") {
    statusLabel = t("updateStatus.failed");
  }

  return (
    <>
      {canManage ? (
        <button
          type="button"
          className="inline-flex items-center gap-1 text-xs text-muted-foreground/80 transition-colors hover:text-foreground disabled:cursor-wait disabled:opacity-70"
          onClick={() => void handleCheckUpdate()}
          disabled={checking || update.pendingAction !== null}
        >
          <RefreshCw className={cn("size-3", checking && "animate-spin")} />
          <span>{checking ? t("checkingUpdate") : t("checkUpdate")}</span>
        </button>
      ) : null}
      {statusLabel || !canManage ? (
        <button
          type="button"
          className={cn(
            "inline-flex items-center text-xs transition-colors",
            view === "failed"
              ? "text-destructive/90 hover:text-destructive"
              : statusLabel
                ? "text-rose-500 hover:text-rose-600"
                : "text-muted-foreground/80 hover:text-foreground",
          )}
          onClick={() => setDialogOpen(true)}
        >
          {statusLabel || t("viewUpdate")}
        </button>
      ) : null}
      <UpdateDialog
        open={dialogOpen}
        update={update}
        canManage={canManage}
        actions={actions}
        onOpenChange={setDialogOpen}
      />
    </>
  );
}

function AdminAboutVersionBadge({ status }: { status: AdminSystemUpdateStatus | null }) {
  const t = useTranslations("adminUsers.aboutPage");

  return (
    <span className="inline-flex items-center gap-1.5">
      <span>{status ? formatSystemVersion(status.currentVersion) : "…"}</span>
      {hasPendingSystemUpdate(status) ? (
        <CircleArrowUp className="size-3.5 text-rose-500" aria-label={t("updateAvailableIndicator")} />
      ) : null}
    </span>
  );
}

function UpdateDialog({
  open,
  update,
  canManage,
  actions,
  onOpenChange,
}: {
  open: boolean;
  update: SystemUpdateSnapshot;
  canManage: boolean;
  actions: ReturnType<typeof useUpdateActions>;
  onOpenChange: (open: boolean) => void;
}) {
  const t = useTranslations("adminUsers.aboutPage");
  const locale = useLocale();
  const localizedError = useLocalizedErrorMessage();
  const { status, pendingAction } = update;
  const view = resolveUpdateView(update);
  const currentVersion = status ? formatSystemVersion(status.currentVersion) : "";
  const latestVersion = status?.latestVersion ? formatSystemVersion(status.latestVersion) : "";
  const channel = status ? t(`updateDialog.channels.${status.channel}`) : "";
  const busy = pendingAction !== null;
  const showRelease = Boolean(status?.latestVersion && hasPendingSystemUpdate(status));
  const releaseNotes = status?.releaseNotes.trim() ?? "";

  const title = {
    loading: t("updateDialog.loadingTitle"),
    unavailable: t("updateDialog.unavailableTitle"),
    current: t("updateDialog.currentTitle"),
    available: t("updateDialog.availableTitle"),
    checking: t("updateDialog.checkingTitle"),
    downloading: t("updateDialog.downloadingTitle"),
    ready: t("updateDialog.readyTitle"),
    applying: t("updateDialog.applyingTitle"),
    restartTimeout: t("updateDialog.restartTimeoutTitle"),
    failed: t("updateDialog.failedTitle"),
  }[view];

  let description = "";
  switch (view) {
    case "loading":
      description = t("updateDialog.loadingDescription");
      break;
    case "unavailable":
      description = localizedError(update.loadError);
      break;
    case "current":
      description = t("updateDialog.currentDescription", { current: currentVersion, channel });
      break;
    case "available":
      description = status?.channel === "dev"
        ? t("updateDialog.availableDevDescription", { latest: latestVersion })
        : t("updateDialog.availableStableDescription", { latest: latestVersion });
      break;
    case "checking":
      description = t("updateDialog.checkingDescription", { channel });
      break;
    case "downloading":
      description = t("updateDialog.downloadingDescription", { latest: latestVersion });
      break;
    case "ready":
      description = t("updateDialog.readyDescription", { latest: latestVersion });
      break;
    case "applying":
      description = t("updateDialog.applyingDescription");
      break;
    case "restartTimeout":
      description = t("updateDialog.restartTimeoutDescription");
      break;
    case "failed":
      description = t("updateDialog.failedDescription");
      break;
  }

  function renderPrimaryAction() {
    if (!canManage || !status) return null;
    switch (view) {
      case "checking":
        return (
          <Button type="button" disabled>
            <SpinnerLabel>{t("updateDialog.checking")}</SpinnerLabel>
          </Button>
        );
      case "downloading":
        return (
          <Button type="button" disabled>
            <SpinnerLabel>{t("updateDialog.downloading")}</SpinnerLabel>
          </Button>
        );
      case "applying":
        return (
          <Button type="button" disabled>
            <SpinnerLabel>{t("updateDialog.restarting")}</SpinnerLabel>
          </Button>
        );
      case "available":
        return (
          <Button type="button" disabled={busy} onClick={() => void actions.apply()}>
            {status.channel === "dev" ? t("updateDialog.download") : t("updateDialog.updateNow")}
          </Button>
        );
      case "ready":
        return (
          <>
            <Button type="button" variant="outline" disabled={busy} onClick={() => void actions.dismiss()}>
              {t("updateDialog.dismiss")}
            </Button>
            <Button type="button" disabled={busy} onClick={() => void actions.apply()}>
              {t("updateDialog.restartToUpdate")}
            </Button>
          </>
        );
      case "failed":
        return (
          <Button
            type="button"
            disabled={busy}
            onClick={() => void (status.hasUpdate ? actions.apply() : actions.check())}
          >
            {t("updateDialog.retry")}
          </Button>
        );
      default:
        return null;
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="gap-0 overflow-hidden p-0 sm:max-w-[420px]">
        <DialogHeightTransition contentClassName="max-h-[min(86vh,760px)]">
          <DialogHeader className="shrink-0 px-4 py-4">
            <DialogTitle>{title}</DialogTitle>
            <DialogDescription>{description}</DialogDescription>
          </DialogHeader>

          <div className="min-h-0 flex-1 space-y-3 overflow-y-auto px-4 py-2">
            {status ? (
              <div className="rounded-md bg-muted/50 px-3 py-2 text-xs">
                <div className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2">
                  <span className="text-muted-foreground">{t("updateDialog.currentVersion")}</span>
                  <span className="font-medium break-all">{currentVersion}</span>
                  {latestVersion ? (
                    <>
                      <span className="text-muted-foreground">{t("updateDialog.latestVersion")}</span>
                      <span className="font-medium break-all">{latestVersion}</span>
                    </>
                  ) : null}
                  <span className="text-muted-foreground">{t("updateDialog.channel")}</span>
                  <span className="font-medium">{channel}</span>
                  <span className="text-muted-foreground">{t("updateDialog.autoCheck")}</span>
                  <span className="font-medium">
                    {status.enabled ? t("updateDialog.autoCheckEnabled") : t("updateDialog.autoCheckDisabled")}
                  </span>
                  <span className="text-muted-foreground">{t("updateDialog.lastCheck")}</span>
                  <span className="font-medium">
                    {status.lastCheck ? formatDateTime(status.lastCheck, locale) : t("updateDialog.neverChecked")}
                  </span>
                </div>
              </div>
            ) : null}

            {view === "downloading" && status ? (
              <div className="space-y-1.5 text-xs">
                <div className="flex items-center justify-between text-muted-foreground">
                  <span>{t("updateDialog.downloadProgress")}</span>
                  <span className="tabular-nums">{Math.round(status.downloadProgress)}%</span>
                </div>
                <div
                  role="progressbar"
                  aria-label={t("updateDialog.downloadProgress")}
                  aria-valuemin={0}
                  aria-valuemax={100}
                  aria-valuenow={Math.round(status.downloadProgress)}
                  className="h-1.5 overflow-hidden rounded-full bg-muted"
                >
                  <div
                    className="h-full rounded-full bg-primary transition-[width] duration-500"
                    style={{ width: `${Math.min(100, Math.max(0, status.downloadProgress))}%` }}
                  />
                </div>
              </div>
            ) : null}

            {view === "failed" && status?.error ? (
              <p className="rounded-md bg-destructive/10 px-3 py-2 font-mono text-xs break-words whitespace-pre-wrap text-destructive">
                {status.error}
              </p>
            ) : null}

            {showRelease && releaseNotes ? (
              <div className="space-y-1.5 text-xs">
                <p className="text-muted-foreground">{t("updateDialog.releaseNotes")}</p>
                <p className="max-h-48 overflow-y-auto rounded-md border border-border/60 px-3 py-2 leading-5 break-words whitespace-pre-wrap">
                  {releaseNotes}
                </p>
              </div>
            ) : null}

            {!canManage && status ? (
              <p className="text-xs text-muted-foreground">{t("updateDialog.readOnlyHint")}</p>
            ) : null}
          </div>

          <DialogFooter className="shrink-0 px-4 py-3">
            <DialogClose asChild>
              <Button type="button" variant="ghost">
                {t("updateDialog.close")}
              </Button>
            </DialogClose>
            {showRelease && status ? (
              <Button asChild type="button" variant="outline">
                <a href={systemUpdateReleaseURL(status)} target="_blank" rel="noopener noreferrer">
                  {t("updateDialog.openRelease")}
                </a>
              </Button>
            ) : null}
            {renderPrimaryAction()}
          </DialogFooter>
        </DialogHeightTransition>
      </DialogContent>
    </Dialog>
  );
}

export function AdminAboutPage() {
  const t = useTranslations("adminUsers.aboutPage");
  const { user } = useAuthSession();
  const update = useSystemUpdate();

  return (
    <AboutSettingsContent
      title={t("title")}
      description={t("description")}
      consoleLabel={t("adminConsole")}
      versionBadgeContent={<AdminAboutVersionBadge status={update.status} />}
      versionBadgeTooltip={<AdminUpdateTooltipContent status={update.status} />}
      versionActions={<AdminUpdateActions update={update} canManage={user?.role === "superadmin"} />}
      labels={{
        details: t("details"),
        repository: t("repository"),
        releases: t("releases"),
        issues: t("issues"),
        upstream: t("upstream"),
        copyright: t("copyright"),
        basedOn: t("basedOn", { project: UPSTREAM_PROJECT.name, author: UPSTREAM_PROJECT.author }),
        license: t("license"),
      }}
    />
  );
}
