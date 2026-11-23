"use client";

import * as React from "react";
import {
  AlertTriangle,
  ArrowRight,
  Copy,
  CornerDownRight,
  Grid2x2,
  List,
  Plus,
  Trash2,
} from "lucide-react";
import { useTranslations } from "next-intl";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ModelOptionIcon, resolveModelOptionLabel } from "@/entities/model";
import { CategoryIcon } from "@/features/admin/components/sections/model-router/model-router-categories";
import { ModelRouterModelChain } from "@/features/admin/components/sections/model-router/model-router-model-chain";
import {
  MODEL_ROUTER_DIFFICULTIES,
  type ModelRouterPolicy,
  type ModelRouterRuleDifficulty,
  resolveMatrixCell,
  type RouterChatModelMeta,
  setMatrixCellModels,
} from "@/features/admin/model/model-router-policy";
import { cn } from "@/lib/utils";
import { isOneOf } from "@/shared/lib/type-guards";

const MATRIX_COLUMNS: readonly ModelRouterRuleDifficulty[] = ["", "easy", "medium", "hard"];
const VIEW_MODES = ["matrix", "list"] as const;
type RulesViewMode = (typeof VIEW_MODES)[number];
const isRulesViewMode = isOneOf(VIEW_MODES);

const ANY_DIFFICULTY_SELECT_VALUE = "__any__";

type ModelRouterMatrixProps = {
  policy: ModelRouterPolicy;
  chatModels: RouterChatModelMeta[];
  modelMetaByName: ReadonlyMap<string, RouterChatModelMeta>;
  disabled?: boolean;
  onChange: (updater: (current: ModelRouterPolicy) => ModelRouterPolicy) => void;
};

function difficultyLabelKey(difficulty: ModelRouterRuleDifficulty): string {
  switch (difficulty) {
    case "easy":
      return "difficulty.easy";
    case "medium":
      return "difficulty.medium";
    case "hard":
      return "difficulty.hard";
    default:
      return "difficulty.any";
  }
}

function difficultyToneClass(difficulty: ModelRouterRuleDifficulty): string {
  switch (difficulty) {
    case "easy":
      return "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300";
    case "medium":
      return "border-sky-500/30 bg-sky-500/10 text-sky-700 dark:text-sky-300";
    case "hard":
      return "border-violet-500/30 bg-violet-500/10 text-violet-700 dark:text-violet-300";
    default:
      return "border-border/70 bg-muted/60 text-muted-foreground";
  }
}

