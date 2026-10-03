package channel

import "github.com/lieyanc/FireCortex/backend/internal/repository"

var (
	ErrUpstreamNotFound           = repository.ErrUpstreamNotFound
	ErrModelNotFound              = repository.ErrModelNotFound
	ErrDuplicatePlatformModelName = repository.ErrDuplicatePlatformModelName
	ErrUpstreamModelNotFound      = repository.ErrUpstreamModelNotFound
	ErrUpstreamModelConflict      = repository.ErrUpstreamModelConflict
	ErrLLMSettingNotFound         = repository.ErrLLMSettingNotFound
)
