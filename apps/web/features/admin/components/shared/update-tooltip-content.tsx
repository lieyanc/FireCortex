"use client";

import { useTranslations } from "next-intl";

import type { AdminSystemUpdateStatus } from "@/features/admin/api/system-update.types";
import { formatSystemVersion, hasPendingSystemUpdate } from "@/features/admin/model/system-update-store";

export function AdminUpdateTooltipContent({ status }: { status: AdminSystemUpdateStatus | null }) {
  const t = useTranslations("adminUsers.aboutPage");

  if (!status) {
    return <p>{t("updateDialog.loadingDescription")}</p>;
  }

  const currentVersion = formatSystemVersion(status.currentVersion);
  const channel = t(`updateDialog.channels.${status.channel}`);

  if (!hasPendingSystemUpdate(status)) {
    return (
      <div className="space-y-1">
        <p>{t("updateTooltip.currentTitle")}</p>
        <p>{t("updateTooltip.currentVersion", { current: currentVersion })}</p>
        <p>{t("updateTooltip.channel", { channel })}</p>
      </div>
    );
  }

  return (
    <div className="space-y-1.5">
      <p>{t("updateTooltip.title")}</p>
      <div className="grid gap-1">
        <p>{t("updateTooltip.currentVersion", { current: currentVersion })}</p>
        <p>{t("updateTooltip.latestVersion", { latest: formatSystemVersion(status.latestVersion) })}</p>
      </div>
      <p>{t("updateTooltip.description")}</p>
    </div>
  );
}
