package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Simple HMAC-SHA256 JWT implementation. No external dependency needed.

type Claims struct {
	Sub            string `json:"sub"`                       // user ID
	Exp            int64  `json:"exp"`                       // expiration unix timestamp
	Iat            int64  `json:"iat"`                       // issued at
	AgentName      string `json:"agent_name,omitempty"`      // agent identity (e.g., "claude-ai" for OAuth MCP)
	GrantID        string `json:"grant_id,omitempty"`        // server-side OAuth grant reference
	ImpersonatorID string `json:"impersonator_id,omitempty"` // dev who initiated impersonation
	// Surface is the surface the sign-in was for, SurfaceWeb or SurfaceCLI
	// (migration 030), set by the server when the login completes. Empty
	// on tokens from before it existed, which count as the CLI.
	Surface string `json:"surface,omitempty"`
	// Iss is the issuer (the authorization server's base URL) and Aud the
	// resources the token is for (RFC 8707): an MCP OAuth token works only
	// at the MCP endpoint it was issued for. Older tokens carry neither.
	Iss string   `json:"iss,omitempty"`
	Aud Audience `json:"aud,omitempty"`
	// Sid is the session (internal/sessions) whose refresh token minted the
	// token, so the API can tell which session a request is (the sessions
	// list marks it current, and "revoke all others" spares it). Tokens
	// from before migration 048, API keys and impersonation tokens have
	// none.
	Sid string `json:"sid,omitempty"`
}

// Audience is a JWT aud claim: one string, or an array of them.
type Audience []string

// MarshalJSON writes one audience as a string, several as an array.
func (a Audience) MarshalJSON() ([]byte, error) {
	if len(a) == 1 {
		return json.Marshal(a[0])
	}
	return json.Marshal([]string(a))
}

// UnmarshalJSON reads a string or an array.
func (a *Audience) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*a = Audience{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return fmt.Errorf("aud: %w", err)
	}
	*a = many
	return nil
}

// Contains reports whether resource is one of the audiences, ignoring a
// trailing slash.
func (a Audience) Contains(resource string) bool {
	resource = strings.TrimRight(resource, "/")
	for _, v := range a {
		if strings.TrimRight(v, "/") == resource {
			return true
		}
	}
	return false
}

// The sign-in surfaces.
const (
	// SurfaceWeb: the login's one-time code was delivered to the web app's
	// origin, so the session lives in a browser on the web app.
	SurfaceWeb = "web"
	// SurfaceCLI: anything else (a loopback redirect, or tokens returned in
	// the response).
	SurfaceCLI = "cli"
)

func SignAccessToken(userID string, secret []byte, ttl time.Duration) (string, error) {
	return SignSessionToken(userID, "", secret, ttl)
}

// SignSessionToken issues a person's access token for a sign-in surface
// (SurfaceWeb, SurfaceCLI, or "" for none).
func SignSessionToken(userID, surface string, secret []byte, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := Claims{
		Sub:     userID,
		Iat:     now.Unix(),
		Exp:     now.Add(ttl).Unix(),
		Surface: surface,
	}
	return signJWT(claims, secret)
}

// SignAgentAccessToken issues a JWT with agent identity embedded.
// Used by MCP OAuth flow where the connecting client is a known agent type.
func SignAgentAccessToken(userID, agentName string, secret []byte, ttl time.Duration) (string, error) {
	return SignGrantAccessToken(userID, agentName, "", secret, ttl)
}

// SignGrantAccessToken issues a JWT that references a server-side grant.
// The token stays small and revocation remains immediate because permissions
// are resolved from the database on every authenticated request.
func SignGrantAccessToken(userID, agentName, grantID string, secret []byte, ttl time.Duration) (string, error) {
	return SignBoundGrantAccessToken(userID, agentName, grantID, "", nil, secret, ttl)
}

// SignBoundGrantAccessToken is SignGrantAccessToken with an issuer and the
// resources the token is for (RFC 8707 audience binding, RFC 9068 iss).
func SignBoundGrantAccessToken(userID, agentName, grantID, issuer string, audience []string, secret []byte, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := Claims{
		Sub:       userID,
		Iat:       now.Unix(),
		Exp:       now.Add(ttl).Unix(),
		AgentName: agentName,
		GrantID:   grantID,
		Iss:       issuer,
		Aud:       audience,
	}
	return signJWT(claims, secret)
}

// SignImpersonationToken issues a short-lived JWT with an impersonator claim.
// The token acts as targetUserID but carries the impersonatorID for audit attribution.
func SignImpersonationToken(targetUserID, impersonatorID string, secret []byte, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := Claims{
		Sub:            targetUserID,
		Iat:            now.Unix(),
		Exp:            now.Add(ttl).Unix(),
		ImpersonatorID: impersonatorID,
	}
	return signJWT(claims, secret)
}

// Sign issues a token with the given claims, issued now and expiring
// after ttl (Iat and Exp are set here).
func Sign(c Claims, secret []byte, ttl time.Duration) (string, error) {
	now := time.Now()
	c.Iat = now.Unix()
	c.Exp = now.Add(ttl).Unix()
	return signJWT(c, secret)
}

func VerifyAccessToken(token string, secret []byte) (*Claims, error) {
	claims, err := verifyJWT(token, secret)
	if err != nil {
		return nil, err
	}
	if time.Now().Unix() > claims.Exp {
		return nil, fmt.Errorf("token expired")
	}
	return claims, nil
}

func signJWT(claims Claims, secret []byte) (string, error) {
	header := base64url([]byte(`{"alg":"HS256","typ":"JWT"}`))

	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64url(claimsJSON)

	sigInput := header + "." + payload
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(sigInput))
	sig := base64url(mac.Sum(nil))

	return sigInput + "." + sig, nil
}

func verifyJWT(token string, secret []byte) (*Claims, error) {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid token format")
	}

	sigInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(sigInput))
	expectedSig := base64url(mac.Sum(nil))

	if !hmac.Equal([]byte(parts[2]), []byte(expectedSig)) {
		return nil, fmt.Errorf("invalid signature")
	}

	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid payload: %w", err)
	}

	var claims Claims
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		return nil, fmt.Errorf("invalid claims: %w", err)
	}
	return &claims, nil
}

func base64url(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}
