package urlvalidation

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Option configures URL validation behavior.
type Option func(*validationConfig)

type validationConfig struct {
	allowPrivate bool
}

// AllowPrivateIPs disables the private IP check. Use only in tests.
func AllowPrivateIPs() Option {
	return func(c *validationConfig) {
		c.allowPrivate = true
	}
}

// ValidateWebhookURL checks that a URL is safe for use as a webhook or hook
// callback endpoint. It rejects private/loopback IPs to prevent SSRF attacks.
func ValidateWebhookURL(rawURL string, opts ...Option) error {
	var cfg validationConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Only allow HTTPS and HTTP schemes.
	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && scheme != "http" {
		return fmt.Errorf("URL scheme %q not allowed; use http or https", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL must have a hostname")
	}

	// Resolve the hostname to check for private IPs.
	ips, err := net.LookupHost(host)
	if err != nil {
		return fmt.Errorf("cannot resolve hostname %q: %w", host, err)
	}

	if !cfg.allowPrivate {
		for _, ipStr := range ips {
			ip := net.ParseIP(ipStr)
			if ip == nil {
				continue
			}
			if isPrivateIP(ip) {
				return fmt.Errorf("URL resolves to private/reserved IP %s", ipStr)
			}
		}
	}

	return nil
}

// isPrivateIP returns true if the IP is in a private, loopback, link-local,
// or other reserved range that should not be used for outbound webhooks.
func isPrivateIP(ip net.IP) bool {
	privateRanges := []struct {
		network *net.IPNet
	}{
		{parseCIDR("10.0.0.0/8")},
		{parseCIDR("172.16.0.0/12")},
		{parseCIDR("192.168.0.0/16")},
		{parseCIDR("127.0.0.0/8")},
		{parseCIDR("169.254.0.0/16")},   // link-local
		{parseCIDR("::1/128")},           // IPv6 loopback
		{parseCIDR("fc00::/7")},          // IPv6 unique local
		{parseCIDR("fe80::/10")},         // IPv6 link-local
		{parseCIDR("100.64.0.0/10")},     // shared address space (CGN)
		{parseCIDR("0.0.0.0/8")},         // "this" network
		{parseCIDR("192.0.0.0/24")},      // IETF protocol assignments
		{parseCIDR("192.0.2.0/24")},      // TEST-NET-1
		{parseCIDR("198.51.100.0/24")},   // TEST-NET-2
		{parseCIDR("203.0.113.0/24")},    // TEST-NET-3
		{parseCIDR("198.18.0.0/15")},     // benchmarking
		{parseCIDR("224.0.0.0/4")},       // multicast
		{parseCIDR("240.0.0.0/4")},       // reserved
		{parseCIDR("255.255.255.255/32")}, // broadcast
	}

	for _, r := range privateRanges {
		if r.network.Contains(ip) {
			return true
		}
	}
	return false
}

func parseCIDR(s string) *net.IPNet {
	_, network, err := net.ParseCIDR(s)
	if err != nil {
		panic(fmt.Sprintf("invalid CIDR %q: %v", s, err))
	}
	return network
}
