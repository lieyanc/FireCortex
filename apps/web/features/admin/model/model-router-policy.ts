import type { AdminModelRouterTestResponse } from "@/features/admin/api";
import type { AdminLLMModelDTO } from "@/features/admin/api/llm-types";
import {
  isRoutableChatPlatformModel,
  type ModelSelectOption,
  resolveModelOptionIconUrl,
  resolveModelOptionLabel,
} from "@/entities/model";
import { isOneOf, isRecord } from "@/shared/lib/type-guards";

export const AUTO_MODEL_NAME = "auto";
export const MODEL_ROUTER_FOLLOW_TASK_MODEL = "__follow_task_model__";

export const MODEL_ROUTER_DIFFICULTIES = ["easy", "medium", "hard"] as const;
export type ModelRouterDifficulty = (typeof MODEL_ROUTER_DIFFICULTIES)[number];
export type ModelRouterRuleDifficulty = "" | ModelRouterDifficulty;
export const isModelRouterDifficulty = isOneOf(MODEL_ROUTER_DIFFICULTIES);

export const MODEL_ROUTER_LIMITS = {
  defaultTimeoutMs: 800,
  minTimeoutMs: 100,
  maxTimeoutMs: 10000,
  defaultHistoryMessages: 4,
  maxHistoryMessages: 20,
  maxPolicyBytes: 20000,
  maxCategoryKeyLength: 32,
  maxDescriptionLength: 300,
  maxModelNameLength: 128,
} as const;

export const MODEL_ROUTER_TIMEOUT_PRESETS = [400, 800, 1500, 3000] as const;
export const MODEL_ROUTER_HISTORY_PRESETS = [0, 2, 4, 8] as const;

export type ModelRouterCategory = {
  key: string;
  description: string;
};

export type ModelRouterRule = {
  category: string;
  difficulty: ModelRouterRuleDifficulty;
  models: string[];
};

export type ModelRouterPolicy = {
  routerModel: string;
  timeoutMs: number;
  historyMessages: number;
  defaultModels: string[];
  categories: ModelRouterCategory[];
  rules: ModelRouterRule[];
};

export type RouterChatModelMeta = {
  name: string;
  label: string;
  iconUrl: string | null;
  vendor: string;
  contextWindow: number;
  vision: boolean | null;
  tools: boolean | null;
  nativeToolTypes: string[];
  activeSourceCount: number;
  status: string;
};

export type ModelRouterCategoryPreset = {
  key: string;
  description: string;
};

export const EXTRA_CATEGORY_PRESETS: readonly ModelRouterCategoryPreset[] = [
  { key: "translation", description: "Translating text across languages while preserving tone and terminology" },
  { key: "creative", description: "Creative writing, brainstorming, storytelling and copywriting" },
  { key: "summarization", description: "Summarizing long documents, meetings or articles concisely" },
  { key: "agent", description: "Multi-step tool use, web research and workflow execution" },
  { key: "roleplay", description: "Persona-driven dialogue, coaching and interactive scenarios" },
];

export type ModelRouterPolicyPresetID = "balanced" | "cost_saver" | "coding_pro";

export function createDefaultModelRouterPolicy(): ModelRouterPolicy {
  return {
    routerModel: "",
    timeoutMs: MODEL_ROUTER_LIMITS.defaultTimeoutMs,
    historyMessages: MODEL_ROUTER_LIMITS.defaultHistoryMessages,
    defaultModels: [],
    categories: [
      { key: "chat", description: "Casual conversation, greetings and simple factual questions" },
      { key: "coding", description: "Writing, reviewing, explaining or debugging code" },
      { key: "math", description: "Math, logic puzzles and quantitative reasoning" },
      { key: "writing", description: "Long-form writing, editing, rewriting and translation" },
      { key: "analysis", description: "Research, planning, and analysis of documents, data or images" },
    ],
    rules: [],
  };
}

export function stringifyModelRouterPolicy(policy: ModelRouterPolicy): string {
  return JSON.stringify(
    {
      routerModel: policy.routerModel.trim(),
      timeoutMs: policy.timeoutMs,
      historyMessages: policy.historyMessages,
      defaultModels: policy.defaultModels,
      categories: policy.categories.map((item) => ({
        key: item.key,
        description: item.description,
      })),
      rules: policy.rules.map((item) => ({
        category: item.category,
        ...(item.difficulty ? { difficulty: item.difficulty } : {}),
        models: item.models,
      })),
    },
    null,
    2,
  );
}

