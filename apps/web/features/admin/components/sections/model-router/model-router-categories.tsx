"use client";

import * as React from "react";
import {
  Brain,
  Code2,
  FileText,
  Layers,
  MessageSquare,
  Pencil,
  Plus,
  Sigma,
  Sparkles,
  Trash2,
} from "lucide-react";
import { useTranslations } from "next-intl";

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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import {
  EXTRA_CATEGORY_PRESETS,
  MODEL_ROUTER_LIMITS,
  type ModelRouterCategory,
  type ModelRouterPolicy,
} from "@/features/admin/model/model-router-policy";
import { cn } from "@/lib/utils";

const CATEGORY_KEY_RE = /^[a-z0-9_-]+$/;

export function CategoryIcon({
  categoryKey,
  className = "size-3.5",
}: {
  categoryKey: string;
  className?: string;
}) {
  switch (categoryKey) {
    case "chat":
      return <MessageSquare className={className} />;
    case "coding":
      return <Code2 className={className} />;
    case "math":
      return <Sigma className={className} />;
    case "writing":
    case "translation":
    case "summarization":
      return <FileText className={className} />;
    case "analysis":
    case "agent":
      return <Brain className={className} />;
    case "creative":
    case "roleplay":
      return <Sparkles className={className} />;
    default:
      return <Layers className={className} />;
  }
}

type CategoryDialogState =
  | { mode: "create" }
  | { mode: "edit"; originalKey: string }
  | null;

type ModelRouterCategoriesProps = {
  policy: ModelRouterPolicy;
  disabled?: boolean;
  onChange: (updater: (current: ModelRouterPolicy) => ModelRouterPolicy) => void;
};

