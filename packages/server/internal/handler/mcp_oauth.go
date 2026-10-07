package handler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/auth"
	"github.com/MemaxLabs/memax/packages/server/internal/deviceauth"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/safefetch"
	"github.com/MemaxLabs/memax/packages/server/internal/sessions"
)

// MCPOAuthHandler implements the MCP-spec OAuth 2.0 authorization flow.
// This enables Claude Desktop (GUI app) to connect to the remote MCP server
// without manual API key setup — it discovers auth via well-known endpoints,
// does OAuth in the browser, and gets tokens automatically.
//
// Compatible with existing Bearer token auth (API keys + JWTs still work).
type MCPOAuthHandler struct {
	authH      *AuthHandler
	baseURL    string // e.g. "https://staging-api.memaxlabs.com"
	appBaseURL string // e.g. "https://staging-app.memaxlabs.com"
	// mcpAlias is a second public origin for the MCP endpoints
	// (MCP_BASE_URL, e.g. https://mcp.memax.app); its resources are valid
	// audiences too.
	mcpAlias string
	// ledger connects the agent to the chosen spaces when consent
	// completes (plan 25 §5.15). Nil (no database) skips it.
	ledger *ledger.Ledger
	// fetchMetadata fetches a Client ID Metadata Document. Tests replace it.
	fetchMetadata func(ctx context.Context, url string) (*safefetch.FetchResult, error)
	// device serves the device authorization grant (oauth_device.go); nil
	// leaves it off. clientIP reads the CLI's address for its rate limit.
	device   *deviceauth.Store
	clientIP func(*http.Request) string
}

func NewMCPOAuthHandler(authH *AuthHandler) *MCPOAuthHandler {
	baseURL := strings.TrimRight(os.Getenv("API_BASE_URL"), "/")
	appBaseURL := strings.TrimRight(os.Getenv("APP_BASE_URL"), "/")
	return &MCPOAuthHandler{
		authH: authH, baseURL: baseURL, appBaseURL: appBaseURL,
		mcpAlias:      strings.TrimRight(os.Getenv("MCP_BASE_URL"), "/"),
		fetchMetadata: defaultMetadataFetcher().Fetch,
	}
}

// SetLedger lets consent connect the agent on the V2 record.
func (h *MCPOAuthHandler) SetLedger(l *ledger.Ledger) { h.ledger = l }

// resolveBaseURL returns the base URL, falling back to deriving it from the request.
func (h *MCPOAuthHandler) resolveBaseURL(r *http.Request) string {
	if h.baseURL != "" {
		return h.baseURL
	}
	scheme := "https"
	if r.TLS == nil && !strings.Contains(r.Host, "fly.dev") && !strings.Contains(r.Host, "memaxlabs.com") {
		scheme = "http"
	}
	return scheme + "://" + r.Host
}

func (h *MCPOAuthHandler) resolveAppBaseURL(r *http.Request) string {
	if h.appBaseURL != "" {
		return h.appBaseURL
	}

	base, err := url.Parse(h.resolveBaseURL(r))
	if err != nil {
		return ""
	}
	host := strings.ToLower(base.Host)
	switch host {
	case "api.memax.app":
		return "https://memax.app"
	case "staging-api.memaxlabs.com":
		return "https://staging-app.memaxlabs.com"
	}
	if strings.HasPrefix(host, "localhost:") || strings.HasPrefix(host, "127.0.0.1:") {
		return "http://localhost:3000"
	}
	return ""
}

// webRequestURL is the web app's page for a pending authorization request
// (OAuthConsent, /oauth/authorize?request=<id>), or "" when there is no web
// app to send the person to.
func (h *MCPOAuthHandler) webRequestURL(r *http.Request, requestID string) string {
	appBase := h.resolveAppBaseURL(r)
	if appBase == "" {
		return ""
	}
	u, err := url.Parse(appBase)
	if err != nil {
		return ""
	}
	u.Path = consentPath
	u.RawQuery = url.Values{"request": {requestID}}.Encode()
	return u.String()
}

// ProtectedResourceMetadata serves GET /.well-known/oauth-protected-resource
// This tells MCP clients where to find the authorization server.
func (h *MCPOAuthHandler) ProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	base := h.resolveBaseURL(r)
	// The resource is the endpoint the client connected to, on whichever
	// public origin it used (api.memax.app or the mcp.memax.app alias);
	// the authorization server is always the API's.
	resourceBase := base
	if h.mcpAlias != "" {
		if u, err := url.Parse(h.mcpAlias); err == nil && strings.EqualFold(u.Host, r.Host) {
			resourceBase = h.mcpAlias
		}
	}
	resource := h.protectedResourceURL(resourceBase, r)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"resource":                 resource,
		"authorization_servers":    []string{base},
		"scopes_supported":         []string{ScopeRead, ScopePropose, ScopeWrite},
		"bearer_methods_supported": []string{"header"},
		"resource_name":            "Memax",
	})
}

