"use client";

import type { ManagedServiceDTO } from "@firecortex/api-contract";
import {
  CheckCircle2,
  Container,
  DownloadCloud,
  FileCode2,
  Loader2,
  Play,
  RefreshCw,
  RotateCw,
  ScrollText,
  ShieldAlert,
  Square,
  XCircle,
} from "lucide-react";
import { useTranslations } from "next-intl";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useAdminInfrastructure } from "@/features/admin/hooks/use-admin-infrastructure";
import { cn } from "@/lib/utils";
import { SettingsPage } from "@/shared/components/settings-layout";
import { InfrastructureBackupsPanel } from "./infrastructure-backups-panel";
import { InfrastructureConfigPanel } from "./infrastructure-config-panel";
import { InfrastructureDangerPanel } from "./infrastructure-danger-panel";

function formatBytes(bytes?: number): string {
  if (!bytes || bytes <= 0) return "-";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = bytes;
  let idx = 0;
  while (value >= 1024 && idx < units.length - 1) {
    value /= 1024;
    idx++;
  }
  return `${value.toFixed(1)} ${units[idx]}`;
}

function statusBadgeVariant(status: string): "default" | "secondary" | "destructive" | "outline" {
  switch (status) {
    case "running":
      return "default";
    case "external":
    case "memory":
      return "secondary";
    case "unhealthy":
    case "unavailable":
      return "destructive";
    default:
      return "outline";
  }
}