const ALLOWED_TOP_LEVEL_KEYS = new Set([
  "routerModel",
  "timeoutMs",
  "historyMessages",
  "defaultModels",
  "categories",
  "rules",
]);

const ALLOWED_CATEGORY_KEYS = new Set(["key", "description"]);
const ALLOWED_RULE_KEYS = new Set(["category", "difficulty", "models"]);
const CATEGORY_KEY_RE = /^[a-z0-9_-]+$/;

export type ParsedModelRouterPolicyResult = {
  policy: ModelRouterPolicy | null;
  error: string | null;
};

function normalizeStringList(
  value: unknown,
  fieldLabel: string,
): { items: string[]; error: string | null } {
  if (value === undefined || value === null) {
    return { items: [], error: null };
  }
  if (!Array.isArray(value)) {
    return { items: [], error: `${fieldLabel} must be an array of strings` };
  }
  const seen = new Set<string>();
  const items: string[] = [];
  for (const raw of value) {
    if (typeof raw !== "string") {
      return { items: [], error: `${fieldLabel} must contain only strings` };
    }
    const name = raw.trim();
    if (!name) continue;
    if (name.length > MODEL_ROUTER_LIMITS.maxModelNameLength) {
      return { items: [], error: `${fieldLabel} contains a model name that is too long` };
    }
    if (name.toLowerCase() === AUTO_MODEL_NAME) {
      return { items: [], error: `${fieldLabel} cannot reference "auto"` };
    }
    if (seen.has(name)) continue;
    seen.add(name);
    items.push(name);
  }
  return { items, error: null };
}

/**
 * Mirrors backend `modelrouter.ParsePolicy` validation so admins get instant
 * feedback before saving.
 */
