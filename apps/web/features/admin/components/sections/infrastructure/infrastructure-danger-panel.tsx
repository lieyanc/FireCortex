"use client";

import type {
  BackupRecordDTO,
  DangerRecreateContainerRequest,
  DangerRestoreBackupRequest,
  DangerSwitchDatabaseRequest,
} from "@firecortex/api-contract";
import { AlertTriangle, DatabaseBackup, Loader2, RefreshCw, ShieldAlert } from "lucide-react";
import { useTranslations } from "next-intl";
import * as React from "react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

export function InfrastructureDangerPanel({
  backups,
  managedPostgresEnabled,
  busy,
  onRestoreBackup,
  onSwitchDatabase,
  onRecreateContainer,
}: {
  backups: BackupRecordDTO[];
  managedPostgresEnabled: boolean;
  busy: boolean;
  onRestoreBackup: (payload: DangerRestoreBackupRequest) => Promise<boolean>;
  onSwitchDatabase: (payload: DangerSwitchDatabaseRequest) => Promise<boolean>;
  onRecreateContainer: (payload: DangerRecreateContainerRequest) => Promise<boolean>;
}) {
  const t = useTranslations("adminInfrastructure.danger");

  // Restore state
  const [restoreBackupID, setRestoreBackupID] = React.useState(backups[0]?.id ?? "");
  const [restorePassword, setRestorePassword] = React.useState("");
  const [restoreConfirm, setRestoreConfirm] = React.useState("");

  // Switch DB state
  const [dbMode, setDbMode] = React.useState<"managed" | "external">("external");
  const [cacheMode, setCacheMode] = React.useState<"managed_redis" | "external_redis" | "memory">("memory");
  const [extDSN, setExtDSN] = React.useState("");
  const [extRedisAddr, setExtRedisAddr] = React.useState("");
  const [extRedisPass, setExtRedisPass] = React.useState("");
  const [switchPassword, setSwitchPassword] = React.useState("");
  const [switchConfirm, setSwitchConfirm] = React.useState("");

  // Recreate state
  const [recreateService, setRecreateService] = React.useState<"postgres" | "redis">("postgres");
  const [recreatePassword, setRecreatePassword] = React.useState("");
  const [recreateConfirm, setRecreateConfirm] = React.useState("");

  React.useEffect(() => {
    if (!restoreBackupID && backups.length > 0) {
      setRestoreBackupID(backups[0].id);
    }
  }, [backups, restoreBackupID]);

  return (
    <div className="space-y-5">
      <Alert variant="destructive">
        <ShieldAlert className="size-4" />
        <AlertTitle>{t("title")}</AlertTitle>
        <AlertDescription>{t("description")}</AlertDescription>
      </Alert>

      {/* 1. Restore Backup */}
      <Card className="border-destructive/40">
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base text-destructive">
            <DatabaseBackup className="size-4" />
            {t("restore.title")}
          </CardTitle>
          <CardDescription>{t("restore.description")}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="space-y-1.5">
            <Label className="text-xs">{t("restore.selectBackup")}</Label>
            <Select value={restoreBackupID} onValueChange={setRestoreBackupID}>
              <SelectTrigger>
                <SelectValue placeholder={t("restore.selectBackup")} />
              </SelectTrigger>
              <SelectContent>
                {backups.map((b) => (
                  <SelectItem key={b.id} value={b.id}>
                    {b.fileName}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label className="text-xs">{t("currentPassword")}</Label>
              <Input
                type="password"
                value={restorePassword}
                onChange={(e) => setRestorePassword(e.target.value)}
                autoComplete="current-password"
              />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">{t("confirmText", { word: "RESTORE" })}</Label>
              <Input
                value={restoreConfirm}
                onChange={(e) => setRestoreConfirm(e.target.value)}
                placeholder="RESTORE"
                className="font-mono"
              />
            </div>
          </div>
        </CardContent>
        <CardFooter className="justify-end">
          <Button
            type="button"
            variant="destructive"
            disabled={
              busy ||
              !managedPostgresEnabled ||
              !restoreBackupID ||
              !restorePassword ||
              restoreConfirm.trim() !== "RESTORE"
            }
            onClick={async () => {
              const ok = await onRestoreBackup({
                backupID: restoreBackupID,
                currentPassword: restorePassword,
                confirmText: restoreConfirm.trim(),
              });
              if (ok) {
                setRestorePassword("");
                setRestoreConfirm("");
              }
            }}
          >
            {busy ? <Loader2 className="mr-1.5 size-4 animate-spin" /> : null}
            {t("restore.submit")}
          </Button>
        </CardFooter>
      </Card>

      {/* 2. Switch Database Mode */}
      <Card className="border-destructive/40">
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base text-destructive">
            <AlertTriangle className="size-4" />
            {t("switchDb.title")}
          </CardTitle>
          <CardDescription>{t("switchDb.description")}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label className="text-xs">{t("switchDb.dbMode")}</Label>
              <Select
                value={dbMode}
                onValueChange={(val) => {
                  if (val === "managed" || val === "external") setDbMode(val);
                }}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="managed">{t("switchDb.dbManaged")}</SelectItem>
                  <SelectItem value="external">{t("switchDb.dbExternal")}</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">{t("switchDb.cacheMode")}</Label>
              <Select
                value={cacheMode}
                onValueChange={(val) => {
                  if (val === "managed_redis" || val === "external_redis" || val === "memory") {
                    setCacheMode(val);
                  }
                }}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="managed_redis">{t("switchDb.cacheManaged")}</SelectItem>
                  <SelectItem value="external_redis">{t("switchDb.cacheExternal")}</SelectItem>
                  <SelectItem value="memory">{t("switchDb.cacheMemory")}</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>

          {dbMode === "external" ? (
            <div className="space-y-1.5">
              <Label className="text-xs">{t("switchDb.externalDsn")}</Label>
              <Input
                value={extDSN}
                onChange={(e) => setExtDSN(e.target.value)}
                placeholder="postgres://user:password@host:5432/firecortex?sslmode=disable"
                className="font-mono text-xs"
              />
            </div>
          ) : null}

          {cacheMode === "external_redis" ? (
            <div className="grid gap-3 sm:grid-cols-2">
              <Input
                value={extRedisAddr}
                onChange={(e) => setExtRedisAddr(e.target.value)}
                placeholder={t("switchDb.externalRedisAddr")}
              />
              <Input
                type="password"
                value={extRedisPass}
                onChange={(e) => setExtRedisPass(e.target.value)}
                placeholder={t("switchDb.externalRedisPass")}
              />
            </div>
          ) : null}

          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label className="text-xs">{t("currentPassword")}</Label>
              <Input
                type="password"
                value={switchPassword}
                onChange={(e) => setSwitchPassword(e.target.value)}
                autoComplete="current-password"
              />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">{t("confirmText", { word: "SWITCH DATABASE" })}</Label>
              <Input
                value={switchConfirm}
                onChange={(e) => setSwitchConfirm(e.target.value)}
                placeholder="SWITCH DATABASE"
                className="font-mono"
              />
            </div>
          </div>
        </CardContent>
        <CardFooter className="justify-end">
          <Button
            type="button"
            variant="destructive"
            disabled={busy || !switchPassword || switchConfirm.trim() !== "SWITCH DATABASE"}
            onClick={async () => {
              const ok = await onSwitchDatabase({
                currentPassword: switchPassword,
                confirmText: switchConfirm.trim(),
                databaseMode: dbMode,
                externalPostgresDSN: extDSN,
                cacheMode,
                externalRedisAddr: extRedisAddr,
                externalRedisUsername: "",
                externalRedisPassword: extRedisPass,
                externalRedisDB: 0,
                externalRedisTLS: false,
                externalRedisSkipTLS: false,
              });
              if (ok) {
                setSwitchPassword("");
                setSwitchConfirm("");
              }
            }}
          >
            {busy ? <Loader2 className="mr-1.5 size-4 animate-spin" /> : null}
            {t("switchDb.submit")}
          </Button>
        </CardFooter>
      </Card>

      {/* 3. Force Recreate Container */}
      <Card className="border-destructive/40">
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base text-destructive">
            <RefreshCw className="size-4" />
            {t("recreate.title")}
          </CardTitle>
          <CardDescription>{t("recreate.description")}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="grid gap-3 sm:grid-cols-3">
            <div className="space-y-1.5">
              <Label className="text-xs">{t("recreate.selectService")}</Label>
              <Select
                value={recreateService}
                onValueChange={(val) => {
                  if (val === "postgres" || val === "redis") setRecreateService(val);
                }}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="postgres">PostgreSQL (firecortex-postgres)</SelectItem>
                  <SelectItem value="redis">Redis (firecortex-redis)</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">{t("currentPassword")}</Label>
              <Input
                type="password"
                value={recreatePassword}
                onChange={(e) => setRecreatePassword(e.target.value)}
                autoComplete="current-password"
              />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">{t("confirmText", { word: "RECREATE" })}</Label>
              <Input
                value={recreateConfirm}
                onChange={(e) => setRecreateConfirm(e.target.value)}
                placeholder="RECREATE"
                className="font-mono"
              />
            </div>
          </div>
        </CardContent>
        <CardFooter className="justify-end">
          <Button
            type="button"
            variant="destructive"
            disabled={busy || !recreatePassword || recreateConfirm.trim() !== "RECREATE"}
            onClick={async () => {
              const ok = await onRecreateContainer({
                serviceID: recreateService,
                currentPassword: recreatePassword,
                confirmText: recreateConfirm.trim(),
              });
              if (ok) {
                setRecreatePassword("");
                setRecreateConfirm("");
              }
            }}
          >
            {busy ? <Loader2 className="mr-1.5 size-4 animate-spin" /> : null}
            {t("recreate.submit")}
          </Button>
        </CardFooter>
      </Card>
    </div>
  );
}
