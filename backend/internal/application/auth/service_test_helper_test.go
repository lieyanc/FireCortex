package auth

import (
	"github.com/lieyanc/FireCortex/backend/internal/infra/config"
	"github.com/lieyanc/FireCortex/backend/internal/infra/identityprovider"
	"github.com/lieyanc/FireCortex/backend/internal/repository"
)

func newTestService(cfg config.Config, repo repository.AuthRepository, geoResolver GeoResolver) *Service {
	return NewServiceWithRuntime(
		config.NewRuntime(cfg),
		repo,
		geoResolver,
		identityprovider.New(cfg.StrictOutboundPolicy()),
	)
}
