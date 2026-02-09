package urlvalidation

import (
	"net"
	"testing"
)

func TestValidateWebhookURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{name: "valid https", url: "https://example.com/webhook", wantErr: false},
		{name: "valid http", url: "http://example.com/webhook", wantErr: false},
		{name: "localhost", url: "http://localhost/webhook", wantErr: true},
		{name: "loopback ip", url: "http://127.0.0.1/webhook", wantErr: true},
		{name: "private 10.x", url: "http://10.0.0.1/webhook", wantErr: true},
		{name: "private 172.16.x", url: "http://172.16.0.1/webhook", wantErr: true},
		{name: "private 192.168.x", url: "http://192.168.1.1/webhook", wantErr: true},
		{name: "ftp scheme", url: "ftp://example.com/file", wantErr: true},
		{name: "file scheme", url: "file:///etc/passwd", wantErr: true},
		{name: "no scheme", url: "example.com/webhook", wantErr: true},
		{name: "empty host", url: "http:///path", wantErr: true},
		{name: "ipv6 loopback", url: "http://[::1]/webhook", wantErr: true},
		{name: "link-local", url: "http://169.254.1.1/webhook", wantErr: true},
		{name: "cgn range", url: "http://100.64.0.1/webhook", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWebhookURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateWebhookURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		ip      string
		private bool
	}{
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"172.32.0.0", false},
		{"192.168.0.1", true},
		{"127.0.0.1", true},
		{"169.254.1.1", true},
		{"0.0.0.0", true},
		{"224.0.0.1", true},
		{"255.255.255.255", true},
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("invalid test IP: %s", tt.ip)
			}
			if isPrivateIP(ip) != tt.private {
				t.Errorf("isPrivateIP(%q) = %v, want %v", tt.ip, !tt.private, tt.private)
			}
		})
	}
}
