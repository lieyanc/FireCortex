"use client";

import type { BackupRecordDTO } from "@firecortex/api-contract";
import { Download, HardDriveDownload, Loader2, ShieldCheck, Trash2 } from "lucide-react";
import { useTranslations } from "next-intl";
import * as React from "react";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

function formatBytes(bytes: number): string {
  if (!bytes || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = bytes;
  let idx = 0;
  while (value >= 1024 && idx < units.length - 1) {
    value /= 1024;
    idx++;
  }
  return `${value.toFixed(1)} ${units[idx]}`;
}

export function InfrastructureBackupsPanel({
  backups,
  schedule,
  retention,
  backupDir,
  managedPostgresEnabled,
  creating,
  onCreate,
  onDownload,
  onDelete,
}: {
  backups: BackupRecordDTO[];
  schedule: string;
  retention: number;
  backupDir: string;
  managedPostgresEnabled: boolean;
  creating: boolean;
  onCreate: () => void;
  onDownload: (backupID: string) => void;
  onDelete: (backupID: string) => void;
}) {
  const t = useTranslations("adminInfrastructure.backups");
  const tActions = useTranslations("common.actions");
  const [pendingDelete, setPendingDelete] = React.useState<BackupRecordDTO | null>(null);

  return (
    <>
      <Card>
        <CardHeader className="flex flex-row flex-wrap items-start justify-between gap-4">
          <div className="space-y-1">
            <CardTitle className="flex items-center gap-2 text-base">
              <HardDriveDownload className="size-4 text-primary" />
              {t("title")}
            </CardTitle>
            <CardDescription>{t("description")}</CardDescription>
            <div className="flex flex-wrap gap-3 pt-1 font-mono text-xs text-muted-foreground">
              <span>
                {t("schedule")}: {schedule || "0 3 * * *"}
              </span>
              <span>·</span>
              <span>
                {t("retention")}: {retention || 7}
              </span>
              {backupDir ? (
                <>
                  <span>·</span>
                  <span>{backupDir}</span>
                </>
              ) : null}
            </div>
          </div>
          <Button type="button" disabled={creating || !managedPostgresEnabled} onClick={onCreate}>
            {creating ? (
              <Loader2 className="mr-1.5 size-4 animate-spin" />
            ) : (
              <HardDriveDownload className="mr-1.5 size-4" />
            )}
            {creating ? t("creating") : t("createNow")}
          </Button>
        </CardHeader>
        <CardContent>
          {backups.length === 0 ? (
            <div className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">
              {t("empty")}
            </div>
          ) : (
            <div className="divide-y rounded-lg border">
              {backups.map((item) => (
                <div key={item.id} className="flex flex-wrap items-center justify-between gap-3 p-3.5 text-sm">
                  <div className="min-w-0 space-y-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="truncate font-mono text-xs font-medium">{item.fileName}</span>
                      <Badge variant="secondary" className="text-[11px]">
                        {item.trigger === "scheduled" ||
                        item.trigger === "manual" ||
                        item.trigger === "pre-upgrade" ||
                        item.trigger === "pre-restore"
                          ? t(`triggers.${item.trigger}`)
                          : item.trigger}
                      </Badge>
                      {item.verified ? (
                        <Badge variant="outline" className="gap-1 text-[11px] text-emerald-600 dark:text-emerald-400">
                          <ShieldCheck className="size-3" />
                          SHA-256
                        </Badge>
                      ) : null}
                    </div>
                    <div className="flex flex-wrap gap-3 font-mono text-[11px] text-muted-foreground">
                      <span>{formatBytes(item.sizeBytes)}</span>
                      <span>·</span>
                      <span>{new Date(item.createdAt).toLocaleString()}</span>
                      {item.sha256 ? (
                        <>
                          <span>·</span>
                          <span className="truncate">{item.sha256.slice(0, 16)}…</span>
                        </>
                      ) : null}
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <Button type="button" size="sm" variant="outline" onClick={() => onDownload(item.id)}>
                      <Download className="mr-1.5 size-3.5" />
                      {t("download")}
                    </Button>
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      className="text-destructive hover:text-destructive"
                      onClick={() => setPendingDelete(item)}
                    >
                      <Trash2 className="mr-1 size-3.5" />
                      {t("delete")}
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      <AlertDialog open={Boolean(pendingDelete)} onOpenChange={(open) => !open && setPendingDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("delete")}</AlertDialogTitle>
            <AlertDialogDescription>
              {pendingDelete ? t("deleteConfirm", { name: pendingDelete.fileName }) : ""}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{tActions("cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (pendingDelete) {
                  onDelete(pendingDelete.id);
                  setPendingDelete(null);
                }
              }}
            >
              {t("delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
