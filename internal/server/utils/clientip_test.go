package utils

import (
	"testing"
)

func TestParseTrustedProxies(t *testing.T) {
	ranges, err := ParseTrustedProxies("")
	if err != nil || len(ranges) != len(loopbackProxyRanges) {
		t.Fatalf("empty spec should mean loopback only; got %v, %v", ranges, err)
	}
	ranges, err = ParseTrustedProxies("private")
	if err != nil || len(ranges) != len(privateProxyRanges) {
		t.Fatalf("private should add private ranges; got %v, %v", ranges, err)
	}
	ranges, err = ParseTrustedProxies("none")
	if err != nil || len(ranges) != 0 {
		t.Fatalf("none should trust nothing; got %v, %v", ranges, err)
	}
	ranges, err = ParseTrustedProxies("203.0.113.7, 198.51.100.0/24, bogus")
	if err == nil {
		t.Fatal("expected an error for the invalid entry")
	}
	if len(ranges) != 2 {
		t.Fatalf("valid entries should still parse; got %v", ranges)
	}
}

func TestClientIPFrom(t *testing.T) {
	private, _ := ParseTrustedProxies("private")
	cloud, _ := ParseTrustedProxies("private,198.51.100.0/24")

	tests := []struct {
		name    string
		remote  string
		xff     []string
		trusted string
		want    string
	}{
		{"direct client ignores forged header", "203.0.113.9:5000", []string{"1.2.3.4"}, "private", "203.0.113.9"},
		{"behind local proxy uses appended hop", "127.0.0.1:5000", []string{"203.0.113.9"}, "private", "203.0.113.9"},
		{"client-prepended entries are skipped", "127.0.0.1:5000", []string{"1.2.3.4, 203.0.113.9"}, "private", "203.0.113.9"},
		{"several headers are joined in order", "10.0.0.2:5000", []string{"1.2.3.4", "203.0.113.9"}, "private", "203.0.113.9"},
		{"chain of trusted proxies", "10.0.0.2:5000", []string{"1.2.3.4, 203.0.113.9, 198.51.100.4"}, "cloud", "203.0.113.9"},
		{"untrusted proxy in chain stops the walk", "10.0.0.2:5000", []string{"1.2.3.4, 198.51.100.4"}, "private", "198.51.100.4"},
		{"malformed hop falls back to peer", "127.0.0.1:5000", []string{"1.2.3.4, not-an-ip"}, "private", "127.0.0.1"},
		{"proxy without header", "127.0.0.1:5000", nil, "private", "127.0.0.1"},
		{"ipv6 peer", "[2001:db8::1]:443", []string{"1.2.3.4"}, "private", "2001:db8::1"},
		{"ipv4-mapped hop is normalized", "127.0.0.1:5000", []string{"::ffff:203.0.113.9"}, "private", "203.0.113.9"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trusted := private
			if tt.trusted == "cloud" {
				trusted = cloud
			}
			if got := clientIPFrom(tt.remote, tt.xff, trusted); got != tt.want {
				t.Fatalf("clientIPFrom() = %q, want %q", got, tt.want)
			}
		})
	}
}

// With the default (loopback) trust, a client reaching the app through a Docker
// published port arrives from the bridge gateway and must not be able to choose
// its IP with a forged header.
func TestDefaultTrustIgnoresHeaderFromDockerGateway(t *testing.T) {
	def, _ := ParseTrustedProxies("")
	if got := clientIPFrom("172.17.0.1:5000", []string{"1.2.3.4"}, def); got != "172.17.0.1" {
		t.Fatalf("got %q; a private peer isn't trusted by default", got)
	}
	if got := clientIPFrom("127.0.0.1:5000", []string{"203.0.113.9"}, def); got != "203.0.113.9" {
		t.Fatalf("got %q; a same-host proxy is trusted by default", got)
	}
}

func TestClientIPFromTrustNone(t *testing.T) {
	none, _ := ParseTrustedProxies("none")
	if got := clientIPFrom("127.0.0.1:5000", []string{"203.0.113.9"}, none); got != "127.0.0.1" {
		t.Fatalf("with no trusted proxies the header must be ignored; got %q", got)
	}
}
