package vlan_test

import (
	"testing"

	"github.com/infrastructure/sims/internal/vlan"
)

func TestParseSpecAndNormalize(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "single VLAN",
			input:    "100",
			expected: "{[100,100]}",
		},
		{
			name:     "single range",
			input:    "100-110",
			expected: "{[100,110]}",
		},
		{
			name:     "multiple ranges with spaces",
			input:    "100-110, 200, 300-305",
			expected: "{[100,110],[200,200],[300,305]}",
		},
		{
			name:     "adjacent ranges merged",
			input:    "100-105, 106-110",
			expected: "{[100,110]}",
		},
		{
			name:     "overlapping ranges merged",
			input:    "100-110, 105-120",
			expected: "{[100,120]}",
		},
		{
			name:     "unordered inputs normalize identically",
			input:    "300-305, 100-110, 200",
			expected: "{[100,110],[200,200],[300,305]}",
		},
		{
			name:     "duplicate single vlan merged",
			input:    "100, 100, 100",
			expected: "{[100,100]}",
		},
		{
			name:     "contained range absorbed",
			input:    "100-200, 150-160",
			expected: "{[100,200]}",
		},
		{
			name:     "trunk range (full 1-4094)",
			input:    "1-4094",
			expected: "{[1,4094]}",
		},
		{
			name:     "empty input",
			input:    "",
			expected: "{}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := vlan.NormalizeSpec(tt.input)
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.input, err)
			}
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestEquivalentInputsNormalizeIdentically(t *testing.T) {
	// Equivalent representations of VLAN 100 to 110, 200, 300 to 305
	inputs := []string{
		"100-110,200,300-305",
		"200, 100-110, 300-305",
		"100-105, 106-110, 200, 300-305",
		"300-305, 200, 100-102, 103-110",
	}

	expected := "{[100,110],[200,200],[300,305]}"
	for _, in := range inputs {
		got, err := vlan.NormalizeSpec(in)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != expected {
			t.Errorf("input %q gave %q, expected %q", in, got, expected)
		}
	}
}

func TestInvalidInputs(t *testing.T) {
	invalids := []string{
		"abc",
		"100-50",   // start > end
		"-10",      // negative
		"5000",     // > 4095
		"100-5000", // > 4095
		"100-110-120",
	}

	for _, inv := range invalids {
		_, err := vlan.NormalizeSpec(inv)
		if err == nil {
			t.Errorf("expected error for invalid input %q, got nil", inv)
		}
	}
}
