"use client";

import * as React from "react";
import {
  AlertTriangle,
  Bot,
  Braces,
  Check,
  Clock,
  Copy,
  Cpu,
  FlaskConical,
  History,
  Info,
  Layers,
  RotateCcw,
  Route,
  Save,
  SlidersHorizontal,
  Sparkles,
  Wand2,
} from "lucide-react";
import { useTranslations } from "next-intl";
import { toast } from "sonner";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { Slider } from "@/components/ui/slider";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ModelSelect } from "@/entities/model";
import { ModelRouterCandidatesPanel } from "@/features/admin/components/sections/model-router/model-router-candidates-panel";
import { ModelRouterCategories } from "@/features/admin/components/sections/model-router/model-router-categories";
import { ModelRouterMatrix } from "@/features/admin/components/sections/model-router/model-router-matrix";
import { ModelRouterModelChain } from "@/features/admin/components/sections/model-router/model-router-model-chain";
import { ModelRouterSimulator } from "@/features/admin/components/sections/model-router/model-router-simulator";
import { useAdminModelRouter } from "@/features/admin/hooks/use-admin-model-router";
import {
  buildSmartPolicyPreset,
  createDefaultModelRouterPolicy,
  MODEL_ROUTER_FOLLOW_TASK_MODEL,
  MODEL_ROUTER_HISTORY_PRESETS,
  MODEL_ROUTER_LIMITS,
  MODEL_ROUTER_TIMEOUT_PRESETS,
  type ModelRouterPolicyPresetID,
  pruneUnavailableModelsFromPolicy,
  stringifyModelRouterPolicy,
} from "@/features/admin/model/model-router-policy";
import { cn } from "@/lib/utils";
import { JsonCodeEditor } from "@/shared/components/json-code-editor";
import { isOneOf } from "@/shared/lib/type-guards";

const WORKSPACE_TABS = ["studio", "simulator", "json"] as const;
type WorkspaceTab = (typeof WORKSPACE_TABS)[number];
const isWorkspaceTab = isOneOf(WORKSPACE_TABS);

function MetricTile({
  label,
  value,
  sublabel,
  icon,
  loading,
}: {
  label: string;
  value: string;
  sublabel?: string;
  icon: React.ReactNode;
  loading: boolean;
}) {
  return (
    <div className="flex min-w-0 flex-col gap-1 rounded-lg bg-muted/35 px-3.5 py-3">
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
        <span className="text-foreground/60">{icon}</span>
        <span className="truncate">{label}</span>
      </div>
      {loading ? (
        <Skeleton className="mt-1 h-6 w-20" />
      ) : (
        <div className="flex items-baseline gap-2">
          <span className="truncate text-base font-semibold tabular-nums text-foreground">
            {value}
          </span>
          {sublabel ? (
            <span className="truncate text-[11px] text-muted-foreground">{sublabel}</span>
          ) : null}
        </div>
      )}
    </div>
  );
}

