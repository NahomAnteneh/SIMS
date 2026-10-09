package vlan

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Range represents an inclusive integer range [Start, End].
type Range struct {
	Start int32
	End   int32
}

// ParseSpec parses a VLAN specification string (e.g., "100", "100-110", "100-110,200,300-305",
// or PostgreSQL multirange literals like "{[100,101)}" and "{[10,21),[100,201)}")
// into a normalized, sorted, merged list of non-overlapping inclusive Ranges.
// It never expands ranges into individual integers.
func ParseSpec(spec string) ([]Range, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" || spec == "{}" {
		return nil, nil
	}

	// Unwrap outer curly braces if present (e.g. from PostgreSQL int4multirange literal)
	if strings.HasPrefix(spec, "{") && strings.HasSuffix(spec, "}") {
		spec = strings.TrimSpace(spec[1 : len(spec)-1])
		if spec == "" {
			return nil, nil
		}
	}

	// If contains bracket range characters, parse using bracket parser
	if strings.ContainsAny(spec, "[]()") {
		return parseBracketRanges(spec)
	}

	parts := strings.Split(spec, ",")
	rawRanges := make([]Range, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if strings.Contains(part, "-") {
			bounds := strings.Split(part, "-")
			if len(bounds) != 2 {
				return nil, fmt.Errorf("invalid range format: %q", part)
			}
			start, err := strconv.ParseInt(strings.TrimSpace(bounds[0]), 10, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid start bound in range %q: %w", part, err)
			}
			end, err := strconv.ParseInt(strings.TrimSpace(bounds[1]), 10, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid end bound in range %q: %w", part, err)
			}
			if start > end {
				return nil, fmt.Errorf("invalid range %q: start %d greater than end %d", part, start, end)
			}
			if start < 0 || end > 4095 {
				return nil, fmt.Errorf("range %q out of valid VLAN bounds [0, 4095]", part)
			}
			rawRanges = append(rawRanges, Range{Start: int32(start), End: int32(end)})
		} else {
			val, err := strconv.ParseInt(part, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid vlan id %q: %w", part, err)
			}
			if val < 0 || val > 4095 {
				return nil, fmt.Errorf("vlan id %d out of valid bounds [0, 4095]", val)
			}
			rawRanges = append(rawRanges, Range{Start: int32(val), End: int32(val)})
		}
	}

	if len(rawRanges) == 0 {
		return nil, nil
	}

	return mergeRanges(rawRanges), nil
}

func parseBracketRanges(spec string) ([]Range, error) {
	var rawRanges []Range

	for len(spec) > 0 {
		startIdx := strings.IndexAny(spec, "[(")
		if startIdx == -1 {
			break
		}
		endIdx := strings.IndexAny(spec[startIdx:], "])")
		if endIdx == -1 {
			return nil, fmt.Errorf("unclosed range in %q", spec)
		}
		endIdx += startIdx

		segment := spec[startIdx : endIdx+1]
		spec = spec[endIdx+1:]

		incStart := segment[0] == '['
		incEnd := segment[len(segment)-1] == ']'
		inner := segment[1 : len(segment)-1]
		bounds := strings.Split(inner, ",")
		if len(bounds) != 2 {
			return nil, fmt.Errorf("invalid range bounds %q", segment)
		}
		s64, err := strconv.ParseInt(strings.TrimSpace(bounds[0]), 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid range start in %q: %w", segment, err)
		}
		e64, err := strconv.ParseInt(strings.TrimSpace(bounds[1]), 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid range end in %q: %w", segment, err)
		}
		start := int32(s64)
		end := int32(e64)
		if !incStart {
			start++
		}
		if !incEnd {
			end--
		}
		if start > end {
			return nil, fmt.Errorf("empty range %q (normalized %d-%d)", segment, start, end)
		}
		if start < 0 || end > 4095 {
			return nil, fmt.Errorf("range %q out of valid VLAN bounds [0, 4095]", segment)
		}
		rawRanges = append(rawRanges, Range{Start: start, End: end})
	}

	if len(rawRanges) == 0 {
		return nil, nil
	}

	return mergeRanges(rawRanges), nil
}

func mergeRanges(rawRanges []Range) []Range {
	// Sort by start, then end
	sort.Slice(rawRanges, func(i, j int) bool {
		if rawRanges[i].Start == rawRanges[j].Start {
			return rawRanges[i].End < rawRanges[j].End
		}
		return rawRanges[i].Start < rawRanges[j].Start
	})

	// Merge overlapping or adjacent ranges
	merged := make([]Range, 0, len(rawRanges))
	current := rawRanges[0]

	for i := 1; i < len(rawRanges); i++ {
		next := rawRanges[i]
		// Adjacent: current.End + 1 >= next.Start
		// Overlapping: current.End >= next.Start
		if current.End+1 >= next.Start {
			if next.End > current.End {
				current.End = next.End
			}
		} else {
			merged = append(merged, current)
			current = next
		}
	}
	merged = append(merged, current)

	return merged
}

// ToPostgresLiteral formats a slice of inclusive Ranges into a PostgreSQL int4multirange literal string.
// E.g. {[100,110],[200,200]}
func ToPostgresLiteral(ranges []Range) string {
	if len(ranges) == 0 {
		return "{}"
	}
	var b strings.Builder
	b.WriteString("{")
	for i, r := range ranges {
		if i > 0 {
			b.WriteString(",")
		}
		// PostgreSQL discrete ranges accept inclusive [start,end] brackets
		fmt.Fprintf(&b, "[%d,%d]", r.Start, r.End)
	}
	b.WriteString("}")
	return b.String()
}

// FormatHuman returns a human-readable VLAN string like "100" or "10-20, 100-200".
func FormatHuman(ranges []Range) string {
	if len(ranges) == 0 {
		return ""
	}
	parts := make([]string, len(ranges))
	for i, r := range ranges {
		if r.Start == r.End {
			parts[i] = fmt.Sprintf("%d", r.Start)
		} else {
			parts[i] = fmt.Sprintf("%d-%d", r.Start, r.End)
		}
	}
	return strings.Join(parts, ", ")
}

// FormatHumanSpec parses any spec (human or postgres multirange) and formats it as human readable.
func FormatHumanSpec(spec string) string {
	ranges, err := ParseSpec(spec)
	if err != nil || len(ranges) == 0 {
		return spec
	}
	return FormatHuman(ranges)
}

// NormalizeSpec takes any valid VLAN specification and produces the canonical PostgreSQL int4multirange literal.
func NormalizeSpec(spec string) (string, error) {
	ranges, err := ParseSpec(spec)
	if err != nil {
		return "", err
	}
	return ToPostgresLiteral(ranges), nil
}