// mcpResources are the MCP endpoints a token may be issued for: /mcp and
// /mcp/chatgpt on the API's origin and on the MCP alias.
func (h *MCPOAuthHandler) mcpResources(r *http.Request) []string {
	bases := []string{h.resolveBaseURL(r)}
	if h.mcpAlias != "" && h.mcpAlias != bases[0] {
		bases = append(bases, h.mcpAlias)
	}
	out := make([]string, 0, 2*len(bases))
	for _, b := range bases {
		out = append(out, b+"/mcp", b+"/mcp/chatgpt")
	}
	return out
}

// validResource reports whether a requested resource (RFC 8707) is one of
// the MCP endpoints, ignoring a trailing slash.
func (h *MCPOAuthHandler) validResource(r *http.Request, resource string) bool {
	return auth.Audience(h.mcpResources(r)).Contains(resource)
}

// audienceFor is the aud of a grant's tokens: the resource it was issued
// for, or every MCP endpoint for a grant whose client named none.
func (h *MCPOAuthHandler) audienceFor(r *http.Request, resource string) []string {
	if resource != "" {
		return []string{strings.TrimRight(resource, "/")}
	}
	return h.mcpResources(r)
}

func (h *MCPOAuthHandler) protectedResourceURL(base string, r *http.Request) string {
	if resource := strings.TrimSpace(r.URL.Query().Get("resource")); resource != "" {
		return resource
	}

	const prefix = "/.well-known/oauth-protected-resource"
	if strings.HasPrefix(r.URL.Path, prefix+"/") {
		return base + strings.TrimPrefix(r.URL.Path, prefix)
	}
	return base + "/mcp"
}

// AuthorizationServerMetadata serves GET /.well-known/oauth-authorization-server
// Standard OAuth 2.0 Authorization Server Metadata (RFC 8414).
func (h *MCPOAuthHandler) AuthorizationServerMetadata(w http.ResponseWriter, r *http.Request) {
	base := h.resolveBaseURL(r)
	meta := map[string]any{
		"issuer":                 base,
		"authorization_endpoint": base + "/oauth/authorize",
		"token_endpoint":         base + "/oauth/token",
		// Dynamic registration is deprecated in MCP 2026-07-28 but Cursor
		// and older clients still use it; newer clients send a Client ID
		// Metadata Document URL as their client_id instead.
		"registration_endpoint":                          base + "/oauth/register",
		"client_id_metadata_document_supported":          true,
		"authorization_response_iss_parameter_supported": true,
		"scopes_supported":                               []string{ScopeRead, ScopePropose, ScopeWrite},
		"response_types_supported":                       []string{"code"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
		"token_endpoint_auth_methods_supported":          []string{"none"},
		"code_challenge_methods_supported":               []string{"S256"},
		// RFC 7009: a client signing out ends its session.
		"revocation_endpoint":                        base + "/oauth/revoke",
		"revocation_endpoint_auth_methods_supported": []string{"none"},
	}
	// The device grant signs the memax CLI in (RFC 8628 §4); MCP clients
	// never use it.
	if h.device != nil {
		meta["device_authorization_endpoint"] = base + "/oauth/device_authorization"
		meta["grant_types_supported"] = []string{"authorization_code", "refresh_token", deviceauth.GrantType}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(meta)
}

// DynamicClientRegistration serves POST /oauth/register
// MCP spec requires dynamic client registration (RFC 7591).
// We accept any client — just echo back a client_id.
func (h *MCPOAuthHandler) DynamicClientRegistration(w http.ResponseWriter, r *http.Request) {
	if h.authH == nil || h.authH.pool == nil {
		oauthError(w, "server_error", "OAuth is not available")
		return
	}
	var req struct {
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		oauthError(w, "invalid_request", "Invalid client registration request")
		return
	}
	req.ClientName = strings.TrimSpace(req.ClientName)
	req.RedirectURIs = uniqueNonEmptyStrings(req.RedirectURIs)
	if len(req.RedirectURIs) == 0 {
		oauthError(w, "invalid_client_metadata", "redirect_uris is required")
		return
	}
	for _, redirectURI := range req.RedirectURIs {
		if !validOAuthRedirectURI(redirectURI) {
			oauthError(w, "invalid_redirect_uri", "redirect_uris must be absolute http(s) URLs or loopback URLs")
			return
		}
	}
	if slug := agentNameFromClientName(req.ClientName); slug == "" || slug == "unknown" {
		oauthError(w, "invalid_client_metadata", "client_name is required and must identify the agent")
		return
	}

	// Generate a simple client ID — we don't enforce client auth
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		oauthError(w, "server_error", "Failed to register client")
		return
	}
	clientID := "mcp_" + hex.EncodeToString(b)

	_, err := h.authH.pool.Exec(context.Background(),
		`INSERT INTO oauth_clients (client_id, client_name, redirect_uris)
		VALUES ($1, $2, $3)`,
		clientID, req.ClientName, req.RedirectURIs)
	if err != nil {
		slog.Error("failed to register MCP OAuth client", "error", err)
		oauthError(w, "server_error", "Failed to register client")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"client_id":                  clientID,
		"client_name":                req.ClientName,
		"redirect_uris":              req.RedirectURIs,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
		"client_id_issued_at":        time.Now().Unix(),
	})
}

