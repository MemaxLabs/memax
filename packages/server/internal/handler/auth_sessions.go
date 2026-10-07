package handler

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/auth"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/sessions"
	"github.com/MemaxLabs/memax/packages/server/internal/websurface"
)

// Sign-in sessions (internal/sessions; plan 25 §5.15): every login starts
// one, its refresh token is hashed and rotates on every use, and its
// access tokens name it (the sid claim).

// sessionTTL is how long a session lives from sign-in. Refreshing rotates
// its token but doesn't extend it.
const sessionTTL = 30 * 24 * time.Hour

// accessTTL is an access token's lifetime: how long a revoked session's
// last access token keeps working at most.
const accessTTL = time.Hour

// SetSessions sets the sessions store (shared with /v2's sessions list).
func (h *AuthHandler) SetSessions(s *sessions.Store) { h.sessions = s }

// Sessions is the sessions store, made from the pool on first use.
func (h *AuthHandler) Sessions() *sessions.Store { return h.sessionStore() }

func (h *AuthHandler) sessionStore() *sessions.Store {
	h.sessionsOnce.Do(func() {
		if h.sessions == nil {
			h.sessions = sessions.New(h.pool)
		}
	})
	return h.sessions
}

// SetWebSurface lets the web app's server say, signed, where a browser
// signing in or refreshing is (websurface.ClientInfo). Nil records what
// the API sees.
func (h *AuthHandler) SetWebSurface(v *websurface.Verifier) { h.web = v }

// SetClientAddress sets how a request's client address (and city, when
// the edge says) is read: the rate limiter's view of which proxy header
// to trust.
func (h *AuthHandler) SetClientAddress(fn func(*http.Request) (ip, city string)) {
	h.clientAddress = fn
}

// where is where the client behind r is: what the web app's server says,
// signed, for a browser; otherwise what the API sees.
func (h *AuthHandler) where(r *http.Request) sessions.Where {
	if ci, ok := h.web.ClientInfo(r); ok {
		return sessions.Where{IP: ci.IP, City: ci.City, UserAgent: ci.UserAgent}
	}
	w := sessions.Where{UserAgent: r.UserAgent()}
	if h.clientAddress != nil {
		w.IP, w.City = h.clientAddress(r)
	} else if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		w.IP = host
	}
	return w
}

// sessionStart is what a new session is.
type sessionStart struct {
	kind      sessions.Kind
	surface   string
	agentName string
	grantID   string
	// client names the client when the request's user agent can't (the
	// device's own words, an MCP client's registered name).
	client string
	// issuer and audience bind an MCP grant's access tokens (RFC 8707).
	issuer   string
	audience []string
}

// startSession starts a session for userID and returns its first pair.
func (h *AuthHandler) startSession(r *http.Request, userID string, st sessionStart) (*model.TokenPair, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return nil, fmt.Errorf("start session: user %q: %w", userID, err)
	}
	w := h.where(r)
	client := st.client
	if client == "" {
		client = describeClient(st.kind, w.UserAgent)
	}
	is, err := h.sessionStore().Issue(r.Context(), sessions.Start{
		UserID: uid, Kind: st.kind, Surface: st.surface, AgentName: st.agentName, GrantID: st.grantID,
		Client: client, Where: w, TTL: sessionTTL,
	})
	if err != nil {
		return nil, err
	}
	access, err := h.accessTokenFor(is.Session, st.agentName, st.issuer, st.audience)
	if err != nil {
		return nil, err
	}
	pair := tokenPair(access, is, h.sessionStore().Now())
	return &pair, nil
}

// accessTokenFor signs an access token for a session: a grant's (bound to
// its resource when issuer and audience are given), a legacy agent
// token's, or a person's with the session's surface. Every one names the
// session (sid).
func (h *AuthHandler) accessTokenFor(ss sessions.Session, agentName, issuer string, audience []string) (string, error) {
	c := auth.Claims{Sub: ss.UserID.String(), Sid: ss.ID.String()}
	switch {
	case ss.GrantID != "":
		if agentName == "" {
			agentName = ss.AgentName
		}
		c.AgentName, c.GrantID, c.Iss, c.Aud = agentName, ss.GrantID, issuer, audience
	case ss.AgentName != "":
		c.AgentName = ss.AgentName
	default:
		// A refreshed token keeps the surface its sign-in was for.
		c.Surface = ss.Surface
	}
	return auth.Sign(c, h.jwtSecret, accessTTL)
}

