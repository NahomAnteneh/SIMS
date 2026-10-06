package collector

import (
	"testing"
)

func TestTargetFSM_Transitions(t *testing.T) {
	tests := []struct {
		name    string
		from    string
		to      string
		allowed bool
	}{
		{"available to assigned", TargetStateAvailable, TargetStateAssigned, true},
		{"available to running (disallowed)", TargetStateAvailable, TargetStateRunning, false},
		{"assigned to running", TargetStateAssigned, TargetStateRunning, true},
		{"assigned to degraded", TargetStateAssigned, TargetStateDegraded, true},
		{"assigned to reassigning", TargetStateAssigned, TargetStateReassigning, true},
		{"running to degraded", TargetStateRunning, TargetStateDegraded, true},
		{"running to reassigning", TargetStateRunning, TargetStateReassigning, true},
		{"running to available (disallowed)", TargetStateRunning, TargetStateAvailable, false},
		{"degraded to running", TargetStateDegraded, TargetStateRunning, true},
		{"degraded to reassigning", TargetStateDegraded, TargetStateReassigning, true},
		{"reassigning to available", TargetStateReassigning, TargetStateAvailable, true},
		{"reassigning to assigned", TargetStateReassigning, TargetStateAssigned, true},
		{"reassigning to running (disallowed)", TargetStateReassigning, TargetStateRunning, false},
		{"self transition allowed", TargetStateRunning, TargetStateRunning, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			allowed := IsValidTransition(tc.from, tc.to)
			if allowed != tc.allowed {
				t.Fatalf("expected IsValidTransition(%s, %s) = %v, got %v", tc.from, tc.to, tc.allowed, allowed)
			}
		})
	}
}