// Authorize serves GET /oauth/authorize
// This is the authorization endpoint. It stores the OAuth request, redirects
// the user to GitHub login, then shows a Memax consent screen before issuing a
// grant-bound authorization code.
func (h *MCPOAuthHandler) Authorize(w http.ResponseWriter, r *http.Request) {
	if h.authH == nil || h.authH.pool == nil {
		http.Error(w, "OAuth is not available", http.StatusServiceUnavailable)
		return
	}
	// Standard OAuth 2.0 authorize params
	clientID := r.URL.Query().Get("client_id")
	redirectURI := r.URL.Query().Get("redirect_uri")
	state := r.URL.Query().Get("state")
	codeChallenge := r.URL.Query().Get("code_challenge")
	codeChallengeMethod := r.URL.Query().Get("code_challenge_method")
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	resource := strings.TrimSpace(r.URL.Query().Get("resource"))

	if clientID == "" || redirectURI == "" {
		http.Error(w, "client_id and redirect_uri are required", http.StatusBadRequest)
		return
	}

	var client oauthClient
	var err error
	if isMetadataClientID(clientID) {
		// A Client ID Metadata Document: the client_id is a URL to the
		// client's metadata, fetched and checked here (mcp_cimd.go).
		client, err = h.resolveMetadataClient(r.Context(), clientID, redirectURI)
		if err != nil {
			slog.Warn("MCP OAuth: client metadata document refused", "client_id", clientID, "error", err)
			http.Error(w, "The client's metadata document can't be used: "+err.Error(), http.StatusBadRequest)
			return
		}
	} else if client, err = h.loadOAuthClient(r.Context(), clientID); err != nil {
		http.Error(w, "Unknown OAuth client", http.StatusBadRequest)
		return
	}
	if !redirectURIAllowed(redirectURI, client.RedirectURIs) {
		http.Error(w, "redirect_uri is not registered for this client", http.StatusBadRequest)
		return
	}
	iss := h.resolveBaseURL(r)
	if codeChallenge == "" || codeChallengeMethod != "S256" {
		redirectOAuthErrorIss(w, r, redirectURI, state, iss, "invalid_request", "PKCE S256 is required")
		return
	}
	// RFC 8707: the token will be bound to this resource, so it must be one
	// of ours. A client that names none gets a token for the MCP endpoints.
	if resource != "" && !h.validResource(r, resource) {
		redirectOAuthErrorIss(w, r, redirectURI, state, iss, "invalid_target", "resource must be this server's MCP endpoint")
		return
	}
	requestedPermissions, normalizedScope, invalidScopes := oauthPermissionsFromScope(scope)
	if len(invalidScopes) > 0 || len(requestedPermissions) == 0 {
		redirectOAuthErrorIss(w, r, redirectURI, state, iss, "invalid_scope", "Unsupported Memax OAuth scope")
		return
	}

	// Store the OAuth params so we can use them after GitHub callback
	sessionID := generateOAuthSession()
	_, err = h.authH.pool.Exec(context.Background(),
		`INSERT INTO oauth_authorization_requests (
			id, client_id, client_name, redirect_uri, state, code_challenge,
			code_challenge_method, requested_permissions, requested_scope,
			resource, expires_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::text[], $9, $10, $11)`,
		sessionID,
		clientID,
		client.ClientName,
		redirectURI,
		state,
		codeChallenge,
		codeChallengeMethod,
		requestedPermissions.Strings(),
		normalizedScope,
		resource,
		time.Now().Add(10*time.Minute),
	)
	if err != nil {
		slog.Error("failed to store MCP OAuth authorization request", "error", err)
		redirectOAuthErrorIss(w, r, redirectURI, state, iss, "server_error", "Failed to start authorization")
		return
	}
	// The person signs in on the web app, with any of its sign-in methods,
	// and answers there (OAuthConsent); the web session says who they are.
	page := h.webRequestURL(r, sessionID)
	if page == "" {
		h.deleteOAuthAuthorizationRequest(sessionID)
		redirectOAuthErrorIss(w, r, redirectURI, state, iss, "temporarily_unavailable",
			"Memax has no web app to sign in on; set APP_BASE_URL")
		return
	}
	http.Redirect(w, r, page, http.StatusSeeOther)
}

// Token serves POST /oauth/token
// Handles both authorization_code and refresh_token grant types.
func (h *MCPOAuthHandler) Token(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	grantType := r.FormValue("grant_type")

	switch grantType {
	case "authorization_code":
		h.tokenAuthCode(w, r)
	case "refresh_token":
		h.tokenRefresh(w, r)
	case deviceauth.GrantType:
		h.tokenDeviceCode(w, r)
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "unsupported_grant_type",
		})
	}
}