export function parseModelRouterPolicyJSON(raw: string): ParsedModelRouterPolicyResult {
  const trimmed = raw.trim();
  if (!trimmed) {
    return { policy: createDefaultModelRouterPolicy(), error: null };
  }
  if (new TextEncoder().encode(trimmed).length > MODEL_ROUTER_LIMITS.maxPolicyBytes) {
    return { policy: null, error: `Policy exceeds ${MODEL_ROUTER_LIMITS.maxPolicyBytes} bytes` };
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(trimmed);
  } catch (err) {
    return { policy: null, error: err instanceof Error ? err.message : "Invalid JSON" };
  }

  if (!isRecord(parsed)) {
    return { policy: null, error: "Policy must be a JSON object" };
  }
  for (const key of Object.keys(parsed)) {
    if (!ALLOWED_TOP_LEVEL_KEYS.has(key)) {
      return { policy: null, error: `Unknown field "${key}"` };
    }
  }

  const routerModelRaw = parsed.routerModel ?? "";
  if (typeof routerModelRaw !== "string") {
    return { policy: null, error: "routerModel must be a string" };
  }
  const routerModel = routerModelRaw.trim();
  if (routerModel.length > MODEL_ROUTER_LIMITS.maxModelNameLength) {
    return { policy: null, error: "routerModel is too long" };
  }

  const timeoutRaw = parsed.timeoutMs ?? 0;
  if (typeof timeoutRaw !== "number" || !Number.isInteger(timeoutRaw)) {
    return { policy: null, error: "timeoutMs must be an integer" };
  }
  const timeoutMs = timeoutRaw === 0 ? MODEL_ROUTER_LIMITS.defaultTimeoutMs : timeoutRaw;
  if (timeoutMs < MODEL_ROUTER_LIMITS.minTimeoutMs || timeoutMs > MODEL_ROUTER_LIMITS.maxTimeoutMs) {
    return {
      policy: null,
      error: `timeoutMs must be between ${MODEL_ROUTER_LIMITS.minTimeoutMs} and ${MODEL_ROUTER_LIMITS.maxTimeoutMs}`,
    };
  }

  const historyRaw = parsed.historyMessages ?? 0;
  if (typeof historyRaw !== "number" || !Number.isInteger(historyRaw)) {
    return { policy: null, error: "historyMessages must be an integer" };
  }
  if (historyRaw < 0 || historyRaw > MODEL_ROUTER_LIMITS.maxHistoryMessages) {
    return {
      policy: null,
      error: `historyMessages must be between 0 and ${MODEL_ROUTER_LIMITS.maxHistoryMessages}`,
    };
  }

  const defaultModelsRes = normalizeStringList(parsed.defaultModels, "defaultModels");
  if (defaultModelsRes.error) {
    return { policy: null, error: defaultModelsRes.error };
  }

  if (!Array.isArray(parsed.categories) || parsed.categories.length === 0) {
    return { policy: null, error: "At least one category is required" };
  }
  const categories: ModelRouterCategory[] = [];
  const categoryKeys = new Set<string>();
  for (const entry of parsed.categories) {
    if (!isRecord(entry)) {
      return { policy: null, error: "Each category must be an object" };
    }
    for (const k of Object.keys(entry)) {
      if (!ALLOWED_CATEGORY_KEYS.has(k)) {
        return { policy: null, error: `Unknown category field "${k}"` };
      }
    }
    const key = typeof entry.key === "string" ? entry.key.trim().toLowerCase() : "";
    const description = typeof entry.description === "string" ? entry.description.trim() : "";
    if (!key || key.length > MODEL_ROUTER_LIMITS.maxCategoryKeyLength || !CATEGORY_KEY_RE.test(key)) {
      return {
        policy: null,
        error: `Category key "${key}" must be 1-${MODEL_ROUTER_LIMITS.maxCategoryKeyLength} chars of a-z, 0-9, _ or -`,
      };
    }
    if ([...description].length > MODEL_ROUTER_LIMITS.maxDescriptionLength) {
      return { policy: null, error: `Description of category "${key}" is too long` };
    }
    if (categoryKeys.has(key)) {
      return { policy: null, error: `Duplicate category "${key}"` };
    }
    categoryKeys.add(key);
    categories.push({ key, description });
  }

  const rulesRaw = parsed.rules ?? [];
  if (!Array.isArray(rulesRaw)) {
    return { policy: null, error: "rules must be an array" };
  }
  const rules: ModelRouterRule[] = [];
  for (let i = 0; i < rulesRaw.length; i += 1) {
    const entry = rulesRaw[i];
    if (!isRecord(entry)) {
      return { policy: null, error: `Rule ${i + 1} must be an object` };
    }
    for (const k of Object.keys(entry)) {
      if (!ALLOWED_RULE_KEYS.has(k)) {
        return { policy: null, error: `Unknown rule field "${k}"` };
      }
    }
    const category = typeof entry.category === "string" ? entry.category.trim().toLowerCase() : "";
    const diffRaw = typeof entry.difficulty === "string" ? entry.difficulty.trim().toLowerCase() : "";
    if (!categoryKeys.has(category)) {
      return { policy: null, error: `Rule ${i + 1} references unknown category "${category}"` };
    }
    if (diffRaw !== "" && !isModelRouterDifficulty(diffRaw)) {
      return { policy: null, error: `Rule ${i + 1} difficulty must be easy, medium, hard or empty` };
    }
    const modelsRes = normalizeStringList(entry.models, `rule ${i + 1} models`);
    if (modelsRes.error) {
      return { policy: null, error: modelsRes.error };
    }
    if (modelsRes.items.length === 0) {
      return { policy: null, error: `Rule ${i + 1} needs at least one model` };
    }
    rules.push({
      category,
      difficulty: diffRaw === "" ? "" : diffRaw,
      models: modelsRes.items,
    });
  }

  return {
    policy: {
      routerModel,
      timeoutMs,
      historyMessages: historyRaw,
      defaultModels: defaultModelsRes.items,
      categories,
      rules,
    },
    error: null,
  };
}

export function normalizeNativeToolFamily(value: string): string {
  let family = value.trim().toLowerCase();
  const dotIndex = family.lastIndexOf(".");
  if (dotIndex >= 0) {
    family = family.slice(dotIndex + 1);
  }
  if (family.endsWith("_preview")) {
    family = family.slice(0, -"_preview".length);
  }
  const underscoreIndex = family.lastIndexOf("_");
  if (underscoreIndex >= 0 && /^\d{8}$/.test(family.slice(underscoreIndex + 1))) {
    family = family.slice(0, underscoreIndex);
  }
  return family;
}

