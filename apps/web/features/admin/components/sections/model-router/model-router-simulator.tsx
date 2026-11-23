"use client";

import * as React from "react";
import {
  AlertTriangle,
  ArrowRight,
  Bot,
  CheckCircle2,
  ChevronDown,
  Eye,
  Filter,
  Globe,
  Layers,
  Play,
  Route,
  Sparkles,
  Terminal,
  Timer,
  Wrench,
  XCircle,
  Zap,
} from "lucide-react";
import { useTranslations } from "next-intl";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Slider } from "@/components/ui/slider";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { ModelOptionIcon, ModelSelect, resolveModelOptionLabel } from "@/entities/model";
import type { AdminModelRouterTestRequest, AdminModelRouterTestResponse } from "@/features/admin/api";
import { CategoryIcon } from "@/features/admin/components/sections/model-router/model-router-categories";
import {
  formatContextWindowLabel,
  isModelRouterDifficulty,
  type ModelRouterDifficulty,
  type ModelRouterPolicy,
  type RouterChatModelMeta,
  simulateModelRouterLocally,
} from "@/features/admin/model/model-router-policy";
import { cn } from "@/lib/utils";
import { isOneOf } from "@/shared/lib/type-guards";

const SIMULATOR_MODES = ["live", "dry_run"] as const;
type SimulatorMode = (typeof SIMULATOR_MODES)[number];
const isSimulatorMode = isOneOf(SIMULATOR_MODES);

const NO_PREVIOUS_MODEL_VALUE = "__none__";
const COMMON_NATIVE_TOOLS = ["web_search", "code_interpreter", "file_search"] as const;

type SamplePromptPreset = {
  id: string;
  needsVision?: boolean;
  needsTools?: boolean;
  nativeTools?: string[];
  estimatedTokens?: number;
  category: string;
  difficulty: ModelRouterDifficulty;
};

const SAMPLE_PRESETS: readonly SamplePromptPreset[] = [
  { id: "greeting", category: "chat", difficulty: "easy" },
  { id: "algorithm", category: "coding", difficulty: "hard" },
  { id: "visionChart", needsVision: true, category: "analysis", difficulty: "medium" },
  { id: "webTool", needsTools: true, nativeTools: ["web_search"], category: "analysis", difficulty: "medium" },
  { id: "longContext", estimatedTokens: 160000, category: "writing", difficulty: "hard" },
];

type ModelRouterSimulatorProps = {
  policy: ModelRouterPolicy;
  parseError: string | null;
  chatModels: RouterChatModelMeta[];
  modelMetaByName: ReadonlyMap<string, RouterChatModelMeta>;
  testing: boolean;
  liveTestResult: AdminModelRouterTestResponse | null;
  liveTestError: string | null;
  onRunLiveTest: (
    request: Omit<AdminModelRouterTestRequest, "policyJSON">,
  ) => Promise<AdminModelRouterTestResponse | null>;
};

function decisionSourceBadgeTone(source: string): string {
  switch (source) {
    case "rule":
      return "border-emerald-500/40 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300";
    case "sticky":
      return "border-sky-500/40 bg-sky-500/10 text-sky-700 dark:text-sky-300";
    case "default":
      return "border-violet-500/40 bg-violet-500/10 text-violet-700 dark:text-violet-300";
    default:
      return "border-amber-500/40 bg-amber-500/10 text-amber-700 dark:text-amber-300";
  }
}

