// Package safefetch implements an SSRF-safe HTTP client for
// fetching arbitrary user-supplied URLs (chat fetch_url tool,
// future plugin tools, anywhere else the agent or user is the
// URL author).
//
// **Threat model.** The agent / user / API caller picks the
// URL. We have to assume the URL is hostile: it may target a
// private internal address (10.0.0.0/8, 192.168.0.0/16,
// 169.254.0.0/16 link-local, etc.), the loopback (127.0.0.1,
// ::1), a multicast/broadcast/unspecified IP, the cloud-metadata
// service (169.254.169.254 — IMDSv1 leaks credentials), or one
// of memax's own internal hostnames (api.memax.app's internal
// service mesh, the worker pool, anything that trusts caller
// position). All of these are blocked.
//
// **Defense in depth.** Two stages of validation, both required
// because either alone has a known bypass:
//
//   1. URL-level checks before the dial. Scheme (only http /
//      https), userinfo (rejected — credentials in URLs are
//      basically always SSRF bait), host shape (literal
//      IP-address hosts are validated against the blocklist
//      directly; hostnames have to wait for DNS).
//   2. IP-level checks at dial time, AFTER DNS resolution,
//      via a custom net.Dialer.ControlContext. This catches
//      DNS-rebinding attacks where the hostname's first DNS
//      response was public but a subsequent re-resolve points
//      at 127.0.0.1 — and it catches A-records that always
//      resolve to private IPs. The ControlContext gets the
//      resolved address Go is about to dial; we parse the IP
//      and reject if it falls in any blocked range.
//
// **Redirect re-validation.** http.Client.CheckRedirect re-runs
// URL-level checks (scheme, userinfo, host blocklist) on every
// hop. The dialer's ControlContext re-runs IP-level checks
// every time a new connection opens (which is every redirect
// since we don't reuse a connection across hostnames). Codex
// Phase 3.6b3f v3 explicitly called out the dial-time IP check
// (not just hostname) as the requirement to catch DNS rebind.
//
// **What this package does NOT do.** No content parsing
// (callers do their own HTML / markdown / JSON extraction); no
// caching (callers handle); no per-user rate limiting (chat
// adapter handles); no auth (this is for fetching public web
// content, not authenticated APIs).
package safefetch

import (
	"net"
	"strings"
)

