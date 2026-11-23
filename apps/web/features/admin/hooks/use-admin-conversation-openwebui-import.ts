import * as React from "react";
import { useTranslations } from "next-intl";
import { toast } from "sonner";

import { importOpenWebUIConversations } from "@/features/admin/api";
import type {
  ImportOpenWebUIConversationsData,
  ImportOpenWebUIConversationsRequest,
} from "@/features/admin/api/admin-types";
import { resolveAdminErrorMessage } from "@/features/admin/utils/admin-error";
import { resolveAccessToken } from "@/shared/auth/resolve-access-token";

function countSkippedConversations(result: ImportOpenWebUIConversationsData): number {
  return (
    result.skippedOutsideTimeRange +
    result.skippedExistingConversation +
    result.skippedUnmatchedUser +
    result.skippedEmptyConversation +
    result.skippedInvalidRow
  );
}

/**
 * OpenWebUI conversation import dialog flow. A dry run previews matched counts
 * without mutating data; a real import closes the dialog on completion.
 */
export function useAdminConversationOpenWebUIImport() {
  const t = useTranslations("adminConversation");
  const [openWebUIImportOpen, setOpenWebUIImportOpen] = React.useState(false);
  const [openWebUIImportPending, setOpenWebUIImportPending] = React.useState(false);
  const [openWebUIImportResult, setOpenWebUIImportResult] =
    React.useState<ImportOpenWebUIConversationsData | null>(null);

  async function handleImportOpenWebUI(payload: ImportOpenWebUIConversationsRequest) {
    if (openWebUIImportPending) {
      return;
    }
    setOpenWebUIImportPending(true);
    try {
      const token = await resolveAccessToken();
      if (!token) {
        toast.error(t("toast.sessionExpired"), { description: t("toast.signInAgain") });
        return;
      }
      const result = await importOpenWebUIConversations(token, payload);
      setOpenWebUIImportResult(result);
      const skipped = countSkippedConversations(result);
      if (payload.dryRun) {
        toast.success(
          t("openWebUIImport.toastPreviewSucceeded", {
            imported: result.imported,
            messages: result.importedMessages,
            skipped,
          }),
        );
        return;
      }
      toast.success(
        t("openWebUIImport.toastSucceeded", {
          imported: result.imported,
          messages: result.importedMessages,
          skipped,
        }),
      );
      setOpenWebUIImportOpen(false);
    } catch (error) {
      toast.error(t("openWebUIImport.toastFailed"), { description: resolveAdminErrorMessage(error) });
    } finally {
      setOpenWebUIImportPending(false);
    }
  }

  return {
    openWebUIImportOpen,
    setOpenWebUIImportOpen,
    openWebUIImportPending,
    openWebUIImportResult,
    setOpenWebUIImportResult,
    handleImportOpenWebUI,
  };
}
