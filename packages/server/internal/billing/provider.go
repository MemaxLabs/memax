// Package billing provides a payment provider abstraction for plan management.
//
// The billing system has a single mutation path for all plan changes:
// Service.ChangePlan(). This ensures subscription rows, audit events, and
// cache invalidation stay consistent regardless of the trigger source
// (admin action, waitlist invite, webhook, or self-service upgrade).
//
// When no external payment provider is configured (provider == nil),
// the system operates in "admin-managed" mode — plans are assigned
// manually by admins or automatically by the invite flow.
package billing

import (
	"context"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// Provider abstracts an external payment/subscription system (Stripe, Polar, etc.).
// Implementations handle customer lifecycle and subscription management on the
// provider side. The billing Service coordinates between the provider and our
// internal plan/usage state.
//
// When provider is nil, the Service operates in admin-managed mode and skips
// all provider calls.
type Provider interface {
	// CreateCustomer registers a user with the payment provider.
	// Returns the provider's customer ID for future subscription calls.
	CreateCustomer(ctx context.Context, user *model.User) (customerID string, err error)

	// CreateSubscription starts a new subscription for a customer on the given plan.
	CreateSubscription(ctx context.Context, customerID string, planID string) (*Subscription, error)

	// CancelSubscription cancels an active subscription. The subscription
	// typically remains active until the end of the current billing period.
	CancelSubscription(ctx context.Context, subscriptionID string) error

	// ChangeSubscription upgrades or downgrades an existing subscription.
	// The provider handles prorating.
	ChangeSubscription(ctx context.Context, subscriptionID string, newPlanID string) (*Subscription, error)

	// HandleWebhook verifies and parses a webhook payload from the provider.
	// Returns the parsed event, or an error if verification fails.
	HandleWebhook(ctx context.Context, payload []byte, signature string) (*WebhookEvent, error)
}

// Subscription represents a subscription record from the payment provider.
type Subscription struct {
	ID               string
	CustomerID       string
	PlanID           string
	Status           string // "active", "past_due", "cancelled", "trialing"
	CurrentPeriodEnd time.Time
}

// WebhookEvent represents a parsed event from a payment provider webhook.
type WebhookEvent struct {
	Type           string // "subscription.created", "subscription.updated", "subscription.cancelled", "payment.failed"
	SubscriptionID string
	CustomerID     string
	PlanID         string
	Status         string
}
