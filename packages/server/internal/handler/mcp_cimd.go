package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MemaxLabs/memax/packages/server/internal/oauthredirect"
	"github.com/MemaxLabs/memax/packages/server/internal/safefetch"
)

// Client ID Metadata Documents (CIMD; MCP 2025-11-25 authorization,
// draft-ietf-oauth-client-id-metadata-document). A client that has one
// sends its URL as client_id; the authorization server fetches the JSON
// document there and trusts the redirect URIs it lists. Claude, Claude
// Code, ChatGPT and VS Code prefer it to dynamic registration, which stays
// for Cursor and older clients. The URL is the agent's verifiable identity:
// it is stored on the agent connection, unlike a self-reported client name.

// cimdRefresh is how long a fetched document is trusted before it's
// fetched again. A redirect URI the cached copy doesn't list triggers a
// fetch at once.
const cimdRefresh = 24 * time.Hour

// cimdMaxBytes bounds a metadata document.
const cimdMaxBytes = 64 << 10

func defaultMetadataFetcher() *safefetch.Client {
	return safefetch.New(safefetch.Options{
		MaxBodyBytes: cimdMaxBytes,
		Timeout:      5 * time.Second,
		UserAgent:    "memax-oauth/1 (client metadata)",
	})
}

// isMetadataClientID reports whether a client_id is a metadata document
// URL: https, with a path, and no query, fragment or credentials.
func isMetadataClientID(clientID string) bool {
	if !strings.HasPrefix(clientID, "https://") {
		return false
	}
	u, err := url.Parse(clientID)
	return err == nil && u.Host != "" && u.Path != "" && u.Path != "/" &&
		u.RawQuery == "" && u.Fragment == "" && u.User == nil && !strings.Contains(u.Path, "/.") &&
		len(clientID) <= 2048
}

// clientMetadata is the part of a metadata document Memax reads.
type clientMetadata struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	ClientSecret            string   `json:"client_secret"`
}

// resolveMetadataClient returns the client a metadata document describes,
// from the cache in oauth_clients when it is fresh and lists redirectURI,
// otherwise fetched and checked now.
func (h *MCPOAuthHandler) resolveMetadataClient(ctx context.Context, clientID, redirectURI string) (oauthClient, error) {
	var cached oauthClient
	var fetchedAt *time.Time
	err := h.authH.pool.QueryRow(ctx,
		`SELECT client_id, client_name, redirect_uris, metadata_fetched_at
		   FROM oauth_clients WHERE client_id = $1 AND metadata_url = $1`, clientID,
	).Scan(&cached.ClientID, &cached.ClientName, &cached.RedirectURIs, &fetchedAt)
	if err == nil && fetchedAt != nil && time.Since(*fetchedAt) < cimdRefresh && oauthredirect.Allowed(redirectURI, cached.RedirectURIs) {
		return cached, nil
	}

	meta, err := h.fetchClientMetadata(ctx, clientID)
	if err != nil {
		return oauthClient{}, err
	}
	client := oauthClient{ClientID: clientID, ClientName: meta.ClientName, RedirectURIs: meta.RedirectURIs}
	if _, err := h.authH.pool.Exec(ctx, `
		INSERT INTO oauth_clients (client_id, client_name, redirect_uris, metadata_url, metadata_fetched_at, updated_at)
		VALUES ($1, $2, $3, $1, now(), now())
		ON CONFLICT (client_id) DO UPDATE
		   SET client_name = EXCLUDED.client_name, redirect_uris = EXCLUDED.redirect_uris,
		       metadata_url = EXCLUDED.metadata_url, metadata_fetched_at = now(), updated_at = now()`,
		clientID, client.ClientName, client.RedirectURIs); err != nil {
		return oauthClient{}, fmt.Errorf("can't record the client: %w", err)
	}
	return client, nil
}

// fetchClientMetadata fetches and checks a metadata document: it must be
// JSON at exactly that URL (no redirects), name itself as client_id, be a
// public client (no secret; token endpoint auth "none"), and list valid
// redirect URIs and a client name that identifies the agent.
func (h *MCPOAuthHandler) fetchClientMetadata(ctx context.Context, clientID string) (clientMetadata, error) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	res, err := h.fetchMetadata(ctx, clientID)
	if err != nil {
		return clientMetadata{}, errors.New("it couldn't be fetched")
	}
	switch {
	case res.StatusCode != 200:
		return clientMetadata{}, fmt.Errorf("fetching it returned HTTP %d", res.StatusCode)
	case res.FinalURL != clientID:
		return clientMetadata{}, errors.New("it redirects; a metadata document must be served at its client_id")
	case res.Truncated:
		return clientMetadata{}, errors.New("it is too large")
	case res.ContentType != "application/json" && !strings.HasSuffix(res.ContentType, "+json"):
		return clientMetadata{}, errors.New("it isn't JSON")
	}
	var meta clientMetadata
	if err := json.Unmarshal(res.Body, &meta); err != nil {
		return clientMetadata{}, errors.New("it isn't valid JSON")
	}
	meta.ClientName = strings.TrimSpace(meta.ClientName)
	meta.RedirectURIs = uniqueNonEmptyStrings(meta.RedirectURIs)
	switch {
	case meta.ClientID != clientID:
		return clientMetadata{}, errors.New("its client_id doesn't match its URL")
	case meta.ClientSecret != "":
		return clientMetadata{}, errors.New("it carries a client_secret, which a public document must not")
	case meta.TokenEndpointAuthMethod != "" && meta.TokenEndpointAuthMethod != "none":
		return clientMetadata{}, fmt.Errorf("token_endpoint_auth_method %q isn't supported; use none", meta.TokenEndpointAuthMethod)
	case len(meta.RedirectURIs) == 0:
		return clientMetadata{}, errors.New("it lists no redirect_uris")
	}
	for _, uri := range meta.RedirectURIs {
		if !oauthredirect.Valid(uri) {
			return clientMetadata{}, fmt.Errorf("redirect URI %q isn't an https, loopback or native app URL (RFC 8252)", uri)
		}
	}
	if meta.ClientName == "" {
		meta.ClientName = res.FinalURL
		if u, err := url.Parse(clientID); err == nil {
			meta.ClientName = u.Host
		}
	}
	if utf8.RuneCountInString(meta.ClientName) > 200 {
		meta.ClientName = string([]rune(meta.ClientName)[:200])
	}
	return meta, nil
}

// connectionDisplayName is the connection's name from the client's: at
// most 100 characters (ledger.MaxDisplayName), or the agent's name.
func connectionDisplayName(clientName string) string {
	name := strings.TrimSpace(clientName)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		return ""
	}
	return name
}
