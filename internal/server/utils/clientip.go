package utils

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"sync"
)

// loopbackProxyRanges are trusted by default: a reverse proxy on the same host.
var loopbackProxyRanges = []string{"127.0.0.0/8", "::1/128"}

// privateProxyRanges add private networks, for a proxy in another container or on
// the LAN. They aren't trusted by default: with a Docker published port, clients
// arrive from the bridge gateway, a private address, and could then forge
// X-Forwarded-For.
var privateProxyRanges = []string{
	"127.0.0.0/8", "::1/128",
	"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
	"fc00::/7",
}

// ParseTrustedProxies reads a TRUSTED_PROXIES value: a comma-separated list of IPs,
// CIDR ranges, "loopback" (the default), "private" (loopback and private ranges),
// or "none". Invalid entries are returned as an error alongside the ranges that
// did parse.
func ParseTrustedProxies(spec string) ([]netip.Prefix, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		spec = "loopback"
	}
	var out []netip.Prefix
	var bad []string
	for _, raw := range strings.Split(spec, ",") {
		entry := strings.ToLower(strings.TrimSpace(raw))
		switch entry {
		case "":
			continue
		case "none":
			return nil, nil
		case "loopback":
			for _, cidr := range loopbackProxyRanges {
				out = append(out, netip.MustParsePrefix(cidr))
			}
			continue
		case "private":
			for _, cidr := range privateProxyRanges {
				out = append(out, netip.MustParsePrefix(cidr))
			}
			continue
		}
		if p, err := netip.ParsePrefix(entry); err == nil {
			out = append(out, p.Masked())
			continue
		}
		if a, err := netip.ParseAddr(entry); err == nil {
			out = append(out, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
			continue
		}
		bad = append(bad, raw)
	}
	if len(bad) > 0 {
		return out, fmt.Errorf("TRUSTED_PROXIES: ignoring invalid entries %q", bad)
	}
	return out, nil
}

var trustedProxies struct {
	once   sync.Once
	ranges []netip.Prefix
}

func loadTrustedProxies() []netip.Prefix {
	trustedProxies.once.Do(func() {
		ranges, err := ParseTrustedProxies(os.Getenv("TRUSTED_PROXIES"))
		if err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		trustedProxies.ranges = ranges
	})
	return trustedProxies.ranges
}

// CheckNetworkConfig reads TRUSTED_PROXIES and PUBLIC_URL at startup so a bad
// value is reported right away instead of on the first request.
func CheckNetworkConfig() {
	loadTrustedProxies()
	if PublicURL() == "" {
		fmt.Println("Warning: PUBLIC_URL is not set. Links in emails (password reset, new accounts) are built " +
			"from the request's Host header; set PUBLIC_URL to your site's public address.")
	}
}

func isTrusted(a netip.Addr, trusted []netip.Prefix) bool {
	a = a.Unmap()
	for _, p := range trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// ClientIP returns the address of the client that made the request. It trusts
// X-Forwarded-For only when the direct peer is a trusted proxy, and then takes the
// rightmost address that isn't itself a trusted proxy. Entries further left were
// written by the client and can say anything.
func ClientIP(r *http.Request) string {
	xff := r.Header.Values("X-Forwarded-For")
	trusted := loadTrustedProxies()
	if len(xff) > 0 {
		if peer, ok := peerAddr(r.RemoteAddr); ok && !isTrusted(peer, trusted) {
			warnUntrustedForwarder(peer)
		}
	}
	return clientIPFrom(r.RemoteAddr, xff, trusted)
}

// PeerIsTrustedProxy reports whether the request came straight from a proxy listed
// in TRUSTED_PROXIES, so its forwarding headers can be believed.
func PeerIsTrustedProxy(r *http.Request) bool {
	peer, ok := peerAddr(r.RemoteAddr)
	return ok && isTrusted(peer, loadTrustedProxies())
}

func peerAddr(remoteAddr string) (netip.Addr, bool) {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return a.Unmap(), true
}

var untrustedForwarderWarned sync.Once

// warnUntrustedForwarder logs, once, that X-Forwarded-For came from an address
// that isn't a trusted proxy. Usually that's a reverse proxy the admin hasn't
// listed yet, and every user then shares the proxy's rate limits.
func warnUntrustedForwarder(peer netip.Addr) {
	untrustedForwarderWarned.Do(func() {
		fmt.Printf("Warning: got X-Forwarded-For from %s, which isn't in TRUSTED_PROXIES, so it was ignored. "+
			"If %s is your reverse proxy, set TRUSTED_PROXIES=%s (or a range that covers it); until then "+
			"all its users share one rate limit.\n", peer, peer, peer)
	})
}

func clientIPFrom(remoteAddr string, xff []string, trusted []netip.Prefix) string {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	peer = peer.Unmap()
	if !isTrusted(peer, trusted) {
		return peer.String()
	}
	var hops []string
	for _, header := range xff {
		hops = append(hops, strings.Split(header, ",")...)
	}
	// Walk from the right. Each trusted hop vouches for the entry to its left;
	// the first untrusted entry is the client.
	nearest := peer
	for i := len(hops) - 1; i >= 0; i-- {
		hop, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			// A malformed hop means nothing to its left can be trusted.
			return nearest.String()
		}
		hop = hop.Unmap()
		if !isTrusted(hop, trusted) {
			return hop.String()
		}
		nearest = hop
	}
	// Every hop was a trusted proxy, or there were none.
	return nearest.String()
}