function MatrixCellEditor({
  categoryKey,
  difficulty,
  policy,
  chatModels,
  modelMetaByName,
  catalogFallbackModel,
  disabled,
  onChange,
}: {
  categoryKey: string;
  difficulty: ModelRouterRuleDifficulty;
  policy: ModelRouterPolicy;
  chatModels: RouterChatModelMeta[];
  modelMetaByName: ReadonlyMap<string, RouterChatModelMeta>;
  catalogFallbackModel: string | null;
  disabled: boolean;
  onChange: (updater: (current: ModelRouterPolicy) => ModelRouterPolicy) => void;
}) {
  const t = useTranslations("adminModelRouter");
  const [open, setOpen] = React.useState(false);

  const summary = React.useMemo(
    () => resolveMatrixCell(policy, categoryKey, difficulty, catalogFallbackModel),
    [catalogFallbackModel, categoryKey, difficulty, policy],
  );
  const directModels = summary.origin === "direct" ? summary.models : [];
  const hasUnavailable =
    summary.origin === "direct" && directModels.some((name) => !modelMetaByName.has(name));

  const handleModelsChange = React.useCallback(
    (nextModels: string[]) => {
      onChange((current) => setMatrixCellModels(current, categoryKey, difficulty, nextModels));
    },
    [categoryKey, difficulty, onChange],
  );

  const handleApplyToAllDifficulties = React.useCallback(() => {
    if (directModels.length === 0) return;
    onChange((current) => {
      let next = current;
      for (const diff of MODEL_ROUTER_DIFFICULTIES) {
        if (diff === difficulty) continue;
        next = setMatrixCellModels(next, categoryKey, diff, directModels);
      }
      return next;
    });
  }, [categoryKey, difficulty, directModels, onChange]);

  const primaryModelName = summary.models[0] ?? "";
  const primaryMeta = primaryModelName ? modelMetaByName.get(primaryModelName) : undefined;

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          disabled={disabled}
          className={cn(
            "group flex min-h-[76px] w-full flex-col justify-between gap-1.5 rounded-lg border p-2.5 text-left transition-all outline-none focus-visible:ring-2 focus-visible:ring-ring",
            summary.origin === "direct"
              ? hasUnavailable
                ? "border-amber-500/60 bg-amber-500/5 hover:border-amber-500"
                : "border-border bg-card shadow-xs hover:border-primary/50 hover:bg-accent/25"
              : "border-dashed border-border/65 bg-muted/20 hover:border-border hover:bg-muted/40",
            open && "border-primary ring-2 ring-primary/20",
          )}
        >
          {summary.origin === "direct" ? (
            <>
              <div className="flex w-full flex-col gap-1">
                {directModels.slice(0, 2).map((name, idx) => {
                  const meta = modelMetaByName.get(name);
                  const missing = !meta;
                  return (
                    <div
                      key={name}
                      className={cn(
                        "flex items-center gap-1.5 rounded px-1.5 py-0.5 text-[11px]",
                        idx === 0 ? "bg-muted/75 font-medium text-foreground" : "text-muted-foreground",
                        missing && "text-amber-600",
                      )}
                    >
                      <span className="font-mono text-[9px] text-muted-foreground">#{idx + 1}</span>
                      <ModelOptionIcon
                        iconUrl={meta?.iconUrl ?? null}
                        label={meta?.label ?? resolveModelOptionLabel(name)}
                        size={13}
                      />
                      <span className="min-w-0 flex-1 truncate">
                        {meta?.label ?? resolveModelOptionLabel(name)}
                      </span>
                      {missing ? <AlertTriangle className="size-3 shrink-0 text-amber-600" /> : null}
                    </div>
                  );
                })}
              </div>
              <div className="flex items-center justify-between gap-1 text-[10px] text-muted-foreground">
                <span>
                  {directModels.length > 2
                    ? t("matrix.moreModels", { count: directModels.length - 2 })
                    : t("matrix.directRule")}
                </span>
                <span className="opacity-0 transition-opacity group-hover:opacity-100">
                  {t("matrix.clickToEdit")}
                </span>
              </div>
            </>
          ) : (
            <>
              <div className="flex items-center gap-1 text-[10px] text-muted-foreground">
                <CornerDownRight className="size-3 shrink-0 opacity-70" />
                <span className="truncate">
                  {summary.origin === "inherit-any"
                    ? t("matrix.inheritAny")
                    : summary.origin === "fallback-default"
                      ? t("matrix.fallbackDefault")
                      : t("matrix.fallbackCatalog")}
                </span>
              </div>
              {primaryModelName ? (
                <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground/85">
                  <ModelOptionIcon
                    iconUrl={primaryMeta?.iconUrl ?? null}
                    label={primaryMeta?.label ?? resolveModelOptionLabel(primaryModelName)}
                    size={12}
                  />
                  <span className="truncate">
                    {primaryMeta?.label ?? resolveModelOptionLabel(primaryModelName)}
                  </span>
                </div>
              ) : (
                <span className="text-[11px] text-muted-foreground/60">{t("matrix.unassigned")}</span>
              )}
            </>
          )}
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[380px] p-3.5">
        <div className="flex flex-col gap-3">
          <div className="flex items-start justify-between gap-2">
            <div className="flex min-w-0 flex-col gap-1">
              <div className="flex items-center gap-1.5">
                <CategoryIcon categoryKey={categoryKey} className="size-3.5 text-muted-foreground" />
                <code className="text-xs font-semibold text-foreground">{categoryKey}</code>
                <Badge
                  variant="outline"
                  className={cn("h-5 px-1.5 text-[10px]", difficultyToneClass(difficulty))}
                >
                  {t(difficultyLabelKey(difficulty))}
                </Badge>
              </div>
              <p className="text-[11px] text-muted-foreground">
                {difficulty === ""
                  ? t("matrix.popoverAnyHint")
                  : t("matrix.popoverExactHint")}
              </p>
            </div>
            {directModels.length > 0 ? (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="h-6 px-2 text-[11px] text-muted-foreground hover:text-destructive"
                onClick={() => handleModelsChange([])}
              >
                <Trash2 className="size-3" />
                {t("matrix.clearCell")}
              </Button>
            ) : null}
          </div>

          <ModelRouterModelChain
            models={directModels}
            chatModels={chatModels}
            modelMetaByName={modelMetaByName}
            disabled={disabled}
            compact
            emptyText={t("matrix.cellEmpty")}
            addLabel={t("matrix.addCandidate")}
            onChange={handleModelsChange}
          />

          {directModels.length > 0 ? (
            <div className="flex items-center justify-between border-t border-border/60 pt-2">
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="h-6 gap-1 px-2 text-[11px] text-muted-foreground"
                onClick={handleApplyToAllDifficulties}
              >
                <Copy className="size-3" />
                {t("matrix.copyToAllDifficulties")}
              </Button>
              <Button type="button" size="sm" className="h-6 px-2.5 text-[11px]" onClick={() => setOpen(false)}>
                {t("actions.done")}
              </Button>
            </div>
          ) : null}
        </div>
      </PopoverContent>
    </Popover>
  );
}