function parseCandidateCapabilities(capabilitiesJSON: string): {
  vision: boolean | null;
  tools: boolean | null;
  nativeToolTypes: string[];
} {
  const raw = capabilitiesJSON.trim();
  if (!raw) {
    return { vision: null, tools: null, nativeToolTypes: [] };
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return { vision: null, tools: null, nativeToolTypes: [] };
  }
  if (!isRecord(parsed)) {
    return { vision: null, tools: null, nativeToolTypes: [] };
  }
  let vision: boolean | null = null;
  let tools: boolean | null = null;
  if (isRecord(parsed.routing)) {
    if (typeof parsed.routing.vision === "boolean") {
      vision = parsed.routing.vision;
    }
    if (typeof parsed.routing.tools === "boolean") {
      tools = parsed.routing.tools;
    }
  }
  const nativeToolTypes: string[] = [];
  const pushUnique = (val: unknown) => {
    if (typeof val !== "string") return;
    const trimmed = val.trim();
    if (!trimmed || nativeToolTypes.includes(trimmed)) return;
    nativeToolTypes.push(trimmed);
  };
  if (Array.isArray(parsed.nativeTools)) {
    for (const tool of parsed.nativeTools) {
      if (!isRecord(tool)) continue;
      if (tool.enabled === false) continue;
      pushUnique(tool.type);
      if (isRecord(tool.payload)) {
        pushUnique(tool.payload.type);
      }
      pushUnique(tool.key);
      pushUnique(tool.toolKey);
    }
  }
  if (Array.isArray(parsed.nativeToolKeys)) {
    for (const key of parsed.nativeToolKeys) {
      pushUnique(key);
    }
  }
  return { vision, tools, nativeToolTypes };
}

export function extractRouterChatModels(models: AdminLLMModelDTO[]): RouterChatModelMeta[] {
  const seen = new Set<string>();
  const results: RouterChatModelMeta[] = [];
  const sorted = [...models].sort((a, b) => (a.sortOrder ?? 0) - (b.sortOrder ?? 0));
  for (const item of sorted) {
    if (!isRoutableChatPlatformModel(item)) continue;
    const name = item.platformModelName.trim();
    if (!name || name.toLowerCase() === AUTO_MODEL_NAME || seen.has(name)) continue;
    seen.add(name);
    const caps = parseCandidateCapabilities(item.capabilitiesJSON ?? "");
    results.push({
      name,
      label: resolveModelOptionLabel(name),
      iconUrl: resolveModelOptionIconUrl({
        platformModelName: name,
        vendor: item.vendor ?? "",
        icon: item.icon ?? "",
      }),
      vendor: item.vendor ?? "",
      contextWindow: item.contextWindow ?? 0,
      vision: caps.vision,
      tools: caps.tools,
      nativeToolTypes: caps.nativeToolTypes,
      activeSourceCount: item.activeSourceCount ?? 0,
      status: item.status ?? "",
    });
  }
  return results;
}

export function buildRouterClassifierModelOptions(
  models: RouterChatModelMeta[],
  followLabel: string,
): ModelSelectOption[] {
  return [
    { label: followLabel, value: MODEL_ROUTER_FOLLOW_TASK_MODEL, iconUrl: null },
    ...models.map((item) => ({
      label: item.label,
      value: item.name,
      iconUrl: item.iconUrl,
    })),
  ];
}

export function formatContextWindowLabel(tokens: number): string {
  if (!Number.isFinite(tokens) || tokens <= 0) return "—";
  if (tokens >= 1_000_000) {
    const millions = tokens / 1_000_000;
    return `${Number.isInteger(millions) ? millions : millions.toFixed(1)}M`;
  }
  if (tokens >= 1000) {
    return `${Math.round(tokens / 1000)}K`;
  }
  return String(tokens);
}

export type MatrixCellOrigin = "direct" | "inherit-any" | "fallback-default" | "fallback-catalog";

export type MatrixCellSummary = {
  origin: MatrixCellOrigin;
  models: string[];
  directRuleCount: number;
};