func (h *MCPOAuthHandler) tokenAuthCode(w http.ResponseWriter, r *http.Request) {
	code := r.FormValue("code")
	codeVerifier := r.FormValue("code_verifier")
	redirectURI := r.FormValue("redirect_uri")

	if code == "" {
		oauthError(w, "invalid_request", "code is required")
		return
	}

	// Look up the auth code
	var userID string
	var expiresAt time.Time
	var used bool
	var codeChallenge string
	var grantID string
	var storedRedirectURI string
	err := h.authH.pool.QueryRow(context.Background(),
		`SELECT user_id, expires_at, used, code_challenge, COALESCE(grant_id::text, ''), COALESCE(redirect_uri, '')
		FROM auth_codes WHERE code = $1`, code,
	).Scan(&userID, &expiresAt, &used, &codeChallenge, &grantID, &storedRedirectURI)
	if err != nil {
		oauthError(w, "invalid_grant", "Invalid authorization code")
		return
	}
	if storedRedirectURI != "" && redirectURI != storedRedirectURI {
		oauthError(w, "invalid_grant", "redirect_uri does not match authorization request")
		return
	}

	if used || time.Now().After(expiresAt) {
		h.authH.pool.Exec(context.Background(), `DELETE FROM auth_codes WHERE code = $1`, code)
		oauthError(w, "invalid_grant", "Authorization code expired or already used")
		return
	}

	// Verify PKCE. MCP OAuth clients are public clients, so PKCE is required.
	if codeChallenge == "" || codeVerifier == "" {
		oauthError(w, "invalid_grant", "PKCE verification failed")
		return
	}
	hash := sha256.Sum256([]byte(codeVerifier))
	computed := base64.RawURLEncoding.EncodeToString(hash[:])
	if computed != codeChallenge {
		oauthError(w, "invalid_grant", "PKCE verification failed")
		return
	}

	// Mark as used
	h.authH.pool.Exec(context.Background(), `UPDATE auth_codes SET used = true WHERE code = $1`, code)

	grant := h.authH.ResolveOAuthGrant(userID, grantID)
	if grant.UserID == "" {
		oauthError(w, "invalid_grant", "Authorization grant is no longer valid")
		return
	}
	aud, ok := h.tokenAudience(w, r, grantID)
	if !ok {
		return
	}

	tokens, err := h.authH.startSession(r, userID, sessionStart{
		kind: sessions.KindMCP, agentName: grant.AgentName, grantID: grantID,
		client: h.grantClientName(r.Context(), grantID, grant.AgentName), issuer: h.resolveBaseURL(r), audience: aud,
	})
	if err != nil {
		slog.Error("MCP OAuth token issuance failed", "error", err)
		oauthError(w, "server_error", "Failed to issue tokens")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"access_token":  tokens.AccessToken,
		"token_type":    "Bearer",
		"expires_in":    tokens.ExpiresIn,
		"refresh_token": tokens.RefreshToken,
		"scope":         grantScope(grant),
	})
}

// tokenAudience is the aud of a grant's tokens. A token request that names
// a resource (RFC 8707) must name the grant's, or one of the MCP endpoints
// for a grant that has none; otherwise it fails with invalid_target.
func (h *MCPOAuthHandler) tokenAudience(w http.ResponseWriter, r *http.Request, grantID string) ([]string, bool) {
	aud, oerr := h.grantAudience(r, grantID)
	if oerr != nil {
		oauthError(w, oerr.code, oerr.description)
		return nil, false
	}
	return aud, true
}

// oauthFailure is an OAuth error answer (RFC 6749 §5.2).
type oauthFailure struct{ code, description string }

func (e *oauthFailure) Error() string { return e.code + ": " + e.description }

// grantAudience is tokenAudience without writing the answer.
func (h *MCPOAuthHandler) grantAudience(r *http.Request, grantID string) ([]string, *oauthFailure) {
	var resource string
	if err := h.authH.pool.QueryRow(r.Context(),
		`SELECT COALESCE(resource, '') FROM oauth_grants WHERE id = $1::uuid`, grantID).Scan(&resource); err != nil {
		return nil, &oauthFailure{"invalid_grant", "Authorization grant is no longer valid"}
	}
	if asked := strings.TrimSpace(r.FormValue("resource")); asked != "" {
		switch {
		case resource != "" && !auth.Audience{resource}.Contains(asked):
			return nil, &oauthFailure{"invalid_target", "resource doesn't match the authorization"}
		case resource == "" && !h.validResource(r, asked):
			return nil, &oauthFailure{"invalid_target", "resource must be this server's MCP endpoint"}
		case resource == "":
			resource = asked
		}
	}
	return h.audienceFor(r, resource), nil
}