export function AdminInfrastructurePage() {
  const t = useTranslations("adminInfrastructure");
  const infra = useAdminInfrastructure();

  if (!infra.isSuperAdmin) {
    return (
      <SettingsPage>
        <div className="space-y-1">
          <h2 className="text-lg font-semibold">{t("title")}</h2>
          <p className="text-sm text-muted-foreground">{t("description")}</p>
        </div>
        <Alert variant="destructive">
          <ShieldAlert className="size-4" />
          <AlertDescription>{t("superAdminOnly")}</AlertDescription>
        </Alert>
      </SettingsPage>
    );
  }

  if (infra.loading) {
    return (
      <SettingsPage>
        <div className="space-y-1">
          <h2 className="text-lg font-semibold">{t("title")}</h2>
          <p className="text-sm text-muted-foreground">{t("description")}</p>
        </div>
        <div className="flex items-center justify-center py-16">
          <Loader2 className="size-6 animate-spin text-muted-foreground" />
        </div>
      </SettingsPage>
    );
  }

  const coreServices = infra.overview?.services.filter((s) => s.category === "core") ?? [];
  const optionalServices = infra.overview?.services.filter((s) => s.category === "optional") ?? [];
  const latestPct = infra.jobEvents.reduce((max, evt) => Math.max(max, evt.percent), 0);

  const renderServiceCard = (svc: ManagedServiceDTO) => (
    <Card key={svc.id} className="flex flex-col justify-between">
      <CardHeader className="pb-3">
        <div className="flex items-start justify-between gap-2">
          <div className="space-y-0.5">
            <CardTitle className="text-sm font-semibold">{svc.name}</CardTitle>
            <CardDescription className="text-xs">{svc.description}</CardDescription>
          </div>
          <Badge variant={statusBadgeVariant(svc.status)} className="shrink-0 capitalize">
            {svc.status}
          </Badge>
        </div>
      </CardHeader>
      <CardContent className="space-y-3 text-xs">
        <div className="space-y-1 rounded-md bg-muted/40 p-2.5 font-mono text-[11px] text-muted-foreground">
          <div className="truncate">
            {t("services.image")}: {svc.image || "-"}
          </div>
          {svc.endpointURL ? (
            <div className="truncate">
              {t("services.endpoint")}: {svc.endpointURL}
            </div>
          ) : null}
          {svc.dataDir ? (
            <div className="truncate">
              {t("services.dataDir")}: {svc.dataDir}
            </div>
          ) : null}
          {svc.status === "running" && svc.mode === "managed" ? (
            <div className="flex gap-3 pt-0.5 text-foreground">
              <span>
                {t("services.cpu")}: {svc.cpuPercent.toFixed(1)}%
              </span>
              <span>
                {t("services.memory")}: {formatBytes(svc.memoryUsageBytes)}
              </span>
            </div>
          ) : null}
        </div>
        {svc.message ? <p className="text-muted-foreground">{svc.message}</p> : null}

        <div className="flex flex-wrap gap-1.5 pt-1">
          {svc.canStart ? (
            <Button
              type="button"
              size="sm"
              onClick={() => void infra.handleServiceAction(svc.id, "start")}
            >
              <Play className="mr-1 size-3.5" />
              {t("services.start")}
            </Button>
          ) : null}
          {svc.canStop ? (
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={() => void infra.handleServiceAction(svc.id, "stop")}
            >
              <Square className="mr-1 size-3.5" />
              {t("services.stop")}
            </Button>
          ) : null}
          {svc.canRestart ? (
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={() => void infra.handleServiceAction(svc.id, "restart")}
            >
              <RotateCw className="mr-1 size-3.5" />
              {t("services.restart")}
            </Button>
          ) : null}
          {svc.canPull ? (
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={() => void infra.handleServiceAction(svc.id, "pull")}
            >
              <DownloadCloud className="mr-1 size-3.5" />
              {t("services.upgrade")}
            </Button>
          ) : null}
          {svc.mode === "managed" ? (
            <Button
              type="button"
              size="sm"
              variant="ghost"
              onClick={() => void infra.handleOpenLogs(svc.id, svc.name)}
            >
              <ScrollText className="mr-1 size-3.5" />
              {t("services.logs")}
            </Button>
          ) : null}
        </div>
      </CardContent>
    </Card>
  );

  return (
    <SettingsPage>
      <div className="space-y-6">
        <div className="space-y-1">
          <h2 className="text-lg font-semibold">{t("title")}</h2>
          <p className="text-sm text-muted-foreground">{t("description")}</p>
        </div>
        {infra.restarting ? (
          <Alert>
            <Loader2 className="size-4 animate-spin" />
            <AlertDescription>{t("config.restartingBanner")}</AlertDescription>
          </Alert>
        ) : null}

        {/* Host Overview Bar */}
        {infra.overview ? (
          <Card>
            <CardContent className="flex flex-wrap items-center justify-between gap-4 p-4 text-xs">
              <div className="flex flex-wrap items-center gap-3">
                <Badge variant={infra.overview.docker.available ? "default" : "destructive"} className="gap-1.5">
                  <Container className="size-3.5" />
                  {infra.overview.docker.available
                    ? `${t("overview.dockerConnected")} v${infra.overview.docker.serverVersion}`
                    : t("overview.dockerOffline")}
                </Badge>
                <span className="font-mono text-muted-foreground">
                  {t("overview.dataRoot")}: {infra.overview.dataDir.resolvedRoot}
                </span>
                <span className="text-muted-foreground">
                  {t("overview.diskFree")}: {formatBytes(infra.overview.dataDir.diskFreeBytes)}
                </span>
                <span className="text-muted-foreground">
                  {t("overview.mirror")}: {infra.overview.registryMirror || t("overview.mirrorNone")}
                </span>
              </div>
              <div className="flex items-center gap-2">
                <Button type="button" size="sm" variant="outline" onClick={() => void infra.handleOpenCompose()}>
                  <FileCode2 className="mr-1.5 size-3.5" />
                  {t("overview.viewCompose")}
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  disabled={infra.refreshing}
                  onClick={() => void infra.loadAll()}
                >
                  <RefreshCw className={cn("mr-1.5 size-3.5", infra.refreshing && "animate-spin")} />
                  {t("overview.refresh")}
                </Button>
              </div>
            </CardContent>
          </Card>
        ) : null}

        {/* Live Async Job Progress Banner */}
        {infra.activeJob ? (
          <Card className="border-primary/40 bg-primary/5">
            <CardHeader className="pb-2">
              <CardTitle className="flex items-center gap-2 text-sm">
                {infra.activeJob.state === "running" ? (
                  <Loader2 className="size-4 animate-spin text-primary" />
                ) : infra.activeJob.state === "done" ? (
                  <CheckCircle2 className="size-4 text-emerald-500" />
                ) : (
                  <XCircle className="size-4 text-destructive" />
                )}
                {infra.activeJob.state === "running"
                  ? t("services.jobRunning", {
                      action: infra.activeJob.action,
                      service: infra.activeJob.serviceID,
                    })
                  : infra.activeJob.state === "done"
                    ? t("services.jobDone")
                    : t("services.jobFailed")}
                <span className="ml-auto font-mono text-xs">{latestPct}%</span>
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-2">
              <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
                <div
                  className={cn(
                    "h-full transition-all duration-300",
                    infra.activeJob.state === "failed"
                      ? "bg-destructive"
                      : infra.activeJob.state === "done"
                        ? "bg-emerald-500"
                        : "bg-primary",
                  )}
                  style={{ width: `${infra.activeJob.state === "done" ? 100 : latestPct}%` }}
                />
              </div>
              <div className="max-h-36 space-y-1 overflow-y-auto rounded border bg-background/80 p-2 font-mono text-[11px]">
                {infra.jobEvents.map((evt, idx) => (
                  <div
                    key={`${evt.timestamp}-${idx}`}
                    className={cn(evt.status === "failed" && "text-destructive")}
                  >
                    [{evt.step}] {evt.message}
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        ) : null}

        <Tabs defaultValue="services" className="space-y-4">
          <TabsList className="grid w-full grid-cols-4">
            <TabsTrigger value="services">{t("tabs.services")}</TabsTrigger>
            <TabsTrigger value="backups">{t("tabs.backups")}</TabsTrigger>
            <TabsTrigger value="config">{t("tabs.config")}</TabsTrigger>
            <TabsTrigger value="danger" className="text-destructive data-[state=active]:text-destructive">
              {t("tabs.danger")}
            </TabsTrigger>
          </TabsList>

          <TabsContent value="services" className="space-y-6">
            <section className="space-y-3">
              <div>
                <h2 className="text-sm font-semibold">{t("services.coreTitle")}</h2>
                <p className="text-xs text-muted-foreground">{t("services.coreDesc")}</p>
              </div>
              <div className="grid gap-4 md:grid-cols-2">{coreServices.map(renderServiceCard)}</div>
            </section>

            <section className="space-y-3">
              <div>
                <h2 className="text-sm font-semibold">{t("services.optionalTitle")}</h2>
                <p className="text-xs text-muted-foreground">{t("services.optionalDesc")}</p>
              </div>
              <div className="grid gap-4 md:grid-cols-2">{optionalServices.map(renderServiceCard)}</div>
            </section>
          </TabsContent>

          <TabsContent value="backups">
            <InfrastructureBackupsPanel
              backups={infra.backups}
              schedule={infra.overview?.backupSchedule ?? "0 3 * * *"}
              retention={infra.overview?.backupRetention ?? 7}
              backupDir={infra.overview?.dataDir.backupDir ?? ""}
              managedPostgresEnabled={Boolean(infra.infraConfig?.managedPostgresEnabled)}
              creating={infra.creatingBackup}
              onCreate={() => void infra.handleCreateBackup()}
              onDownload={(id) => void infra.handleDownloadBackup(id)}
              onDelete={(id) => void infra.handleDeleteBackup(id)}
            />
          </TabsContent>

          <TabsContent value="config">
            {infra.infraConfig ? (
              <InfrastructureConfigPanel
                config={infra.infraConfig}
                saving={infra.savingConfig}
                onSave={(payload) => void infra.handleSaveConfig(payload)}
              />
            ) : null}
          </TabsContent>

          <TabsContent value="danger">
            <InfrastructureDangerPanel
              backups={infra.backups}
              managedPostgresEnabled={Boolean(infra.infraConfig?.managedPostgresEnabled)}
              busy={infra.dangerBusy}
              onRestoreBackup={infra.handleDangerRestore}
              onSwitchDatabase={infra.handleDangerSwitchDatabase}
              onRecreateContainer={infra.handleDangerRecreate}
            />
          </TabsContent>
        </Tabs>

        {/* Logs Dialog */}
        <Dialog open={Boolean(infra.logsTarget)} onOpenChange={(open) => !open && infra.setLogsTarget(null)}>
          <DialogContent className="max-w-3xl">
            <DialogHeader>
              <DialogTitle>
                {t("services.logsTitle", { name: infra.logsTarget?.name ?? "" })}
              </DialogTitle>
            </DialogHeader>
            {infra.logsLoading ? (
              <div className="flex items-center justify-center py-12">
                <Loader2 className="size-5 animate-spin text-muted-foreground" />
              </div>
            ) : (
              <pre className="max-h-[60vh] overflow-auto rounded-lg border bg-muted/40 p-3 font-mono text-xs whitespace-pre-wrap">
                {infra.logsContent || t("services.logsEmpty")}
              </pre>
            )}
          </DialogContent>
        </Dialog>

        {/* Compose Preview Dialog */}
        <Dialog open={infra.composeOpen} onOpenChange={infra.setComposeOpen}>
          <DialogContent className="max-w-3xl">
            <DialogHeader>
              <DialogTitle>{t("overview.composeTitle")}</DialogTitle>
            </DialogHeader>
            {infra.composeLoading ? (
              <div className="flex items-center justify-center py-12">
                <Loader2 className="size-5 animate-spin text-muted-foreground" />
              </div>
            ) : (
              <pre className="max-h-[60vh] overflow-auto rounded-lg border bg-muted/40 p-3 font-mono text-xs whitespace-pre-wrap">
                {infra.composeContent || t("overview.composeEmpty")}
              </pre>
            )}
          </DialogContent>
        </Dialog>
      </div>
    </SettingsPage>
  );
}
