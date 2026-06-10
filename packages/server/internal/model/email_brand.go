package model

import "time"

// EmailBrandSettings is the singleton row storing layout + brand values
// that wrap every outbound email: logo header, footer chrome, and color
// tokens surfaced to templates. See migrations 007 (initial) and 019
// (structured compliance footer).
//
// Footer composition (see base.html, base.txt):
//   - FooterHTML / FooterText — optional admin-authored "notes" slot
//     (tagline, soft reassurance copy). Rendered above the structured
//     block. Kept as template.HTML — trusted admin input.
//   - SupportEmail — rendered as a "Contact" line when set.
//   - CompanyName + CompanyAddress — CAN-SPAM §7704(a)(5) sender
//     identity; required to satisfy commercial-email compliance.
//   - PrivacyURL + TermsURL — legal links.
//   - Unsubscribe — marketing-only, injected by the campaign worker
//     per recipient; not admin-configurable.
type EmailBrandSettings struct {
	LogoURL         string    `json:"logo_url"`
	LogoAlt         string    `json:"logo_alt"`
	ProductName     string    `json:"product_name"`
	FooterHTML      string    `json:"footer_html"`
	FooterText      string    `json:"footer_text"`
	PrimaryColor    string    `json:"primary_color"`
	BackgroundColor string    `json:"background_color"`
	SurfaceColor    string    `json:"surface_color"`
	BorderColor     string    `json:"border_color"`
	MutedColor      string    `json:"muted_color"`
	BodyColor       string    `json:"body_color"`
	SupportEmail    string    `json:"support_email"`
	CompanyName     string    `json:"company_name"`
	CompanyAddress  string    `json:"company_address"`
	PrivacyURL      string    `json:"privacy_url"`
	TermsURL        string    `json:"terms_url"`
	UpdatedAt       time.Time `json:"updated_at"`
	UpdatedBy       *string   `json:"updated_by,omitempty"`
}

// EmailBrandSettingsPatch is the partial-update payload sent by admins.
// Nil fields are left untouched; empty strings are valid writes (clear).
type EmailBrandSettingsPatch struct {
	LogoURL         *string `json:"logo_url,omitempty"`
	LogoAlt         *string `json:"logo_alt,omitempty"`
	ProductName     *string `json:"product_name,omitempty"`
	FooterHTML      *string `json:"footer_html,omitempty"`
	FooterText      *string `json:"footer_text,omitempty"`
	PrimaryColor    *string `json:"primary_color,omitempty"`
	BackgroundColor *string `json:"background_color,omitempty"`
	SurfaceColor    *string `json:"surface_color,omitempty"`
	BorderColor     *string `json:"border_color,omitempty"`
	MutedColor      *string `json:"muted_color,omitempty"`
	BodyColor       *string `json:"body_color,omitempty"`
	SupportEmail    *string `json:"support_email,omitempty"`
	CompanyName     *string `json:"company_name,omitempty"`
	CompanyAddress  *string `json:"company_address,omitempty"`
	PrivacyURL      *string `json:"privacy_url,omitempty"`
	TermsURL        *string `json:"terms_url,omitempty"`
}
