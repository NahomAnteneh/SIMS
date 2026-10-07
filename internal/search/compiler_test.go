package search

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestSQLCompiler_Determinism(t *testing.T) {
	// Two ASTs with identical logical filters but different operand ordering
	tree1 := &FilterNode{
		Op: OpAnd,
		Operands: []*FilterNode{
			{Op: OpEQ, Field: "power_state", Value: "poweredOn"},
			{Op: OpContains, Field: "name", Value: "db"},
			{Op: OpDescendantsOfFolder, Field: "folder_path", Value: "Datacenters.DC1.vm"},
		},
	}

	tree2 := &FilterNode{
		Op: OpAnd,
		Operands: []*FilterNode{
			{Op: OpDescendantsOfFolder, Field: "folder_path", Value: "Datacenters.DC1.vm"},
			{Op: OpContains, Field: "name", Value: "db"},
			{Op: OpEQ, Field: "power_state", Value: "poweredOn"},
		},
	}

	opts := CompileOptions{
		Limit: 20,
	}

	q1, err := CompileVMQuery(tree1, opts)
	if err != nil {
		t.Fatalf("failed to compile tree1: %v", err)
	}

	q2, err := CompileVMQuery(tree2, opts)
	if err != nil {
		t.Fatalf("failed to compile tree2: %v", err)
	}

	if q1.SQL != q2.SQL {
		t.Errorf("expected deterministic SQL, got mismatch:\nq1: %s\nq2: %s", q1.SQL, q2.SQL)
	}

	if len(q1.Args) != len(q2.Args) {
		t.Fatalf("args length mismatch: %d vs %d", len(q1.Args), len(q2.Args))
	}

	for i := range q1.Args {
		if q1.Args[i] != q2.Args[i] {
			t.Errorf("arg[%d] mismatch: %v vs %v", i, q1.Args[i], q2.Args[i])
		}
	}
}

func TestSQLCompiler_ParameterizationNoInterpolation(t *testing.T) {
	maliciousInput := "'; DROP TABLE virtual_machines; --"
	tree := &FilterNode{
		Op:    OpContains,
		Field: "name",
		Value: maliciousInput,
	}

	q, err := CompileVMQuery(tree, CompileOptions{})
	if err != nil {
		t.Fatalf("CompileVMQuery failed: %v", err)
	}

	// The malicious input MUST NOT appear directly in the SQL string
	if strings.Contains(q.SQL, "DROP TABLE") {
		t.Fatalf("SQL injection vulnerability detected! Raw input found in SQL: %s", q.SQL)
	}

	// The input should be wrapped as an argument
	found := false
	for _, arg := range q.Args {
		if arg == "%"+maliciousInput+"%" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected parameterized arg containing %q, got: %v", maliciousInput, q.Args)
	}
}

func TestSQLCompiler_ScopeInjection(t *testing.T) {
	vcID := uuid.MustParse("a0000000-0000-0000-0000-000000000001")
	scope := &ScopeConstraint{
		AllowedVCenterIDs:  []uuid.UUID{vcID},
		AllowedFolderPaths: []string{"Datacenters.DC1.vm"},
	}

	// Malicious user attempts an OR 1=1 style bypass or NOT
	maliciousFilter := &FilterNode{
		Op: OpOr,
		Operands: []*FilterNode{
			{Op: OpEQ, Field: "power_state", Value: "poweredOn"},
			{Op: OpEQ, Field: "power_state", Value: "poweredOff"},
		},
	}

	q, err := CompileVMQuery(maliciousFilter, CompileOptions{Scope: scope})
	if err != nil {
		t.Fatalf("CompileVMQuery failed: %v", err)
	}

	// Both scope conditions must be top-level ANDed
	if !strings.Contains(q.WhereClause, "vm.vcenter_id IN ($1)") {
		t.Errorf("expected vcenter_id scope in where clause, got: %s", q.WhereClause)
	}
	if !strings.Contains(q.WhereClause, "vm.folder_path <@ $2::ltree") {
		t.Errorf("expected folder_path scope in where clause, got: %s", q.WhereClause)
	}

	// Must be enclosed such that OR cannot leak out of the scope
	expectedClause := "vm.deleted_at IS NULL AND vm.vcenter_id IN ($1) AND vm.folder_path <@ $2::ltree AND (vm.power_state = $3 OR vm.power_state = $4)"
	if q.WhereClause != expectedClause {
		t.Errorf("unexpected where clause:\nGot:  %s\nWant: %s", q.WhereClause, expectedClause)
	}
}

