package auth

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/infrastructure/sims/internal/search"
)

func TestNegativeScopeBypass_Penetration(t *testing.T) {
	allowedVC := uuid.MustParse("a0000000-0000-0000-0000-000000000001")
	otherVC := uuid.MustParse("b0000000-0000-0000-0000-000000000002")

	id := &Identity{
		Subject:           "viewer@corp.local",
		Role:              RoleViewer,
		AllowedVCenterIDs: []uuid.UUID{allowedVC},
		AllowedFolderPaths: []string{
			"Datacenters.DC1.vm.Production",
		},
	}

	scope := id.ToScopeConstraint()
	if scope == nil {
		t.Fatalf("expected non-nil scope constraint for restricted viewer")
	}

	attackCases := []struct {
		name        string
		attackNode  *search.FilterNode
		description string
	}{
		{
			name: "OR 1=1 bypass attempt",
			attackNode: &search.FilterNode{
				Op: search.OpOr,
				Operands: []*search.FilterNode{
					{Op: search.OpEQ, Field: "power_state", Value: "poweredOn"},
					{Op: search.OpEQ, Field: "power_state", Value: "poweredOff"},
				},
			},
			description: "Attacker attempts to create a tautology in OR to negate previous clauses",
		},
		{
			name: "Explicit NOT vcenter_id attempt",
			attackNode: &search.FilterNode{
				Op: search.OpNot,
				Operand: &search.FilterNode{
					Op:    search.OpEQ,
					Field: "vcenter_id",
					Value: allowedVC.String(),
				},
			},
			description: "Attacker explicitly queries for resources outside their allowed vcenter",
		},
		{
			name: "Direct query for unallowed vCenter ID",
			attackNode: &search.FilterNode{
				Op:    search.OpEQ,
				Field: "vcenter_id",
				Value: otherVC.String(),
			},
			description: "Attacker specifies another vCenter directly in filter",
		},
		{
			name: "Folder path escaping with parent folder",
			attackNode: &search.FilterNode{
				Op:    search.OpDescendantsOfFolder,
				Field: "folder_path",
				Value: "Datacenters.DC2", // Root of another DC
			},
			description: "Attacker queries for descendants of an unallowed folder hierarchy",
		},
		{
			name: "Deeply nested compound NOT and OR tree",
			attackNode: &search.FilterNode{
				Op: search.OpAnd,
				Operands: []*search.FilterNode{
					{
						Op: search.OpOr,
						Operands: []*search.FilterNode{
							{Op: search.OpEQ, Field: "guest_os", Value: "windows"},
							{
								Op: search.OpNot,
								Operand: &search.FilterNode{
									Op:    search.OpEQ,
									Field: "name",
									Value: "secret-vm",
								},
							},
						},
					},
				},
			},
			description: "Nested compound expressions trying to isolate clauses",
		},
	}

	for _, tc := range attackCases {
		t.Run(tc.name, func(t *testing.T) {
			q, err := search.CompileVMQuery(tc.attackNode, search.CompileOptions{
				Scope: scope,
				Limit: 20,
			})
			if err != nil {
				t.Fatalf("compilation unexpectedly failed: %v", err)
			}

			// Invariant 1: Top-level where clause MUST contain the allowed vcenter restriction
			if !strings.Contains(q.WhereClause, "vm.vcenter_id IN ($1)") {
				t.Fatalf("[%s] scope boundary violated! vcenter_id IN ($1) missing in: %s", tc.name, q.WhereClause)
			}

			// Invariant 2: Top-level where clause MUST contain the allowed folder path restriction
			if !strings.Contains(q.WhereClause, "vm.folder_path <@ $2::ltree") {
				t.Fatalf("[%s] scope boundary violated! folder_path <@ $2::ltree missing in: %s", tc.name, q.WhereClause)
			}

			// Invariant 3: The argument $1 MUST be the user's allowed vCenter
			if len(q.Args) < 2 {
				t.Fatalf("expected at least 2 scope arguments, got %d", len(q.Args))
			}
			if q.Args[0] != allowedVC {
				t.Errorf("arg[0] expected allowedVC %s, got %v", allowedVC, q.Args[0])
			}
			if q.Args[1] != "Datacenters.DC1.vm.Production" {
				t.Errorf("arg[1] expected folder path, got %v", q.Args[1])
			}
		})
	}
}

func TestIdentity_CanAccessVCenter(t *testing.T) {
	vc1 := uuid.MustParse("a0000000-0000-0000-0000-000000000001")
	vc2 := uuid.MustParse("b0000000-0000-0000-0000-000000000002")

	// 1. Admin without restrictions: access all
	adminAll := &Identity{Role: RoleAdmin}
	if !adminAll.CanAccessVCenter(vc1) || !adminAll.CanAccessVCenter(vc2) {
		t.Errorf("admin without restrictions should access any vCenter")
	}

	// 2. Admin with restrictions: access only restricted
	adminRestricted := &Identity{Role: RoleAdmin, AllowedVCenterIDs: []uuid.UUID{vc1}}
	if !adminRestricted.CanAccessVCenter(vc1) {
		t.Errorf("restricted admin should access vc1")
	}
	if adminRestricted.CanAccessVCenter(vc2) {
		t.Errorf("restricted admin should NOT access vc2")
	}

	// 3. User with empty allowed list: FAIL CLOSED (must not allow access)
	userEmpty := &Identity{Role: RoleUser, AllowedVCenterIDs: []uuid.UUID{}}
	if userEmpty.CanAccessVCenter(vc1) {
		t.Errorf("user with empty AllowedVCenterIDs must NOT access vc1 (fail closed)")
	}

	// 4. User with allowed list
	userRestricted := &Identity{Role: RoleUser, AllowedVCenterIDs: []uuid.UUID{vc1}}
	if !userRestricted.CanAccessVCenter(vc1) {
		t.Errorf("user should access vc1")
	}
	if userRestricted.CanAccessVCenter(vc2) {
		t.Errorf("user should NOT access vc2")
	}
}

func TestIdentity_HasRole(t *testing.T) {
	admin := &Identity{Role: RoleAdmin}
	if !admin.HasRole(RoleAdmin) || !admin.HasRole(RoleInventoryAdmin) || !admin.HasRole(RoleUser) || !admin.HasRole(RoleViewer) {
		t.Errorf("admin should satisfy all role checks")
	}

	user := &Identity{Role: RoleUser}
	if user.HasRole(RoleAdmin) || user.HasRole(RoleInventoryAdmin) {
		t.Errorf("user should not satisfy admin role")
	}
	if !user.HasRole(RoleUser) || !user.HasRole(RoleViewer) {
		t.Errorf("user should satisfy user and viewer roles")
	}
}