// grantClientName is what the sessions list calls an MCP grant's client:
// its registered name, or its agent.
func (h *MCPOAuthHandler) grantClientName(ctx context.Context, grantID, agentName string) string {
	var name string
	_ = h.authH.pool.QueryRow(ctx, `SELECT COALESCE(NULLIF(c.client_name, ''), '') FROM oauth_grants g
		JOIN oauth_clients c ON c.client_id = g.client_id WHERE g.id = $1::uuid`, grantID).Scan(&name)
	if name == "" {
		name = agentName
	}
	if name == "" {
		name = "MCP client"
	}
	return name
}

// grantScope is the scope a grant's tokens carry: the scope the person
// granted, or (for grants from before it was recorded) the scope its
// permissions amount to.
func grantScope(grant APIKeyResult) string {
	if grant.OAuthScope != "" {
		return grant.OAuthScope
	}
	return oauthScopeFromPermissions(grant.DefaultPermissions)
}

// tokenRefresh is the refresh_token grant: an MCP client trades its
// refresh token for a new pair. The refresh token rotates on every use
// (OAuth 2.1 §4.3.1 for public clients) and is stored hashed; a retired one
// presented after the grace window revokes the session (internal/sessions).
// Only an MCP grant's session refreshes here; a person's refreshes at
// /v1/auth/refresh.
func (h *MCPOAuthHandler) tokenRefresh(w http.ResponseWriter, r *http.Request) {
	refreshToken := r.FormValue("refresh_token")
	if refreshToken == "" {
		oauthError(w, "invalid_request", "refresh_token is required")
		return
	}

	var (
		grant APIKeyResult
		aud   []string
	)
	is, err := h.authH.sessionStore().Refresh(r.Context(), refreshToken, sessions.RefreshOptions{
		Where: h.authH.where(r),
		// Everything that could refuse the refresh is checked before it
		// rotates, so a refused client keeps a token that works.
		Accept: func(ss sessions.Session) error {
			if ss.GrantID == "" {
				return &oauthFailure{"invalid_grant", "Invalid refresh token"}
			}
			grant = h.authH.ResolveOAuthGrant(ss.UserID.String(), ss.GrantID)
			if grant.UserID == "" {
				return sessions.ErrGrantRevoked
			}
			var oerr *oauthFailure
			if aud, oerr = h.grantAudience(r, ss.GrantID); oerr != nil {
				return oerr
			}
			return nil
		},
	})
	var oerr *oauthFailure
	switch {
	case errors.As(err, &oerr):
		oauthError(w, oerr.code, oerr.description)
		return
	case errors.Is(err, sessions.ErrExpired):
		oauthError(w, "invalid_grant", "Refresh token expired")
		return
	case errors.Is(err, sessions.ErrGrantRevoked):
		oauthError(w, "invalid_grant", "Authorization grant is no longer valid")
		return
	case errors.Is(err, sessions.ErrReused), errors.Is(err, sessions.ErrRevoked):
		oauthError(w, "invalid_grant", "This session was revoked. Authorize the client again.")
		return
	case errors.Is(err, sessions.ErrUnknown):
		oauthError(w, "invalid_grant", "Invalid refresh token")
		return
	case err != nil:
		slog.Error("MCP OAuth refresh failed", "error", err)
		oauthError(w, "server_error", "Failed to refresh the session")
		return
	}
	accessToken, err := h.authH.accessTokenFor(is.Session, grant.AgentName, h.resolveBaseURL(r), aud)
	if err != nil {
		oauthError(w, "server_error", "Failed to issue access token")
		return
	}
	oauthJSON(w, http.StatusOK, map[string]any{
		"access_token":  accessToken,
		"token_type":    "Bearer",
		"expires_in":    int(accessTTL / time.Second),
		"refresh_token": is.RefreshToken,
		"scope":         grantScope(grant),
	})
}

// Revoke serves POST /oauth/revoke (RFC 7009): a client signing out ends
// its session. The token is a refresh token (token_type_hint
// refresh_token, the default), which ends its session even when it was
// just rotated out, or a session's access token (hint access_token), which
// ends the session it names. A token that names nothing is answered the
// same way (RFC 7009 §2.2), so this tells nobody whether a token existed.
// Public clients don't authenticate here, as at the token endpoint.
func (h *MCPOAuthHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		oauthError(w, "invalid_request", "Send the request as a form (application/x-www-form-urlencoded).")
		return
	}
	token := strings.TrimSpace(r.PostFormValue("token"))
	if token == "" {
		oauthError(w, "invalid_request", "token is required")
		return
	}
	store := h.authH.sessionStore()
	revoked := false
	if claims, err := auth.VerifyAccessToken(token, h.authH.jwtSecret); err == nil {
		if user, uerr := uuid.Parse(claims.Sub); uerr == nil {
			if sid, serr := uuid.Parse(claims.Sid); serr == nil && claims.ImpersonatorID == "" {
				if _, err := store.Revoke(r.Context(), user, sid, sessions.ReasonSignedOut); err == nil {
					revoked = true
				} else if !errors.Is(err, sessions.ErrNotFound) {
					slog.Error("token revocation failed", "error", err)
					oauthError(w, "server_error", "Memax couldn't revoke the token. Try again.")
					return
				}
			}
		}
	} else {
		ss, err := store.RevokeToken(r.Context(), token, sessions.ReasonSignedOut)
		if err != nil {
			slog.Error("token revocation failed", "error", err)
			oauthError(w, "server_error", "Memax couldn't revoke the token. Try again.")
			return
		}
		revoked = ss != nil
	}
	if revoked {
		slog.Info("session signed out", "via", "oauth_revoke")
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
}

