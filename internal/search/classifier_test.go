package search

import (
	"testing"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantClass QueryClass
		wantNorm  string
		wantField string
	}{
		{
			name:      "standard UUID",
			input:     "550e8400-e29b-41d4-a716-446655440000",
			wantClass: ClassUUID,
			wantNorm:  "550e8400-e29b-41d4-a716-446655440000",
			wantField: "instance_uuid",
		},
		{
			name:      "uppercase UUID",
			input:     "5024B486-11F2-76BB-9CB9-43C242C1618A",
			wantClass: ClassUUID,
			wantNorm:  "5024b486-11f2-76bb-9cb9-43c242c1618a",
			wantField: "instance_uuid",
		},
		{
			name:      "vSphere MoRef",
			input:     "vm-42",
			wantClass: ClassMoRef,
			wantNorm:  "vm-42",
			wantField: "moref",
		},
		{
			name:      "large vSphere MoRef",
			input:     "vm-1029384",
			wantClass: ClassMoRef,
			wantNorm:  "vm-1029384",
			wantField: "moref",
		},
		{
			name:      "IPv4 address",
			input:     "10.20.10.15",
			wantClass: ClassIPv4,
			wantNorm:  "10.20.10.15",
			wantField: "guest_ip_address",
		},
		{
			name:      "IPv4 CIDR prefix",
			input:     "192.168.1.0/24",
			wantClass: ClassIPv4,
			wantNorm:  "192.168.1.0/24",
			wantField: "guest_ip_address",
		},
		{
			name:      "IPv6 address",
			input:     "2001:db8::1",
			wantClass: ClassIPv6,
			wantNorm:  "2001:db8::1",
			wantField: "guest_ip_address",
		},
		{
			name:      "IPv6 link-local",
			input:     "fe80::1ff:fe00:1",
			wantClass: ClassIPv6,
			wantNorm:  "fe80::1ff:fe00:1",
			wantField: "guest_ip_address",
		},
		{
			name:      "MAC address with colons",
			input:     "00:50:56:A1:B2:C3",
			wantClass: ClassMAC,
			wantNorm:  "00:50:56:a1:b2:c3",
			wantField: "mac_address",
		},
		{
			name:      "MAC address with hyphens",
			input:     "00-50-56-a1-b2-c3",
			wantClass: ClassMAC,
			wantNorm:  "00:50:56:a1:b2:c3",
			wantField: "mac_address",
		},
		{
			name:      "MAC address bare 12 hex",
			input:     "005056a1b2c3",
			wantClass: ClassMAC,
			wantNorm:  "00:50:56:a1:b2:c3",
			wantField: "mac_address",
		},
		{
			name:      "Exact name in quotes",
			input:     `"prod-db-master"`,
			wantClass: ClassExactName,
			wantNorm:  "prod-db-master",
			wantField: "name",
		},
		{
			name:      "Hostname FQDN",
			input:     "web-frontend-01.corp.internal",
			wantClass: ClassHostname,
			wantNorm:  "web-frontend-01.corp.internal",
			wantField: "guest_host_name",
		},
		{
			name:      "General substring / trigram",
			input:     "prod-cluster",
			wantClass: ClassSubstring,
			wantNorm:  "prod-cluster",
			wantField: "name",
		},
		{
			name:      "Empty string",
			input:     "   ",
			wantClass: ClassSubstring,
			wantNorm:  "",
			wantField: "name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.input)
			if got.Class != tt.wantClass {
				t.Errorf("Classify(%q).Class = %v, want %v", tt.input, got.Class, tt.wantClass)
			}
			if got.Normalized != tt.wantNorm {
				t.Errorf("Classify(%q).Normalized = %v, want %v", tt.input, got.Normalized, tt.wantNorm)
			}
			if got.TargetField != tt.wantField {
				t.Errorf("Classify(%q).TargetField = %v, want %v", tt.input, got.TargetField, tt.wantField)
			}
		})
	}
}