// tokenPair is the answer to a sign-in or a refresh.
func tokenPair(access string, is *sessions.Issued, now time.Time) model.TokenPair {
	return model.TokenPair{
		AccessToken:      access,
		RefreshToken:     is.RefreshToken,
		ExpiresIn:        int(accessTTL / time.Second),
		RefreshExpiresIn: max(0, int(is.Session.ExpiresAt.Sub(now)/time.Second)),
	}
}

// refreshFailure is how /v1/auth/refresh answers a token it can't refresh.
// Every failure is 401, which every client takes as "sign in again".
func refreshFailure(err error) (status int, code, message string) {
	switch {
	case errors.Is(err, sessions.ErrUnknown):
		return http.StatusUnauthorized, "invalid_token", "Invalid refresh token."
	case errors.Is(err, sessions.ErrExpired):
		return http.StatusUnauthorized, "expired_token", "Refresh token expired. Please log in again."
	case errors.Is(err, sessions.ErrGrantRevoked):
		return http.StatusUnauthorized, "invalid_token", "Authorization grant is no longer valid."
	case errors.Is(err, sessions.ErrReused):
		return http.StatusUnauthorized, "session_revoked",
			"This refresh token was already used, so the session was signed out to be safe. Sign in again."
	case errors.Is(err, sessions.ErrRevoked):
		return http.StatusUnauthorized, "session_revoked", "This session was signed out. Sign in again."
	}
	return http.StatusInternalServerError, "internal", "Failed to refresh the session."
}

var cliPattern = regexp.MustCompile(`^memax-cli/(\S+)`)

// describeClient names a session's client from its user agent, in a few
// words for the sessions list ("Chrome on macOS", "memax CLI 0.9.0").
func describeClient(kind sessions.Kind, ua string) string {
	if m := cliPattern.FindStringSubmatch(ua); m != nil {
		return "memax CLI " + m[1]
	}
	switch kind {
	case sessions.KindCLI, sessions.KindDevice:
		return "memax CLI"
	case sessions.KindMCP:
		return "MCP client"
	}
	// Most specific first: Edge and Opera say Chrome too, and Chrome says
	// Safari.
	browser := ""
	for _, b := range []struct{ marker, name string }{
		{"Edg/", "Edge"}, {"OPR/", "Opera"}, {"Firefox/", "Firefox"}, {"FxiOS/", "Firefox"},
		{"CriOS/", "Chrome"}, {"Chrome/", "Chrome"}, {"Safari/", "Safari"},
	} {
		if strings.Contains(ua, b.marker) {
			browser = b.name
			break
		}
	}
	os := ""
	for _, c := range []struct{ marker, name string }{
		{"iPhone", "iOS"}, {"iPad", "iPadOS"}, {"Android", "Android"}, {"Mac OS X", "macOS"},
		{"Windows", "Windows"}, {"CrOS", "ChromeOS"}, {"Linux", "Linux"},
	} {
		if strings.Contains(ua, c.marker) {
			os = c.name
			break
		}
	}
	switch {
	case browser != "" && os != "":
		return browser + " on " + os
	case browser != "":
		return browser
	case os != "":
		return "A browser on " + os
	}
	return "A browser"
}

// TouchSessions records, at most every few minutes per session, that a
// person's session was used (the sessions list's "last used"). It runs
// after RequireAuth, off the request path.
func TouchSessions(store *sessions.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if store == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if id, err := uuid.Parse(GetGrant(r).SessionID); err == nil {
				store.Touch(id)
			}
			next.ServeHTTP(w, r)
		})
	}
}
