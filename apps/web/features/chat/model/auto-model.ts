/** 虚拟模型名：选中后由后端用快速模型分类，再路由到具体模型。 */
export const AUTO_MODEL_NAME = "auto";

export function isAutoModelName(platformModelName: string | null | undefined): boolean {
  return (platformModelName ?? "").trim().toLowerCase() === AUTO_MODEL_NAME;
}
