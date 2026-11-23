"use client";

import * as React from "react";
import { ChevronDown, Cpu, Eye, Globe, Wrench } from "lucide-react";
import { useTranslations } from "next-intl";

import { Badge } from "@/components/ui/badge";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { ModelOptionIcon } from "@/entities/model";
import {
  formatContextWindowLabel,
  normalizeNativeToolFamily,
  type RouterChatModelMeta,
} from "@/features/admin/model/model-router-policy";
import { cn } from "@/lib/utils";

function CapabilityStateBadge({
  value,
  yesLabel,
  noLabel,
  unknownLabel,
}: {
  value: boolean | null;
  yesLabel: string;
  noLabel: string;
  unknownLabel: string;
}) {
  if (value === true) {
    return (
      <Badge
        variant="outline"
        className="h-5 border-emerald-500/35 bg-emerald-500/10 px-1.5 text-[10px] font-normal text-emerald-700 dark:text-emerald-300"
      >
        {yesLabel}
      </Badge>
    );
  }
  if (value === false) {
    return (
      <Badge
        variant="outline"
        className="h-5 border-destructive/30 bg-destructive/5 px-1.5 text-[10px] font-normal text-destructive"
      >
        {noLabel}
      </Badge>
    );
  }
  return (
    <Badge variant="outline" className="h-5 px-1.5 text-[10px] font-normal text-muted-foreground">
      {unknownLabel}
    </Badge>
  );
}

export function ModelRouterCandidatesPanel({
  chatModels,
  referencedModels,
}: {
  chatModels: RouterChatModelMeta[];
  referencedModels: string[];
}) {
  const t = useTranslations("adminModelRouter");
  const [open, setOpen] = React.useState(false);
  const referencedSet = React.useMemo(() => new Set(referencedModels), [referencedModels]);

  return (
    <Collapsible
      open={open}
      onOpenChange={setOpen}
      className="rounded-lg border border-border/70 bg-card/40"
    >
      <CollapsibleTrigger asChild>
        <button
          type="button"
          className="flex w-full items-center justify-between gap-2 px-4 py-3 text-left transition-colors hover:bg-muted/35"
        >
          <div className="flex min-w-0 items-center gap-2">
            <Cpu className="size-4 shrink-0 text-muted-foreground" />
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <span className="text-xs font-semibold text-foreground">
                  {t("candidatesPanel.title")}
                </span>
                <Badge variant="secondary" className="h-4 px-1.5 font-mono text-[10px]">
                  {chatModels.length}
                </Badge>
              </div>
              <p className="truncate text-[11px] text-muted-foreground">
                {t("candidatesPanel.description")}
              </p>
            </div>
          </div>
          <ChevronDown
            className={cn(
              "size-4 shrink-0 text-muted-foreground transition-transform",
              open && "rotate-180",
            )}
          />
        </button>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="border-t border-border/60">
          {chatModels.length === 0 ? (
            <div className="px-4 py-6 text-center text-xs text-muted-foreground">
              {t("chain.noActiveChatModels")}
            </div>
          ) : (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-12 text-center text-[11px]">#</TableHead>
                    <TableHead className="text-[11px]">{t("candidatesPanel.colModel")}</TableHead>
                    <TableHead className="text-[11px]">{t("candidatesPanel.colContext")}</TableHead>
                    <TableHead className="text-[11px]">
                      <span className="inline-flex items-center gap-1">
                        <Eye className="size-3" />
                        {t("capabilities.vision")}
                      </span>
                    </TableHead>
                    <TableHead className="text-[11px]">
                      <span className="inline-flex items-center gap-1">
                        <Wrench className="size-3" />
                        {t("capabilities.tools")}
                      </span>
                    </TableHead>
                    <TableHead className="text-[11px]">
                      <span className="inline-flex items-center gap-1">
                        <Globe className="size-3" />
                        {t("candidatesPanel.colNativeTools")}
                      </span>
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {chatModels.map((item, idx) => {
                    const families = Array.from(
                      new Set(item.nativeToolTypes.map(normalizeNativeToolFamily).filter(Boolean)),
                    );
                    const inPolicy = referencedSet.has(item.name);
                    return (
                      <TableRow key={item.name}>
                        <TableCell className="text-center font-mono text-[11px] text-muted-foreground tabular-nums">
                          {idx + 1}
                        </TableCell>
                        <TableCell>
                          <div className="flex items-center gap-2">
                            <ModelOptionIcon iconUrl={item.iconUrl} label={item.label} size={15} />
                            <div className="min-w-0">
                              <div className="flex items-center gap-1.5">
                                <span className="truncate text-xs font-medium text-foreground">
                                  {item.label}
                                </span>
                                {inPolicy ? (
                                  <Badge variant="secondary" className="h-4 px-1 text-[9px]">
                                    {t("candidatesPanel.inPolicy")}
                                  </Badge>
                                ) : null}
                              </div>
                              <code className="text-[10px] text-muted-foreground">{item.name}</code>
                            </div>
                          </div>
                        </TableCell>
                        <TableCell className="font-mono text-xs tabular-nums">
                          {formatContextWindowLabel(item.contextWindow)}
                        </TableCell>
                        <TableCell>
                          <CapabilityStateBadge
                            value={item.vision}
                            yesLabel={t("capabilities.supported")}
                            noLabel={t("capabilities.unsupported")}
                            unknownLabel={t("capabilities.undeclared")}
                          />
                        </TableCell>
                        <TableCell>
                          <CapabilityStateBadge
                            value={item.tools}
                            yesLabel={t("capabilities.supported")}
                            noLabel={t("capabilities.unsupported")}
                            unknownLabel={t("capabilities.undeclared")}
                          />
                        </TableCell>
                        <TableCell>
                          {families.length === 0 ? (
                            <span className="text-[11px] text-muted-foreground">—</span>
                          ) : (
                            <div className="flex flex-wrap gap-1">
                              {families.map((family) => (
                                <Badge
                                  key={family}
                                  variant="outline"
                                  className="h-5 px-1.5 font-mono text-[10px]"
                                >
                                  {family}
                                </Badge>
                              ))}
                            </div>
                          )}
                        </TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            </div>
          )}
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}
