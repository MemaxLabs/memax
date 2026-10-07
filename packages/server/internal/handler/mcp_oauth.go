package handler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
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

// webConsentURL is the web app's consent page for a request: the Ledger
// page (/oauth/authorize) for a person with a space on the V2 record, V1's
// (/oauth/consent) for everyone else. The web app also sends a browser
// that opted into the V2 UI from V1's page to the Ledger one, with the
// same query (src/lib/ui-gate.ts).
func (h *MCPOAuthHandler) webConsentURL(r *http.Request, requestID string, consentToken string, v2 bool) string {
	appBase := h.resolveAppBaseURL(r)
	if appBase == "" {
		return ""
	}
	u, err := url.Parse(appBase)
	if err != nil {
		return ""
	}
	u.Path = consentPathV1
	if v2 {
		u.Path = consentPathV2
	}
	u.RawQuery = ""
	q := u.Query()
	q.Set("request_id", requestID)
	q.Set("consent_token", consentToken)
	u.RawQuery = q.Encode()
	return u.String()
}

func (h *MCPOAuthHandler) consentSubmitURL(r *http.Request) string {
	return h.resolveBaseURL(r) + "/oauth/authorize/consent"
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
	// Redirect to GitHub OAuth, passing our session ID as the state
	// After GitHub auth, our callback will look up this session and redirect
	// to the MCP client's redirect_uri
	http.Redirect(w, r, h.githubAuthorizeURL(sessionID, false), http.StatusTemporaryRedirect)
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

// HandleMCPCallback is called from the GitHub OAuth callback when state starts with "mcp:".
// It shows the Memax consent screen for the pending MCP OAuth request.
func (h *MCPOAuthHandler) HandleMCPCallback(w http.ResponseWriter, r *http.Request, userID string, mcpSessionID string) {
	session, err := h.loadOAuthAuthorizationRequest(r.Context(), mcpSessionID)
	if err != nil {
		http.Error(w, "Invalid or expired MCP OAuth session", http.StatusBadRequest)
		return
	}

	// Check expiry (10 minutes)
	if time.Now().After(session.expiresAt) {
		h.deleteOAuthAuthorizationRequest(mcpSessionID)
		http.Error(w, "MCP OAuth session expired", http.StatusBadRequest)
		return
	}

	csrfToken := generateOAuthSession()
	_, err = h.authH.pool.Exec(context.Background(),
		`UPDATE oauth_authorization_requests
		SET user_id = $1::uuid, csrf_token = $2
		WHERE id = $3`,
		userID, csrfToken, mcpSessionID)
	if err != nil {
		slog.Error("failed to prepare MCP OAuth consent", "error", err)
		http.Error(w, "Failed to prepare consent screen", http.StatusInternalServerError)
		return
	}
	session.userID = userID
	session.csrfToken = csrfToken

	if consentURL := h.webConsentURL(r, session.id, csrfToken, h.personOnV2(r.Context(), userID)); consentURL != "" {
		http.Redirect(w, r, consentURL, http.StatusSeeOther)
		return
	}

	h.renderConsent(w, r, session, "")
}

// ConsentRequest serves the pending authorization request to the web app
// consent route. The API remains the OAuth authority; the web app only renders
// the decision UI and posts back to Consent.
func (h *MCPOAuthHandler) ConsentRequest(w http.ResponseWriter, r *http.Request) {
	if h.authH == nil || h.authH.pool == nil {
		writeError(w, http.StatusServiceUnavailable, "oauth_unavailable", "OAuth is not available")
		return
	}

	requestID := strings.TrimSpace(r.URL.Query().Get("request_id"))
	consentToken := strings.TrimSpace(r.URL.Query().Get("consent_token"))
	if requestID == "" || consentToken == "" {
		writeError(w, http.StatusBadRequest, "missing_consent_request", "Authorization request is missing.")
		return
	}

	session, err := h.loadOAuthAuthorizationRequest(r.Context(), requestID)
	if err != nil || session.userID == "" {
		writeError(w, http.StatusNotFound, "consent_request_not_found", "Authorization request was not found.")
		return
	}
	if time.Now().After(session.expiresAt) {
		h.deleteOAuthAuthorizationRequest(session.id)
		writeError(w, http.StatusGone, "consent_request_expired", "Authorization request expired.")
		return
	}
	if !subtleConstantTimeCompare(consentToken, session.csrfToken) {
		writeError(w, http.StatusForbidden, "invalid_consent_token", "Authorization request token is invalid.")
		return
	}

	data, err := h.buildConsentData(r, session, "")
	if err != nil {
		slog.Error("failed to build MCP OAuth consent request", "error", err)
		writeError(w, http.StatusInternalServerError, "consent_request_failed", "Failed to load authorization request.")
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: data})
}