export function AdminModelRouterPage() {
  const t = useTranslations("adminModelRouter");
  const tCommon = useTranslations("common.states");
  const router = useAdminModelRouter();
  const [activeTab, setActiveTab] = React.useState<WorkspaceTab>("studio");
  const [copiedJSON, setCopiedJSON] = React.useState(false);

  const handleApplyPreset = React.useCallback(
    (presetID: ModelRouterPolicyPresetID) => {
      const next = buildSmartPolicyPreset(presetID, router.chatModels);
      router.updatePolicy(() => next);
      toast.success(t("toast.presetApplied"));
    },
    [router, t],
  );

  const handleResetDefault = React.useCallback(() => {
    router.updatePolicy(() => createDefaultModelRouterPolicy());
    toast.success(t("toast.resetToDefault"));
  }, [router, t]);

  const handlePruneUnavailable = React.useCallback(() => {
    router.updatePolicy((current) => pruneUnavailableModelsFromPolicy(current, router.chatModels));
    toast.success(t("toast.prunedUnavailable"));
  }, [router, t]);

  const handleCopyJSON = React.useCallback(async () => {
    try {
      await navigator.clipboard.writeText(router.policyJSON);
      setCopiedJSON(true);
      toast.success(t("toast.copiedJSON"));
      window.setTimeout(() => setCopiedJSON(false), 1800);
    } catch {
      toast.error(t("toast.copyFailed"));
    }
  }, [router.policyJSON, t]);

  const handleFormatJSON = React.useCallback(() => {
    if (router.parseError) return;
    router.updatePolicyJSON(stringifyModelRouterPolicy(router.policy));
  }, [router]);

  const classifierSelectValue =
    router.policy.routerModel.trim() || MODEL_ROUTER_FOLLOW_TASK_MODEL;

  const hasUnavailableAlert =
    router.diagnostics.unavailableModels.length > 0 || router.diagnostics.routerModelUnavailable;

  return (
    <div className="flex flex-col gap-5 pb-10">
      {/* Top Header Bar */}
      <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
        <div className="flex min-w-0 flex-col gap-1">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="text-base font-semibold tracking-tight text-foreground">
              {t("title")}
            </h2>
            <Badge
              variant={router.enabled ? "default" : "secondary"}
              className="h-5 px-2 text-[11px]"
            >
              {router.enabled ? t("status.enabled") : t("status.disabled")}
            </Badge>
            {router.dirty ? (
              <Badge
                variant="outline"
                className="h-5 border-amber-500/40 bg-amber-500/10 px-2 text-[10px] font-medium text-amber-700 dark:text-amber-300"
              >
                {tCommon("unsaved")}
              </Badge>
            ) : null}
          </div>
          <p className="text-xs text-muted-foreground">{t("subtitle")}</p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <div className="flex items-center gap-2 rounded-md border border-border/70 bg-card/50 px-3 py-1.5">
            <span className="text-xs font-medium text-foreground">{t("enableToggleLabel")}</span>
            <Switch
              checked={router.enabled}
              disabled={router.loading || router.saving}
              onCheckedChange={router.setEnabled}
            />
          </div>

          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="h-8 gap-1.5 text-xs"
                disabled={router.loading || router.saving}
              >
                <Wand2 className="size-3.5" />
                {t("presets.trigger")}
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-64">
              <DropdownMenuLabel className="text-xs">{t("presets.menuTitle")}</DropdownMenuLabel>
              <DropdownMenuGroup>
                <DropdownMenuItem onSelect={() => handleApplyPreset("balanced")}>
                  <div className="flex flex-col gap-0.5">
                    <span className="text-xs font-medium">{t("presets.balanced.title")}</span>
                    <span className="text-[11px] text-muted-foreground">
                      {t("presets.balanced.description")}
                    </span>
                  </div>
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => handleApplyPreset("cost_saver")}>
                  <div className="flex flex-col gap-0.5">
                    <span className="text-xs font-medium">{t("presets.costSaver.title")}</span>
                    <span className="text-[11px] text-muted-foreground">
                      {t("presets.costSaver.description")}
                    </span>
                  </div>
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => handleApplyPreset("coding_pro")}>
                  <div className="flex flex-col gap-0.5">
                    <span className="text-xs font-medium">{t("presets.codingPro.title")}</span>
                    <span className="text-[11px] text-muted-foreground">
                      {t("presets.codingPro.description")}
                    </span>
                  </div>
                </DropdownMenuItem>
              </DropdownMenuGroup>
              <DropdownMenuSeparator />
              <DropdownMenuItem onSelect={handleResetDefault}>
                <RotateCcw className="size-3.5" />
                <span>{t("presets.resetDefault")}</span>
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>

          {router.dirty ? (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="h-8 text-xs text-muted-foreground"
              disabled={router.saving}
              onClick={router.handleResetUnsaved}
            >
              {t("actions.discard")}
            </Button>
          ) : null}

          <Button
            type="button"
            size="sm"
            className="h-8 gap-1.5 text-xs"
            disabled={router.loading || router.saving || !router.dirty || Boolean(router.parseError)}
            onClick={() => void router.handleSave()}
          >
            {router.saving ? <Spinner data-icon="inline-start" /> : <Save data-icon="inline-start" />}
            {t("actions.save")}
          </Button>
        </div>
      </div>

      {/* KPI / Overview Tiles */}
      <div className="grid grid-cols-2 gap-2.5 lg:grid-cols-4">
        <MetricTile
          label={t("metrics.categories")}
          value={String(router.policy.categories.length)}
          sublabel={t("metrics.coveredCategories", {
            covered: router.diagnostics.coveredCategoryCount,
          })}
          icon={<Layers className="size-3.5" />}
          loading={router.loading}
        />
        <MetricTile
          label={t("metrics.rules")}
          value={String(router.policy.rules.length)}
          sublabel={
            router.policy.rules.length === 0
              ? t("metrics.classifierSkipped")
              : t("metrics.classifierActive")
          }
          icon={<Route className="size-3.5" />}
          loading={router.loading}
        />
        <MetricTile
          label={t("metrics.activeChatModels")}
          value={String(router.chatModels.length)}
          sublabel={t("metrics.referencedModels", {
            count: router.diagnostics.referencedModels.length,
          })}
          icon={<Cpu className="size-3.5" />}
          loading={router.loading}
        />
        <MetricTile
          label={t("metrics.classifierTimeout")}
          value={`${router.policy.timeoutMs} ms`}
          sublabel={t("metrics.historyCount", { count: router.policy.historyMessages })}
          icon={<Clock className="size-3.5" />}
          loading={router.loading}
        />
      </div>

      {/* Policy Parse Error / Health Diagnostics */}
      {router.parseError ? (
        <Alert variant="destructive">
          <AlertTriangle />
          <AlertTitle className="text-xs">{t("alerts.invalidJsonTitle")}</AlertTitle>
          <AlertDescription className="text-xs">{router.parseError}</AlertDescription>
        </Alert>
      ) : null}

      {!router.loading && !router.parseError && hasUnavailableAlert ? (
        <Alert>
          <AlertTriangle />
          <AlertTitle className="text-xs">{t("alerts.unavailableModelsTitle")}</AlertTitle>
          <AlertDescription className="flex flex-wrap items-center justify-between gap-2 text-xs">
            <span>
              {t("alerts.unavailableModelsDescription", {
                models: [
                  ...(router.diagnostics.routerModelUnavailable ? [router.policy.routerModel] : []),
                  ...router.diagnostics.unavailableModels,
                ].join(", "),
              })}
            </span>
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-7 text-xs"
              onClick={handlePruneUnavailable}
            >
              {t("alerts.pruneUnavailableButton")}
            </Button>
          </AlertDescription>
        </Alert>
      ) : null}

      {!router.loading && !router.parseError && router.policy.rules.length === 0 ? (
        <Alert>
          <Info />
          <AlertTitle className="text-xs">{t("alerts.noRulesTitle")}</AlertTitle>
          <AlertDescription className="text-xs">{t("alerts.noRulesDescription")}</AlertDescription>
        </Alert>
      ) : null}

      {/* Main 3-Tab Workspace */}
      <Tabs
        value={activeTab}
        onValueChange={(value) => {
          if (isWorkspaceTab(value)) {
            setActiveTab(value);
          }
        }}
        className="flex flex-col gap-4"
      >
        <div className="flex flex-wrap items-center justify-between gap-2">
          <TabsList>
            <TabsTrigger value="studio" className="gap-1.5 text-xs">
              <SlidersHorizontal className="size-3.5" />
              {t("tabs.studio")}
            </TabsTrigger>
            <TabsTrigger value="simulator" className="gap-1.5 text-xs">
              <FlaskConical className="size-3.5" />
              {t("tabs.simulator")}
            </TabsTrigger>
            <TabsTrigger value="json" className="gap-1.5 text-xs">
              <Braces className="size-3.5" />
              {t("tabs.json")}
              {router.parseError ? (
                <span className="size-1.5 rounded-full bg-destructive" />
              ) : null}
            </TabsTrigger>
          </TabsList>

          {activeTab === "studio" ? (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="h-8 gap-1.5 text-xs text-muted-foreground hover:text-foreground"
              onClick={() => setActiveTab("simulator")}
            >
              <Sparkles className="size-3.5" />
              {t("actions.openSimulator")}
            </Button>
          ) : null}
        </div>

        {/* TAB 1: Visual Policy Studio */}
        <TabsContent value="studio" className="flex flex-col gap-5">
          {/* Classifier & Fallback Row */}
          <div className="grid gap-4 lg:grid-cols-2">
            <Card className="gap-4 py-4 shadow-none">
              <CardHeader className="px-4">
                <div className="flex items-center gap-2">
                  <Bot className="size-4 text-muted-foreground" />
                  <CardTitle className="text-sm">{t("classifier.title")}</CardTitle>
                </div>
                <CardDescription className="text-xs">{t("classifier.description")}</CardDescription>
              </CardHeader>
              <CardContent className="px-4">
                <FieldGroup className="gap-4">
                  <Field>
                    <FieldLabel htmlFor="router-classifier-model" className="text-xs">
                      {t("classifier.modelLabel")}
                    </FieldLabel>
                    <ModelSelect
                      id="router-classifier-model"
                      value={classifierSelectValue}
                      fallbackValue={MODEL_ROUTER_FOLLOW_TASK_MODEL}
                      disabled={router.loading || router.saving}
                      options={router.classifierModelOptions}
                      valueAlign="start"
                      triggerClassName="w-full"
                      onChange={(value) => {
                        const nextModel = value === MODEL_ROUTER_FOLLOW_TASK_MODEL ? "" : value;
                        router.updatePolicy((current) => ({ ...current, routerModel: nextModel }));
                      }}
                    />
                    <FieldDescription className="text-[11px]">
                      {router.policy.routerModel
                        ? t("classifier.modelSelectedHint", { model: router.policy.routerModel })
                        : router.conversationTaskModel && router.conversationTaskModel !== "follow"
                          ? t("classifier.followingTaskModelHint", {
                              model: router.conversationTaskModel,
                            })
                          : t("classifier.modelFallbackHint")}
                    </FieldDescription>
                  </Field>

                  {/* Timeout Slider + Presets */}
                  <Field>
                    <div className="flex items-center justify-between gap-2">
                      <FieldLabel htmlFor="router-timeout-ms" className="text-xs">
                        {t("classifier.timeoutLabel")}
                      </FieldLabel>
                      <div className="flex items-center gap-1.5">
                        <Input
                          id="router-timeout-ms"
                          type="number"
                          min={MODEL_ROUTER_LIMITS.minTimeoutMs}
                          max={MODEL_ROUTER_LIMITS.maxTimeoutMs}
                          step={50}
                          disabled={router.loading || router.saving}
                          value={router.policy.timeoutMs}
                          className="h-7 w-24 text-right font-mono text-xs tabular-nums"
                          onChange={(event) => {
                            const num = Number.parseInt(event.target.value, 10);
                            if (!Number.isFinite(num)) return;
                            const clamped = Math.min(
                              MODEL_ROUTER_LIMITS.maxTimeoutMs,
                              Math.max(MODEL_ROUTER_LIMITS.minTimeoutMs, num),
                            );
                            router.updatePolicy((current) => ({ ...current, timeoutMs: clamped }));
                          }}
                        />
                        <span className="text-xs text-muted-foreground">ms</span>
                      </div>
                    </div>
                    <Slider
                      min={MODEL_ROUTER_LIMITS.minTimeoutMs}
                      max={5000}
                      step={100}
                      disabled={router.loading || router.saving}
                      value={[Math.min(5000, router.policy.timeoutMs)]}
                      onValueChange={([value]) => {
                        if (typeof value === "number") {
                          router.updatePolicy((current) => ({ ...current, timeoutMs: value }));
                        }
                      }}
                    />
                    <div className="flex flex-wrap items-center gap-1.5">
                      {MODEL_ROUTER_TIMEOUT_PRESETS.map((preset) => (
                        <Button
                          key={preset}
                          type="button"
                          variant={router.policy.timeoutMs === preset ? "secondary" : "ghost"}
                          size="sm"
                          disabled={router.loading || router.saving}
                          className={cn(
                            "h-6 rounded-full px-2 font-mono text-[11px]",
                            router.policy.timeoutMs !== preset && "text-muted-foreground",
                          )}
                          onClick={() =>
                            router.updatePolicy((current) => ({ ...current, timeoutMs: preset }))
                          }
                        >
                          {preset}ms
                        </Button>
                      ))}
                    </div>
                    <FieldDescription className="text-[11px]">
                      {t("classifier.timeoutHint")}
                    </FieldDescription>
                  </Field>

                  {/* History Messages Slider + Presets */}
                  <Field>
                    <div className="flex items-center justify-between gap-2">
                      <FieldLabel htmlFor="router-history-messages" className="flex items-center gap-1.5 text-xs">
                        <History className="size-3.5 text-muted-foreground" />
                        {t("classifier.historyLabel")}
                      </FieldLabel>
                      <span className="font-mono text-xs font-medium tabular-nums text-foreground">
                        {t("classifier.historyValue", { count: router.policy.historyMessages })}
                      </span>
                    </div>
                    <Slider
                      id="router-history-messages"
                      min={0}
                      max={MODEL_ROUTER_LIMITS.maxHistoryMessages}
                      step={1}
                      disabled={router.loading || router.saving}
                      value={[router.policy.historyMessages]}
                      onValueChange={([value]) => {
                        if (typeof value === "number") {
                          router.updatePolicy((current) => ({
                            ...current,
                            historyMessages: value,
                          }));
                        }
                      }}
                    />
                    <div className="flex flex-wrap items-center gap-1.5">
                      {MODEL_ROUTER_HISTORY_PRESETS.map((preset) => (
                        <Button
                          key={preset}
                          type="button"
                          variant={router.policy.historyMessages === preset ? "secondary" : "ghost"}
                          size="sm"
                          disabled={router.loading || router.saving}
                          className={cn(
                            "h-6 rounded-full px-2 font-mono text-[11px]",
                            router.policy.historyMessages !== preset && "text-muted-foreground",
                          )}
                          onClick={() =>
                            router.updatePolicy((current) => ({
                              ...current,
                              historyMessages: preset,
                            }))
                          }
                        >
                          {preset}
                        </Button>
                      ))}
                    </div>
                    <FieldDescription className="text-[11px]">
                      {t("classifier.historyHint")}
                    </FieldDescription>
                  </Field>
                </FieldGroup>
              </CardContent>
            </Card>

            {/* Default Models Priority Chain */}
            <Card className="gap-4 py-4 shadow-none">
              <CardHeader className="px-4">
                <div className="flex items-center gap-2">
                  <Route className="size-4 text-muted-foreground" />
                  <CardTitle className="text-sm">{t("defaultModels.title")}</CardTitle>
                </div>
                <CardDescription className="text-xs">
                  {t("defaultModels.description")}
                </CardDescription>
              </CardHeader>
              <CardContent className="flex flex-col gap-3 px-4">
                <ModelRouterModelChain
                  models={router.policy.defaultModels}
                  chatModels={router.chatModels}
                  modelMetaByName={router.modelMetaByName}
                  disabled={router.loading || router.saving}
                  emptyText={t("defaultModels.empty")}
                  addLabel={t("defaultModels.addModel")}
                  onChange={(nextModels) =>
                    router.updatePolicy((current) => ({ ...current, defaultModels: nextModels }))
                  }
                />
                <div className="rounded-md bg-muted/35 px-3 py-2.5 text-[11px] leading-relaxed text-muted-foreground">
                  <strong className="font-medium text-foreground">
                    {t("defaultModels.orderTitle")}
                  </strong>{" "}
                  {t("defaultModels.orderDescription")}
                </div>
              </CardContent>
            </Card>
          </div>

          <Separator />

          {/* Interactive Routing Matrix */}
          <ModelRouterMatrix
            policy={router.policy}
            chatModels={router.chatModels}
            modelMetaByName={router.modelMetaByName}
            disabled={router.loading || router.saving}
            onChange={router.updatePolicy}
          />

          <Separator />

          {/* Task Categories Manager */}
          <ModelRouterCategories
            policy={router.policy}
            disabled={router.loading || router.saving}
            onChange={router.updatePolicy}
          />

          <Separator />

          {/* Candidate Model Capabilities Reference */}
          <ModelRouterCandidatesPanel
            chatModels={router.chatModels}
            referencedModels={router.diagnostics.referencedModels}
          />
        </TabsContent>

        {/* TAB 2: Interactive Simulator & Live Tester */}
        <TabsContent value="simulator" className="flex flex-col gap-4">
          <ModelRouterSimulator
            policy={router.policy}
            parseError={router.parseError}
            chatModels={router.chatModels}
            modelMetaByName={router.modelMetaByName}
            testing={router.testing}
            liveTestResult={router.liveTestResult}
            liveTestError={router.liveTestError}
            onRunLiveTest={router.runLiveTest}
          />
        </TabsContent>

        {/* TAB 3: Raw JSON Source (Bidirectional Sync) */}
        <TabsContent value="json" className="flex flex-col gap-3">
          <Card className="gap-3 py-4 shadow-none">
            <CardHeader className="px-4">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="min-w-0">
                  <CardTitle className="text-sm">{t("json.title")}</CardTitle>
                  <CardDescription className="text-xs">{t("json.description")}</CardDescription>
                </div>
                <div className="flex items-center gap-1.5">
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="h-7 gap-1.5 text-xs"
                    disabled={Boolean(router.parseError)}
                    onClick={handleFormatJSON}
                  >
                    <Braces className="size-3.5" />
                    {t("json.format")}
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="h-7 gap-1.5 text-xs"
                    onClick={() => void handleCopyJSON()}
                  >
                    {copiedJSON ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
                    {copiedJSON ? t("json.copied") : t("json.copy")}
                  </Button>
                </div>
              </div>
            </CardHeader>
            <CardContent className="px-4">
              <JsonCodeEditor
                id="admin-model-router-policy-json"
                value={router.policyJSON}
                disabled={router.loading || router.saving}
                height={420}
                showFormatAction={false}
                onChange={router.updatePolicyJSON}
              />
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  );
}