func TestSQLCompiler_EmptyScopeFailsClosed(t *testing.T) {
	// A non-admin user with 0 permitted vCenters and 0 permitted folders must fail closed
	emptyScope := &ScopeConstraint{
		AllowedVCenterIDs:  []uuid.UUID{},
		AllowedFolderPaths: []string{},
	}
	q, err := CompileVMQuery(nil, CompileOptions{Scope: emptyScope})
	if err != nil {
		t.Fatalf("CompileVMQuery failed: %v", err)
	}
	expectedClause := "vm.deleted_at IS NULL AND 1 = 0"
	if q.WhereClause != expectedClause {
		t.Errorf("unexpected where clause:\nGot:  %s\nWant: %s", q.WhereClause, expectedClause)
	}
}

func TestSQLCompiler_AllOperators(t *testing.T) {
	tests := []struct {
		name      string
		node      *FilterNode
		expectSub string
	}{
		{
			name:      "EQ",
			node:      &FilterNode{Op: OpEQ, Field: "power_state", Value: "poweredOn"},
			expectSub: "vm.power_state = $1",
		},
		{
			name:      "NE",
			node:      &FilterNode{Op: OpNE, Field: "connection_state", Value: "disconnected"},
			expectSub: "vm.connection_state != $1",
		},
		{
			name:      "IN",
			node:      &FilterNode{Op: OpIN, Field: "power_state", Values: []any{"poweredOn", "suspended"}},
			expectSub: "vm.power_state IN ($1, $2)",
		},
		{
			name:      "PREFIX",
			node:      &FilterNode{Op: OpPrefix, Field: "name", Value: "app-"},
			expectSub: "vm.name LIKE $1",
		},
		{
			name:      "CIDR_CONTAINS",
			node:      &FilterNode{Op: OpCIDRContains, Field: "guest_ip_address", Value: "10.0.0.0/24"},
			expectSub: "vm.guest_ip_address <<= $1::inet",
		},
		{
			name:      "CIDR_OVERLAP",
			node:      &FilterNode{Op: OpCIDROverlap, Field: "guest_ip_address", Value: "10.0.0.0/16"},
			expectSub: "vm.guest_ip_address && $1::inet",
		},
		{
			name:      "VLAN_OVERLAP",
			node:      &FilterNode{Op: OpVLANOverlap, Field: "vlan_ids", Value: "100"},
			expectSub: "EXISTS (SELECT 1 FROM vm_network_interfaces vnic WHERE vnic.vm_id = vm.id AND vnic.vlan_ids && $1::int4multirange)",
		},
		{
			name:      "DESCENDANTS_OF_FOLDER",
			node:      &FilterNode{Op: OpDescendantsOfFolder, Field: "folder_path", Value: "Datacenters.DC1.vm"},
			expectSub: "vm.folder_path <@ $1::ltree",
		},
		{
			name:      "HAS_TAG with tag",
			node:      &FilterNode{Op: OpHasTag, Category: "Env", Tag: "Prod"},
			expectSub: "EXISTS (SELECT 1 FROM vm_tags vt WHERE vt.vm_id = vm.id AND vt.category_name = $1 AND vt.tag_name = $2)",
		},
		{
			name:      "HAS_TAG category only",
			node:      &FilterNode{Op: OpHasTag, Category: "Env"},
			expectSub: "EXISTS (SELECT 1 FROM vm_tags vt WHERE vt.vm_id = vm.id AND vt.category_name = $1)",
		},
		{
			name:      "HAS_ATTRIBUTE with value",
			node:      &FilterNode{Op: OpHasAttribute, Name: "Owner", Value: "Finance"},
			expectSub: "EXISTS (SELECT 1 FROM vm_custom_attributes vca WHERE vca.vm_id = vm.id AND vca.name = $1 AND vca.value = $2)",
		},
		{
			name:      "HAS_ATTRIBUTE name only",
			node:      &FilterNode{Op: OpHasAttribute, Name: "Owner"},
			expectSub: "EXISTS (SELECT 1 FROM vm_custom_attributes vca WHERE vca.vm_id = vm.id AND vca.name = $1)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := CompileVMQuery(tt.node, CompileOptions{})
			if err != nil {
				t.Fatalf("CompileVMQuery failed: %v", err)
			}
			if !strings.Contains(q.WhereClause, tt.expectSub) {
				t.Errorf("expected substring %q in where clause:\nGot: %s", tt.expectSub, q.WhereClause)
			}
		})
	}
}