// intersectScopes is the scope the person granted: the requested scope's
// tokens they kept checked (all of them when the form sent none, as V1's
// consent did for an unchanged form). A client that asked to write and was
// allowed to propose gets memax:propose, a narrower grant than it asked
// for (RFC 6749 §3.3: the token response names the scope granted): the
// Ledger page grants no more than Propose.
func intersectScopes(requested, selected string) string {
	_, req, _ := oauthPermissionsFromScope(requested)
	if strings.TrimSpace(selected) == "" {
		return req
	}
	sel := strings.Fields(selected)
	reqFields := strings.Fields(req)
	var out []string
	for _, s := range reqFields {
		switch {
		case slices.Contains(sel, s):
			out = append(out, s)
		case s == ScopeWrite && slices.Contains(sel, ScopePropose) && !slices.Contains(reqFields, ScopePropose):
			out = append(out, ScopePropose)
		}
	}
	return strings.Join(out, " ")
}

// connectAgent connects the new grant's agent to the chosen space on the
// V2 record (plan 25 §5.15) at the level the consent page showed
// (consentLevel): where the agent starts (Cursor and Gemini CLI read; the
// rest take the space's default for new agents), never above Propose. The
// grant's scope may allow more (up to memax:write, as the client asked);
// the connection is what's enforced, and only a person on the web raises
// it, in Agents. A failure leaves the V1 grant working; the agent then
// only reads on V2 until it is connected (cmd/v2-backfill-agents does it
// too).
func (h *MCPOAuthHandler) connectAgent(ctx context.Context, session oauthPendingSession, grantID string, hubIDs []string, scope string) {
	if h.ledger == nil {
		return
	}
	userID, err1 := uuid.Parse(session.userID)
	credID, err2 := uuid.Parse(grantID)
	if err1 != nil || err2 != nil {
		return
	}
	userScope, err := h.ledger.UserScope(ctx, userID)
	if err != nil {
		slog.Warn("MCP OAuth: can't connect the agent on the V2 record", "grant_id", grantID, "error", err)
		return
	}
	agentSlug := agentNameFromClientName(session.clientName)
	// Cursor and Gemini CLI start at Read; the rest at the space's default.
	start := ledger.AgentFromV1(agentSlug).StartAutonomy()
	spaces := make([]ledger.SpaceAutonomy, 0, len(hubIDs))
	var ids []uuid.UUID
	for _, h := range hubIDs {
		if id, err := uuid.Parse(h); err == nil {
			spaces = append(spaces, ledger.SpaceAutonomy{SpaceID: id, Autonomy: start})
			ids = append(ids, id)
		}
	}
	if len(spaces) == 0 {
		return
	}
	clientID := ""
	if isMetadataClientID(session.clientID) {
		clientID = session.clientID // the CIMD URL: the agent's verifiable identity
	}
	cmd := &ledger.ConnectAgent{
		Meta: ledger.Meta{
			Actor: ledger.Actor{Kind: policy.ActorPerson, ID: userID, Credential: policy.CredentialSession},
			Scope: userScope.Narrow(ids...), Via: policy.ViaMCP,
			IdempotencyKey: "oauth-consent:" + grantID,
		},
		Person: userID, Credential: ledger.CredentialOAuthGrant, CredentialID: credID,
		Agent: ledger.AgentFromV1(agentSlug), DisplayName: connectionDisplayName(session.clientName),
		ClientID: clientID, Spaces: spaces,
		Cap: policy.MinAutonomy(policy.Autonomy(scopeCeiling(scope)), policy.AutonomyPropose),
	}
	res, err := h.ledger.Apply(ctx, cmd)
	switch {
	case err != nil:
		slog.Warn("MCP OAuth: can't connect the agent on the V2 record", "grant_id", grantID, "error", err)
	case res.Outcome == ledger.OutcomeRefused:
		slog.Warn("MCP OAuth: connecting the agent was refused", "grant_id", grantID, "policy", res.Policy.Code)
	}
}

// --- Session storage ---

type oauthPendingSession struct {
	id                   string
	clientID             string
	clientName           string
	redirectURI          string
	state                string
	codeChallenge        string
	codeChallengeMethod  string
	requestedPermissions PermissionSet
	requestedScope       string
	resource             string
	userID               string
	csrfToken            string
	expiresAt            time.Time
}