// Consent handles the Memax authorization screen submission: V1's page,
// the Ledger page (ui=v2) and the API's own HTML page all post here.
func (h *MCPOAuthHandler) Consent(w http.ResponseWriter, r *http.Request) {
	if h.authH == nil || h.authH.pool == nil {
		http.Error(w, "OAuth is not available", http.StatusServiceUnavailable)
		return
	}
	if problem := consentFetchProblem(r, h.consentOrigins(r)); problem != "" {
		slog.Warn("MCP OAuth: refused a consent post from another site", "reason", problem)
		http.Error(w, "This form was sent from another site, so Memax didn't accept it. Start again from your agent.", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid consent request", http.StatusBadRequest)
		return
	}
	// The Ledger page hears about a request that can't go on as a page of
	// its own, not as the API's text or V1's HTML.
	ledgerPage := r.FormValue("ui") == "v2"

	sessionID := r.FormValue("session_id")
	session, err := h.loadOAuthAuthorizationRequest(r.Context(), sessionID)
	if err != nil || session.userID == "" || time.Now().After(session.expiresAt) {
		expired := err == nil && time.Now().After(session.expiresAt)
		if sessionID != "" {
			h.deleteOAuthAuthorizationRequest(sessionID)
		}
		if ledgerPage && h.consentEnded(w, r, expired) {
			return
		}
		http.Error(w, "Invalid or expired authorization request", http.StatusBadRequest)
		return
	}
	if subtleConstantTimeCompare(r.FormValue("csrf_token"), session.csrfToken) == false {
		if ledgerPage && h.consentEnded(w, r, false) {
			return
		}
		http.Error(w, "Invalid consent token", http.StatusBadRequest)
		return
	}

	iss := h.resolveBaseURL(r)
	if consentDecisionDenied(r.FormValue("decision")) {
		h.deleteOAuthAuthorizationRequest(session.id)
		redirectOAuthErrorIss(w, r, session.redirectURI, session.state, iss, "access_denied", "The authorization request was canceled")
		return
	}
	if strings.TrimSpace(r.FormValue("decision")) == consentSwitch {
		h.switchAccount(w, r, session)
		return
	}
	retry := func(code, message string) {
		if !ledgerPage || !h.consentRetry(w, r, session, code) {
			h.renderConsent(w, r, session, message)
		}
	}

	selectedPermissions := PermissionSet{}
	var invalid []string
	selectedScope := strings.TrimSpace(strings.Join(r.Form["permission"], " "))
	if selectedScope != "" {
		selectedPermissions, _, invalid = oauthPermissionsFromScope(selectedScope)
		if len(invalid) > 0 {
			retry("permission", "Only supported Memax permissions can be granted.")
			return
		}
	}
	selectedPermissions = selectedPermissions.Intersect(session.requestedPermissions)
	grantedScope := intersectScopes(session.requestedScope, selectedScope)
	if len(selectedPermissions) == 0 {
		retry("permission", "Select at least one capability.")
		return
	}

	selectedHubIDs := uniqueNonEmptyStrings(r.Form["hub_id"])
	validHubIDs, err := h.validConsentHubIDs(session.userID, selectedHubIDs)
	if err != nil {
		slog.Error("failed to validate MCP OAuth consent hubs", "error", err)
		http.Error(w, "Failed to validate hubs", http.StatusInternalServerError)
		return
	}
	if len(validHubIDs) == 0 {
		retry("space", "Select at least one hub.")
		return
	}

	grantID, err := h.createOAuthGrant(r.Context(), session, validHubIDs, selectedPermissions, grantedScope)
	if err != nil {
		slog.Error("failed to create MCP OAuth grant", "error", err)
		http.Error(w, "Failed to create authorization grant", http.StatusInternalServerError)
		return
	}
	h.connectAgent(r.Context(), session, grantID, validHubIDs, grantedScope)

	authCode := generateOAuthSession()
	_, err = h.authH.pool.Exec(context.Background(),
		`INSERT INTO auth_codes (code, user_id, expires_at, code_challenge, grant_id, client_id, redirect_uri)
		VALUES ($1, $2::uuid, $3, $4, $5::uuid, $6, $7)`,
		authCode, session.userID, time.Now().Add(5*time.Minute), session.codeChallenge, grantID, session.clientID, session.redirectURI)
	if err != nil {
		slog.Error("failed to store MCP auth code", "error", err)
		http.Error(w, "Failed to issue authorization code", http.StatusInternalServerError)
		return
	}
	h.deleteOAuthAuthorizationRequest(session.id)
	redirectWithCodeIss(w, r, session.redirectURI, session.state, iss, authCode)
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

// connectAgent connects the new grant's agent to the chosen spaces on the
// V2 record (plan 25 §5.15), at each space's default autonomy, capped by
// the granted scope. A space whose default for new agents is above
// Propose would need a person on the web to raise it, so the agent is
// connected at Propose there and the person raises it in Agents. A failure
// leaves the V1 grant working; the agent then only reads on V2 until it is
// connected (cmd/v2-backfill-agents does it too).
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
		ClientID: clientID, Spaces: spaces, Cap: policy.Autonomy(scopeCeiling(scope)),
	}
	res, err := h.ledger.Apply(ctx, cmd)
	if err == nil && res.Outcome == ledger.OutcomeRefused && res.Policy.Code == policy.CodeAutonomyNeedsWeb {
		cmd.Cap = policy.MinAutonomy(cmd.Cap, policy.AutonomyPropose)
		cmd.IdempotencyKey = "oauth-consent-propose:" + grantID
		res, err = h.ledger.Apply(ctx, cmd)
	}
	switch {
	case err != nil:
		slog.Warn("MCP OAuth: can't connect the agent on the V2 record", "grant_id", grantID, "error", err)
	case res.Outcome == ledger.OutcomeRefused:
		slog.Warn("MCP OAuth: connecting the agent was refused", "grant_id", grantID, "policy", res.Policy.Code)
	}
}

