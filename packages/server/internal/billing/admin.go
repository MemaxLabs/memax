package billing

import (
	"context"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// AdminProvider is the no-op provider used when no external payment system
// is configured. All plan management happens through admin actions or the
// invite flow — no customer/subscription lifecycle on an external platform.
//
// This satisfies the Provider interface so the Service can call provider
// methods unconditionally without nil checks at every call site.
type AdminProvider struct{}

func (p *AdminProvider) CreateCustomer(_ context.Context, _ *model.User) (string, error) {
	return "", nil
}

func (p *AdminProvider) CreateSubscription(_ context.Context, _, _ string) (*Subscription, error) {
	return nil, nil
}

func (p *AdminProvider) CancelSubscription(_ context.Context, _ string) error {
	return nil
}

func (p *AdminProvider) ChangeSubscription(_ context.Context, _, _ string) (*Subscription, error) {
	return nil, nil
}

func (p *AdminProvider) HandleWebhook(_ context.Context, _ []byte, _ string) (*WebhookEvent, error) {
	return nil, nil
}
