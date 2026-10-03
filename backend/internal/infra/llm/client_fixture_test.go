package llm

import "github.com/lieyanc/FireCortex/backend/internal/shared/security"

func newTestClient() *Client {
	return NewClient(security.OutboundPolicy{})
}
