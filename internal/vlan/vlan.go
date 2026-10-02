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

// ParseSpec parses a VLAN specification string (e.g., "100", "100-110", "100-110,200,300-305")
// into a normalized, sorted, merged list of non-overlapping inclusive Ranges.
// It never expands ranges into individual integers.
func ParseSpec(spec string) ([]Range, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
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

	return merged, nil
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

// NormalizeSpec takes any valid VLAN specification and produces the canonical PostgreSQL int4multirange literal.
func NormalizeSpec(spec string) (string, error) {
	ranges, err := ParseSpec(spec)
	if err != nil {
		return "", err
	}
	return ToPostgresLiteral(ranges), nil
}