export function resolveMatrixCell(
  policy: ModelRouterPolicy,
  category: string,
  difficulty: ModelRouterRuleDifficulty,
  catalogFallbackModel: string | null,
): MatrixCellSummary {
  const directRules = policy.rules.filter(
    (rule) => rule.category === category && rule.difficulty === difficulty,
  );
  if (directRules.length > 0) {
    const models = Array.from(new Set(directRules.flatMap((rule) => rule.models)));
    return { origin: "direct", models, directRuleCount: directRules.length };
  }
  if (difficulty !== "") {
    const anyRules = policy.rules.filter(
      (rule) => rule.category === category && rule.difficulty === "",
    );
    if (anyRules.length > 0) {
      const models = Array.from(new Set(anyRules.flatMap((rule) => rule.models)));
      return { origin: "inherit-any", models, directRuleCount: 0 };
    }
  }
  if (policy.defaultModels.length > 0) {
    return { origin: "fallback-default", models: policy.defaultModels, directRuleCount: 0 };
  }
  return {
    origin: "fallback-catalog",
    models: catalogFallbackModel ? [catalogFallbackModel] : [],
    directRuleCount: 0,
  };
}

export function setMatrixCellModels(
  policy: ModelRouterPolicy,
  category: string,
  difficulty: ModelRouterRuleDifficulty,
  models: string[],
): ModelRouterPolicy {
  const cleaned = Array.from(
    new Set(
      models
        .map((item) => item.trim())
        .filter((item) => item.length > 0 && item.toLowerCase() !== AUTO_MODEL_NAME),
    ),
  );
  const firstMatchIndex = policy.rules.findIndex(
    (rule) => rule.category === category && rule.difficulty === difficulty,
  );
  const remaining = policy.rules.filter(
    (rule) => !(rule.category === category && rule.difficulty === difficulty),
  );
  if (cleaned.length === 0) {
    return { ...policy, rules: remaining };
  }
  const nextRule: ModelRouterRule = { category, difficulty, models: cleaned };
  if (firstMatchIndex >= 0) {
    const nextRules = [...remaining];
    nextRules.splice(Math.min(firstMatchIndex, nextRules.length), 0, nextRule);
    return { ...policy, rules: nextRules };
  }
  return { ...policy, rules: [...remaining, nextRule] };
}

export type ModelRouterDiagnostics = {
  referencedModels: string[];
  unavailableModels: string[];
  routerModelUnavailable: boolean;
  uncoveredCategories: string[];
  coveredCategoryCount: number;
  skipsClassifier: boolean;
};

export function collectPolicyDiagnostics(
  policy: ModelRouterPolicy,
  activeModels: RouterChatModelMeta[],
): ModelRouterDiagnostics {
  const activeSet = new Set(activeModels.map((item) => item.name));
  const referenced = new Set<string>();
  for (const rule of policy.rules) {
    for (const model of rule.models) {
      referenced.add(model);
    }
  }
  for (const model of policy.defaultModels) {
    referenced.add(model);
  }
  const referencedModels = [...referenced];
  const unavailableModels = referencedModels.filter((name) => !activeSet.has(name));
  const routerModelUnavailable =
    policy.routerModel.trim() !== "" && !activeSet.has(policy.routerModel.trim());

  const coveredCategories = new Set(policy.rules.map((rule) => rule.category));
  const uncoveredCategories = policy.categories
    .map((item) => item.key)
    .filter((key) => !coveredCategories.has(key));

  return {
    referencedModels,
    unavailableModels,
    routerModelUnavailable,
    uncoveredCategories,
    coveredCategoryCount: policy.categories.length - uncoveredCategories.length,
    skipsClassifier: policy.rules.length === 0 || activeModels.length <= 1,
  };
}

export function pruneUnavailableModelsFromPolicy(
  policy: ModelRouterPolicy,
  activeModels: RouterChatModelMeta[],
): ModelRouterPolicy {
  const activeSet = new Set(activeModels.map((item) => item.name));
  const nextRouterModel =
    policy.routerModel && activeSet.has(policy.routerModel) ? policy.routerModel : "";
  const nextDefaultModels = policy.defaultModels.filter((name) => activeSet.has(name));
  const nextRules = policy.rules
    .map((rule) => ({
      ...rule,
      models: rule.models.filter((name) => activeSet.has(name)),
    }))
    .filter((rule) => rule.models.length > 0);
  return {
    ...policy,
    routerModel: nextRouterModel,
    defaultModels: nextDefaultModels,
    rules: nextRules,
  };
}