export function ModelRouterMatrix({
  policy,
  chatModels,
  modelMetaByName,
  disabled = false,
  onChange,
}: ModelRouterMatrixProps) {
  const t = useTranslations("adminModelRouter");
  const [viewMode, setViewMode] = React.useState<RulesViewMode>("matrix");
  const catalogFallbackModel = chatModels[0]?.name ?? null;

  const handleAddRuleRow = React.useCallback(() => {
    const firstCategory = policy.categories[0]?.key;
    const firstModel = chatModels[0]?.name;
    if (!firstCategory || !firstModel) return;
    onChange((current) => ({
      ...current,
      rules: [
        ...current.rules,
        { category: firstCategory, difficulty: "", models: [firstModel] },
      ],
    }));
  }, [chatModels, onChange, policy.categories]);

  const handleUpdateRuleAt = React.useCallback(
    (index: number, patch: Partial<{ category: string; difficulty: ModelRouterRuleDifficulty; models: string[] }>) => {
      onChange((current) => ({
        ...current,
        rules: current.rules.map((rule, i) => (i === index ? { ...rule, ...patch } : rule)),
      }));
    },
    [onChange],
  );

  const handleDeleteRuleAt = React.useCallback(
    (index: number) => {
      onChange((current) => ({
        ...current,
        rules: current.rules.filter((_, i) => i !== index),
      }));
    },
    [onChange],
  );

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="min-w-0">
          <h4 className="text-sm font-semibold text-foreground">{t("matrix.title")}</h4>
          <p className="text-xs text-muted-foreground">{t("matrix.description")}</p>
        </div>
        <div className="flex items-center gap-2">
          {viewMode === "list" ? (
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-8 gap-1.5 text-xs"
              disabled={disabled || policy.categories.length === 0 || chatModels.length === 0}
              onClick={handleAddRuleRow}
            >
              <Plus className="size-3.5" />
              {t("matrix.addRule")}
            </Button>
          ) : null}
          <Tabs
            value={viewMode}
            onValueChange={(value) => {
              if (isRulesViewMode(value)) {
                setViewMode(value);
              }
            }}
          >
            <TabsList className="h-8">
              <TabsTrigger value="matrix" className="gap-1.5 px-2.5 text-xs">
                <Grid2x2 className="size-3.5" />
                {t("matrix.viewMatrix")}
              </TabsTrigger>
              <TabsTrigger value="list" className="gap-1.5 px-2.5 text-xs">
                <List className="size-3.5" />
                {t("matrix.viewList", { count: policy.rules.length })}
              </TabsTrigger>
            </TabsList>
          </Tabs>
        </div>
      </div>

      {viewMode === "matrix" ? (
        <div className="overflow-x-auto rounded-lg border border-border/70 bg-card/30">
          <div className="min-w-[740px]">
            <div className="grid grid-cols-[170px_repeat(4,minmax(135px,1fr))] items-center gap-2 border-b border-border/70 bg-muted/40 px-3 py-2 text-[11px] font-medium text-muted-foreground">
              <span>{t("matrix.categoryColumn")}</span>
              {MATRIX_COLUMNS.map((col) => (
                <div key={col || "any"} className="flex items-center gap-1.5">
                  <Badge
                    variant="outline"
                    className={cn("h-5 px-1.5 text-[10px] font-medium", difficultyToneClass(col))}
                  >
                    {t(difficultyLabelKey(col))}
                  </Badge>
                  {col === "" ? (
                    <span className="truncate text-[10px] font-normal text-muted-foreground/80">
                      {t("matrix.anyColumnSub")}
                    </span>
                  ) : null}
                </div>
              ))}
            </div>

            <div className="divide-y divide-border/60">
              {policy.categories.map((category) => (
                <div
                  key={category.key}
                  className="grid grid-cols-[170px_repeat(4,minmax(135px,1fr))] items-stretch gap-2 px-3 py-2.5"
                >
                  <div className="flex flex-col justify-center gap-1 pr-2">
                    <div className="flex items-center gap-1.5">
                      <span className="inline-flex size-6 shrink-0 items-center justify-center rounded bg-muted text-foreground/75">
                        <CategoryIcon categoryKey={category.key} className="size-3.5" />
                      </span>
                      <code className="truncate text-xs font-semibold text-foreground">
                        {category.key}
                      </code>
                    </div>
                    {category.description ? (
                      <p className="line-clamp-2 text-[10px] leading-snug text-muted-foreground">
                        {category.description}
                      </p>
                    ) : null}
                  </div>
                  {MATRIX_COLUMNS.map((difficulty) => (
                    <MatrixCellEditor
                      key={`${category.key}-${difficulty || "any"}`}
                      categoryKey={category.key}
                      difficulty={difficulty}
                      policy={policy}
                      chatModels={chatModels}
                      modelMetaByName={modelMetaByName}
                      catalogFallbackModel={catalogFallbackModel}
                      disabled={disabled}
                      onChange={onChange}
                    />
                  ))}
                </div>
              ))}
            </div>
          </div>
        </div>
      ) : policy.rules.length === 0 ? (
        <div className="flex flex-col items-center justify-center gap-2 rounded-lg border border-dashed border-border/80 bg-muted/20 px-4 py-8 text-center">
          <p className="text-xs font-medium text-foreground">{t("matrix.emptyRulesTitle")}</p>
          <p className="max-w-md text-[11px] text-muted-foreground">
            {t("matrix.emptyRulesDescription")}
          </p>
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="mt-1 h-7 gap-1.5 text-xs"
            disabled={disabled || policy.categories.length === 0 || chatModels.length === 0}
            onClick={handleAddRuleRow}
          >
            <Plus className="size-3.5" />
            {t("matrix.addRule")}
          </Button>
        </div>
      ) : (
        <div className="flex flex-col gap-2.5">
          {policy.rules.map((rule, index) => (
            <div
              key={`${rule.category}-${rule.difficulty}-${index}`}
              className="flex flex-col gap-3 rounded-lg border border-border/70 bg-card/50 p-3"
            >
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-mono text-[11px] text-muted-foreground">#{index + 1}</span>
                  <Select
                    value={rule.category}
                    disabled={disabled}
                    onValueChange={(value) => handleUpdateRuleAt(index, { category: value })}
                  >
                    <SelectTrigger className="h-7 w-36 text-xs">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        {policy.categories.map((cat) => (
                          <SelectItem key={cat.key} value={cat.key}>
                            {cat.key}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>

                  <Select
                    value={rule.difficulty || ANY_DIFFICULTY_SELECT_VALUE}
                    disabled={disabled}
                    onValueChange={(value) => {
                      const nextDiff: ModelRouterRuleDifficulty =
                        value === ANY_DIFFICULTY_SELECT_VALUE
                          ? ""
                          : value === "easy" || value === "medium" || value === "hard"
                            ? value
                            : "";
                      handleUpdateRuleAt(index, { difficulty: nextDiff });
                    }}
                  >
                    <SelectTrigger className="h-7 w-32 text-xs">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        <SelectItem value={ANY_DIFFICULTY_SELECT_VALUE}>
                          {t("difficulty.any")}
                        </SelectItem>
                        <SelectItem value="easy">{t("difficulty.easy")}</SelectItem>
                        <SelectItem value="medium">{t("difficulty.medium")}</SelectItem>
                        <SelectItem value="hard">{t("difficulty.hard")}</SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>

                  <ArrowRight className="size-3.5 text-muted-foreground" />
                  <span className="text-[11px] text-muted-foreground">
                    {t("matrix.priorityOrderHint")}
                  </span>
                </div>

                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  className="size-7 text-muted-foreground hover:text-destructive"
                  disabled={disabled}
                  aria-label={t("matrix.deleteRule")}
                  onClick={() => handleDeleteRuleAt(index)}
                >
                  <Trash2 className="size-3.5" />
                </Button>
              </div>

              <ModelRouterModelChain
                models={rule.models}
                chatModels={chatModels}
                modelMetaByName={modelMetaByName}
                disabled={disabled}
                compact
                onChange={(nextModels) => {
                  if (nextModels.length === 0) {
                    handleDeleteRuleAt(index);
                  } else {
                    handleUpdateRuleAt(index, { models: nextModels });
                  }
                }}
              />
            </div>
          ))}
        </div>
      )}

      <div className="flex flex-wrap items-center gap-3 text-[11px] text-muted-foreground">
        <span className="inline-flex items-center gap-1.5">
          <span className="inline-block size-2.5 rounded-xs border border-border bg-card" />
          {t("matrix.legendDirect")}
        </span>
        <span className="inline-flex items-center gap-1.5">
          <span className="inline-block size-2.5 rounded-xs border border-dashed border-border bg-muted/30" />
          {t("matrix.legendInherited")}
        </span>
        <span>· {t("matrix.priorityNote")}</span>
      </div>
    </div>
  );
}
