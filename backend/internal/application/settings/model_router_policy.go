package settings

import (
	"errors"
	"strings"

	"github.com/lieyanc/FireCortex/backend/internal/application/modelrouter"
)

// validateModelRouterPolicyJSON 校验 Auto 路由策略；param 携带具体原因供管理端展示。
func validateModelRouterPolicyJSON(value string, key string) error {
	if strings.TrimSpace(value) == "" {
		return settingRule("required", "")
	}
	if err := modelrouter.ValidatePolicyJSON(value); err != nil {
		detail := strings.TrimPrefix(err.Error(), modelrouter.ErrInvalidPolicy.Error()+": ")
		if !errors.Is(err, modelrouter.ErrInvalidPolicy) {
			detail = ""
		}
		return settingRule("model_router_policy", detail)
	}
	return nil
}
