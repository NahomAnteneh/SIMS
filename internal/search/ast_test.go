package search

import (
	"encoding/json"
	"testing"
)

func TestFilterASTValidation(t *testing.T) {
	tests := []struct {
		name    string
		node    *FilterNode
		wantErr bool
		errCode string
	}{
		{
			name: "valid simple EQ",
			node: &FilterNode{
				Op:    OpEQ,
				Field: "power_state",
				Value: "poweredOn",
			},
			wantErr: false,
		},
		{
			name: "valid AND compound",
			node: &FilterNode{
				Op: OpAnd,
				Operands: []*FilterNode{
					{Op: OpEQ, Field: "power_state", Value: "poweredOn"},
					{Op: OpContains, Field: "name", Value: "web"},
				},
			},
			wantErr: false,
		},
		{
			name: "valid NOT",
			node: &FilterNode{
				Op: OpNot,
				Operand: &FilterNode{
					Op:    OpEQ,
					Field: "connection_state",
					Value: "disconnected",
				},
			},
			wantErr: false,
		},
		{
			name: "valid CIDR_CONTAINS",
			node: &FilterNode{
				Op:    OpCIDRContains,
				Field: "guest_ip_address",
				Value: "10.0.0.0/16",
			},
			wantErr: false,
		},
		{
			name: "valid VLAN_OVERLAP",
			node: &FilterNode{
				Op:    OpVLANOverlap,
				Field: "vlan_ids",
				Value: "10-20, 100",
			},
			wantErr: false,
		},
		{
			name: "valid DESCENDANTS_OF_FOLDER",
			node: &FilterNode{
				Op:    OpDescendantsOfFolder,
				Field: "folder_path",
				Value: "Datacenters.DC1.vm.Production",
			},
			wantErr: false,
		},
		{
			name: "valid HAS_TAG with category and tag",
			node: &FilterNode{
				Op:       OpHasTag,
				Category: "Environment",
				Tag:      "Production",
			},
			wantErr: false,
		},
		{
			name: "valid HAS_TAG with category only",
			node: &FilterNode{
				Op:       OpHasTag,
				Category: "Environment",
			},
			wantErr: false,
		},
		{
			name: "valid HAS_ATTRIBUTE with name and value",
			node: &FilterNode{
				Op:    OpHasAttribute,
				Name:  "CostCenter",
				Value: "CC-100",
			},
			wantErr: false,
		},
		{
			name: "valid HAS_ATTRIBUTE with name only",
			node: &FilterNode{
				Op:   OpHasAttribute,
				Name: "CostCenter",
			},
			wantErr: false,
		},
		{
			name: "invalid unknown field",
			node: &FilterNode{
				Op:    OpEQ,
				Field: "non_existent_column",
				Value: "foo",
			},
			wantErr: true,
			errCode: "invalid_filter",
		},
		{
			name: "invalid operator on field (CONTAINS on integer cpu_cores)",
			node: &FilterNode{
				Op:    OpContains,
				Field: "cpu_cores",
				Value: "4",
			},
			wantErr: true,
			errCode: "invalid_filter",
		},
		{
			name: "invalid CIDR syntax",
			node: &FilterNode{
				Op:    OpCIDRContains,
				Field: "guest_ip_address",
				Value: "999.999.999.999/99",
			},
			wantErr: true,
			errCode: "invalid_filter",
		},
		{
			name: "invalid VLAN spec",
			node: &FilterNode{
				Op:    OpVLANOverlap,
				Field: "vlan_ids",
				Value: "5000", // > 4094
			},
			wantErr: true,
			errCode: "invalid_filter",
		},
		{
			name: "invalid ltree characters in DESCENDANTS_OF_FOLDER",
			node: &FilterNode{
				Op:    OpDescendantsOfFolder,
				Field: "folder_path",
				Value: "Datacenters/DC1; DROP TABLE virtual_machines;",
			},
			wantErr: true,
			errCode: "invalid_filter",
		},
		{
			name: "invalid empty operands in AND",
			node: &FilterNode{
				Op:       OpAnd,
				Operands: []*FilterNode{},
			},
			wantErr: true,
			errCode: "invalid_filter",
		},
		{
			name: "invalid missing category in HAS_TAG",
			node: &FilterNode{
				Op:  OpHasTag,
				Tag: "Production",
			},
			wantErr: true,
			errCode: "invalid_filter",
		},
		{
			name: "invalid missing name in HAS_ATTRIBUTE",
			node: &FilterNode{
				Op:    OpHasAttribute,
				Value: "CC-100",
			},
			wantErr: true,
			errCode: "invalid_filter",
		},
		{
			name:    "nil filter node",
			node:    nil,
			wantErr: true,
			errCode: "invalid_filter",
		},
		{
			name: "recursion depth exceeds MaxASTDepth",
			node: func() *FilterNode {
				root := &FilterNode{Op: OpEQ, Field: "power_state", Value: "poweredOn"}
				// Nest 12 NOT nodes deep
				curr := root
				for i := 0; i < 12; i++ {
					curr = &FilterNode{Op: OpNot, Operand: curr}
				}
				return curr
			}(),
			wantErr: true,
			errCode: "query_too_complex",
		},
		{
			name: "node count exceeds MaxASTNodes",
			node: func() *FilterNode {
				operands := make([]*FilterNode, 60)
				for i := 0; i < 60; i++ {
					operands[i] = &FilterNode{Op: OpEQ, Field: "power_state", Value: "poweredOn"}
				}
				return &FilterNode{Op: OpOr, Operands: operands}
			}(),
			wantErr: true,
			errCode: "query_too_complex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.node)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if err != nil && tt.errCode != "" {
				if valErr, ok := err.(*ValidationError); ok {
					if valErr.Code != tt.errCode {
						t.Errorf("expected code %s, got %s", tt.errCode, valErr.Code)
					}
				} else {
					t.Errorf("expected *ValidationError, got %T", err)
				}
			}
		})
	}
}

func TestFilterAST_JSONSerialization(t *testing.T) {
	jsonPayload := `{
		"op": "AND",
		"operands": [
			{"op": "EQ", "field": "power_state", "value": "poweredOn"},
			{"op": "DESCENDANTS_OF_FOLDER", "field": "folder_path", "value": "Datacenters.DC1.vm"},
			{"op": "HAS_TAG", "category": "Environment", "tag": "Production"}
		]
	}`

	var node FilterNode
	if err := json.Unmarshal([]byte(jsonPayload), &node); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if err := Validate(&node); err != nil {
		t.Fatalf("Validate() failed on deserialized node: %v", err)
	}

	if len(node.Operands) != 3 {
		t.Fatalf("expected 3 operands, got %d", len(node.Operands))
	}
}
