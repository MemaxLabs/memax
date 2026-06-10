package handler

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// hexColorPattern accepts 3-digit or 6-digit hex tokens (optionally 8-
// digit for alpha). We reject named CSS colors / gradients / anything
// else to avoid injecting unescaped garbage into the base layout CSS.
// Empty string is allowed — it clears the field (server applies defaults).
var hexColorPattern = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)

// adminEmailBrandUpdateRequest is the PUT /v1/admin/email/brand payload.
// All fields are optional — nil fields leave the existing value untouched;
// explicit empty strings clear the value.
type adminEmailBrandUpdateRequest struct {
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

// GetEmailBrand returns the current singleton email brand settings.
// GET /v1/admin/email/brand
func (h *AdminHandler) GetEmailBrand(w http.ResponseWriter, r *http.Request) {
	settings, err := h.store.GetEmailBrandSettings(r.Context())
	if err != nil {
		slog.Error("failed to load email brand settings", "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "Failed to load email brand settings.")
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: settings})
}

// UpdateEmailBrand applies a partial patch to the singleton row.
// PUT /v1/admin/email/brand
func (h *AdminHandler) UpdateEmailBrand(w http.ResponseWriter, r *http.Request) {
	adminID := GetUserID(r)
	if adminID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Admin session required.")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Could not read request body.")
		return
	}
	var req adminEmailBrandUpdateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON.")
		return
	}

	for _, c := range []struct {
		value *string
		field string
	}{
		{req.PrimaryColor, "primary_color"},
		{req.BackgroundColor, "background_color"},
		{req.SurfaceColor, "surface_color"},
		{req.BorderColor, "border_color"},
		{req.MutedColor, "muted_color"},
		{req.BodyColor, "body_color"},
	} {
		if c.value != nil && *c.value != "" && !hexColorPattern.MatchString(*c.value) {
			writeError(w, http.StatusBadRequest, "invalid_color", "Color "+c.field+" must be a hex token like #111 or #111111 (empty clears to default).")
			return
		}
	}

	patch := model.EmailBrandSettingsPatch{
		LogoURL:         req.LogoURL,
		LogoAlt:         req.LogoAlt,
		ProductName:     req.ProductName,
		FooterHTML:      req.FooterHTML,
		FooterText:      req.FooterText,
		PrimaryColor:    req.PrimaryColor,
		BackgroundColor: req.BackgroundColor,
		SurfaceColor:    req.SurfaceColor,
		BorderColor:     req.BorderColor,
		MutedColor:      req.MutedColor,
		BodyColor:       req.BodyColor,
		SupportEmail:    req.SupportEmail,
		CompanyName:     req.CompanyName,
		CompanyAddress:  req.CompanyAddress,
		PrivacyURL:      req.PrivacyURL,
		TermsURL:        req.TermsURL,
	}

	settings, err := h.store.UpdateEmailBrandSettings(r.Context(), patch, &adminID)
	if err != nil {
		slog.Error("failed to update email brand settings", "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "Failed to update email brand settings.")
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: settings})
}
