// Package oauthredirect decides which redirect URIs an OAuth client may
// register and be sent to: web clients' https URIs, and native apps'
// loopback and private-use scheme URIs (RFC 8252). The rules are data
// (redirects.json), which the web app's consent page reads too, so it
// follows exactly what the server accepts.
//
// Native apps:
//
//   - Loopback (§7.3): http on 127.0.0.1, [::1] or localhost. The app
//     listens on whatever port is free, so a registered loopback redirect
//     matches on scheme, host, path and query, with any port (VS Code
//     registers 33418 and falls back to a random one).
//   - Private-use schemes (§7.1): a reverse domain name, such as
//     com.example.app:/oauth, or one of the native agents' own schemes
//     (Cursor's cursor://, VS Code's vscode://). Matched exactly.
//
// Another app on the device can claim a private scheme, or listen on a
// loopback port, and receive the authorization code (§8.1). That is why
// every client must use PKCE (S256): a code is worthless without the
// verifier, which never leaves the app that started the request
// (internal/handler: Authorize requires the challenge, the token endpoint
// the verifier).
package oauthredirect

import (
	_ "embed"
	"encoding/json"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

//go:embed redirects.json
var rulesJSON []byte

type rulesFile struct {
	LoopbackHosts  []string          `json:"loopback_hosts"`
	NativeSchemes  map[string]string `json:"native_schemes"`
	RefusedSchemes []string          `json:"refused_schemes"`
}

var rules = func() rulesFile {
	var r rulesFile
	if err := json.Unmarshal(rulesJSON, &r); err != nil {
		panic("oauthredirect: redirects.json: " + err.Error())
	}
	return r
}()

// scheme is RFC 3986's scheme grammar.
var scheme = regexp.MustCompile(`^[a-z][a-z0-9+.-]*$`)

// isLoopback reports whether host (as url.URL.Hostname gives it, without
// brackets) is a loopback host.
func isLoopback(host string) bool {
	return slices.Contains(rules.LoopbackHosts, strings.ToLower(host))
}

// Valid reports whether a client may register uri as a redirect URI: an
// absolute URI, without userinfo or a fragment, whose scheme is https,
// http on a loopback host, a native agent's scheme or a reverse-domain
// private scheme, and never a refused one.
func Valid(uri string) bool {
	if strings.Contains(uri, "#") || strings.ContainsAny(uri, " \t\r\n\\") {
		return false
	}
	u, err := url.Parse(uri)
	if err != nil || u.User != nil {
		return false
	}
	s := strings.ToLower(u.Scheme)
	if !scheme.MatchString(s) || slices.Contains(rules.RefusedSchemes, s) {
		return false
	}
	switch {
	case s == "https":
		return u.Hostname() != ""
	case s == "http":
		return isLoopback(u.Hostname())
	case rules.NativeSchemes[s] != "", strings.Contains(s, "."):
		// Something must follow "scheme:".
		return u.Opaque != "" || u.Host != "" || (u.Path != "" && u.Path != "/")
	}
	return false
}

// Matches reports whether a presented redirect URI is the registered one:
// the same string, or, for a loopback redirect, the same scheme, host,
// path and query with any port (RFC 8252 §7.3). An empty path and "/" are
// the same path.
func Matches(presented, registered string) bool {
	if presented == registered {
		return Valid(presented)
	}
	p, err1 := url.Parse(presented)
	r, err2 := url.Parse(registered)
	if err1 != nil || err2 != nil || !Valid(presented) || !Valid(registered) {
		return false
	}
	if !strings.EqualFold(p.Scheme, "http") || !strings.EqualFold(r.Scheme, "http") ||
		!isLoopback(p.Hostname()) || !strings.EqualFold(p.Hostname(), r.Hostname()) {
		return false
	}
	path := func(u *url.URL) string {
		if u.EscapedPath() == "" {
			return "/"
		}
		return u.EscapedPath()
	}
	return path(p) == path(r) && p.RawQuery == r.RawQuery
}

// Allowed reports whether a presented redirect URI matches any registered one.
func Allowed(presented string, registered []string) bool {
	for _, r := range registered {
		if Matches(presented, r) {
			return true
		}
	}
	return false
}
