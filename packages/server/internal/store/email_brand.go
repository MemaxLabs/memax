package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// brandSingletonID is the fixed primary key for the singleton row.
// Enforced at the schema level via a CHECK constraint.
const brandSingletonID = "singleton"

func (s *PostgresStore) GetEmailBrandSettings(ctx context.Context) (*model.EmailBrandSettings, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT logo_url, logo_alt, product_name, footer_html, footer_text,
		        primary_color, background_color, surface_color, border_color,
		        muted_color, body_color, support_email,
		        company_name, company_address, privacy_url, terms_url,
		        updated_at, updated_by::text
		 FROM email_brand_settings
		 WHERE id = $1`, brandSingletonID)

	settings := &model.EmailBrandSettings{}
	// updated_by is nullable (freshly-seeded row has never been edited);
	// scan into sql.NullString so a NULL doesn't 500 the whole endpoint.
	var updatedBy sql.NullString
	if err := row.Scan(
		&settings.LogoURL,
		&settings.LogoAlt,
		&settings.ProductName,
		&settings.FooterHTML,
		&settings.FooterText,
		&settings.PrimaryColor,
		&settings.BackgroundColor,
		&settings.SurfaceColor,
		&settings.BorderColor,
		&settings.MutedColor,
		&settings.BodyColor,
		&settings.SupportEmail,
		&settings.CompanyName,
		&settings.CompanyAddress,
		&settings.PrivacyURL,
		&settings.TermsURL,
		&settings.UpdatedAt,
		&updatedBy,
	); err != nil {
		if err == pgx.ErrNoRows {
			// Migration 007 seeds the row; this is a safety net only.
			return defaultBrandSettings(), nil
		}
		return nil, fmt.Errorf("get email brand settings: %w", err)
	}
	if updatedBy.Valid && updatedBy.String != "" {
		value := updatedBy.String
		settings.UpdatedBy = &value
	}
	return settings, nil
}

// UpdateEmailBrandSettings applies a partial patch. Nil fields preserve
// the existing value; non-nil (including empty string) overwrite.
func (s *PostgresStore) UpdateEmailBrandSettings(ctx context.Context, patch model.EmailBrandSettingsPatch, updatedBy *string) (*model.EmailBrandSettings, error) {
	_, err := s.pool.Exec(ctx,
		`UPDATE email_brand_settings SET
			logo_url = COALESCE($2, logo_url),
			logo_alt = COALESCE($3, logo_alt),
			product_name = COALESCE($4, product_name),
			footer_html = COALESCE($5, footer_html),
			footer_text = COALESCE($6, footer_text),
			primary_color = COALESCE($7, primary_color),
			background_color = COALESCE($8, background_color),
			surface_color = COALESCE($9, surface_color),
			border_color = COALESCE($10, border_color),
			muted_color = COALESCE($11, muted_color),
			body_color = COALESCE($12, body_color),
			support_email = COALESCE($13, support_email),
			company_name = COALESCE($14, company_name),
			company_address = COALESCE($15, company_address),
			privacy_url = COALESCE($16, privacy_url),
			terms_url = COALESCE($17, terms_url),
			updated_by = NULLIF($18, '')::uuid,
			updated_at = now()
		 WHERE id = $1`,
		brandSingletonID,
		patch.LogoURL,
		patch.LogoAlt,
		patch.ProductName,
		patch.FooterHTML,
		patch.FooterText,
		patch.PrimaryColor,
		patch.BackgroundColor,
		patch.SurfaceColor,
		patch.BorderColor,
		patch.MutedColor,
		patch.BodyColor,
		patch.SupportEmail,
		patch.CompanyName,
		patch.CompanyAddress,
		patch.PrivacyURL,
		patch.TermsURL,
		ptrStringValue(updatedBy),
	)
	if err != nil {
		return nil, fmt.Errorf("update email brand settings: %w", err)
	}
	return s.GetEmailBrandSettings(ctx)
}

// defaultBrandSettings mirrors the migration defaults and is returned
// when the DB row is unexpectedly missing. Keeps render paths from
// crashing in that edge case.
func defaultBrandSettings() *model.EmailBrandSettings {
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

// --- InMemoryStore implementation (dev/test) -----------------------------

func (s *InMemoryStore) GetEmailBrandSettings(_ context.Context) (*model.EmailBrandSettings, error) {
	s.brandMu.RLock()
	defer s.brandMu.RUnlock()
	if s.emailBrandSettings == nil {
		return defaultBrandSettings(), nil
	}
	copy := *s.emailBrandSettings
	return &copy, nil
}

func (s *InMemoryStore) UpdateEmailBrandSettings(_ context.Context, patch model.EmailBrandSettingsPatch, updatedBy *string) (*model.EmailBrandSettings, error) {
	s.brandMu.Lock()
	defer s.brandMu.Unlock()
	if s.emailBrandSettings == nil {
		s.emailBrandSettings = defaultBrandSettings()
	}
	applyBrandPatch(s.emailBrandSettings, patch)
	if updatedBy != nil {
		value := *updatedBy
		s.emailBrandSettings.UpdatedBy = &value
	}
	copy := *s.emailBrandSettings
	return &copy, nil
}

func applyBrandPatch(current *model.EmailBrandSettings, patch model.EmailBrandSettingsPatch) {
	if patch.LogoURL != nil {
		current.LogoURL = *patch.LogoURL
	}
	if patch.LogoAlt != nil {
		current.LogoAlt = *patch.LogoAlt
	}
	if patch.ProductName != nil {
		current.ProductName = *patch.ProductName
	}
	if patch.FooterHTML != nil {
		current.FooterHTML = *patch.FooterHTML
	}
	if patch.FooterText != nil {
		current.FooterText = *patch.FooterText
	}
	if patch.PrimaryColor != nil {
		current.PrimaryColor = *patch.PrimaryColor
	}
	if patch.BackgroundColor != nil {
		current.BackgroundColor = *patch.BackgroundColor
	}
	if patch.SurfaceColor != nil {
		current.SurfaceColor = *patch.SurfaceColor
	}
	if patch.BorderColor != nil {
		current.BorderColor = *patch.BorderColor
	}
	if patch.MutedColor != nil {
		current.MutedColor = *patch.MutedColor
	}
	if patch.BodyColor != nil {
		current.BodyColor = *patch.BodyColor
	}
	if patch.SupportEmail != nil {
		current.SupportEmail = *patch.SupportEmail
	}
	if patch.CompanyName != nil {
		current.CompanyName = *patch.CompanyName
	}
	if patch.CompanyAddress != nil {
		current.CompanyAddress = *patch.CompanyAddress
	}
	if patch.PrivacyURL != nil {
		current.PrivacyURL = *patch.PrivacyURL
	}
	if patch.TermsURL != nil {
		current.TermsURL = *patch.TermsURL
	}
}
