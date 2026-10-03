package channel

import (
	"github.com/lieyanc/FireCortex/backend/internal/infra/config"
	"github.com/lieyanc/FireCortex/backend/internal/repository"
)

func newTestService(
	cfg config.Config,
	repo repository.ChannelRepository,
	presentationRepo repository.ModelPresentationRepository,
	cache repository.ChannelCacheRepository,
	llmClient llmGateway,
) *Service {
	return NewServiceWithRuntime(config.NewRuntime(cfg), repo, presentationRepo, cache, llmClient)
}
