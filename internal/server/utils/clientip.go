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

// privateProxyRanges are the addresses a reverse proxy on the same host or private
// network connects from. They're trusted by default so typical deployments behind
// nginx, Caddy or a container network keep seeing real client IPs.
var privateProxyRanges = []string{
	"127.0.0.0/8", "::1/128",
	"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
	"fc00::/7",
}

// ParseTrustedProxies reads a TRUSTED_PROXIES value: a comma-separated list of IPs,
// CIDR ranges, the word "private" (loopback and private ranges), or "none". An empty
// value means "private". Invalid entries are returned as an error alongside the
// ranges that did parse.
func ParseTrustedProxies(spec string) ([]netip.Prefix, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		spec = "private"
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
	return clientIPFrom(r.RemoteAddr, r.Header.Values("X-Forwarded-For"), loadTrustedProxies())
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