func consentDecisionDenied(decision string) bool {
	return strings.EqualFold(strings.TrimSpace(decision), consentDeny)
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

type oauthConsentHub struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	Slug                 string   `json:"slug"`
	Role                 string   `json:"role"`
	HubType              string   `json:"hub_type"`
	MemoryCount          int      `json:"memory_count"`
	Checked              bool     `json:"checked"`
	Disabled             bool     `json:"disabled"`
	CapabilityLabel      string   `json:"capability_label"`
	SupportedPermissions []string `json:"supported_permissions"`

	// What the Ledger page shows (mcp_oauth_v2.go); V1's page ignores it.
	// SpaceKind is personal, project or team; OnV2 whether the space is on
	// the V2 record. KeptCount (V2 only) and Targets, the files it
	// compiles to, are absent when the ledger can't say.
	SpaceKind   string               `json:"space_kind,omitempty"`
	OnV2        bool                 `json:"on_v2"`
	PeopleCount int                  `json:"people_count,omitempty"`
	KeptCount   *int                 `json:"kept_count,omitempty"`
	Targets     []oauthConsentTarget `json:"targets,omitempty"`
	// Autonomy is the level the agent is connected at here (V2 only).
	Autonomy string `json:"autonomy,omitempty"`
	// Can and Cannot are abilities (abilityReadBrief, …), decided by policy
	// for that level and the scope the Ledger page grants.
	Can    []string `json:"can,omitempty"`
	Cannot []string `json:"cannot,omitempty"`
}