type oauthClient struct {
	ClientID     string
	ClientName   string
	RedirectURIs []string
}

func (h *MCPOAuthHandler) loadOAuthClient(ctx context.Context, clientID string) (oauthClient, error) {
	var client oauthClient
	err := h.authH.pool.QueryRow(ctx,
		`SELECT client_id, client_name, redirect_uris
		FROM oauth_clients WHERE client_id = $1`,
		clientID,
	).Scan(&client.ClientID, &client.ClientName, &client.RedirectURIs)
	return client, err
}

func (h *MCPOAuthHandler) loadOAuthAuthorizationRequest(ctx context.Context, id string) (oauthPendingSession, error) {
	var session oauthPendingSession
	var requestedPermissions []string
	err := h.authH.pool.QueryRow(ctx,
		`SELECT id, client_id, client_name, redirect_uri, state, code_challenge,
			code_challenge_method, requested_permissions, requested_scope, resource,
			COALESCE(user_id::text, ''), csrf_token, expires_at
		FROM oauth_authorization_requests WHERE id = $1`,
		id,
	).Scan(
		&session.id,
		&session.clientID,
		&session.clientName,
		&session.redirectURI,
		&session.state,
		&session.codeChallenge,
		&session.codeChallengeMethod,
		&requestedPermissions,
		&session.requestedScope,
		&session.resource,
		&session.userID,
		&session.csrfToken,
		&session.expiresAt,
	)
	if err != nil {
		return oauthPendingSession{}, err
	}
	perms, invalid := PermissionSetFromStrings(requestedPermissions)
	if len(invalid) > 0 {
		return oauthPendingSession{}, fmt.Errorf("authorization request has invalid permissions: %s", strings.Join(invalid, ", "))
	}
	session.requestedPermissions = perms
	return session, nil
}

func (h *MCPOAuthHandler) deleteOAuthAuthorizationRequest(id string) {
	_, _ = h.authH.pool.Exec(context.Background(), `DELETE FROM oauth_authorization_requests WHERE id = $1`, id)
}

func (h *MCPOAuthHandler) validConsentHubIDs(userID string, selectedHubIDs []string) ([]string, error) {
	if h.authH.store == nil {
		return nil, fmt.Errorf("store is not configured")
	}
	hubs, err := h.authH.store.ListUserHubs(userID)
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, item := range hubs {
		allowed[item.Hub.ID] = true
	}
	out := make([]string, 0, len(selectedHubIDs))
	for _, hubID := range selectedHubIDs {
		if allowed[hubID] {
			out = append(out, hubID)
		}
	}
	return out, nil
}

func (h *MCPOAuthHandler) createOAuthGrant(ctx context.Context, session oauthPendingSession, hubIDs []string, permissions PermissionSet, scope string) (string, error) {
	agentName := agentNameFromClientName(session.clientName)
	if agentName == "" {
		agentName = "unknown"
	}
	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	var grantID string
	err := h.authH.pool.QueryRow(ctx,
		`INSERT INTO oauth_grants (
			user_id, client_id, agent_name, hub_scope_mode, hub_ids,
			default_permissions, trust_level, rate_limit_tier, expires_at,
			resource, scope
		)
		VALUES ($1::uuid, $2, $3, $4, $5::text[]::uuid[], $6::text[], $7, $8, $9, NULLIF($10, ''), NULLIF($11, ''))
		RETURNING id`,
		session.userID,
		session.clientID,
		agentName,
		HubScopeAllowlist,
		hubIDs,
		permissions.Strings(),
		TrustStandard,
		TrustStandard,
		expiresAt,
		strings.TrimRight(session.resource, "/"),
		scope,
	).Scan(&grantID)
	if err == nil {
		EnsureConnectedAgent(h.authH.store, session.userID, agentName)
	}
	return grantID, err
}

func displayOAuthClientName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "an AI agent"
	}
	return name
}

func validOAuthRedirectURI(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		host := strings.ToLower(u.Hostname())
		return host == "localhost" || host == "127.0.0.1" || host == "::1"
	default:
		return false
	}
}

func redirectURIAllowed(redirectURI string, allowed []string) bool {
	for _, candidate := range allowed {
		if redirectURI == candidate {
			return true
		}
	}
	return false
}

func oauthPermissionsFromScope(scope string) (PermissionSet, string, []string) {
	if strings.TrimSpace(scope) == "" {
		scope = "memax:read memax:write"
	}
	var fields []string
	out := PermissionSet{}
	var invalid []string
	for _, field := range strings.Fields(scope) {
		if slices.Contains(fields, field) {
			continue
		}
		fields = append(fields, field)
		switch field {
		case ScopeRead:
			out = out.Union(NewPermissionSet(PermMemoryRead, PermTopicRead, PermDreamRead, PermHubRead, PermHubMembersRead))
		case ScopePropose, ScopeWrite:
			// Both write to the record. On V2 the grant's scope caps the
			// agent's autonomy: propose never keeps (GrantContext
			// .AutonomyCeiling).
			out = out.Union(NewPermissionSet(PermMemoryWrite))
		default:
			invalid = append(invalid, field)
		}
	}
	return out, strings.Join(fields, " "), invalid
}

