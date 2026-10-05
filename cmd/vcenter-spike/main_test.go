package main

import (
	"testing"
)

func TestNormalizeHostPort(t *testing.T) {
	tests := []struct {
		name        string
		host        string
		defaultPort string
		want        string
	}{
		{
			name:        "empty host",
			host:        "",
			defaultPort: "443",
			want:        "",
		},
		{
			name:        "host without port",
			host:        "vcenter.corp.example.com",
			defaultPort: "443",
			want:        "vcenter.corp.example.com:443",
		},
		{
			name:        "host with custom port",
			host:        "vcenter.corp.example.com:8989",
			defaultPort: "443",
			want:        "vcenter.corp.example.com:8989",
		},
		{
			name:        "ipv4 without port",
			host:        "192.168.1.100",
			defaultPort: "443",
			want:        "192.168.1.100:443",
		},
		{
			name:        "ipv4 with custom port",
			host:        "192.168.1.100:9443",
			defaultPort: "443",
			want:        "192.168.1.100:9443",
		},
		{
			name:        "ipv6 without port",
			host:        "[2001:db8::1]",
			defaultPort: "443",
			want:        "[2001:db8::1]:443",
		},
		{
			name:        "ipv6 with port",
			host:        "[2001:db8::1]:8443",
			defaultPort: "443",
			want:        "[2001:db8::1]:8443",
		},
		{
			name:        "whitespace trimmed",
			host:        "  vcenter.local  ",
			defaultPort: "443",
			want:        "vcenter.local:443",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeHostPort(tt.host, tt.defaultPort)
			if got != tt.want {
				t.Errorf("normalizeHostPort(%q, %q) = %q; want %q", tt.host, tt.defaultPort, got, tt.want)
			}
		})
	}
}