// blockedCIDRs is the canonical list of IP ranges we refuse to
// dial. Includes:
//
//   - Loopback (127.0.0.0/8, ::1/128) — local services.
//   - Link-local (169.254.0.0/16, fe80::/10) — includes the
//     AWS/GCP/Azure metadata service at 169.254.169.254.
//   - Private IPv4 (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16)
//     — corporate / VPC internals.
//   - Unique-local IPv6 (fc00::/7) — IPv6 equivalent.
//   - Multicast (224.0.0.0/4, ff00::/8) — never makes sense for
//     HTTP.
//   - Unspecified (0.0.0.0/8, ::/128) — "this network" + the
//     IPv6 unspecified sentinel.
//   - Reserved future (240.0.0.0/4) — RFC 1112 future reserved,
//     Linux kernel won't route here normally; treat as blocked.
//   - Documentation/TEST-NET (192.0.2.0/24, 198.51.100.0/24,
//     203.0.113.0/24, 2001:db8::/32) — should never appear in
//     real traffic.
//   - Carrier-grade NAT (100.64.0.0/10) — RFC 6598; can route
//     to ISP-side NAT translators.
//   - 6to4 relay anycast (192.88.99.0/24) — deprecated but
//     still has live infrastructure.
//
// The list intentionally omits NOTHING that even moderately
// well-known security guidance flags as "don't dial from a
// server SSRF-vulnerable code path." Adding a range to this
// list is cheap; removing one is not.
var blockedCIDRs = func() []*net.IPNet {
	raw := []string{
		// IPv4 loopback / unspecified / link-local / private / multicast /
		// docs / future / 6to4 / CGNAT.
		"0.0.0.0/8",
		"10.0.0.0/8",
		"100.64.0.0/10",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"172.16.0.0/12",
		"192.0.0.0/24",
		"192.0.2.0/24",
		"192.88.99.0/24",
		"192.168.0.0/16",
		"198.18.0.0/15",
		"198.51.100.0/24",
		"203.0.113.0/24",
		"224.0.0.0/4",
		"240.0.0.0/4",
		"255.255.255.255/32",
		// IPv6 loopback / unspecified / link-local / unique-local /
		// multicast / docs. NOTE: ::ffff:0:0/96 (the IPv4-mapped
		// IPv6 prefix) is intentionally NOT blocked here — that
		// CIDR matches every IPv4 address, including public ones.
		// IsBlockedIP promotes mapped addresses to 4-byte form
		// via ip.To4() before the CIDR scan, so the embedded
		// IPv4 gets re-checked against the IPv4 blocklist —
		// which catches ::ffff:127.0.0.1 and ::ffff:10.0.0.1
		// the same way it catches the literal v4 addresses.
		"::/128",
		"::1/128",
		"64:ff9b::/96", // IPv4/IPv6 translation.
		"100::/64",      // Discard prefix.
		"2001::/23",     // IETF protocol assignments.
		"2001:db8::/32",
		"fc00::/7",
		"fe80::/10",
		"ff00::/8",
	}
	nets := make([]*net.IPNet, 0, len(raw))
	for _, c := range raw {
		_, ipNet, err := net.ParseCIDR(c)
		if err != nil {
			panic("safefetch: invalid CIDR in blocked list: " + c + ": " + err.Error())
		}
		nets = append(nets, ipNet)
	}
	return nets
}()

// IsBlockedIP reports whether the IP falls in any of the
// blocked ranges. Exported for callers that want to validate an
// IP they obtained outside the package (e.g. tests or future
// admin tools).
func IsBlockedIP(ip net.IP) bool {
	if ip == nil {
		// nil IP is "unknown / not parseable" — fail closed.
		return true
	}
	// IPv4-mapped IPv6 (::ffff:1.2.3.4) — extract the embedded
	// IPv4 and re-check. Without this, a hostile DNS response
	// of "::ffff:127.0.0.1" sneaks past the IPv4 loopback check.
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	for _, n := range blockedCIDRs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// defaultBlockedHosts are hostnames the SSRF check rejects
// regardless of their DNS resolution. memax-owned production
// hosts are in here because:
//
//   - api.memax.app sits behind our own auth + would expose a
//     server-position credential to a hostile prompt.
//   - The internal admin / worker / metrics endpoints aren't
//     publicly addressable but the hostname might be — and
//     even if DNS rejects, fetching them wastes resources.
//
// Callers can extend the list via Options.BlockedHosts. The
// match is a case-insensitive suffix match: "memax.app" blocks
// "api.memax.app", "internal.memax.app", "memax.app" itself,
// and ".memax.app" with any subdomain. Bare-domain matches
// also block exact equality.
var defaultBlockedHosts = []string{
	"memax.app",
	"memax.dev",
	// memax.com is reserved-but-not-active; include defensively.
	"memax.com",
}

// IsBlockedHost reports whether the host matches any of the
// blocklist entries. Match is case-insensitive suffix-or-equal
// — "memax.app" blocks "api.memax.app" too.
func IsBlockedHost(host string, extras []string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return true
	}
	all := make([]string, 0, len(defaultBlockedHosts)+len(extras))
	all = append(all, defaultBlockedHosts...)
	all = append(all, extras...)
	for _, blocked := range all {
		blocked = strings.ToLower(strings.TrimSpace(blocked))
		if blocked == "" {
			continue
		}
		if host == blocked {
			return true
		}
		// Suffix match with leading dot so "memax.app" blocks
		// "*.memax.app" but doesn't accidentally block
		// "notmemax.app".
		if strings.HasSuffix(host, "."+blocked) {
			return true
		}
	}
	return false
}