export function ModelRouterCategories({
  policy,
  disabled = false,
  onChange,
}: ModelRouterCategoriesProps) {
  const t = useTranslations("adminModelRouter");
  const [dialog, setDialog] = React.useState<CategoryDialogState>(null);
  const [draftKey, setDraftKey] = React.useState("");
  const [draftDescription, setDraftDescription] = React.useState("");
  const [deleteTarget, setDeleteTarget] = React.useState<ModelRouterCategory | null>(null);

  const ruleCountByCategory = React.useMemo(() => {
    const map = new Map<string, number>();
    for (const rule of policy.rules) {
      map.set(rule.category, (map.get(rule.category) ?? 0) + 1);
    }
    return map;
  }, [policy.rules]);

  const existingKeys = React.useMemo(
    () => new Set(policy.categories.map((item) => item.key)),
    [policy.categories],
  );

  const availablePresets = React.useMemo(
    () => EXTRA_CATEGORY_PRESETS.filter((preset) => !existingKeys.has(preset.key)),
    [existingKeys],
  );

  const openCreate = React.useCallback(() => {
    setDraftKey("");
    setDraftDescription("");
    setDialog({ mode: "create" });
  }, []);

  const openEdit = React.useCallback((category: ModelRouterCategory) => {
    setDraftKey(category.key);
    setDraftDescription(category.description);
    setDialog({ mode: "edit", originalKey: category.key });
  }, []);

  const normalizedDraftKey = draftKey.trim().toLowerCase();
  const keyError = React.useMemo(() => {
    if (!dialog) return null;
    if (!normalizedDraftKey) return t("categories.validation.keyRequired");
    if (
      normalizedDraftKey.length > MODEL_ROUTER_LIMITS.maxCategoryKeyLength ||
      !CATEGORY_KEY_RE.test(normalizedDraftKey)
    ) {
      return t("categories.validation.keyPattern");
    }
    const isDuplicate =
      existingKeys.has(normalizedDraftKey) &&
      (dialog.mode === "create" || dialog.originalKey !== normalizedDraftKey);
    if (isDuplicate) return t("categories.validation.keyDuplicate");
    return null;
  }, [dialog, existingKeys, normalizedDraftKey, t]);

  const descriptionError =
    [...draftDescription.trim()].length > MODEL_ROUTER_LIMITS.maxDescriptionLength
      ? t("categories.validation.descriptionTooLong")
      : null;

  const handleSaveCategory = React.useCallback(() => {
    if (!dialog || keyError || descriptionError) return;
    const key = normalizedDraftKey;
    const description = draftDescription.trim();
    if (dialog.mode === "create") {
      onChange((current) => ({
        ...current,
        categories: [...current.categories, { key, description }],
      }));
    } else {
      const originalKey = dialog.originalKey;
      onChange((current) => ({
        ...current,
        categories: current.categories.map((item) =>
          item.key === originalKey ? { key, description } : item,
        ),
        rules:
          originalKey === key
            ? current.rules
            : current.rules.map((rule) =>
                rule.category === originalKey ? { ...rule, category: key } : rule,
              ),
      }));
    }
    setDialog(null);
  }, [descriptionError, dialog, draftDescription, keyError, normalizedDraftKey, onChange]);

  const handleAddPreset = React.useCallback(
    (preset: { key: string; description: string }) => {
      if (existingKeys.has(preset.key)) return;
      onChange((current) => ({
        ...current,
        categories: [...current.categories, { key: preset.key, description: preset.description }],
      }));
    },
    [existingKeys, onChange],
  );

  const handleConfirmDelete = React.useCallback(() => {
    if (!deleteTarget) return;
    const targetKey = deleteTarget.key;
    onChange((current) => ({
      ...current,
      categories: current.categories.filter((item) => item.key !== targetKey),
      rules: current.rules.filter((rule) => rule.category !== targetKey),
    }));
    setDeleteTarget(null);
  }, [deleteTarget, onChange]);

  const canDeleteCategory = policy.categories.length > 1;
  const deleteRuleCount = deleteTarget ? (ruleCountByCategory.get(deleteTarget.key) ?? 0) : 0;

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="min-w-0">
          <h4 className="text-sm font-semibold text-foreground">{t("categories.title")}</h4>
          <p className="text-xs text-muted-foreground">{t("categories.description")}</p>
        </div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="h-8 gap-1.5 text-xs"
          disabled={disabled}
          onClick={openCreate}
        >
          <Plus className="size-3.5" />
          {t("categories.addButton")}
        </Button>
      </div>

      <div className="grid gap-2.5 sm:grid-cols-2 lg:grid-cols-3">
        {policy.categories.map((category) => {
          const count = ruleCountByCategory.get(category.key) ?? 0;
          return (
            <div
              key={category.key}
              className="group flex flex-col justify-between gap-2 rounded-lg border border-border/70 bg-card/50 p-3 transition-colors hover:border-border"
            >
              <div className="flex items-start justify-between gap-2">
                <div className="flex min-w-0 items-center gap-2">
                  <span className="inline-flex size-7 shrink-0 items-center justify-center rounded-md bg-muted text-foreground/75">
                    <CategoryIcon categoryKey={category.key} className="size-3.5" />
                  </span>
                  <div className="min-w-0">
                    <code className="block truncate text-xs font-semibold text-foreground">
                      {category.key}
                    </code>
                    <Badge
                      variant={count > 0 ? "secondary" : "outline"}
                      className={cn(
                        "mt-0.5 h-4 px-1.5 text-[10px] font-normal",
                        count === 0 && "text-muted-foreground",
                      )}
                    >
                      {count > 0
                        ? t("categories.ruleCount", { count })
                        : t("categories.noRulesBadge")}
                    </Badge>
                  </div>
                </div>
                <div className="flex shrink-0 items-center gap-0.5">
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    className="size-6 text-muted-foreground hover:text-foreground"
                    disabled={disabled}
                    aria-label={t("categories.editButton")}
                    onClick={() => openEdit(category)}
                  >
                    <Pencil className="size-3" />
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    className="size-6 text-muted-foreground hover:text-destructive"
                    disabled={disabled || !canDeleteCategory}
                    aria-label={t("categories.deleteButton")}
                    onClick={() => setDeleteTarget(category)}
                  >
                    <Trash2 className="size-3" />
                  </Button>
                </div>
              </div>
              <p className="line-clamp-2 text-[11px] leading-relaxed text-muted-foreground">
                {category.description || t("categories.emptyDescription")}
              </p>
            </div>
          );
        })}
      </div>

      {availablePresets.length > 0 ? (
        <div className="flex flex-wrap items-center gap-1.5 rounded-md bg-muted/30 px-3 py-2">
          <span className="text-[11px] font-medium text-muted-foreground">
            {t("categories.quickAddLabel")}
          </span>
          {availablePresets.map((preset) => (
            <Button
              key={preset.key}
              type="button"
              variant="ghost"
              size="sm"
              disabled={disabled}
              className="h-6 gap-1 rounded-full border border-border/60 bg-background px-2 text-[11px] font-normal text-muted-foreground hover:text-foreground"
              onClick={() => handleAddPreset(preset)}
            >
              <Plus className="size-3" />
              <code>{preset.key}</code>
            </Button>
          ))}
        </div>
      ) : null}

      <Dialog open={dialog !== null} onOpenChange={(open) => !open && setDialog(null)}>
        <DialogContent className="sm:max-w-[460px]">
          <DialogHeader>
            <DialogTitle>
              {dialog?.mode === "edit" ? t("categories.editTitle") : t("categories.createTitle")}
            </DialogTitle>
            <DialogDescription>{t("categories.dialogDescription")}</DialogDescription>
          </DialogHeader>
          <FieldGroup>
            <Field data-invalid={Boolean(draftKey && keyError) || undefined}>
              <FieldLabel htmlFor="router-category-key">{t("categories.keyLabel")}</FieldLabel>
              <Input
                id="router-category-key"
                value={draftKey}
                aria-invalid={Boolean(draftKey && keyError) || undefined}
                placeholder="e.g. coding, reasoning, sql"
                onChange={(event) => setDraftKey(event.target.value)}
              />
              <FieldDescription className="text-[11px]">
                {draftKey && keyError ? (
                  <span className="text-destructive">{keyError}</span>
                ) : (
                  t("categories.keyHint")
                )}
              </FieldDescription>
            </Field>
            <Field data-invalid={Boolean(descriptionError) || undefined}>
              <FieldLabel htmlFor="router-category-desc">
                {t("categories.descriptionLabel")}
              </FieldLabel>
              <Textarea
                id="router-category-desc"
                rows={3}
                value={draftDescription}
                aria-invalid={Boolean(descriptionError) || undefined}
                placeholder={t("categories.descriptionPlaceholder")}
                onChange={(event) => setDraftDescription(event.target.value)}
              />
              <FieldDescription className="text-[11px]">
                {descriptionError ? (
                  <span className="text-destructive">{descriptionError}</span>
                ) : (
                  t("categories.descriptionHint")
                )}
              </FieldDescription>
            </Field>
          </FieldGroup>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setDialog(null)}>
              {t("actions.cancel")}
            </Button>
            <Button
              type="button"
              disabled={Boolean(keyError) || Boolean(descriptionError)}
              onClick={handleSaveCategory}
            >
              {dialog?.mode === "edit" ? t("actions.apply") : t("categories.createConfirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={deleteTarget !== null} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t("categories.deleteConfirmTitle", { key: deleteTarget?.key ?? "" })}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {deleteRuleCount > 0
                ? t("categories.deleteConfirmWithRules", { count: deleteRuleCount })
                : t("categories.deleteConfirmDescription")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("actions.cancel")}</AlertDialogCancel>
            <AlertDialogAction onClick={handleConfirmDelete}>
              {t("categories.deleteButton")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
