"use client";

import * as React from "react";
import { useTranslations } from "next-intl";
import { toast } from "sonner";

import {
  type AdminModelRouterTestRequest,
  type AdminModelRouterTestResponse,
  getAdminReferenceData,
  listAdminSettings,
  patchAdminSettings,
  testAdminModelRouter,
} from "@/features/admin/api";
import {
  buildRouterClassifierModelOptions,
  collectPolicyDiagnostics,
  createDefaultModelRouterPolicy,
  extractRouterChatModels,
  type ModelRouterPolicy,
  parseModelRouterPolicyJSON,
  type RouterChatModelMeta,
  stringifyModelRouterPolicy,
} from "@/features/admin/model/model-router-policy";
import { resolveAdminErrorMessage } from "@/features/admin/utils/admin-error";
import type { PatchSettingItem, SettingsGrouped } from "@/shared/api/settings-types";
import { resolveAccessToken } from "@/shared/auth/resolve-access-token";
import { overrideFeaturePolicy } from "@/shared/hooks/use-feature-policy";

function extractModelRouterSettings(grouped: SettingsGrouped): {
  enabled: boolean;
  policyJSON: string;
  conversationTaskModel: string;
} {
  const chatItems = grouped.chat ?? [];
  const byKey = new Map(chatItems.map((item) => [item.key, item.value]));
  const enabled = (byKey.get("model_router_enabled") ?? "").trim() === "true";
  const rawPolicy = (byKey.get("model_router_policy") ?? "").trim();
  const policyJSON = rawPolicy || stringifyModelRouterPolicy(createDefaultModelRouterPolicy());
  const conversationTaskModel = (byKey.get("conversation_task_model") ?? "").trim();
  return { enabled, policyJSON, conversationTaskModel };
}