func TestSQLCompiler_KeysetPagination(t *testing.T) {
	cursor := &CursorValues{
		SortField: "updated_at",
		LastValue: "2026-10-06T08:00:00Z",
		LastID:    "550e8400-e29b-41d4-a716-446655440000",
	}

	opts := CompileOptions{
		Sort: SortSpec{
			Field:     "updated_at",
			Direction: "desc",
		},
		Cursor: cursor,
		Limit:  50,
	}

	q, err := CompileVMQuery(nil, opts)
	if err != nil {
		t.Fatalf("CompileVMQuery failed: %v", err)
	}

	if !strings.Contains(q.WhereClause, "(vm.updated_at, vm.id) < ($1, $2::uuid)") {
		t.Errorf("expected keyset cursor clause for updated_at DESC, got: %s", q.WhereClause)
	}
	if !strings.Contains(q.SQL, "ORDER BY vm.updated_at DESC, vm.id DESC") {
		t.Errorf("expected ORDER BY updated_at DESC, got: %s", q.SQL)
	}
	if !strings.Contains(q.SQL, "LIMIT $3") {
		t.Errorf("expected LIMIT $3, got: %s", q.SQL)
	}
}

func TestSQLCompiler_LimitProbePreservation(t *testing.T) {
	// 1. When limit = MaxPageLimit + 1 (probe for hasMore at limit=100)
	optsProbe := CompileOptions{
		Limit: MaxPageLimit + 1,
	}
	qProbe, err := CompileVMQuery(nil, optsProbe)
	if err != nil {
		t.Fatalf("CompileVMQuery failed: %v", err)
	}

	// The limit argument must be 101, preserving the +1 probe
	found101 := false
	for _, arg := range qProbe.Args {
		if val, ok := arg.(int); ok && val == MaxPageLimit+1 {
			found101 = true
			break
		}
	}
	if !found101 {
		t.Errorf("expected limit argument %d to be preserved, args: %v", MaxPageLimit+1, qProbe.Args)
	}

	// 2. When limit = MaxPageLimit (100)
	opts100 := CompileOptions{
		Limit: MaxPageLimit,
	}
	q100, err := CompileVMQuery(nil, opts100)
	if err != nil {
		t.Fatalf("CompileVMQuery failed: %v", err)
	}
	found100 := false
	for _, arg := range q100.Args {
		if val, ok := arg.(int); ok && val == MaxPageLimit {
			found100 = true
			break
		}
	}
	if !found100 {
		t.Errorf("expected limit argument %d, args: %v", MaxPageLimit, q100.Args)
	}

	// 3. Excess limit is clamped to MaxPageLimit + 1
	optsExcess := CompileOptions{
		Limit: 500,
	}
	qExcess, err := CompileVMQuery(nil, optsExcess)
	if err != nil {
		t.Fatalf("CompileVMQuery failed: %v", err)
	}
	foundClamped := false
	for _, arg := range qExcess.Args {
		if val, ok := arg.(int); ok && val == MaxPageLimit+1 {
			foundClamped = true
			break
		}
	}
	if !foundClamped {
		t.Errorf("expected excess limit to clamp to %d, args: %v", MaxPageLimit+1, qExcess.Args)
	}
}