export function ModelRouterSimulator({
  policy,
  parseError,
  chatModels,
  modelMetaByName,
  testing,
  liveTestResult,
  liveTestError,
  onRunLiveTest,
}: ModelRouterSimulatorProps) {
  const t = useTranslations("adminModelRouter");
  const [mode, setMode] = React.useState<SimulatorMode>("live");
  const [message, setMessage] = React.useState(() => t("simulator.samples.algorithm.prompt"));
  const [previousModel, setPreviousModel] = React.useState(NO_PREVIOUS_MODEL_VALUE);
  const [needsVision, setNeedsVision] = React.useState(false);
  const [needsTools, setNeedsTools] = React.useState(false);
  const [nativeToolTypes, setNativeToolTypes] = React.useState<string[]>([]);
  const [estimatedTokens, setEstimatedTokens] = React.useState(0);
  const [rawOutputOpen, setRawOutputOpen] = React.useState(false);

  // Dry-run controls
  const [simCategory, setSimCategory] = React.useState(policy.categories[0]?.key ?? "chat");
  const [simDifficulty, setSimDifficulty] = React.useState<ModelRouterDifficulty>("medium");
  const [simSwitch, setSimSwitch] = React.useState(true);
  const [simFailure, setSimFailure] = React.useState(false);

  React.useEffect(() => {
    if (policy.categories.length > 0 && !policy.categories.some((item) => item.key === simCategory)) {
      setSimCategory(policy.categories[0].key);
    }
  }, [policy.categories, simCategory]);

  const previousModelOptions = React.useMemo(
    () => [
      { label: t("simulator.noPreviousModel"), value: NO_PREVIOUS_MODEL_VALUE, iconUrl: null },
      ...chatModels.map((item) => ({
        label: item.label,
        value: item.name,
        iconUrl: item.iconUrl,
      })),
    ],
    [chatModels, t],
  );

  const toggleNativeTool = React.useCallback((tool: string) => {
    setNativeToolTypes((current) =>
      current.includes(tool) ? current.filter((item) => item !== tool) : [...current, tool],
    );
  }, []);

  const applySamplePreset = React.useCallback(
    (preset: SamplePromptPreset) => {
      setMessage(t(`simulator.samples.${preset.id}.prompt`));
      setNeedsVision(Boolean(preset.needsVision));
      setNeedsTools(Boolean(preset.needsTools));
      setNativeToolTypes(preset.nativeTools ?? []);
      setEstimatedTokens(preset.estimatedTokens ?? 0);
      if (policy.categories.some((cat) => cat.key === preset.category)) {
        setSimCategory(preset.category);
      }
      setSimDifficulty(preset.difficulty);
      setSimFailure(false);
    },
    [policy.categories, t],
  );

  const dryRunResult = React.useMemo(
    () =>
      simulateModelRouterLocally(policy, chatModels, {
        message,
        previousModel: previousModel === NO_PREVIOUS_MODEL_VALUE ? "" : previousModel,
        needsVision,
        needsTools,
        nativeToolTypes,
        estimatedTokens,
        simulateFailure: simFailure,
        category: simCategory,
        difficulty: simDifficulty,
        switchModel: simSwitch,
        reason: t("simulator.dryRunReason"),
      }),
    [
      chatModels,
      estimatedTokens,
      message,
      nativeToolTypes,
      needsTools,
      needsVision,
      policy,
      previousModel,
      simCategory,
      simDifficulty,
      simFailure,
      simSwitch,
      t,
    ],
  );

  const activeResult = mode === "dry_run" ? dryRunResult : liveTestResult;

  const handleRunLive = React.useCallback(() => {
    void onRunLiveTest({
      message: message.trim(),
      previousModel: previousModel === NO_PREVIOUS_MODEL_VALUE ? "" : previousModel,
      needsVision,
      needsTools,
      nativeToolTypes,
      estimatedTokens,
      dryRun: false,
    });
  }, [
    estimatedTokens,
    message,
    nativeToolTypes,
    needsTools,
    needsVision,
    onRunLiveTest,
    previousModel,
  ]);

  const formatFailureReason = React.useCallback(
    (reason: string): string => {
      if (reason === "missing_vision") return t("simulator.reasons.missingVision");
      if (reason === "missing_tools") return t("simulator.reasons.missingTools");
      if (reason === "context_exceeded") return t("simulator.reasons.contextExceeded");
      if (reason.startsWith("missing_native_tool:")) {
        return t("simulator.reasons.missingNativeTool", {
          tool: reason.slice("missing_native_tool:".length),
        });
      }
      return reason;
    },
    [t],
  );

  const formatSourceLabel = React.useCallback(
    (source: string): string => {
      switch (source) {
        case "rule":
          return t("sources.rule");
        case "sticky":
          return t("sources.sticky");
        case "default":
          return t("sources.default");
        case "fallback":
          return t("sources.fallback");
        default:
          return source;
      }
    },
    [t],
  );

  const passedCount = activeResult
    ? activeResult.candidates.filter((item) => item.passed).length
    : 0;

  return (
    <div className="grid gap-4 lg:grid-cols-[minmax(320px,420px)_1fr]">
      {/* Left: Interactive Scenario Controls */}
      <Card className="gap-4 py-4 shadow-none">
        <CardHeader className="px-4">
          <div className="flex items-center justify-between gap-2">
            <CardTitle className="text-sm">{t("simulator.controlsTitle")}</CardTitle>
            <Tabs
              value={mode}
              onValueChange={(value) => {
                if (isSimulatorMode(value)) {
                  setMode(value);
                }
              }}
            >
              <TabsList className="h-7">
                <TabsTrigger value="live" className="gap-1 px-2 text-[11px]">
                  <Zap className="size-3" />
                  {t("simulator.modeLive")}
                </TabsTrigger>
                <TabsTrigger value="dry_run" className="gap-1 px-2 text-[11px]">
                  <Route className="size-3" />
                  {t("simulator.modeDryRun")}
                </TabsTrigger>
              </TabsList>
            </Tabs>
          </div>
          <CardDescription className="text-xs">
            {mode === "live"
              ? t("simulator.modeLiveDescription")
              : t("simulator.modeDryRunDescription")}
          </CardDescription>
        </CardHeader>

        <CardContent className="flex flex-col gap-4 px-4">
          {/* Quick sample prompts */}
          <div className="flex flex-col gap-1.5">
            <span className="text-[11px] font-medium text-muted-foreground">
              {t("simulator.quickSamples")}
            </span>
            <div className="flex flex-wrap gap-1.5">
              {SAMPLE_PRESETS.map((preset) => (
                <Button
                  key={preset.id}
                  type="button"
                  variant="outline"
                  size="sm"
                  className="h-6 rounded-full px-2.5 text-[11px] font-normal"
                  onClick={() => applySamplePreset(preset)}
                >
                  {t(`simulator.samples.${preset.id}.label`)}
                </Button>
              ))}
            </div>
          </div>

          <FieldGroup className="gap-3.5">
            {mode === "live" ? (
              <Field>
                <FieldLabel htmlFor="sim-message" className="text-xs">
                  {t("simulator.messageLabel")}
                </FieldLabel>
                <Textarea
                  id="sim-message"
                  rows={3}
                  value={message}
                  placeholder={t("simulator.messagePlaceholder")}
                  className="text-xs"
                  onChange={(event) => setMessage(event.target.value)}
                />
              </Field>
            ) : (
              <div className="flex flex-col gap-3 rounded-lg border border-border/70 bg-muted/25 p-3">
                <div className="flex items-center justify-between gap-2">
                  <span className="text-xs font-medium text-foreground">
                    {t("simulator.simulatedClassification")}
                  </span>
                  <div className="flex items-center gap-2">
                    <span className="text-[11px] text-muted-foreground">
                      {t("simulator.simulateTimeout")}
                    </span>
                    <Switch checked={simFailure} onCheckedChange={setSimFailure} />
                  </div>
                </div>

                {!simFailure ? (
                  <div className="grid grid-cols-2 gap-2">
                    <Field>
                      <FieldLabel className="text-[11px]">{t("simulator.categoryLabel")}</FieldLabel>
                      <Select value={simCategory} onValueChange={setSimCategory}>
                        <SelectTrigger className="h-8 text-xs">
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
                    </Field>
                    <Field>
                      <FieldLabel className="text-[11px]">
                        {t("simulator.difficultyLabel")}
                      </FieldLabel>
                      <Select
                        value={simDifficulty}
                        onValueChange={(value) => {
                          if (isModelRouterDifficulty(value)) {
                            setSimDifficulty(value);
                          }
                        }}
                      >
                        <SelectTrigger className="h-8 text-xs">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectGroup>
                            <SelectItem value="easy">{t("difficulty.easy")}</SelectItem>
                            <SelectItem value="medium">{t("difficulty.medium")}</SelectItem>
                            <SelectItem value="hard">{t("difficulty.hard")}</SelectItem>
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                    </Field>
                  </div>
                ) : null}

                {!simFailure && previousModel !== NO_PREVIOUS_MODEL_VALUE ? (
                  <div className="flex items-center justify-between gap-2 pt-0.5">
                    <span className="text-[11px] text-muted-foreground">
                      {t("simulator.switchSignalLabel")}
                    </span>
                    <Switch checked={simSwitch} onCheckedChange={setSimSwitch} />
                  </div>
                ) : null}
              </div>
            )}

            {/* Hard constraints */}
            <div className="flex flex-col gap-2.5 rounded-lg border border-border/70 bg-muted/20 p-3">
              <span className="text-xs font-medium text-foreground">
                {t("simulator.constraintsTitle")}
              </span>

              <div className="flex flex-wrap gap-2">
                <Button
                  type="button"
                  variant={needsVision ? "default" : "outline"}
                  size="sm"
                  className="h-7 gap-1.5 text-xs"
                  onClick={() => setNeedsVision((v) => !v)}
                >
                  <Eye className="size-3.5" />
                  {t("capabilities.vision")}
                </Button>
                <Button
                  type="button"
                  variant={needsTools ? "default" : "outline"}
                  size="sm"
                  className="h-7 gap-1.5 text-xs"
                  onClick={() => setNeedsTools((v) => !v)}
                >
                  <Wrench className="size-3.5" />
                  {t("capabilities.tools")}
                </Button>
              </div>

              <div className="flex flex-col gap-1.5">
                <span className="text-[11px] text-muted-foreground">
                  {t("simulator.nativeToolsLabel")}
                </span>
                <div className="flex flex-wrap gap-1.5">
                  {COMMON_NATIVE_TOOLS.map((tool) => {
                    const active = nativeToolTypes.includes(tool);
                    return (
                      <Button
                        key={tool}
                        type="button"
                        variant={active ? "secondary" : "outline"}
                        size="sm"
                        className={cn(
                          "h-6 gap-1 rounded-full px-2 text-[11px] font-mono",
                          active && "border-primary/40 text-primary",
                        )}
                        onClick={() => toggleNativeTool(tool)}
                      >
                        <Globe className="size-3" />
                        {tool}
                      </Button>
                    );
                  })}
                </div>
              </div>

              <div className="flex flex-col gap-1.5 pt-1">
                <div className="flex items-center justify-between text-[11px]">
                  <span className="text-muted-foreground">{t("simulator.estimatedTokensLabel")}</span>
                  <span className="font-mono font-medium text-foreground tabular-nums">
                    {estimatedTokens === 0
                      ? t("simulator.autoEstimated")
                      : formatContextWindowLabel(estimatedTokens)}
                  </span>
                </div>
                <Slider
                  min={0}
                  max={256000}
                  step={8000}
                  value={[estimatedTokens]}
                  onValueChange={([value]) => setEstimatedTokens(value ?? 0)}
                />
                <div className="flex justify-between text-[10px] text-muted-foreground tabular-nums">
                  <span>{t("simulator.autoShort")}</span>
                  <span>64K</span>
                  <span>128K</span>
                  <span>256K</span>
                </div>
              </div>
            </div>

            {/* Sticky previous model */}
            <Field>
              <FieldLabel className="text-xs">{t("simulator.previousModelLabel")}</FieldLabel>
              <ModelSelect
                value={previousModel}
                fallbackValue={NO_PREVIOUS_MODEL_VALUE}
                options={previousModelOptions}
                valueAlign="start"
                triggerClassName="w-full"
                onChange={setPreviousModel}
              />
              <FieldDescription className="text-[11px]">
                {t("simulator.previousModelHint")}
              </FieldDescription>
            </Field>
          </FieldGroup>

          {mode === "live" ? (
            <Button
              type="button"
              disabled={testing || Boolean(parseError) || chatModels.length === 0 || !message.trim()}
              onClick={handleRunLive}
            >
              {testing ? <Spinner data-icon="inline-start" /> : <Play data-icon="inline-start" />}
              {testing ? t("simulator.runningButton") : t("simulator.runLiveButton")}
            </Button>
          ) : null}
        </CardContent>
      </Card>

      {/* Right: 4-Stage Visual Routing Pipeline */}
      <div className="flex flex-col gap-3">
        {liveTestError && mode === "live" ? (
          <Alert variant="destructive">
            <AlertTriangle />
            <AlertTitle>{t("simulator.liveErrorTitle")}</AlertTitle>
            <AlertDescription>{liveTestError}</AlertDescription>
          </Alert>
        ) : null}

        {!activeResult ? (
          <Card className="flex min-h-[420px] items-center justify-center border-dashed py-8 shadow-none">
            <CardContent className="flex max-w-sm flex-col items-center gap-2 text-center">
              <span className="inline-flex size-10 items-center justify-center rounded-full bg-muted text-muted-foreground">
                <Sparkles className="size-5" />
              </span>
              <p className="text-sm font-medium text-foreground">{t("simulator.emptyTitle")}</p>
              <p className="text-xs text-muted-foreground">{t("simulator.emptyDescription")}</p>
            </CardContent>
          </Card>
        ) : (
          <div className="flex flex-col gap-3">
            {/* Stage 4 Highlight Banner at Top for Immediate Clarity */}
            <Card className="gap-3 border-primary/35 bg-primary/[0.03] py-4 shadow-none">
              <CardHeader className="px-4">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div className="flex items-center gap-2">
                    <Badge
                      variant="outline"
                      className={cn(
                        "h-5 px-2 text-[11px] font-medium",
                        decisionSourceBadgeTone(activeResult.decision.source),
                      )}
                    >
                      {formatSourceLabel(activeResult.decision.source)}
                    </Badge>
                    {activeResult.decision.category ? (
                      <Badge variant="secondary" className="h-5 gap-1 px-2 text-[11px]">
                        <CategoryIcon
                          categoryKey={activeResult.decision.category}
                          className="size-3"
                        />
                        {activeResult.decision.category}
                      </Badge>
                    ) : null}
                    {activeResult.decision.difficulty ? (
                      <Badge variant="outline" className="h-5 px-2 text-[11px]">
                        {t(`difficulty.${activeResult.decision.difficulty}`)}
                      </Badge>
                    ) : null}
                  </div>
                  {activeResult.classifier.attempted ? (
                    <span className="inline-flex items-center gap-1 font-mono text-[11px] text-muted-foreground tabular-nums">
                      <Timer className="size-3" />
                      {activeResult.classifier.latencyMS} ms
                    </span>
                  ) : null}
                </div>
              </CardHeader>
              <CardContent className="flex flex-col gap-2 px-4">
                <div className="flex items-center gap-3">
                  <span className="inline-flex size-9 shrink-0 items-center justify-center rounded-lg border border-border/70 bg-background">
                    <ModelOptionIcon
                      iconUrl={modelMetaByName.get(activeResult.decision.model)?.iconUrl ?? null}
                      label={resolveModelOptionLabel(activeResult.decision.model)}
                      size={20}
                    />
                  </span>
                  <div className="min-w-0 flex-1">
                    <p className="text-[11px] text-muted-foreground">
                      {t("simulator.finalModelLabel")}
                    </p>
                    <p className="truncate text-base font-semibold text-foreground">
                      {resolveModelOptionLabel(activeResult.decision.model)}
                    </p>
                  </div>
                  <code className="hidden rounded bg-muted px-2 py-0.5 text-[11px] text-muted-foreground sm:inline">
                    {activeResult.decision.model}
                  </code>
                </div>
                {activeResult.decision.reason ? (
                  <p className="rounded-md bg-muted/50 px-2.5 py-1.5 text-xs text-muted-foreground">
                    “{activeResult.decision.reason}”
                  </p>
                ) : null}
              </CardContent>
            </Card>

            {/* Stage 1: Hard Constraints Filter */}
            <Card className="gap-3 py-4 shadow-none">
              <CardHeader className="px-4">
                <div className="flex items-center justify-between gap-2">
                  <div className="flex items-center gap-2">
                    <span className="inline-flex size-6 items-center justify-center rounded-full bg-muted text-xs font-semibold text-foreground">
                      1
                    </span>
                    <Filter className="size-3.5 text-muted-foreground" />
                    <CardTitle className="text-xs font-semibold">
                      {t("simulator.stage1Title")}
                    </CardTitle>
                  </div>
                  <Badge variant="secondary" className="font-mono text-[11px] tabular-nums">
                    {t("simulator.passedSummary", {
                      passed: passedCount,
                      total: activeResult.candidates.length,
                    })}
                  </Badge>
                </div>
              </CardHeader>
              <CardContent className="flex flex-col gap-2.5 px-4">
                {activeResult.constraintsRelaxed ? (
                  <Alert>
                    <AlertTriangle />
                    <AlertTitle className="text-xs">{t("simulator.relaxedTitle")}</AlertTitle>
                    <AlertDescription className="text-[11px]">
                      {t("simulator.relaxedDescription")}
                    </AlertDescription>
                  </Alert>
                ) : null}

                <div className="grid gap-1.5 sm:grid-cols-2">
                  {activeResult.candidates.map((candidate) => {
                    const meta = modelMetaByName.get(candidate.name);
                    const isWinner = candidate.name === activeResult.decision.model;
                    return (
                      <div
                        key={candidate.name}
                        className={cn(
                          "flex flex-col gap-1 rounded-md border px-2.5 py-2 text-xs",
                          isWinner
                            ? "border-primary/50 bg-primary/5"
                            : candidate.passed
                              ? "border-border/70 bg-background"
                              : "border-border/50 bg-muted/30 opacity-75",
                        )}
                      >
                        <div className="flex items-center gap-1.5">
                          {candidate.passed ? (
                            <CheckCircle2 className="size-3.5 shrink-0 text-emerald-600" />
                          ) : (
                            <XCircle className="size-3.5 shrink-0 text-destructive" />
                          )}
                          <ModelOptionIcon
                            iconUrl={meta?.iconUrl ?? null}
                            label={meta?.label ?? resolveModelOptionLabel(candidate.name)}
                            size={13}
                          />
                          <span
                            className={cn(
                              "min-w-0 flex-1 truncate font-medium",
                              !candidate.passed && "line-through text-muted-foreground",
                            )}
                          >
                            {meta?.label ?? resolveModelOptionLabel(candidate.name)}
                          </span>
                          {candidate.contextWindow > 0 ? (
                            <span className="shrink-0 font-mono text-[10px] text-muted-foreground">
                              {formatContextWindowLabel(candidate.contextWindow)}
                            </span>
                          ) : null}
                        </div>
                        {candidate.failedReasons.length > 0 ? (
                          <div className="flex flex-wrap gap-1 pl-5">
                            {candidate.failedReasons.map((reason) => (
                              <Badge
                                key={reason}
                                variant="destructive"
                                className="h-4 px-1.5 text-[10px] font-normal"
                              >
                                {formatFailureReason(reason)}
                              </Badge>
                            ))}
                          </div>
                        ) : null}
                      </div>
                    );
                  })}
                </div>
              </CardContent>
            </Card>

            {/* Stage 2: Classifier */}
            <Card className="gap-3 py-4 shadow-none">
              <CardHeader className="px-4">
                <div className="flex items-center justify-between gap-2">
                  <div className="flex items-center gap-2">
                    <span className="inline-flex size-6 items-center justify-center rounded-full bg-muted text-xs font-semibold text-foreground">
                      2
                    </span>
                    <Bot className="size-3.5 text-muted-foreground" />
                    <CardTitle className="text-xs font-semibold">
                      {t("simulator.stage2Title")}
                    </CardTitle>
                  </div>
                  {activeResult.classifier.routerModel ? (
                    <Badge variant="outline" className="gap-1 font-mono text-[10px]">
                      <ModelOptionIcon
                        iconUrl={modelMetaByName.get(activeResult.classifier.routerModel)?.iconUrl ?? null}
                        label={activeResult.classifier.routerModel}
                        size={12}
                      />
                      {activeResult.classifier.routerModel}
                    </Badge>
                  ) : null}
                </div>
              </CardHeader>
              <CardContent className="flex flex-col gap-2.5 px-4 text-xs">
                {activeResult.classifier.error ? (
                  <Alert variant="destructive">
                    <AlertTriangle />
                    <AlertTitle className="text-xs">{t("simulator.classifierFailedTitle")}</AlertTitle>
                    <AlertDescription className="text-[11px]">
                      {activeResult.classifier.error}
                    </AlertDescription>
                  </Alert>
                ) : null}

                {activeResult.classifier.skippedReason ? (
                  <p className="text-xs text-muted-foreground">
                    {t(`simulator.skipped.${activeResult.classifier.skippedReason}`)}
                  </p>
                ) : null}

                {activeResult.classifier.classification ? (
                  <div className="grid grid-cols-3 gap-2">
                    <div className="rounded-md bg-muted/40 px-2.5 py-2">
                      <span className="block text-[10px] text-muted-foreground">
                        {t("simulator.categoryLabel")}
                      </span>
                      <span className="mt-0.5 inline-flex items-center gap-1 font-mono text-xs font-semibold text-foreground">
                        <CategoryIcon
                          categoryKey={activeResult.classifier.classification.category}
                          className="size-3"
                        />
                        {activeResult.classifier.classification.category}
                      </span>
                    </div>
                    <div className="rounded-md bg-muted/40 px-2.5 py-2">
                      <span className="block text-[10px] text-muted-foreground">
                        {t("simulator.difficultyLabel")}
                      </span>
                      <span className="mt-0.5 block text-xs font-semibold text-foreground">
                        {t(`difficulty.${activeResult.classifier.classification.difficulty}`)}
                      </span>
                    </div>
                    <div className="rounded-md bg-muted/40 px-2.5 py-2">
                      <span className="block text-[10px] text-muted-foreground">
                        {t("simulator.switchSignalShort")}
                      </span>
                      <span className="mt-0.5 block text-xs font-semibold text-foreground">
                        {activeResult.classifier.classification.switch
                          ? t("simulator.switchYes")
                          : t("simulator.switchNo")}
                      </span>
                    </div>
                  </div>
                ) : null}

                {activeResult.classifier.rawOutput ? (
                  <Collapsible open={rawOutputOpen} onOpenChange={setRawOutputOpen}>
                    <CollapsibleTrigger asChild>
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        className="h-6 gap-1 px-1.5 text-[11px] text-muted-foreground"
                      >
                        <Terminal className="size-3" />
                        {t("simulator.rawOutputLabel")}
                        <ChevronDown
                          className={cn(
                            "size-3 transition-transform",
                            rawOutputOpen && "rotate-180",
                          )}
                        />
                      </Button>
                    </CollapsibleTrigger>
                    <CollapsibleContent>
                      <pre className="mt-1.5 max-h-36 overflow-auto rounded-md bg-muted/50 p-2.5 font-mono text-[11px] text-foreground">
                        {activeResult.classifier.rawOutput}
                      </pre>
                    </CollapsibleContent>
                  </Collapsible>
                ) : null}
              </CardContent>
            </Card>

            {/* Stage 3: Rule Match & Priority Selection */}
            <Card className="gap-3 py-4 shadow-none">
              <CardHeader className="px-4">
                <div className="flex items-center gap-2">
                  <span className="inline-flex size-6 items-center justify-center rounded-full bg-muted text-xs font-semibold text-foreground">
                    3
                  </span>
                  <Layers className="size-3.5 text-muted-foreground" />
                  <CardTitle className="text-xs font-semibold">
                    {t("simulator.stage3Title")}
                  </CardTitle>
                </div>
              </CardHeader>
              <CardContent className="flex flex-col gap-2 px-4 text-xs">
                {activeResult.decision.source === "sticky" ? (
                  <p className="text-xs text-muted-foreground">
                    {t("simulator.stickyExplanation", { model: activeResult.decision.model })}
                  </p>
                ) : activeResult.matchedRules.length === 0 ? (
                  <p className="text-xs text-muted-foreground">
                    {t("simulator.noMatchedRuleExplanation")}
                  </p>
                ) : (
                  <div className="flex flex-col gap-2">
                    {activeResult.matchedRules.map((rule) => (
                      <div
                        key={`${rule.index}-${rule.category}-${rule.difficulty}`}
                        className="flex flex-wrap items-center gap-1.5 rounded-md border border-border/70 bg-muted/25 px-2.5 py-2"
                      >
                        <Badge variant="outline" className="h-5 font-mono text-[10px]">
                          #{rule.index + 1}
                        </Badge>
                        <code className="text-xs font-medium">{rule.category}</code>
                        <Badge variant="secondary" className="h-5 text-[10px]">
                          {rule.difficulty
                            ? t(`difficulty.${rule.difficulty}`)
                            : t("difficulty.any")}
                        </Badge>
                        <ArrowRight className="size-3 text-muted-foreground" />
                        <div className="flex flex-wrap items-center gap-1">
                          {rule.models.map((modelName, idx) => {
                            const selected =
                              modelName === activeResult.decision.model &&
                              activeResult.decision.source === "rule";
                            return (
                              <React.Fragment key={modelName}>
                                {idx > 0 ? (
                                  <span className="text-[10px] text-muted-foreground">→</span>
                                ) : null}
                                <Badge
                                  variant={selected ? "default" : "outline"}
                                  className="h-5 gap-1 px-1.5 font-mono text-[10px]"
                                >
                                  {modelName}
                                </Badge>
                              </React.Fragment>
                            );
                          })}
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </CardContent>
            </Card>
          </div>
        )}
      </div>
    </div>
  );
}