export function useAdminModelRouter() {
  const t = useTranslations("adminModelRouter");
  const [loading, setLoading] = React.useState(true);
  const [saving, setSaving] = React.useState(false);
  const [enabled, setEnabled] = React.useState(false);
  const [savedEnabled, setSavedEnabled] = React.useState(false);
  const [policyJSON, setPolicyJSON] = React.useState(() =>
    stringifyModelRouterPolicy(createDefaultModelRouterPolicy()),
  );
  const [savedPolicyJSON, setSavedPolicyJSON] = React.useState(() =>
    stringifyModelRouterPolicy(createDefaultModelRouterPolicy()),
  );
  const [conversationTaskModel, setConversationTaskModel] = React.useState("");
  const [chatModels, setChatModels] = React.useState<RouterChatModelMeta[]>([]);
  const [testing, setTesting] = React.useState(false);
  const [liveTestResult, setLiveTestResult] = React.useState<AdminModelRouterTestResponse | null>(null);
  const [liveTestError, setLiveTestError] = React.useState<string | null>(null);
  const lastValidPolicyRef = React.useRef<ModelRouterPolicy>(createDefaultModelRouterPolicy());

  const loadData = React.useCallback(async () => {
    setLoading(true);
    try {
      const token = await resolveAccessToken();
      if (!token) {
        toast.error(t("toast.sessionExpired"), { description: t("toast.signInAgain") });
        return;
      }
      const [grouped, referenceData] = await Promise.all([
        listAdminSettings(token),
        getAdminReferenceData(token).catch((): null => null),
      ]);
      const extracted = extractModelRouterSettings(grouped);
      const parsed = parseModelRouterPolicyJSON(extracted.policyJSON);
      if (parsed.policy) {
        lastValidPolicyRef.current = parsed.policy;
      }
      setEnabled(extracted.enabled);
      setSavedEnabled(extracted.enabled);
      setPolicyJSON(extracted.policyJSON);
      setSavedPolicyJSON(extracted.policyJSON);
      setConversationTaskModel(extracted.conversationTaskModel);
      setChatModels(extractRouterChatModels(referenceData?.models ?? []));
    } catch (error) {
      toast.error(t("toast.loadFailed"), { description: resolveAdminErrorMessage(error) });
    } finally {
      setLoading(false);
    }
  }, [t]);

  React.useEffect(() => {
    void loadData();
  }, [loadData]);

  const parsedResult = React.useMemo(() => parseModelRouterPolicyJSON(policyJSON), [policyJSON]);
  const policy = parsedResult.policy ?? lastValidPolicyRef.current;
  const parseError = parsedResult.error;

  React.useEffect(() => {
    if (parsedResult.policy) {
      lastValidPolicyRef.current = parsedResult.policy;
    }
  }, [parsedResult.policy]);

  const updatePolicy = React.useCallback(
    (updater: (current: ModelRouterPolicy) => ModelRouterPolicy) => {
      const base = lastValidPolicyRef.current;
      const next = updater(base);
      lastValidPolicyRef.current = next;
      setPolicyJSON(stringifyModelRouterPolicy(next));
    },
    [],
  );

  const updatePolicyJSON = React.useCallback((raw: string) => {
    setPolicyJSON(raw);
  }, []);

  const enabledDirty = enabled !== savedEnabled;
  const policyDirty = policyJSON !== savedPolicyJSON;
  const dirty = enabledDirty || policyDirty;

  const handleResetUnsaved = React.useCallback(() => {
    setEnabled(savedEnabled);
    setPolicyJSON(savedPolicyJSON);
    const parsed = parseModelRouterPolicyJSON(savedPolicyJSON);
    if (parsed.policy) {
      lastValidPolicyRef.current = parsed.policy;
    }
  }, [savedEnabled, savedPolicyJSON]);

  const handleSave = React.useCallback(async () => {
    if (!dirty) return;
    if (parseError) {
      toast.error(t("toast.invalidPolicy"), { description: parseError });
      return;
    }
    const items: PatchSettingItem[] = [];
    if (enabledDirty) {
      items.push({
        namespace: "chat",
        key: "model_router_enabled",
        value: enabled ? "true" : "false",
      });
    }
    if (policyDirty) {
      items.push({
        namespace: "chat",
        key: "model_router_policy",
        value: policyJSON,
      });
    }
    if (items.length === 0) return;
    setSaving(true);
    try {
      const token = await resolveAccessToken();
      if (!token) {
        toast.error(t("toast.sessionExpired"), { description: t("toast.signInAgain") });
        return;
      }
      const grouped = await patchAdminSettings(token, { items });
      const extracted = extractModelRouterSettings(grouped);
      setEnabled(extracted.enabled);
      setSavedEnabled(extracted.enabled);
      setPolicyJSON(extracted.policyJSON);
      setSavedPolicyJSON(extracted.policyJSON);
      overrideFeaturePolicy({ modelRouterEnabled: extracted.enabled });
      toast.success(t("toast.saved"));
    } catch (error) {
      toast.error(t("toast.saveFailed"), { description: resolveAdminErrorMessage(error) });
    } finally {
      setSaving(false);
    }
  }, [dirty, enabled, enabledDirty, parseError, policyDirty, policyJSON, t]);

  const runLiveTest = React.useCallback(
    async (request: Omit<AdminModelRouterTestRequest, "policyJSON">) => {
      if (parseError) {
        setLiveTestError(parseError);
        toast.error(t("toast.invalidPolicy"), { description: parseError });
        return null;
      }
      setTesting(true);
      setLiveTestError(null);
      try {
        const token = await resolveAccessToken();
        if (!token) {
          toast.error(t("toast.sessionExpired"), { description: t("toast.signInAgain") });
          return null;
        }
        const res = await testAdminModelRouter(token, {
          ...request,
          policyJSON,
        });
        setLiveTestResult(res);
        return res;
      } catch (error) {
        const msg = resolveAdminErrorMessage(error);
        setLiveTestError(msg);
        toast.error(t("toast.testFailed"), { description: msg });
        return null;
      } finally {
        setTesting(false);
      }
    },
    [parseError, policyJSON, t],
  );

  const classifierModelOptions = React.useMemo(
    () => buildRouterClassifierModelOptions(chatModels, t("classifier.followTaskModel")),
    [chatModels, t],
  );

  const diagnostics = React.useMemo(
    () => collectPolicyDiagnostics(policy, chatModels),
    [chatModels, policy],
  );

  const modelMetaByName = React.useMemo(
    () => new Map(chatModels.map((item) => [item.name, item])),
    [chatModels],
  );

  return {
    loading,
    saving,
    enabled,
    setEnabled,
    policy,
    policyJSON,
    parseError,
    conversationTaskModel,
    chatModels,
    modelMetaByName,
    classifierModelOptions,
    diagnostics,
    dirty,
    enabledDirty,
    policyDirty,
    updatePolicy,
    updatePolicyJSON,
    handleResetUnsaved,
    handleSave,
    reload: loadData,
    testing,
    liveTestResult,
    liveTestError,
    runLiveTest,
  };
}
