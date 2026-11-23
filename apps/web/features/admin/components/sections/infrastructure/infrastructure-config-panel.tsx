"use client";

import type { InfraConfigResponse, PatchInfraConfigRequest } from "@firecortex/api-contract";
import { Loader2, RotateCw, Save } from "lucide-react";
import { useTranslations } from "next-intl";
import * as React from "react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";

export function InfrastructureConfigPanel({
  config,
  saving,
  onSave,
}: {
  config: InfraConfigResponse;
  saving: boolean;
  onSave: (payload: PatchInfraConfigRequest) => void;
}) {
  const t = useTranslations("adminInfrastructure.config");

  const [env, setEnv] = React.useState(config.env);
  const [publicAPIBaseURL, setPublicAPIBaseURL] = React.useState(config.publicAPIBaseURL);
  const [publicWebBaseURL, setPublicWebBaseURL] = React.useState(config.publicWebBaseURL);
  const [corsAllowOrigin, setCorsAllowOrigin] = React.useState(config.corsAllowOrigin);
  const [trustedProxies, setTrustedProxies] = React.useState(config.trustedProxies);
  const [ssrfEnabled, setSsrfEnabled] = React.useState(config.ssrfProtectionEnabled);
  const [ssrfAllowedHosts, setSsrfAllowedHosts] = React.useState(config.ssrfAllowedHosts);
  const [ssrfAllowedCIDRs, setSsrfAllowedCIDRs] = React.useState(config.ssrfAllowedCIDRs);

  const [storageBackend, setStorageBackend] = React.useState(config.storageBackend || "local");
  const [storageRootDir, setStorageRootDir] = React.useState(config.storageRootDir);
  const [s3Endpoint, setS3Endpoint] = React.useState(config.storageS3Endpoint);
  const [s3Region, setS3Region] = React.useState(config.storageS3Region);
  const [s3Bucket, setS3Bucket] = React.useState(config.storageS3Bucket);
  const [s3Prefix, setS3Prefix] = React.useState(config.storageS3Prefix);
  const [s3AccessKey, setS3AccessKey] = React.useState(config.storageS3AccessKeyID);
  const [s3SecretKey, setS3SecretKey] = React.useState("");
  const [s3ForcePathStyle, setS3ForcePathStyle] = React.useState(config.storageS3ForcePathStyle);

  const [registryMirror, setRegistryMirror] = React.useState(config.managedRegistryMirror);
  const [composeSync, setComposeSync] = React.useState(config.managedComposeSync);
  const [pgMemLimit, setPgMemLimit] = React.useState(config.managedPostgresMemoryLimitMB);
  const [redisMemLimit, setRedisMemLimit] = React.useState(config.managedRedisMemoryLimitMB);
  const [backupEnabled, setBackupEnabled] = React.useState(config.managedBackupEnabled);
  const [backupSchedule, setBackupSchedule] = React.useState(config.managedBackupSchedule);
  const [backupRetention, setBackupRetention] = React.useState(config.managedBackupRetentionCount);

  const [updateEnabled, setUpdateEnabled] = React.useState(config.updateEnabled);
  const [updateChannel, setUpdateChannel] = React.useState(config.updateChannel || "stable");
  const [updateSource, setUpdateSource] = React.useState(config.updateSource || "github");
  const [updateProxy, setUpdateProxy] = React.useState(config.updateProxyBaseURL);

  React.useEffect(() => {
    setEnv(config.env);
    setPublicAPIBaseURL(config.publicAPIBaseURL);
    setPublicWebBaseURL(config.publicWebBaseURL);
    setCorsAllowOrigin(config.corsAllowOrigin);
    setTrustedProxies(config.trustedProxies);
    setSsrfEnabled(config.ssrfProtectionEnabled);
    setSsrfAllowedHosts(config.ssrfAllowedHosts);
    setSsrfAllowedCIDRs(config.ssrfAllowedCIDRs);
    setStorageBackend(config.storageBackend || "local");
    setStorageRootDir(config.storageRootDir);
    setS3Endpoint(config.storageS3Endpoint);
    setS3Region(config.storageS3Region);
    setS3Bucket(config.storageS3Bucket);
    setS3Prefix(config.storageS3Prefix);
    setS3AccessKey(config.storageS3AccessKeyID);
    setS3ForcePathStyle(config.storageS3ForcePathStyle);
    setRegistryMirror(config.managedRegistryMirror);
    setComposeSync(config.managedComposeSync);
    setPgMemLimit(config.managedPostgresMemoryLimitMB);
    setRedisMemLimit(config.managedRedisMemoryLimitMB);
    setBackupEnabled(config.managedBackupEnabled);
    setBackupSchedule(config.managedBackupSchedule);
    setBackupRetention(config.managedBackupRetentionCount);
    setUpdateEnabled(config.updateEnabled);
    setUpdateChannel(config.updateChannel || "stable");
    setUpdateSource(config.updateSource || "github");
    setUpdateProxy(config.updateProxyBaseURL);
  }, [config]);

  const buildPayload = (restartNow: boolean): PatchInfraConfigRequest => ({
    env,
    publicAPIBaseURL,
    publicWebBaseURL,
    corsAllowOrigin,
    trustedProxies,
    ssrfProtectionEnabled: ssrfEnabled,
    ssrfAllowedHosts,
    ssrfAllowedCIDRs,
    storageBackend,
    storageRootDir,
    storageS3Endpoint: s3Endpoint,
    storageS3Region: s3Region,
    storageS3Bucket: s3Bucket,
    storageS3Prefix: s3Prefix,
    storageS3AccessKeyID: s3AccessKey,
    storageS3SecretAccessKey: s3SecretKey.trim(),
    storageS3ForcePathStyle: s3ForcePathStyle,
    managedRegistryMirror: registryMirror,
    managedComposeSync: composeSync,
    managedPostgresMemoryLimitMB: pgMemLimit,
    managedRedisMemoryLimitMB: redisMemLimit,
    managedBackupEnabled: backupEnabled,
    managedBackupSchedule: backupSchedule,
    managedBackupRetentionCount: backupRetention,
    updateEnabled,
    updateChannel,
    updateCheckIntervalSeconds: config.updateCheckIntervalSeconds || 3600,
    updateSource,
    updateProxyBaseURL: updateProxy,
    restartNow,
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{t("title")}</CardTitle>
        <CardDescription>{t("description")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        {/* Site & Network */}
        <div className="space-y-3">
          <h3 className="text-sm font-semibold">{t("siteGroup")}</h3>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label className="text-xs">{t("env")}</Label>
              <Input value={env} onChange={(e) => setEnv(e.target.value)} className="font-mono text-xs" />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">{t("trustedProxies")}</Label>
              <Input value={trustedProxies} onChange={(e) => setTrustedProxies(e.target.value)} className="font-mono text-xs" />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">{t("publicApiUrl")}</Label>
              <Input value={publicAPIBaseURL} onChange={(e) => setPublicAPIBaseURL(e.target.value)} className="font-mono text-xs" />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">{t("publicWebUrl")}</Label>
              <Input value={publicWebBaseURL} onChange={(e) => setPublicWebBaseURL(e.target.value)} className="font-mono text-xs" />
            </div>
            <div className="space-y-1.5 sm:col-span-2">
              <Label className="text-xs">{t("corsOrigin")}</Label>
              <Input value={corsAllowOrigin} onChange={(e) => setCorsAllowOrigin(e.target.value)} className="font-mono text-xs" />
            </div>
            <div className="flex items-center justify-between rounded-lg border px-3 py-2 sm:col-span-2">
              <Label className="text-xs">{t("ssrfEnabled")}</Label>
              <Switch checked={ssrfEnabled} onCheckedChange={setSsrfEnabled} />
            </div>
            {ssrfEnabled ? (
              <>
                <div className="space-y-1.5">
                  <Label className="text-xs">{t("ssrfAllowedHosts")}</Label>
                  <Input value={ssrfAllowedHosts} onChange={(e) => setSsrfAllowedHosts(e.target.value)} className="font-mono text-xs" />
                </div>
                <div className="space-y-1.5">
                  <Label className="text-xs">{t("ssrfAllowedCidrs")}</Label>
                  <Input value={ssrfAllowedCIDRs} onChange={(e) => setSsrfAllowedCIDRs(e.target.value)} className="font-mono text-xs" />
                </div>
              </>
            ) : null}
          </div>
        </div>

        <Separator />

        {/* Storage */}
        <div className="space-y-3">
          <h3 className="text-sm font-semibold">{t("storageGroup")}</h3>
          <div className="flex gap-2">
            {(["local", "s3"] as const).map((mode) => (
              <Button
                key={mode}
                type="button"
                size="sm"
                variant={storageBackend === mode ? "default" : "outline"}
                onClick={() => setStorageBackend(mode)}
              >
                {mode.toUpperCase()}
              </Button>
            ))}
          </div>
          {storageBackend === "local" ? (
            <div className="space-y-1.5">
              <Label className="text-xs">{t("storageRootDir")}</Label>
              <Input value={storageRootDir} onChange={(e) => setStorageRootDir(e.target.value)} className="font-mono text-xs" />
            </div>
          ) : (
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label className="text-xs">{t("s3Endpoint")}</Label>
                <Input value={s3Endpoint} onChange={(e) => setS3Endpoint(e.target.value)} className="font-mono text-xs" />
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs">{t("s3Region")}</Label>
                <Input value={s3Region} onChange={(e) => setS3Region(e.target.value)} className="font-mono text-xs" />
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs">{t("s3Bucket")}</Label>
                <Input value={s3Bucket} onChange={(e) => setS3Bucket(e.target.value)} className="font-mono text-xs" />
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs">{t("s3Prefix")}</Label>
                <Input value={s3Prefix} onChange={(e) => setS3Prefix(e.target.value)} className="font-mono text-xs" />
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs">{t("s3AccessKey")}</Label>
                <Input value={s3AccessKey} onChange={(e) => setS3AccessKey(e.target.value)} className="font-mono text-xs" />
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs">{t("s3SecretKey")}</Label>
                <Input
                  type="password"
                  value={s3SecretKey}
                  onChange={(e) => setS3SecretKey(e.target.value)}
                  placeholder={config.storageS3SecretConfigured ? "••••••••" : ""}
                />
              </div>
              <div className="flex items-center justify-between rounded-lg border px-3 py-2 sm:col-span-2">
                <Label className="text-xs">{t("s3ForcePathStyle")}</Label>
                <Switch checked={s3ForcePathStyle} onCheckedChange={setS3ForcePathStyle} />
              </div>
            </div>
          )}
        </div>

        <Separator />

        {/* Managed & Backups */}
        <div className="space-y-3">
          <h3 className="text-sm font-semibold">{t("managedGroup")}</h3>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5 sm:col-span-2">
              <Label className="text-xs">{t("registryMirror")}</Label>
              <Input
                value={registryMirror}
                onChange={(e) => setRegistryMirror(e.target.value)}
                placeholder="docker.m.daocloud.io"
                className="font-mono text-xs"
              />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">{t("pgMemLimit")}</Label>
              <Input
                type="number"
                value={pgMemLimit}
                onChange={(e) => setPgMemLimit(Math.max(0, Number(e.target.value) || 0))}
              />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">{t("redisMemLimit")}</Label>
              <Input
                type="number"
                value={redisMemLimit}
                onChange={(e) => setRedisMemLimit(Math.max(0, Number(e.target.value) || 0))}
              />
            </div>
            <div className="flex items-center justify-between rounded-lg border px-3 py-2">
              <Label className="text-xs">{t("composeSync")}</Label>
              <Switch checked={composeSync} onCheckedChange={setComposeSync} />
            </div>
            <div className="flex items-center justify-between rounded-lg border px-3 py-2">
              <Label className="text-xs">{t("backupEnabled")}</Label>
              <Switch checked={backupEnabled} onCheckedChange={setBackupEnabled} />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">{t("backupSchedule")}</Label>
              <Input value={backupSchedule} onChange={(e) => setBackupSchedule(e.target.value)} className="font-mono text-xs" />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">{t("backupRetention")}</Label>
              <Input
                type="number"
                value={backupRetention}
                onChange={(e) => setBackupRetention(Math.max(1, Number(e.target.value) || 7))}
              />
            </div>
          </div>
        </div>

        <Separator />

        {/* OTA Update */}
        <div className="space-y-3">
          <h3 className="text-sm font-semibold">{t("updateGroup")}</h3>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="flex items-center justify-between rounded-lg border px-3 py-2 sm:col-span-2">
              <Label className="text-xs">{t("updateEnabled")}</Label>
              <Switch checked={updateEnabled} onCheckedChange={setUpdateEnabled} />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">{t("updateChannel")}</Label>
              <Input value={updateChannel} onChange={(e) => setUpdateChannel(e.target.value)} className="font-mono text-xs" />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">{t("updateSource")}</Label>
              <Input value={updateSource} onChange={(e) => setUpdateSource(e.target.value)} className="font-mono text-xs" />
            </div>
            <div className="space-y-1.5 sm:col-span-2">
              <Label className="text-xs">{t("updateProxy")}</Label>
              <Input value={updateProxy} onChange={(e) => setUpdateProxy(e.target.value)} className="font-mono text-xs" />
            </div>
          </div>
        </div>
      </CardContent>
      <CardFooter className="justify-end gap-2">
        <Button type="button" variant="outline" disabled={saving} onClick={() => onSave(buildPayload(false))}>
          {saving ? <Loader2 className="mr-1.5 size-4 animate-spin" /> : <Save className="mr-1.5 size-4" />}
          {saving ? t("saving") : t("saveOnly")}
        </Button>
        <Button type="button" disabled={saving} onClick={() => onSave(buildPayload(true))}>
          {saving ? <Loader2 className="mr-1.5 size-4 animate-spin" /> : <RotateCw className="mr-1.5 size-4" />}
          {t("saveAndRestart")}
        </Button>
      </CardFooter>
    </Card>
  );
}
