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
}

func SignAccessToken(userID string, secret []byte, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := Claims{
		Sub: userID,
		Iat: now.Unix(),
		Exp: now.Add(ttl).Unix(),
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
	now := time.Now()
	claims := Claims{
		Sub:       userID,
		Iat:       now.Unix(),
		Exp:       now.Add(ttl).Unix(),
		AgentName: agentName,
		GrantID:   grantID,
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
