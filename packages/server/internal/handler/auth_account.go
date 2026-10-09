package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/auth"
)

// The identity side of Settings › Account, for /v2/me/account
// (internal/handler/v2api, account.go): the profile, the ways a person
// signs in, linking and unlinking them, and the one-time code a passkey
// sign-in turns into a web session. The decisions (who may, on which
// surface, with a passkey) are policy.DecideAccount's, made by /v2 before
// these run.

// SetPasskeys turns on the refusal of V1's link and unlink for a person
// with a passkey (PasskeyHolders).
func (h *AuthHandler) SetPasskeys(p PasskeyHolders) { h.passkeys = p }

// AccountIdentity is a provider a person signs in with.
type AccountIdentity struct {
	Provider string
	// Account is the provider account, in words: its email, or its name.
	Account  string
	LinkedAt time.Time
}

// ErrProviderUnavailable: the provider isn't configured on this server.
var ErrProviderUnavailable = errors.New("auth: that sign-in provider isn't configured on this server")

// AccountProfile is a person's name and email and the providers they sign
// in with.
func (h *AuthHandler) AccountProfile(ctx context.Context, user uuid.UUID) (name, email string, ids []AccountIdentity, err error) {
	var githubID int64
	err = h.pool.QueryRow(ctx, `
		SELECT COALESCE(NULLIF(display_name, ''), name, ''), email, COALESCE(github_id, 0)
		  FROM users WHERE id = $1`, user).Scan(&name, &email, &githubID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil, fmt.Errorf("account %s: %w", user, pgx.ErrNoRows)
	}
	if err != nil {
		return "", "", nil, fmt.Errorf("account: %w", err)
	}
	rows, err := h.pool.Query(ctx, `
		SELECT provider, provider_email, provider_name, created_at
		  FROM auth_identities WHERE user_id = $1 ORDER BY created_at, provider`, user)
	if err != nil {
		return "", "", nil, fmt.Errorf("account identities: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id AccountIdentity
		var providerEmail, providerName string
		if err := rows.Scan(&id.Provider, &providerEmail, &providerName, &id.LinkedAt); err != nil {
			return "", "", nil, fmt.Errorf("account identities: %w", err)
		}
		id.Account = providerEmail
		if id.Account == "" {
			id.Account = providerName
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return "", "", nil, fmt.Errorf("account identities: %w", err)
	}
	// Accounts from before identities were recorded sign in with GitHub.
	if len(ids) == 0 && githubID > 0 {
		ids = append(ids, AccountIdentity{Provider: "github"})
	}
	return name, email, ids, nil
}

// SetAccountName changes the name receipts show for a person.
func (h *AuthHandler) SetAccountName(ctx context.Context, user uuid.UUID, name string) error {
	tag, err := h.pool.Exec(ctx, `UPDATE users SET display_name = $2, updated_at = now() WHERE id = $1`, user, name)
	if err != nil {
		return fmt.Errorf("account name: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("account %s: %w", user, pgx.ErrNoRows)
	}
	return nil
}

// AccountLinkURL starts linking a provider to a person's account: the
// provider's sign-in page, which comes back to redirect (on the web app).
func (h *AuthHandler) AccountLinkURL(ctx context.Context, user uuid.UUID, provider, redirect string) (string, error) {
	return h.linkAuthURL(ctx, provider, redirect, user.String())
}

// linkAuthURL is the provider's authorize URL for a link flow, its state
// stored.
func (h *AuthHandler) linkAuthURL(ctx context.Context, provider, clientRedirect, userID string) (string, error) {
	switch provider {
	case "github":
		if h.clientID == "" {
			return "", ErrProviderUnavailable
		}
	case "google":
		if h.googleClientID == "" {
			return "", ErrProviderUnavailable
		}
	default:
		return "", fmt.Errorf("unsupported provider %q", provider)
	}
	if h.store == nil {
		return "", ErrProviderUnavailable
	}
	state, err := h.createOAuthState(ctx, provider, "link", clientRedirect, userID)
	if err != nil {
		return "", err
	}
	if provider == "github" {
		return fmt.Sprintf(
			"https://github.com/login/oauth/authorize?client_id=%s&redirect_uri=%s&scope=read:user,user:email,read:org&state=%s",
			h.clientID, h.redirectURL, state), nil
	}
	return fmt.Sprintf(
		"https://accounts.google.com/o/oauth2/v2/auth?client_id=%s&redirect_uri=%s&scope=openid+email+profile&response_type=code&state=%s",
		h.googleClientID, url.QueryEscape(h.googleRedirectURL), state), nil
}

// AllowedRedirect reports whether a link may come back to redirect.
func (h *AuthHandler) AllowedRedirect(redirect string) bool {
	return strings.TrimSpace(redirect) != "" && h.isAllowedRedirect(redirect)
}

// AccountUnlink unlinks a provider from a person's account. Their last way
// to sign in can't go (ErrLastIdentity).
func (h *AuthHandler) AccountUnlink(ctx context.Context, user uuid.UUID, provider string) error {
	if h.store == nil {
		return ErrProviderUnavailable
	}
	return h.unlinkProvider(user.String(), provider)
}

// IsLastIdentity reports whether err is unlinking the last way in.
func IsLastIdentity(err error) bool { return errors.Is(err, ErrLastIdentity) }

// webCodeTTL is how long a passkey sign-in's code lasts: the web app's
// server exchanges it at once.
const webCodeTTL = 60 * time.Second

// WebSignInCode issues a one-time code for a web session, as a login
// redirected to the web app does (completeLogin): a passkey sign-in on the
// web app's origin, verified by /v2, is a sign-in on the web.
func (h *AuthHandler) WebSignInCode(ctx context.Context, user uuid.UUID) (string, time.Duration, error) {
	code := generateToken()
	if _, err := h.pool.Exec(ctx,
		`INSERT INTO auth_codes (code, user_id, expires_at, surface) VALUES ($1, $2, $3, $4)`,
		code, user, time.Now().Add(webCodeTTL), auth.SurfaceWeb); err != nil {
		return "", 0, fmt.Errorf("sign-in code: %w", err)
	}
	track(user.String(), "api.auth.login", map[string]any{"method": "passkey"})
	return code, webCodeTTL, nil
}

// refuseLinkForPasskey answers V1's link and unlink for a person with a
// passkey: those change how they sign in, which asks for the passkey, and
// V1 can't.
func (h *AuthHandler) refuseLinkForPasskey(w http.ResponseWriter, r *http.Request, userID string) bool {
	person, err := uuid.Parse(userID)
	if err != nil {
		return false
	}
	return refusedForPasskey(w, r, h.passkeys, person, "Change how you sign in in Settings › Account")
}