function pickHeuristicModels(models: RouterChatModelMeta[]): {
  fast: string | null;
  general: string | null;
  strong: string | null;
} {
  if (models.length === 0) {
    return { fast: null, general: null, strong: null };
  }
  const names = models.map((item) => item.name);
  const fast =
    names.find((n) => /mini|flash|haiku|nano|lite|small|fast/i.test(n)) ?? names[0] ?? null;
  const strong =
    names.find((n) => /opus|pro|sonnet|reasoning|o3|o1|r1|max|ultra|codex|5\.|4\.5/i.test(n) && n !== fast) ??
    names[names.length - 1] ??
    fast;
  const general =
    names.find((n) => n !== fast && n !== strong) ?? strong ?? fast;
  return { fast, general, strong };
}

function uniqueNonEmpty(items: Array<string | null | undefined>): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const item of items) {
    const v = item?.trim();
    if (!v || seen.has(v)) continue;
    seen.add(v);
    out.push(v);
  }
  return out;
}

export function buildSmartPolicyPreset(
  preset: ModelRouterPolicyPresetID,
  activeModels: RouterChatModelMeta[],
): ModelRouterPolicy {
  const base = createDefaultModelRouterPolicy();
  const { fast, general, strong } = pickHeuristicModels(activeModels);
  if (!fast) {
    return base;
  }

  const fastChain = uniqueNonEmpty([fast, general, strong]);
  const balancedChain = uniqueNonEmpty([general, strong, fast]);
  const strongChain = uniqueNonEmpty([strong, general, fast]);

  if (preset === "cost_saver") {
    return {
      ...base,
      routerModel: fast,
      timeoutMs: 700,
      historyMessages: 3,
      defaultModels: fastChain,
      rules: [
        { category: "chat", difficulty: "", models: fastChain },
        { category: "writing", difficulty: "easy", models: fastChain },
        { category: "writing", difficulty: "hard", models: balancedChain },
        { category: "coding", difficulty: "easy", models: fastChain },
        { category: "coding", difficulty: "hard", models: strongChain },
        { category: "math", difficulty: "hard", models: strongChain },
        { category: "analysis", difficulty: "hard", models: strongChain },
      ],
    };
  }

  if (preset === "coding_pro") {
    return {
      ...base,
      routerModel: fast,
      timeoutMs: 900,
      historyMessages: 6,
      defaultModels: balancedChain,
      rules: [
        { category: "chat", difficulty: "easy", models: fastChain },
        { category: "chat", difficulty: "", models: balancedChain },
        { category: "coding", difficulty: "", models: strongChain },
        { category: "math", difficulty: "", models: strongChain },
        { category: "analysis", difficulty: "hard", models: strongChain },
        { category: "writing", difficulty: "", models: balancedChain },
      ],
    };
  }

  // balanced
  return {
    ...base,
    routerModel: fast,
    timeoutMs: 800,
    historyMessages: 4,
    defaultModels: balancedChain,
    rules: [
      { category: "chat", difficulty: "easy", models: fastChain },
      { category: "chat", difficulty: "", models: balancedChain },
      { category: "coding", difficulty: "easy", models: balancedChain },
      { category: "coding", difficulty: "hard", models: strongChain },
      { category: "coding", difficulty: "", models: balancedChain },
      { category: "math", difficulty: "hard", models: strongChain },
      { category: "math", difficulty: "", models: balancedChain },
      { category: "writing", difficulty: "easy", models: fastChain },
      { category: "writing", difficulty: "", models: balancedChain },
      { category: "analysis", difficulty: "hard", models: strongChain },
      { category: "analysis", difficulty: "", models: balancedChain },
    ],
  };
}

export type LocalSimulationScenario = {
  message: string;
  previousModel: string;
  needsVision: boolean;
  needsTools: boolean;
  nativeToolTypes: string[];
  estimatedTokens: number;
  simulateFailure: boolean;
  category: string;
  difficulty: ModelRouterDifficulty;
  switchModel: boolean;
  reason: string;
};

/**
 * Mirrors backend `TestAutoModelRoute` in dry-run mode so admins see
 * instantaneous feedback while toggling constraints, categories or rules.
 */
