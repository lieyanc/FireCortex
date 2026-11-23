"use client";

import * as React from "react";
import { Database } from "lucide-react";
import { useTranslations } from "next-intl";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogHeightTransition,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { SpinnerLabel } from "@/components/ui/spinner";
import type {
  ImportOpenWebUIConversationsData,
  ImportOpenWebUIConversationsRequest,
} from "@/features/admin/api/admin-types";
import { useFeature } from "@/shared/capabilities";

const MAX_RECENT_DAYS = 36500;
const RECENT_DAY_PRESETS = ["", "7", "30", "90"] as const;

function parseOptionalRecentDays(raw: string): { valid: boolean; value?: number } {
  const trimmed = raw.trim();
  if (!trimmed) {
    return { valid: true, value: undefined };
  }
  if (!/^\d+$/.test(trimmed)) {
    return { valid: false };
  }
  const parsed = Number(trimmed);
  if (!Number.isSafeInteger(parsed) || parsed < 1 || parsed > MAX_RECENT_DAYS) {
    return { valid: false };
  }
  return { valid: true, value: parsed };
}

type ConversationOpenWebUIImportDialogProps = {
  open: boolean;
  pending: boolean;
  result: ImportOpenWebUIConversationsData | null;
  onOpenChange: (open: boolean) => void;
  onPreviewReset: () => void;
  onSubmit: (payload: ImportOpenWebUIConversationsRequest) => Promise<void>;
};

export function ConversationOpenWebUIImportDialog({
  open,
  pending,
  result,
  onOpenChange,
  onPreviewReset,
  onSubmit,
}: ConversationOpenWebUIImportDialogProps) {
  const t = useTranslations("adminConversation.openWebUIImport");
  const multiUser = useFeature("multiUser");
  const [dsn, setDsn] = React.useState("");
  const [recentDays, setRecentDays] = React.useState("");
  const previewReady = result !== null;
  const parsedRecentDays = React.useMemo(() => parseOptionalRecentDays(recentDays), [recentDays]);

  React.useEffect(() => {
    if (open) {
      return;
    }
    setDsn("");
    setRecentDays("");
  }, [open]);

  function handleDSNChange(value: string) {
    setDsn(value);
    if (result) {
      onPreviewReset();
    }
  }

  function handleRecentDaysChange(value: string) {
    setRecentDays(value);
    if (result) {
      onPreviewReset();
    }
  }

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (pending || !dsn.trim() || !parsedRecentDays.valid) {
      return;
    }
    await onSubmit({
      dsn: dsn.trim(),
      ...(parsedRecentDays.value !== undefined ? { recentDays: parsedRecentDays.value } : {}),
      dryRun: !previewReady,
    });
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[calc(100vw-2rem)] gap-0 overflow-hidden p-0 sm:max-w-[560px]">
        <DialogHeightTransition contentClassName="max-h-[min(86vh,760px)]">
          <DialogHeader className="shrink-0 px-4 py-4">
            <DialogTitle>{t("title")}</DialogTitle>
            <DialogDescription>{t("description")}</DialogDescription>
          </DialogHeader>

          <form className="flex min-h-0 flex-1 flex-col" onSubmit={handleSubmit}>
            <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-4 py-2">
              <div className="flex items-start gap-3 rounded-md bg-muted/45 px-3 py-2">
                <Database className="mt-0.5 size-3.5 shrink-0 text-foreground" />
                <div className="min-w-0 space-y-0.5">
                  <div className="text-xs font-medium text-foreground">{t("dedupeTitle")}</div>
                  <div className="text-xs text-muted-foreground">
                    {multiUser ? t("dedupeDescriptionMultiUser") : t("dedupeDescriptionLocal")}
                  </div>
                </div>
              </div>

              <div className="space-y-1">
                <Label htmlFor="openwebui-conversation-dsn" className="text-xs font-normal text-muted-foreground">
                  {t("dsnLabel")}
                </Label>
                <Input
                  id="openwebui-conversation-dsn"
                  value={dsn}
                  onChange={(event) => handleDSNChange(event.target.value)}
                  placeholder={t("dsnPlaceholder")}
                  disabled={pending}
                  autoComplete="off"
                />
              </div>

              <div className="space-y-2">
                <div className="flex items-center justify-between gap-2">
                  <Label
                    htmlFor="openwebui-conversation-recent-days"
                    className="text-xs font-normal text-muted-foreground"
                  >
                    {t("recentDaysLabel")}
                  </Label>
                  <div className="flex flex-wrap items-center gap-1">
                    {RECENT_DAY_PRESETS.map((preset) => {
                      const active = recentDays.trim() === preset;
                      return (
                        <Button
                          key={preset || "all"}
                          type="button"
                          variant={active ? "secondary" : "outline"}
                          size="sm"
                          className="h-6 px-2 text-xs font-normal"
                          disabled={pending}
                          onClick={() => handleRecentDaysChange(preset)}
                        >
                          {preset === "" ? t("presets.all") : t("presets.days", { days: Number(preset) })}
                        </Button>
                      );
                    })}
                  </div>
                </div>
                <Input
                  id="openwebui-conversation-recent-days"
                  type="number"
                  min="1"
                  max={MAX_RECENT_DAYS}
                  step="1"
                  value={recentDays}
                  onChange={(event) => handleRecentDaysChange(event.target.value)}
                  placeholder={t("recentDaysPlaceholder")}
                  disabled={pending}
                />
                {!parsedRecentDays.valid ? (
                  <p className="text-xs text-destructive">{t("recentDaysInvalid")}</p>
                ) : (
                  <p className="text-xs text-muted-foreground">{t("recentDaysHint")}</p>
                )}
              </div>

              {result ? (
                <div className="space-y-2 rounded-md bg-muted/45 p-3 text-xs text-muted-foreground">
                  {result.recentDays !== undefined ? (
                    <div className="font-medium text-foreground">
                      {t("summary.recentDays", { days: result.recentDays })}
                    </div>
                  ) : null}
                  <div className="grid grid-cols-2 gap-2">
                    <span>{t("summary.scanned", { count: result.scanned })}</span>
                    <span>{t("summary.imported", { count: result.imported })}</span>
                    <span>{t("summary.importedMessages", { count: result.importedMessages })}</span>
                    <span>{t("summary.skippedOutsideTimeRange", { count: result.skippedOutsideTimeRange })}</span>
                    <span>{t("summary.skippedExistingConversation", { count: result.skippedExistingConversation })}</span>
                    {multiUser || result.skippedUnmatchedUser > 0 ? (
                      <span>{t("summary.skippedUnmatchedUser", { count: result.skippedUnmatchedUser })}</span>
                    ) : null}
                    <span>{t("summary.skippedEmptyConversation", { count: result.skippedEmptyConversation })}</span>
                    <span>{t("summary.skippedInvalidRow", { count: result.skippedInvalidRow })}</span>
                  </div>
                </div>
              ) : null}
            </div>

            <DialogFooter className="shrink-0 px-4 py-3">
              <Button type="button" variant="ghost" disabled={pending} onClick={() => onOpenChange(false)}>
                {t("cancel")}
              </Button>
              <Button type="submit" disabled={pending || !dsn.trim() || !parsedRecentDays.valid}>
                {pending ? (
                  <SpinnerLabel>{t(previewReady ? "importing" : "previewing")}</SpinnerLabel>
                ) : (
                  t(previewReady ? "submit" : "preview")
                )}
              </Button>
            </DialogFooter>
          </form>
        </DialogHeightTransition>
      </DialogContent>
    </Dialog>
  );
}