type oauthConsentPermission struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Checked     bool   `json:"checked"`
	// Essential permissions are pre-checked AND locked in the UI — the
	// agent cannot meaningfully operate without them (e.g. memax:read
	// is the minimum grant for any recall-capable agent). The frontend
	// uses this to mark the checkbox with an "essential" badge and
	// prevent unchecking.
	Essential bool `json:"essential"`
}

type oauthConsentData struct {
	SessionID    string                   `json:"session_id"`
	CSRFToken    string                   `json:"csrf_token"`
	ClientName   string                   `json:"client_name"`
	AgentName    string                   `json:"agent_name"`
	Resource     string                   `json:"resource"`
	SubmitURL    string                   `json:"submit_url"`
	ExpiresAt    time.Time                `json:"expires_at"`
	Hubs         []oauthConsentHub        `json:"hubs"`
	Permissions  []oauthConsentPermission `json:"permissions"`
	NotRequested []string                 `json:"not_requested"`
	Error        string                   `json:"error,omitempty"`

	// For the Ledger page: who the request is signed in as, the host a
	// metadata-document client's client_id is served from (verified, unlike
	// its name), the scope the page grants, and the seconds left.
	Person       *oauthConsentPerson `json:"person,omitempty"`
	ClientHost   string              `json:"client_host,omitempty"`
	ConsentScope string              `json:"consent_scope,omitempty"`
	ExpiresIn    int                 `json:"expires_in"`
}

func (h *MCPOAuthHandler) buildConsentData(r *http.Request, session oauthPendingSession, message string) (oauthConsentData, error) {
	if h.authH.store == nil {
		return oauthConsentData{}, fmt.Errorf("store is not configured")
	}
	hubs, err := h.authH.store.ListUserHubs(session.userID)
	if err != nil {
		return oauthConsentData{}, err
	}

	consentHubs := make([]oauthConsentHub, 0, len(hubs))
	for _, item := range hubs {
		supportedPermissions := rolePermissionSet(item.Role, &item.Hub).Intersect(session.requestedPermissions)
		consentHubs = append(consentHubs, oauthConsentHub{
			ID:                   item.Hub.ID,
			Name:                 item.Hub.Name,
			Slug:                 item.Hub.Slug,
			Role:                 item.Role,
			HubType:              item.Hub.HubType,
			MemoryCount:          item.MemoryCount,
			Checked:              len(supportedPermissions) > 0,
			Disabled:             len(supportedPermissions) == 0,
			CapabilityLabel:      consentHubCapabilityLabel(supportedPermissions),
			SupportedPermissions: supportedPermissions.Strings(),
		})
	}
	h.describeSpaces(r.Context(), session, hubs, consentHubs)
	return oauthConsentData{
		Person:       h.consentPerson(r.Context(), session.userID),
		ClientHost:   clientHost(session.clientID),
		ConsentScope: consentScope(session.requestedScope),
		ExpiresIn:    consentExpiresIn(session.expiresAt),
		SessionID:    session.id,
		CSRFToken:    session.csrfToken,
		ClientName:   displayOAuthClientName(session.clientName),
		AgentName:    agentNameFromClientName(session.clientName),
		Resource:     session.resource,
		SubmitURL:    h.consentSubmitURL(r),
		ExpiresAt:    session.expiresAt,
		Hubs:         consentHubs,
		Permissions:  consentPermissions(session.requestedPermissions, session.requestedScope),
		NotRequested: consentNotRequested(session.requestedPermissions),
		Error:        message,
	}, nil
}

