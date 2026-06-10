package email

import (
	"context"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// BrandProvider returns the current email brand/layout settings. The
// TemplateManager depends on this narrow interface (not the full Store)
// so callers can inject a static provider in tests.
type BrandProvider interface {
	GetEmailBrandSettings(ctx context.Context) (*model.EmailBrandSettings, error)
}

// StaticBrandProvider returns a fixed brand settings snapshot. Useful
// for tests and for local dev when the DB row might be missing.
type StaticBrandProvider struct {
	Settings *model.EmailBrandSettings
}

func (p *StaticBrandProvider) GetEmailBrandSettings(_ context.Context) (*model.EmailBrandSettings, error) {
	if p == nil || p.Settings == nil {
		return defaultBrand(), nil
	}
	settings := *p.Settings
	return &settings, nil
}

// defaultBrand mirrors the migration defaults. Used as a last-resort
// fallback when no provider is wired and when the provider returns nil.
func defaultBrand() *model.EmailBrandSettings {
	return &model.EmailBrandSettings{
		LogoAlt:         "memax",
		ProductName:     "memax",
		FooterHTML:      "<p>memax — memory for your AI agents</p>",
		FooterText:      "memax — memory for your AI agents",
		PrimaryColor:    "#111111",
		BackgroundColor: "#fafafa",
		SurfaceColor:    "#ffffff",
		BorderColor:     "#e5e5e5",
		MutedColor:      "#888888",
		BodyColor:       "#444444",
	}
}
