"use client";

import * as React from "react";
import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  Check,
  Eye,
  Plus,
  Wrench,
  X,
} from "lucide-react";
import { useTranslations } from "next-intl";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { ModelOptionIcon, resolveModelOptionLabel } from "@/entities/model";
import {
  AdminSortableHandle,
  AdminSortableItem,
  AdminSortableList,
  moveSortableItem,
} from "@/features/admin/components/shared/sortable-list";
import {
  formatContextWindowLabel,
  type RouterChatModelMeta,
} from "@/features/admin/model/model-router-policy";
import { cn } from "@/lib/utils";

type ModelRouterModelChainProps = {
  models: string[];
  chatModels: RouterChatModelMeta[];
  modelMetaByName: ReadonlyMap<string, RouterChatModelMeta>;
  disabled?: boolean;
  compact?: boolean;
  emptyText?: string;
  addLabel?: string;
  onChange: (nextModels: string[]) => void;
};

/**
 * Drag-and-drop prioritized model chain with capability indicators and search picker.
 */
export function ModelRouterModelChain({
  models,
  chatModels,
  modelMetaByName,
  disabled = false,
  compact = false,
  emptyText,
  addLabel,
  onChange,
}: ModelRouterModelChainProps) {
  const t = useTranslations("adminModelRouter");
  const [pickerOpen, setPickerOpen] = React.useState(false);

  const selectedSet = React.useMemo(() => new Set(models), [models]);

  const handleMove = React.useCallback(
    (activeID: string, overID: string) => {
      const fromIndex = models.indexOf(activeID);
      const toIndex = models.indexOf(overID);
      if (fromIndex < 0 || toIndex < 0) return;
      onChange(moveSortableItem(models, fromIndex, toIndex));
    },
    [models, onChange],
  );

  const handleStepMove = React.useCallback(
    (index: number, delta: number) => {
      const target = index + delta;
      if (target < 0 || target >= models.length) return;
      onChange(moveSortableItem(models, index, target));
    },
    [models, onChange],
  );

  const handleRemove = React.useCallback(
    (name: string) => {
      onChange(models.filter((item) => item !== name));
    },
    [models, onChange],
  );

  const handleToggleAdd = React.useCallback(
    (name: string) => {
      if (selectedSet.has(name)) {
        onChange(models.filter((item) => item !== name));
      } else {
        onChange([...models, name]);
      }
    },
    [models, onChange, selectedSet],
  );

  return (
    <div className="flex flex-col gap-2">
      {models.length === 0 ? (
        emptyText ? (
          <div className="rounded-md border border-dashed border-border/80 bg-muted/20 px-3 py-2.5 text-xs text-muted-foreground">
            {emptyText}
          </div>
        ) : null
      ) : (
        <AdminSortableList items={models} disabled={disabled} onMove={handleMove}>
          <div className="flex flex-col gap-1.5">
            {models.map((name, index) => {
              const meta = modelMetaByName.get(name);
              const unavailable = !meta;
              return (
                <AdminSortableItem key={name} id={name} disabled={disabled}>
                  {({ attributes, listeners, isDragging }) => (
                    <div
                      className={cn(
                        "group flex items-center gap-2 rounded-md border bg-background px-2 py-1.5 transition-colors",
                        unavailable
                          ? "border-amber-500/50 bg-amber-500/5"
                          : "border-border/70 hover:border-border",
                        isDragging && "z-10 shadow-md ring-1 ring-primary/30",
                      )}
                    >
                      <AdminSortableHandle
                        attributes={attributes}
                        listeners={listeners}
                        disabled={disabled}
                        label={t("chain.dragToReorder")}
                      />
                      <span
                        className={cn(
                          "inline-flex h-5 min-w-5 shrink-0 items-center justify-center rounded px-1 font-mono text-[10px] font-semibold tabular-nums",
                          index === 0
                            ? "bg-primary/10 text-primary"
                            : "bg-muted text-muted-foreground",
                        )}
                      >
                        #{index + 1}
                      </span>
                      <ModelOptionIcon
                        iconUrl={meta?.iconUrl ?? null}
                        label={meta?.label ?? resolveModelOptionLabel(name)}
                        size={15}
                      />
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-1.5">
                          <span className="truncate text-xs font-medium text-foreground">
                            {meta?.label ?? resolveModelOptionLabel(name)}
                          </span>
                          {unavailable ? (
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <span className="inline-flex items-center gap-0.5 text-[10px] font-medium text-amber-600">
                                  <AlertTriangle className="size-3" />
                                  {t("chain.unavailable")}
                                </span>
                              </TooltipTrigger>
                              <TooltipContent>{t("chain.unavailableHint")}</TooltipContent>
                            </Tooltip>
                          ) : null}
                        </div>
                        {!compact && meta ? (
                          <div className="mt-0.5 flex flex-wrap items-center gap-1.5 text-[10px] text-muted-foreground">
                            <span className="font-mono">{name}</span>
                            {meta.contextWindow > 0 ? (
                              <span>· {formatContextWindowLabel(meta.contextWindow)}</span>
                            ) : null}
                            {meta.vision === true ? (
                              <span className="inline-flex items-center gap-0.5 text-foreground/75">
                                <Eye className="size-2.5" />
                                {t("capabilities.vision")}
                              </span>
                            ) : null}
                            {meta.tools === true ? (
                              <span className="inline-flex items-center gap-0.5 text-foreground/75">
                                <Wrench className="size-2.5" />
                                {t("capabilities.tools")}
                              </span>
                            ) : null}
                          </div>
                        ) : null}
                      </div>

                      <div className="flex shrink-0 items-center gap-0.5 opacity-80 transition-opacity group-hover:opacity-100">
                        {models.length > 1 ? (
                          <>
                            <Button
                              type="button"
                              variant="ghost"
                              size="icon"
                              className="size-6 text-muted-foreground"
                              disabled={disabled || index === 0}
                              aria-label={t("chain.moveUp")}
                              onClick={() => handleStepMove(index, -1)}
                            >
                              <ArrowUp className="size-3" />
                            </Button>
                            <Button
                              type="button"
                              variant="ghost"
                              size="icon"
                              className="size-6 text-muted-foreground"
                              disabled={disabled || index === models.length - 1}
                              aria-label={t("chain.moveDown")}
                              onClick={() => handleStepMove(index, 1)}
                            >
                              <ArrowDown className="size-3" />
                            </Button>
                          </>
                        ) : null}
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon"
                          className="size-6 text-muted-foreground hover:text-destructive"
                          disabled={disabled}
                          aria-label={t("chain.remove")}
                          onClick={() => handleRemove(name)}
                        >
                          <X className="size-3.5" />
                        </Button>
                      </div>
                    </div>
                  )}
                </AdminSortableItem>
              );
            })}
          </div>
        </AdminSortableList>
      )}

      <div className="flex items-center gap-2">
        <Popover open={pickerOpen} onOpenChange={setPickerOpen}>
          <PopoverTrigger asChild>
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-7 gap-1.5 text-xs"
              disabled={disabled || chatModels.length === 0}
            >
              <Plus className="size-3.5" />
              {addLabel ?? t("chain.addModel")}
            </Button>
          </PopoverTrigger>
          <PopoverContent align="start" className="w-[340px] p-0">
            <Command>
              <CommandInput placeholder={t("chain.searchPlaceholder")} />
              <CommandList className="max-h-64">
                <CommandEmpty>{t("chain.noModelsFound")}</CommandEmpty>
                <CommandGroup>
                  {chatModels.map((item) => {
                    const selected = selectedSet.has(item.name);
                    return (
                      <CommandItem
                        key={item.name}
                        value={`${item.label} ${item.name} ${item.vendor}`}
                        onSelect={() => handleToggleAdd(item.name)}
                        className="flex items-center gap-2"
                      >
                        <ModelOptionIcon iconUrl={item.iconUrl} label={item.label} size={15} />
                        <div className="min-w-0 flex-1">
                          <div className="truncate text-xs font-medium">{item.label}</div>
                          <div className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
                            <span className="truncate font-mono">{item.name}</span>
                            {item.contextWindow > 0 ? (
                              <span className="shrink-0">
                                {formatContextWindowLabel(item.contextWindow)}
                              </span>
                            ) : null}
                            {item.vision === true ? (
                              <Badge variant="secondary" className="h-4 px-1 text-[9px]">
                                {t("capabilities.visionShort")}
                              </Badge>
                            ) : null}
                            {item.tools === true ? (
                              <Badge variant="secondary" className="h-4 px-1 text-[9px]">
                                {t("capabilities.toolsShort")}
                              </Badge>
                            ) : null}
                          </div>
                        </div>
                        {selected ? <Check className="size-3.5 shrink-0 text-primary" /> : null}
                      </CommandItem>
                    );
                  })}
                </CommandGroup>
              </CommandList>
            </Command>
          </PopoverContent>
        </Popover>
        {chatModels.length === 0 ? (
          <span className="text-[11px] text-muted-foreground">{t("chain.noActiveChatModels")}</span>
        ) : null}
      </div>
    </div>
  );
}