export function simulateModelRouterLocally(
  policy: ModelRouterPolicy,
  activeModels: RouterChatModelMeta[],
  scenario: LocalSimulationScenario,
): AdminModelRouterTestResponse | null {
  if (activeModels.length === 0) {
    return null;
  }
  const estimatedTokens =
    scenario.estimatedTokens > 0
      ? scenario.estimatedTokens
      : Math.max(1, Math.ceil(scenario.message.trim().length / 3));
  const nativeToolTypes = scenario.nativeToolTypes
    .map((item) => item.trim())
    .filter((item) => item.length > 0);

  const candidates = activeModels.map((item) => {
    const failedReasons: string[] = [];
    if (scenario.needsVision && item.vision === false) {
      failedReasons.push("missing_vision");
    }
    if (scenario.needsTools && item.tools === false) {
      failedReasons.push("missing_tools");
    }
    const supportedFamilies = new Set(item.nativeToolTypes.map(normalizeNativeToolFamily));
    for (const toolType of nativeToolTypes) {
      const family = normalizeNativeToolFamily(toolType);
      if (family && !supportedFamilies.has(family)) {
        failedReasons.push(`missing_native_tool:${family}`);
      }
    }
    if (estimatedTokens > 0 && item.contextWindow > 0 && estimatedTokens > item.contextWindow) {
      failedReasons.push("context_exceeded");
    }
    return {
      name: item.name,
      contextWindow: item.contextWindow,
      vision: item.vision,
      tools: item.tools,
      nativeToolTypes: item.nativeToolTypes,
      passed: failedReasons.length === 0,
      failedReasons,
    };
  });

  const passedCandidates = candidates.filter((item) => item.passed);
  const constraintsRelaxed = passedCandidates.length === 0;
  const effective = constraintsRelaxed ? candidates : passedCandidates;
  const availableSet = new Set(effective.map((item) => item.name));

  const pick = (models: string[]) => models.find((name) => availableSet.has(name)) ?? "";
  const previous = pick(scenario.previousModel.trim() ? [scenario.previousModel.trim()] : []);

  const classification =
    scenario.simulateFailure || !scenario.category
      ? null
      : {
          category: scenario.category,
          difficulty: scenario.difficulty,
          switch: scenario.switchModel,
          reason: scenario.reason.trim(),
        };

  const matchedRules: AdminModelRouterTestResponse["matchedRules"] = [];
  if (classification) {
    const exact: AdminModelRouterTestResponse["matchedRules"] = [];
    const anyDiff: AdminModelRouterTestResponse["matchedRules"] = [];
    policy.rules.forEach((rule, index) => {
      if (rule.category !== classification.category) return;
      if (rule.difficulty === classification.difficulty) {
        exact.push({ index, category: rule.category, difficulty: rule.difficulty, models: rule.models });
      } else if (rule.difficulty === "") {
        anyDiff.push({ index, category: rule.category, difficulty: rule.difficulty, models: rule.models });
      }
    });
    matchedRules.push(...exact, ...anyDiff);
  }

  let decision: AdminModelRouterTestResponse["decision"];
  if (!classification) {
    if (previous) {
      decision = { model: previous, source: "fallback" };
    } else {
      const def = pick(policy.defaultModels);
      decision = { model: def || effective[0].name, source: "fallback" };
    }
  } else {
    const baseDecision = {
      category: classification.category,
      difficulty: classification.difficulty,
      reason: classification.reason || undefined,
    };
    if (previous && !classification.switch) {
      decision = { ...baseDecision, model: previous, source: "sticky" };
    } else {
      const ruleHit = pick(matchedRules.flatMap((rule) => rule.models));
      if (ruleHit) {
        decision = { ...baseDecision, model: ruleHit, source: "rule" };
      } else {
        const defHit = pick(policy.defaultModels);
        if (defHit) {
          decision = { ...baseDecision, model: defHit, source: "default" };
        } else if (previous) {
          decision = { ...baseDecision, model: previous, source: "sticky" };
        } else {
          decision = { ...baseDecision, model: effective[0].name, source: "default" };
        }
      }
    }
  }

  return {
    requirements: {
      needsVision: scenario.needsVision,
      needsTools: scenario.needsTools,
      nativeToolTypes,
      estimatedTokens,
    },
    candidates,
    constraintsRelaxed,
    classifier: {
      attempted: false,
      skippedReason: "dry_run",
      latencyMS: 0,
      classification: classification ?? undefined,
    },
    matchedRules,
    decision,
  };
}
