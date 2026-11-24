"use client";

import {
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  Container,
  Database,
  FolderCheck,
  HardDrive,
  KeyRound,
  Loader2,
  RefreshCw,
  Server,
  ShieldCheck,
  XCircle,
} from "lucide-react";
import { useTranslations } from "next-intl";
import * as React from "react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
import { type CacheMode, type DatabaseMode, type RuntimeEnvMode, type SetupStep, useSetupWizard } from "../hooks/use-setup-wizard";

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

const ORDERED_STEPS: SetupStep[] = ["unlock", "environment", "database", "site", "progress"];

export function SetupWizard() {
  const t = useTranslations("setup");
  const wizard = useSetupWizard();
  const [advancedOpen, setAdvancedOpen] = React.useState(false);

  if (wizard.loading) {
    return (
      <div className="flex min-h-svh items-center justify-center p-6">
        <Loader2 className="size-6 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (wizard.alreadyInitialized) {
    return (
      <div className="flex min-h-svh items-center justify-center p-6">
        <Card className="w-full max-w-md">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-lg">
              <CheckCircle2 className="size-5 text-emerald-500" />
              {t("title")}
            </CardTitle>
            <CardDescription>{t("alreadyInitialized")}</CardDescription>
          </CardHeader>
        </Card>
      </div>
    );
  }

  const currentIdx = ORDERED_STEPS.indexOf(wizard.step);
  const needsAdoptConfirm =
    wizard.form.databaseMode === "managed" &&
    (wizard.dirInspection?.status === "adoptable_pgdata" || wizard.dirInspection?.status === "managed_existing");

  const canLeaveDatabaseStep =
    wizard.form.databaseMode === "external"
      ? wizard.form.externalPostgresDSN.trim().length > 0
      : Boolean(wizard.dirInspection?.valid) && (!needsAdoptConfirm || wizard.form.adoptExistingPGData);

  return (
    <div className="mx-auto flex min-h-svh w-full max-w-3xl flex-col justify-center gap-6 px-4 py-10 sm:px-6">
      {/* Header */}
      <div className="space-y-2 text-center">
        <div className="inline-flex items-center gap-2 rounded-full border bg-muted/40 px-3 py-1 text-xs font-medium text-muted-foreground">
          <ShieldCheck className="size-3.5 text-primary" />
          <span>FireCortex {wizard.version ? `v${wizard.version}` : ""}</span>
        </div>
        <h1 className="text-2xl font-semibold tracking-tight sm:text-3xl">{t("title")}</h1>
        <p className="text-sm text-muted-foreground">{t("subtitle")}</p>
      </div>

      {/* Step Indicator */}
      <ol className="grid grid-cols-5 gap-2">
        {ORDERED_STEPS.map((stepKey, idx) => {
          const active = idx === currentIdx;
          const completed = idx < currentIdx;
          return (
            <li
              key={stepKey}
              className={cn(
                "flex flex-col items-center gap-1 rounded-lg border px-2 py-2 text-center text-xs transition-colors",
                active && "border-primary bg-primary/5 font-medium text-foreground",
                completed && "border-emerald-500/40 bg-emerald-500/5 text-emerald-700 dark:text-emerald-300",
                !active && !completed && "text-muted-foreground",
              )}
            >
              <span className="font-mono text-[11px]">{idx + 1}</span>
              <span className="truncate">{t(`steps.${stepKey}`)}</span>
            </li>
          );
        })}
      </ol>

      {/* Step 0: Unlock */}
      {wizard.step === "unlock" ? (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-lg">
              <KeyRound className="size-5 text-primary" />
              {t("unlock.title")}
            </CardTitle>
            <CardDescription>{t("unlock.description")}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="setup-token">{t("unlock.tokenLabel")}</Label>
              <Input
                id="setup-token"
                value={wizard.tokenInput}
                onChange={(e) => wizard.setTokenInput(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && wizard.tokenInput.trim()) {
                    void wizard.handleVerifyToken();
                  }
                }}
                placeholder={t("unlock.tokenPlaceholder")}
                className="font-mono"
                autoComplete="off"
                autoFocus
              />
            </div>
            {wizard.authError ? (
              <Alert variant="destructive">
                <XCircle className="size-4" />
                <AlertDescription>{wizard.authError}</AlertDescription>
              </Alert>
            ) : null}
          </CardContent>
          <CardFooter className="justify-end">
            <Button
              type="button"
              disabled={!wizard.tokenInput.trim() || wizard.verifying}
              onClick={() => void wizard.handleVerifyToken()}
            >
              {wizard.verifying ? <Loader2 className="mr-2 size-4 animate-spin" /> : null}
              {wizard.verifying ? t("unlock.verifying") : t("unlock.submit")}
            </Button>
          </CardFooter>
        </Card>
      ) : null}

      {/* Step 1: Environment */}
      {wizard.step === "environment" ? (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-lg">
              <Container className="size-5 text-primary" />
              {t("environment.title")}
            </CardTitle>
            <CardDescription>{t("environment.description")}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-5">
            {wizard.preflight ? (
              <div className="space-y-3">
                <Alert variant={wizard.preflight.docker.available ? "default" : "destructive"}>
                  {wizard.preflight.docker.available ? (
                    <CheckCircle2 className="size-4 text-emerald-500" />
                  ) : (
                    <AlertTriangle className="size-4" />
                  )}
                  <AlertTitle>
                    {wizard.preflight.docker.available
                      ? `${t("environment.dockerConnected")} (v${wizard.preflight.docker.serverVersion || "OK"})`
                      : t("environment.dockerUnavailable")}
                  </AlertTitle>
                  <AlertDescription>
                    {wizard.preflight.docker.available ? (
                      <div className="mt-1 flex flex-wrap gap-2 text-xs text-muted-foreground">
                        <span>{wizard.preflight.docker.osType}/{wizard.preflight.docker.architecture}</span>
                        <span>·</span>
                        <span>RAM {formatBytes(wizard.preflight.docker.memTotalBytes)}</span>
                        <span>·</span>
                        <span>{t("environment.diskFree")}: {formatBytes(wizard.preflight.defaultDir.diskFreeBytes)}</span>
                      </div>
                    ) : (
                      <span>{wizard.preflight.dockerError || t("environment.dockerUnavailableHint")}</span>
                    )}
                  </AlertDescription>
                </Alert>

                <div className="grid gap-3 sm:grid-cols-2">
                  <div className="flex items-center justify-between rounded-lg border px-3 py-2.5 text-sm">
                    <span>PostgreSQL (:{wizard.preflight.defaults.postgresPort})</span>
                    <Badge variant={wizard.preflight.postgresPortAvailable ? "secondary" : "destructive"}>
                      {wizard.preflight.postgresPortAvailable ? t("environment.portOk") : t("environment.portBusy")}
                    </Badge>
                  </div>
                  <div className="flex items-center justify-between rounded-lg border px-3 py-2.5 text-sm">
                    <span>Redis (:{wizard.preflight.defaults.redisPort})</span>
                    <Badge variant={wizard.preflight.redisPortAvailable ? "secondary" : "destructive"}>
                      {wizard.preflight.redisPortAvailable ? t("environment.portOk") : t("environment.portBusy")}
                    </Badge>
                  </div>
                </div>
              </div>
            ) : null}

            <div className="space-y-2">
              <Label htmlFor="docker-host">{t("environment.dockerHostLabel")}</Label>
              <div className="flex gap-2">
                <Input
                  id="docker-host"
                  value={wizard.form.dockerHost}
                  onChange={(e) => wizard.updateForm("dockerHost", e.target.value)}
                  placeholder="unix:///var/run/docker.sock"
                  className="font-mono text-sm"
                />
                <Button
                  type="button"
                  variant="outline"
                  disabled={wizard.preflighting}
                  onClick={() => void wizard.loadPreflight(wizard.form.dockerHost)}
                >
                  <RefreshCw className={cn("mr-1.5 size-4", wizard.preflighting && "animate-spin")} />
                  {wizard.preflighting ? t("environment.rechecking") : t("environment.recheck")}
                </Button>
              </div>
              <p className="text-xs text-muted-foreground">{t("environment.dockerHostHint")}</p>
            </div>

            <div className="space-y-2">
              <Label htmlFor="registry-mirror">{t("environment.mirrorLabel")}</Label>
              <Input
                id="registry-mirror"
                value={wizard.form.registryMirror}
                onChange={(e) => wizard.updateForm("registryMirror", e.target.value)}
                placeholder={t("environment.mirrorPlaceholder")}
                className="font-mono text-sm"
              />
              <p className="text-xs text-muted-foreground">{t("environment.mirrorHint")}</p>
            </div>
          </CardContent>
          <CardFooter className="justify-end">
            <Button type="button" onClick={() => wizard.setStep("database")}>
              {t("actions.next")}
            </Button>
          </CardFooter>
        </Card>
      ) : null}

      {/* Step 2: Database & Cache */}
      {wizard.step === "database" ? (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-lg">
              <Database className="size-5 text-primary" />
              {t("database.title")}
            </CardTitle>
            <CardDescription>{t("database.description")}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-5">
            {/* Mode Cards */}
            <div className="grid gap-3 sm:grid-cols-2">
              {(["managed", "external"] as const).map((mode: DatabaseMode) => {
                const selected = wizard.form.databaseMode === mode;
                const disabled = mode === "managed" && wizard.preflight && !wizard.preflight.docker.available;
                return (
                  <button
                    key={mode}
                    type="button"
                    disabled={Boolean(disabled)}
                    onClick={() => {
                      wizard.updateForm("databaseMode", mode);
                      if (mode === "managed" && wizard.form.cacheMode === "external_redis") {
                        wizard.updateForm("cacheMode", "managed_redis");
                      }
                      if (mode === "external" && wizard.form.cacheMode === "managed_redis") {
                        wizard.updateForm("cacheMode", "memory");
                      }
                    }}
                    className={cn(
                      "flex flex-col items-start gap-1.5 rounded-xl border p-4 text-left transition-colors",
                      selected ? "border-primary bg-primary/5" : "hover:bg-muted/40",
                      disabled && "cursor-not-allowed opacity-50",
                    )}
                  >
                    <span className="text-sm font-semibold">
                      {mode === "managed" ? t("database.modeManaged") : t("database.modeExternal")}
                    </span>
                    <span className="text-xs text-muted-foreground">
                      {mode === "managed" ? t("database.modeManagedDesc") : t("database.modeExternalDesc")}
                    </span>
                  </button>
                );
              })}
            </div>

            {wizard.form.databaseMode === "managed" ? (
              <div className="space-y-4">
                <div className="space-y-2">
                  <Label htmlFor="managed-data-dir">{t("database.dataDirLabel")}</Label>
                  <div className="flex gap-2">
                    <Input
                      id="managed-data-dir"
                      value={wizard.form.dataDir}
                      onChange={(e) => wizard.updateForm("dataDir", e.target.value)}
                      placeholder={t("database.dataDirPlaceholder")}
                      className="font-mono text-sm"
                    />
                    <Button
                      type="button"
                      variant="outline"
                      disabled={wizard.inspectingDir || !wizard.form.dataDir.trim()}
                      onClick={() => void wizard.handleInspectDir()}
                    >
                      {wizard.inspectingDir ? (
                        <Loader2 className="mr-1.5 size-4 animate-spin" />
                      ) : (
                        <FolderCheck className="mr-1.5 size-4" />
                      )}
                      {wizard.inspectingDir ? t("database.inspecting") : t("database.inspectDir")}
                    </Button>
                  </div>
                </div>

                {wizard.dirInspection ? (
                  <Alert variant={wizard.dirInspection.valid ? "default" : "destructive"}>
                    {wizard.dirInspection.valid ? (
                      <HardDrive className="size-4 text-emerald-500" />
                    ) : (
                      <XCircle className="size-4" />
                    )}
                    <AlertTitle>
                      {!wizard.dirInspection.valid
                        ? t("database.dirInvalid")
                        : wizard.dirInspection.status === "adoptable_pgdata"
                          ? t("database.dirAdoptable", { version: wizard.dirInspection.pgVersion || "?" })
                          : wizard.dirInspection.status === "managed_existing"
                            ? t("database.dirManagedExisting")
                            : t("database.dirEmpty")}
                    </AlertTitle>
                    <AlertDescription className="space-y-1 text-xs">
                      {wizard.dirInspection.reason ? <p>{wizard.dirInspection.reason}</p> : null}
                      {wizard.dirInspection.valid ? (
                        <div className="font-mono text-[11px] text-muted-foreground">
                          {wizard.dirInspection.pgVersion ? (
                            <div>PostgreSQL: v{wizard.dirInspection.pgVersion}</div>
                          ) : null}
                          <div>pgdata: {wizard.dirInspection.postgresDataDir}</div>
                          <div>backups: {wizard.dirInspection.backupDir}</div>
                          <div>redis: {wizard.dirInspection.redisDataDir}</div>
                          <div>
                            {t("environment.diskFree")}: {formatBytes(wizard.dirInspection.diskFreeBytes)}
                          </div>
                        </div>
                      ) : null}
                      {wizard.dirInspection.warnings?.map((warn) => (
                        <p key={warn} className="text-amber-600 dark:text-amber-400">
                          {warn}
                        </p>
                      ))}
                    </AlertDescription>
                  </Alert>
                ) : null}

                {needsAdoptConfirm ? (
                  <div className="space-y-3 rounded-lg border border-amber-500/40 bg-amber-500/5 p-4">
                    <div className="flex items-start gap-2.5">
                      <Checkbox
                        id="adopt-pgdata"
                        checked={wizard.form.adoptExistingPGData}
                        onCheckedChange={(checked) => wizard.updateForm("adoptExistingPGData", checked === true)}
                      />
                      <div className="space-y-1">
                        <Label htmlFor="adopt-pgdata" className="font-medium">
                          {t("database.adoptTitle")}
                        </Label>
                        <p className="text-xs text-muted-foreground">{t("database.adoptDesc")}</p>
                      </div>
                    </div>
                    {wizard.form.adoptExistingPGData ? (
                      <div className="space-y-2.5 pt-1">
                        <p className="text-xs text-muted-foreground">{t("database.existingCredentialsHint")}</p>
                        <div className="grid gap-2.5 sm:grid-cols-2">
                          <Input
                            value={wizard.form.existingPGUser}
                            onChange={(e) => wizard.updateForm("existingPGUser", e.target.value)}
                            placeholder={t("database.existingPGUser")}
                          />
                          <Input
                            type="password"
                            value={wizard.form.existingPGPassword}
                            onChange={(e) => wizard.updateForm("existingPGPassword", e.target.value)}
                            placeholder={t("database.existingPGPassword")}
                          />
                          <Input
                            value={wizard.form.existingPGDatabase}
                            onChange={(e) => wizard.updateForm("existingPGDatabase", e.target.value)}
                            placeholder={t("database.existingPGDatabase")}
                          />
                          <Input
                            type="password"
                            value={wizard.form.existingRedisPassword}
                            onChange={(e) => wizard.updateForm("existingRedisPassword", e.target.value)}
                            placeholder={t("database.existingRedisPassword")}
                          />
                        </div>
                      </div>
                    ) : null}
                  </div>
                ) : null}

                {/* Cache mode */}
                <div className="space-y-2">
                  <Label>{t("database.cacheLabel")}</Label>
                  <div className="grid gap-2">
                    {(["managed_redis", "memory", "external_redis"] as const).map((mode: CacheMode) => (
                      <button
                        key={mode}
                        type="button"
                        onClick={() => wizard.updateForm("cacheMode", mode)}
                        className={cn(
                          "flex items-center justify-between rounded-lg border px-3 py-2 text-left text-sm transition-colors",
                          wizard.form.cacheMode === mode ? "border-primary bg-primary/5 font-medium" : "hover:bg-muted/40",
                        )}
                      >
                        <span>
                          {mode === "managed_redis"
                            ? t("database.cacheManagedRedis")
                            : mode === "memory"
                              ? t("database.cacheMemory")
                              : t("database.cacheExternalRedis")}
                        </span>
                      </button>
                    ))}
                  </div>
                </div>

                {wizard.form.cacheMode === "external_redis" ? (
                  <div className="grid gap-3 rounded-lg border p-3 sm:grid-cols-2">
                    <Input
                      value={wizard.form.externalRedisAddr}
                      onChange={(e) => wizard.updateForm("externalRedisAddr", e.target.value)}
                      placeholder={t("database.externalRedisAddr")}
                    />
                    <Input
                      type="password"
                      value={wizard.form.externalRedisPass}
                      onChange={(e) => wizard.updateForm("externalRedisPass", e.target.value)}
                      placeholder={t("database.externalRedisPass")}
                    />
                  </div>
                ) : null}

                {/* Advanced options */}
                <Collapsible open={advancedOpen} onOpenChange={setAdvancedOpen}>
                  <CollapsibleTrigger asChild>
                    <Button type="button" variant="ghost" size="sm" className="w-full justify-between px-2 text-muted-foreground">
                      <span>{t("database.advancedToggle")}</span>
                      <ChevronDown className={cn("size-4 transition-transform", advancedOpen && "rotate-180")} />
                    </Button>
                  </CollapsibleTrigger>
                  <CollapsibleContent className="mt-3 space-y-3 rounded-lg border bg-muted/20 p-4">
                    <div className="grid gap-3 sm:grid-cols-2">
                      <div className="space-y-1.5">
                        <Label className="text-xs">{t("database.pgPort")}</Label>
                        <Input
                          type="number"
                          value={wizard.form.postgresPort}
                          onChange={(e) => wizard.updateForm("postgresPort", Number(e.target.value) || 5432)}
                        />
                      </div>
                      <div className="space-y-1.5">
                        <Label className="text-xs">{t("database.pgMemoryMB")}</Label>
                        <Input
                          type="number"
                          value={wizard.form.postgresMemoryLimitMB}
                          onChange={(e) => wizard.updateForm("postgresMemoryLimitMB", Number(e.target.value) || 0)}
                        />
                      </div>
                      <div className="space-y-1.5">
                        <Label className="text-xs">{t("database.pgImage")}</Label>
                        <Input
                          value={wizard.form.postgresImage}
                          onChange={(e) => wizard.updateForm("postgresImage", e.target.value)}
                          className="font-mono text-xs"
                        />
                      </div>
                      <div className="space-y-1.5">
                        <Label className="text-xs">{t("database.redisPort")}</Label>
                        <Input
                          type="number"
                          value={wizard.form.redisPort}
                          onChange={(e) => wizard.updateForm("redisPort", Number(e.target.value) || 6379)}
                        />
                      </div>
                      <div className="space-y-1.5">
                        <Label className="text-xs">{t("database.redisImage")}</Label>
                        <Input
                          value={wizard.form.redisImage}
                          onChange={(e) => wizard.updateForm("redisImage", e.target.value)}
                          className="font-mono text-xs"
                        />
                      </div>
                      <div className="space-y-1.5">
                        <Label className="text-xs">{t("database.redisMemoryMB")}</Label>
                        <Input
                          type="number"
                          value={wizard.form.redisMemoryLimitMB}
                          onChange={(e) => wizard.updateForm("redisMemoryLimitMB", Number(e.target.value) || 0)}
                        />
                      </div>
                    </div>
                    <Separator />
                    <div className="space-y-2">
                      <Input
                        value={wizard.form.postgresDataDir}
                        onChange={(e) => wizard.updateForm("postgresDataDir", e.target.value)}
                        placeholder={t("database.pgDataDirOverride")}
                        className="font-mono text-xs"
                      />
                      <Input
                        value={wizard.form.redisDataDir}
                        onChange={(e) => wizard.updateForm("redisDataDir", e.target.value)}
                        placeholder={t("database.redisDataDirOverride")}
                        className="font-mono text-xs"
                      />
                      <Input
                        value={wizard.form.backupDir}
                        onChange={(e) => wizard.updateForm("backupDir", e.target.value)}
                        placeholder={t("database.backupDirOverride")}
                        className="font-mono text-xs"
                      />
                    </div>
                    <div className="flex items-center justify-between pt-1">
                      <Label htmlFor="compose-sync" className="text-xs font-medium">
                        {t("database.composeSync")}
                      </Label>
                      <Switch
                        id="compose-sync"
                        checked={wizard.form.composeSync}
                        onCheckedChange={(checked) => wizard.updateForm("composeSync", checked)}
                      />
                    </div>
                  </CollapsibleContent>
                </Collapsible>
              </div>
            ) : (
              /* External DB */
              <div className="space-y-4">
                <div className="space-y-2">
                  <Label htmlFor="ext-pg-dsn">{t("database.externalPGDSN")}</Label>
                  <Input
                    id="ext-pg-dsn"
                    value={wizard.form.externalPostgresDSN}
                    onChange={(e) => wizard.updateForm("externalPostgresDSN", e.target.value)}
                    placeholder={t("database.externalPGDSNPlaceholder")}
                    className="font-mono text-sm"
                  />
                </div>
                <div className="space-y-2">
                  <Label>{t("database.cacheLabel")}</Label>
                  <div className="grid gap-2 sm:grid-cols-2">
                    {(["memory", "external_redis"] as const).map((mode: CacheMode) => (
                      <button
                        key={mode}
                        type="button"
                        onClick={() => wizard.updateForm("cacheMode", mode)}
                        className={cn(
                          "rounded-lg border px-3 py-2 text-left text-sm transition-colors",
                          wizard.form.cacheMode === mode ? "border-primary bg-primary/5 font-medium" : "hover:bg-muted/40",
                        )}
                      >
                        {mode === "memory" ? t("database.cacheMemory") : t("database.cacheExternalRedis")}
                      </button>
                    ))}
                  </div>
                </div>
                {wizard.form.cacheMode === "external_redis" ? (
                  <div className="grid gap-3 sm:grid-cols-2">
                    <Input
                      value={wizard.form.externalRedisAddr}
                      onChange={(e) => wizard.updateForm("externalRedisAddr", e.target.value)}
                      placeholder={t("database.externalRedisAddr")}
                    />
                    <Input
                      value={wizard.form.externalRedisUser}
                      onChange={(e) => wizard.updateForm("externalRedisUser", e.target.value)}
                      placeholder={t("database.externalRedisUser")}
                    />
                    <Input
                      type="password"
                      value={wizard.form.externalRedisPass}
                      onChange={(e) => wizard.updateForm("externalRedisPass", e.target.value)}
                      placeholder={t("database.externalRedisPass")}
                    />
                    <div className="flex items-center gap-2">
                      <Switch
                        id="ext-redis-tls"
                        checked={wizard.form.externalRedisTLS}
                        onCheckedChange={(checked) => wizard.updateForm("externalRedisTLS", checked)}
                      />
                      <Label htmlFor="ext-redis-tls" className="text-xs">
                        {t("database.externalRedisTLS")}
                      </Label>
                    </div>
                  </div>
                ) : null}

                <Button
                  type="button"
                  variant="outline"
                  disabled={wizard.testingExternal || !wizard.form.externalPostgresDSN.trim()}
                  onClick={() => void wizard.handleTestExternal()}
                >
                  {wizard.testingExternal ? <Loader2 className="mr-1.5 size-4 animate-spin" /> : null}
                  {wizard.testingExternal ? t("database.testing") : t("database.testExternal")}
                </Button>

                {wizard.externalResult ? (
                  <Alert variant={wizard.externalResult.postgresOK && (!wizard.externalResult.redisChecked || wizard.externalResult.redisOK) ? "default" : "destructive"}>
                    <AlertDescription className="space-y-1 text-xs">
                      <div>
                        {wizard.externalResult.postgresOK
                          ? `✓ ${t("database.pgOk")}: ${wizard.externalResult.postgresVersion || ""}`
                          : `✗ PostgreSQL: ${wizard.externalResult.postgresError || "failed"}`}
                      </div>
                      {wizard.externalResult.postgresOK ? (
                        <div>
                          {wizard.externalResult.postgresVectorAvailable || wizard.externalResult.postgresVectorInstalled
                            ? `✓ ${t("database.pgVectorReady")}`
                            : `⚠ ${t("database.pgVectorMissing")}`}
                        </div>
                      ) : null}
                      {wizard.externalResult.redisChecked ? (
                        <div>
                          {wizard.externalResult.redisOK
                            ? `✓ ${t("database.redisOk")}`
                            : `✗ Redis: ${wizard.externalResult.redisError || "failed"}`}
                        </div>
                      ) : null}
                    </AlertDescription>
                  </Alert>
                ) : null}
              </div>
            )}
            {(wizard.form.databaseMode === "external" || wizard.form.adoptExistingPGData) && (
              <div className="space-y-2">
                <Label htmlFor="original-key">{t("database.originalKey")}</Label>
                <Input
                  id="original-key"
                  type="password"
                  autoComplete="off"
                  value={wizard.form.existingDataEncryptionKey}
                  onChange={(e) => wizard.updateForm("existingDataEncryptionKey", e.target.value)}
                />
              </div>
            )}
          </CardContent>
          <CardFooter className="justify-between">
            <Button type="button" variant="outline" onClick={() => wizard.setStep("environment")}>
              {t("actions.prev")}
            </Button>
            <Button type="button" disabled={!canLeaveDatabaseStep} onClick={() => wizard.setStep("site")}>
              {t("actions.next")}
            </Button>
          </CardFooter>
        </Card>
      ) : null}

      {/* Step 3: Site & Admin */}
      {wizard.step === "site" ? (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-lg">
              <Server className="size-5 text-primary" />
              {t("site.title")}
            </CardTitle>
            <CardDescription>{t("site.description")}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-5">
            <div className="space-y-2">
              <Label htmlFor="public-url">{t("site.publicUrlLabel")}</Label>
              <Input
                id="public-url"
                value={wizard.form.publicBaseURL}
                onChange={(e) => {
                  const val = e.target.value;
                  wizard.updateForm("publicBaseURL", val);
                  if (val.trim().startsWith("https://")) {
                    wizard.updateForm("env", "prod");
                  } else if (val.trim().startsWith("http://")) {
                    wizard.updateForm("env", "dev");
                  }
                }}
                placeholder={t("site.publicUrlPlaceholder")}
                className="font-mono text-sm"
              />
            </div>

            <div className="space-y-2">
              <Label>{t("site.envLabel")}</Label>
              <div className="grid gap-2">
                {(["prod", "dev"] as const).map((mode: RuntimeEnvMode) => (
                  <button
                    key={mode}
                    type="button"
                    onClick={() => wizard.updateForm("env", mode)}
                    className={cn(
                      "rounded-lg border px-3 py-2 text-left text-sm transition-colors",
                      wizard.form.env === mode ? "border-primary bg-primary/5 font-medium" : "hover:bg-muted/40",
                    )}
                  >
                    {mode === "prod" ? t("site.envProd") : t("site.envDev")}
                  </button>
                ))}
              </div>
            </div>

            <div className="space-y-2">
              <Label htmlFor="cors-origin">{t("site.corsLabel")}</Label>
              <Input
                id="cors-origin"
                value={wizard.form.corsAllowOrigin}
                onChange={(e) => wizard.updateForm("corsAllowOrigin", e.target.value)}
                placeholder={wizard.form.publicBaseURL || "https://chat.example.com"}
                className="font-mono text-sm"
              />
            </div>

            <Separator />

            <div className="space-y-3">
              <h3 className="text-sm font-semibold">{t("site.adminSectionTitle")}</h3>
              {wizard.form.adoptExistingPGData ? (
                <p className="text-xs text-muted-foreground">{t("site.adoptAdminHint")}</p>
              ) : null}
              <div className="grid gap-3 sm:grid-cols-2">
                <div className="space-y-1.5">
                  <Label htmlFor="admin-username">{t("site.adminUsername")}</Label>
                  <Input
                    id="admin-username"
                    value={wizard.form.adminUsername}
                    onChange={(e) => wizard.updateForm("adminUsername", e.target.value)}
                    autoComplete="username"
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="admin-display-name">{t("site.adminDisplayName")}</Label>
                  <Input
                    id="admin-display-name"
                    value={wizard.form.adminDisplayName}
                    onChange={(e) => wizard.updateForm("adminDisplayName", e.target.value)}
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="admin-password">{t("site.adminPassword")}</Label>
                  <Input
                    id="admin-password"
                    type="password"
                    value={wizard.form.adminPassword}
                    onChange={(e) => wizard.updateForm("adminPassword", e.target.value)}
                    autoComplete="new-password"
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="admin-password-confirm">{t("site.adminPasswordConfirm")}</Label>
                  <Input
                    id="admin-password-confirm"
                    type="password"
                    value={wizard.form.adminPasswordConfirm}
                    onChange={(e) => wizard.updateForm("adminPasswordConfirm", e.target.value)}
                    autoComplete="new-password"
                  />
                </div>
              </div>
            </div>

            {wizard.submitError ? (
              <Alert variant="destructive">
                <XCircle className="size-4" />
                <AlertDescription>
                  {wizard.submitError === "passwordMismatch" ? t("site.passwordMismatch") : wizard.submitError}
                </AlertDescription>
              </Alert>
            ) : null}
          </CardContent>
          <CardFooter className="justify-between">
            <Button type="button" variant="outline" onClick={() => wizard.setStep("database")}>
              {t("actions.prev")}
            </Button>
            <Button type="button" disabled={wizard.submitting} onClick={() => void wizard.handleStartDeploy()}>
              {wizard.submitting ? <Loader2 className="mr-2 size-4 animate-spin" /> : null}
              {t("site.startDeploy")}
            </Button>
          </CardFooter>
        </Card>
      ) : null}

      {/* Step 4: Progress */}
      {wizard.step === "progress" ? (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-lg">
              {wizard.jobDone ? (
                <CheckCircle2 className="size-5 text-emerald-500" />
              ) : wizard.jobFailed ? (
                <XCircle className="size-5 text-destructive" />
              ) : (
                <Loader2 className="size-5 animate-spin text-primary" />
              )}
              {wizard.jobDone
                ? t("progress.doneTitle")
                : wizard.jobFailed
                  ? t("progress.failedTitle")
                  : t("progress.title")}
            </CardTitle>
            <CardDescription>
              {wizard.jobDone ? t("progress.doneDesc") : t("progress.subtitle")}
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="space-y-1.5">
              <div className="flex justify-between text-xs text-muted-foreground">
                <span>{wizard.jobDone ? t("progress.waitingReady") : `${wizard.latestPercent}%`}</span>
                <span className="font-mono">{wizard.latestPercent}%</span>
              </div>
              <div className="h-2 w-full overflow-hidden rounded-full bg-muted">
                <div
                  className={cn(
                    "h-full transition-all duration-300",
                    wizard.jobFailed ? "bg-destructive" : wizard.jobDone ? "bg-emerald-500" : "bg-primary",
                  )}
                  style={{ width: `${wizard.latestPercent}%` }}
                />
              </div>
            </div>

            {wizard.job?.composePath || wizard.job?.configPath ? (
              <div className="space-y-1 rounded-lg border bg-muted/30 p-3 font-mono text-xs">
                {wizard.job.configPath ? (
                  <div>
                    {t("progress.configSaved")}: {wizard.job.configPath}
                  </div>
                ) : null}
                {wizard.job.composePath ? (
                  <div>
                    {t("progress.composeSaved")}: {wizard.job.composePath}
                  </div>
                ) : null}
              </div>
            ) : null}

            <div className="max-h-72 space-y-1.5 overflow-y-auto rounded-lg border bg-muted/20 p-3 font-mono text-xs">
              {wizard.events.map((evt, idx) => (
                <div
                  key={`${evt.timestamp}-${evt.step}-${idx}`}
                  className={cn(
                    "flex items-start gap-2",
                    evt.status === "failed" && "text-destructive",
                    evt.status === "done" && "text-emerald-600 dark:text-emerald-400",
                  )}
                >
                  <span className="shrink-0 opacity-60">[{evt.step}]</span>
                  <span className="break-all">{evt.message}</span>
                </div>
              ))}
            </div>
          </CardContent>
          <CardFooter className="justify-end gap-2">
            {wizard.jobFailed ? (
              <Button type="button" variant="outline" onClick={() => wizard.setStep("site")}>
                {t("progress.backToEdit")}
              </Button>
            ) : null}
            {wizard.jobDone ? (
              <Button type="button" onClick={() => window.location.replace("/login")}>
                {t("progress.goLogin")}
              </Button>
            ) : null}
          </CardFooter>
        </Card>
      ) : null}
    </div>
  );
}
