"use client";

import { useTranslations } from "next-intl";

import { AboutSettingsContent } from "@/shared/components/about-settings-content";
import { UPSTREAM_PROJECT } from "@/shared/config/upstream";

export function SettingsAbout() {
  const t = useTranslations("settings.aboutPage");

  return (
    <AboutSettingsContent
      title={t("title")}
      description={t("description")}
      consoleLabel={t("userConsole")}
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