func oauthScopeFromPermissions(perms PermissionSet) string {
	var scopes []string
	if perms.Has(PermMemoryRead) || perms.Has(PermTopicRead) || perms.Has(PermDreamRead) || perms.Has(PermHubRead) {
		scopes = append(scopes, "memax:read")
	}
	if perms.Has(PermMemoryWrite) {
		scopes = append(scopes, "memax:write")
	}
	return strings.Join(scopes, " ")
}

func redirectWithCode(w http.ResponseWriter, r *http.Request, redirectURI string, state string, code string) {
	redirectWithCodeIss(w, r, redirectURI, state, "", code)
}

// redirectWithCodeIss sends the authorization response with the issuer
// (RFC 9207), so a client talking to several authorization servers can
// tell which one answered (mix-up attacks).
func redirectWithCodeIss(w http.ResponseWriter, r *http.Request, redirectURI, state, iss, code string) {
	u, err := codeResponseURL(redirectURI, state, iss, code)
	if err != nil {
		http.Error(w, "Invalid redirect URI", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, u, http.StatusSeeOther)
}

func redirectOAuthError(w http.ResponseWriter, r *http.Request, redirectURI string, state string, code string, desc string) {
	redirectOAuthErrorIss(w, r, redirectURI, state, "", code, desc)
}

// redirectOAuthErrorIss is an authorization error response with the
// issuer (RFC 9207 covers error responses too).
func redirectOAuthErrorIss(w http.ResponseWriter, r *http.Request, redirectURI, state, iss, code, desc string) {
	u, err := errorResponseURL(redirectURI, state, iss, code, desc)
	if err != nil {
		oauthError(w, code, desc)
		return
	}
	http.Redirect(w, r, u, http.StatusSeeOther)
}

// codeResponseURL is the authorization response (RFC 6749 §4.1.2): the
// client's redirect_uri with the code, its state and the issuer.
func codeResponseURL(redirectURI, state, iss, code string) (string, error) {
	return authorizationResponseURL(redirectURI, state, iss, url.Values{"code": {code}})
}

// errorResponseURL is the authorization error response (§4.1.2.1).
func errorResponseURL(redirectURI, state, iss, code, desc string) (string, error) {
	return authorizationResponseURL(redirectURI, state, iss, url.Values{"error": {code}, "error_description": {desc}})
}

func authorizationResponseURL(redirectURI, state, iss string, params url.Values) (string, error) {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return "", err
	}
	q := u.Query()
	for k, v := range params {
		q[k] = v
	}
	if state != "" {
		q.Set("state", state)
	}
	if iss != "" {
		q.Set("iss", iss)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func generateOAuthSession() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// agentNameFromClientName maps an OAuth client_name to a memax agent slug.
// The slugs must match the IDs used by `memax setup` (setup.ts agent definitions)
// so that API-key-based and OAuth-based connections register the same connected agent.
// New agents are auto-supported — unknown names get normalized to a slug.
func agentNameFromClientName(clientName string) string {
	lower := strings.ToLower(clientName)
	switch {
	// Claude Code (CLI) is distinct from Claude Desktop (chat app)
	case strings.Contains(lower, "claude code"):
		return "claude-code"
	case strings.Contains(lower, "claude"):
		return "claude-ai"
	case strings.Contains(lower, "cursor"):
		return "cursor"
	case strings.Contains(lower, "windsurf"):
		return "windsurf"
	case strings.Contains(lower, "gemini"):
		return "gemini"
	case strings.Contains(lower, "codex"):
		return "codex"
	case strings.Contains(lower, "copilot"):
		return "copilot"
	case strings.Contains(lower, "opencode"):
		return "opencode"
	case strings.Contains(lower, "openclaw"):
		return "openclaw"
	case strings.Contains(lower, "chatgpt"), strings.Contains(lower, "openai"):
		return "chatgpt"
	default:
		slug := strings.ToLower(strings.TrimSpace(clientName))
		slug = strings.ReplaceAll(slug, " ", "-")
		if slug == "" {
			return "unknown"
		}
		return slug
	}
}

func isKnownAgentSlug(slug string) bool {
	switch model.NormalizeAgentSlug(slug) {
	case "claude-code",
		"claude-ai",
		"cursor",
		"windsurf",
		"gemini",
		"codex",
		"copilot",
		"opencode",
		"openclaw",
		"chatgpt":
		return true
	default:
		return false
	}
}

func oauthError(w http.ResponseWriter, code string, desc string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]string{
		"error":             code,
		"error_description": desc,
	})
}