func (h *MCPOAuthHandler) renderConsent(w http.ResponseWriter, r *http.Request, session oauthPendingSession, message string) {
	data, err := h.buildConsentData(r, session, message)
	if err != nil {
		slog.Error("failed to list hubs for MCP OAuth consent", "error", err)
		http.Error(w, "Failed to load hubs", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := oauthConsentTemplate.Execute(w, data); err != nil {
		slog.Error("failed to render MCP OAuth consent", "error", err)
	}
}

func consentPermissions(requested PermissionSet, scope string) []oauthConsentPermission {
	var out []oauthConsentPermission
	fields := strings.Fields(scope)
	if slices.Contains(fields, ScopePropose) && !slices.Contains(fields, ScopeWrite) {
		out = append(out, consentPermissions(requested.Intersect(NewPermissionSet(PermMemoryRead)), ScopeRead)...)
		out = append(out, oauthConsentPermission{
			Value:       ScopePropose,
			Label:       "Propose memories",
			Description: "Save memories into selected hubs. In spaces on the V2 record they wait for you in Review.",
			Checked:     true,
		})
		return out
	}
	if requested.Has(PermMemoryRead) {
		out = append(out, oauthConsentPermission{
			Value:       "memax:read",
			Label:       "Read memories",
			Description: "Search, recall, list, and open memories in selected hubs.",
			Checked:     true,
			// Read is the floor of any meaningful memax integration —
			// without it the agent has no way to check existing state.
			// Lock it so the user can focus on the real decisions.
			Essential: true,
		})
	}
	if requested.Has(PermMemoryWrite) {
		out = append(out, oauthConsentPermission{
			Value:       "memax:write",
			Label:       "Write memories",
			Description: "Save new memories and session captures into selected hubs.",
			Checked:     true,
		})
	}
	return out
}

func consentHubCapabilityLabel(perms PermissionSet) string {
	switch {
	case perms.Has(PermMemoryRead) && perms.Has(PermMemoryWrite):
		return "Read and write available"
	case perms.Has(PermMemoryWrite):
		return "Write available"
	case perms.Has(PermMemoryRead):
		return "Read only"
	default:
		return "Current role cannot use the requested capabilities"
	}
}

func consentNotRequested(requested PermissionSet) []string {
	var out []string
	if !requested.Has(PermMemoryDelete) {
		out = append(out, "Delete memories")
	}
	if !requested.Has(PermTopicWrite) && !requested.Has(PermDreamRun) {
		out = append(out, "Manage topics or run dreams")
	}
	if !requested.Has(PermHubManage) {
		out = append(out, "Manage hub settings or members")
	}
	return out
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
	u, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(w, "Invalid redirect URI", http.StatusInternalServerError)
		return
	}
	q := u.Query()
	q.Set("code", code)
	if state != "" {
		q.Set("state", state)
	}
	if iss != "" {
		q.Set("iss", iss)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusSeeOther)
}

func redirectOAuthError(w http.ResponseWriter, r *http.Request, redirectURI string, state string, code string, desc string) {
	redirectOAuthErrorIss(w, r, redirectURI, state, "", code, desc)
}

// redirectOAuthErrorIss is an authorization error response with the
// issuer (RFC 9207 covers error responses too).
func redirectOAuthErrorIss(w http.ResponseWriter, r *http.Request, redirectURI, state, iss, code, desc string) {
	u, err := url.Parse(redirectURI)
	if err != nil {
		oauthError(w, code, desc)
		return
	}
	q := u.Query()
	q.Set("error", code)
	q.Set("error_description", desc)
	if state != "" {
		q.Set("state", state)
	}
	if iss != "" {
		q.Set("iss", iss)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusSeeOther)
}

func subtleConstantTimeCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

var oauthConsentTemplate = template.Must(template.New("mcp_oauth_consent").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Connect {{.ClientName}} to Memax</title>
  <style>
    :root { color-scheme: light dark; font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    body { margin: 0; min-height: 100vh; display: grid; place-items: center; background: #f7f7f4; color: #171717; }
    main { width: min(720px, calc(100vw - 32px)); background: rgba(255,255,255,.86); border: 1px solid rgba(0,0,0,.1); border-radius: 8px; padding: 28px; box-shadow: 0 24px 80px rgba(20,20,20,.12); }
    h1 { margin: 0 0 8px; font-size: 28px; line-height: 1.15; }
    p { margin: 0; color: #565656; line-height: 1.5; }
    section { margin-top: 24px; }
    h2 { font-size: 15px; margin: 0 0 12px; letter-spacing: 0; }
    .item { display: flex; gap: 12px; align-items: flex-start; padding: 12px 0; border-top: 1px solid rgba(0,0,0,.08); }
    .item:first-of-type { border-top: 0; }
    .meta { color: #6b6b6b; font-size: 13px; }
    .muted { color: #767676; font-size: 13px; line-height: 1.5; margin: 8px 0 0 0; padding-left: 20px; }
    .error { margin-top: 16px; padding: 10px 12px; border-radius: 8px; background: #fff0f0; color: #8a1f1f; }
    .actions { display: flex; gap: 12px; justify-content: flex-end; margin-top: 28px; }
    button { border-radius: 8px; border: 1px solid rgba(0,0,0,.15); padding: 10px 16px; font: inherit; cursor: pointer; }
    button[value="approve"] { background: #171717; color: #fff; border-color: #171717; }
    button[value="deny"] { background: transparent; color: inherit; }
    input { margin-top: 3px; }
    @media (prefers-color-scheme: dark) {
      body { background: #10100f; color: #f4f4f0; }
      main { background: rgba(28,28,26,.92); border-color: rgba(255,255,255,.12); }
      p, .meta { color: #b8b8b0; }
      .item { border-top-color: rgba(255,255,255,.12); }
      .error { background: #3b1818; color: #ffd3d3; }
      button { border-color: rgba(255,255,255,.18); }
      button[value="approve"] { background: #f4f4f0; color: #10100f; border-color: #f4f4f0; }
    }
  </style>
</head>
<body>
  <main>
    <h1>Connect {{.ClientName}} to Memax</h1>
    <p>This agent will only access the hubs and capabilities you approve here. You can revoke the connection later from Memax.</p>
    {{if .Error}}<div class="error">{{.Error}}</div>{{end}}
    <form method="post" action="{{.SubmitURL}}">
      <input type="hidden" name="session_id" value="{{.SessionID}}">
      <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
      <section>
        <h2>Capabilities</h2>
        {{range .Permissions}}
          <label class="item">
            <input type="checkbox" name="permission" value="{{.Value}}" {{if .Checked}}checked{{end}}>
            <span>
              <strong>{{.Label}}</strong><br>
              <span class="meta">{{.Description}}</span>
            </span>
          </label>
        {{end}}
      </section>
      <section>
        <h2>Hubs</h2>
        {{range .Hubs}}
          <label class="item">
            <input type="checkbox" name="hub_id" value="{{.ID}}" {{if .Checked}}checked{{end}} {{if .Disabled}}disabled{{end}}>
            <span>
              <strong>{{.Name}}</strong><br>
              <span class="meta">{{.HubType}} hub &middot; {{.Role}} &middot; {{.MemoryCount}} memories &middot; {{.CapabilityLabel}}</span>
            </span>
          </label>
        {{end}}
      </section>
      {{if .NotRequested}}
      <section>
        <h2>Not Requested</h2>
        <ul class="muted">
          {{range .NotRequested}}<li>{{.}}</li>{{end}}
        </ul>
      </section>
      {{end}}
      <div class="actions">
        <button type="submit" name="decision" value="deny">Cancel</button>
        <button type="submit" name="decision" value="approve">Connect</button>
      </div>
    </form>
  </main>
</body>
</html>`))

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
