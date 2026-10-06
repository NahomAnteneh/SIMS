package search

import (
	"net/netip"
	"regexp"
	"strings"
)

// QueryClass represents the detected category of a search query string.
type QueryClass string

const (
	ClassUUID      QueryClass = "uuid"
	ClassMoRef     QueryClass = "moref"
	ClassIPv4      QueryClass = "ipv4"
	ClassIPv6      QueryClass = "ipv6"
	ClassMAC       QueryClass = "mac"
	ClassExactName QueryClass = "exact_name"
	ClassHostname  QueryClass = "hostname"
	ClassSubstring QueryClass = "substring"
)

var (
	// Standard UUID regex (8-4-4-4-12 hex).
	uuidRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

	// vSphere VM Managed Object Reference regex (e.g. vm-42, vm-1002).
	morefRegex = regexp.MustCompile(`^vm-\d+$`)

	// Standard MAC address regex: 6 octets separated by colons or hyphens, or 12 hex digits.
	macColonRegex  = regexp.MustCompile(`^([0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}$`)
	macHyphenRegex = regexp.MustCompile(`^([0-9a-fA-F]{2}-){5}[0-9a-fA-F]{2}$`)
	macBareRegex   = regexp.MustCompile(`^[0-9a-fA-F]{12}$`)

	// Hostname / FQDN pattern: valid DNS labels separated by dots (e.g. web-01.corp.local).
	// Must contain at least one dot, or start with letters/digits and end with standard domain ending.
	hostnameRegex = regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$`)
)

// Classification contains the classified type and normalized parameters for query execution.
type Classification struct {
	Class       QueryClass `json:"class"`
	RawQuery    string     `json:"raw_query"`
	Normalized  string     `json:"normalized"`
	TargetField string     `json:"target_field"`
}

// Classify inspects the raw query string and determines the optimal indexed query route.
func Classify(raw string) Classification {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Classification{
			Class:       ClassSubstring,
			RawQuery:    raw,
			Normalized:  "",
			TargetField: "name",
		}
	}

	// 1. Exact Name (explicitly quoted string, e.g. "web-prod-01")
	if strings.HasPrefix(trimmed, `"`) && strings.HasSuffix(trimmed, `"`) && len(trimmed) >= 2 {
		unquoted := strings.Trim(trimmed, `"`)
		return Classification{
			Class:       ClassExactName,
			RawQuery:    raw,
			Normalized:  unquoted,
			TargetField: "name",
		}
	}

	// 2. UUID (instance_uuid, bios_uuid, or internal id)
	if uuidRegex.MatchString(trimmed) {
		return Classification{
			Class:       ClassUUID,
			RawQuery:    raw,
			Normalized:  strings.ToLower(trimmed),
			TargetField: "instance_uuid",
		}
	}

	// 3. MoRef (vm-123)
	if morefRegex.MatchString(trimmed) {
		return Classification{
			Class:       ClassMoRef,
			RawQuery:    raw,
			Normalized:  trimmed,
			TargetField: "moref",
		}
	}

	// 4. IPv4 / CIDR
	if addr, err := netip.ParseAddr(trimmed); err == nil {
		if addr.Is4() {
			return Classification{
				Class:       ClassIPv4,
				RawQuery:    raw,
				Normalized:  addr.String(),
				TargetField: "guest_ip_address",
			}
		}
		if addr.Is6() {
			return Classification{
				Class:       ClassIPv6,
				RawQuery:    raw,
				Normalized:  addr.String(),
				TargetField: "guest_ip_address",
			}
		}
	}
	if prefix, err := netip.ParsePrefix(trimmed); err == nil {
		if prefix.Addr().Is4() {
			return Classification{
				Class:       ClassIPv4,
				RawQuery:    raw,
				Normalized:  prefix.String(),
				TargetField: "guest_ip_address",
			}
		}
		if prefix.Addr().Is6() {
			return Classification{
				Class:       ClassIPv6,
				RawQuery:    raw,
				Normalized:  prefix.String(),
				TargetField: "guest_ip_address",
			}
		}
	}

	// 5. MAC Address
	if normMAC, ok := parseAndNormalizeMAC(trimmed); ok {
		return Classification{
			Class:       ClassMAC,
			RawQuery:    raw,
			Normalized:  normMAC,
			TargetField: "mac_address",
		}
	}

	// 6. Hostname / FQDN (contains dots and valid DNS domain name)
	if hostnameRegex.MatchString(trimmed) {
		return Classification{
			Class:       ClassHostname,
			RawQuery:    raw,
			Normalized:  strings.ToLower(trimmed),
			TargetField: "guest_host_name",
		}
	}

	// 7. Substring fallback (trigram indexed search on name)
	return Classification{
		Class:       ClassSubstring,
		RawQuery:    raw,
		Normalized:  trimmed,
		TargetField: "name",
	}
}

func parseAndNormalizeMAC(s string) (string, bool) {
	if macColonRegex.MatchString(s) {
		return strings.ToLower(s), true
	}
	if macHyphenRegex.MatchString(s) {
		return strings.ToLower(strings.ReplaceAll(s, "-", ":")), true
	}
	if macBareRegex.MatchString(s) {
		sLower := strings.ToLower(s)
		var b strings.Builder
		for i := 0; i < 12; i += 2 {
			if i > 0 {
				b.WriteByte(':')
			}
			b.WriteString(sLower[i : i+2])
		}
		return b.String(), true
	}
	return "", false
}
