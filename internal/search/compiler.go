package search

import (
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/infrastructure/sims/internal/vlan"
)

// ScopeConstraint defines tenant isolation parameters that MUST be enforced.
type ScopeConstraint struct {
	AllowedVCenterIDs  []uuid.UUID
	AllowedFolderPaths []string
}

// SortSpec defines ordering and direction.
type SortSpec struct {
	Field     string // "name", "updated_at", "first_seen_at"
	Direction string // "asc" or "desc"
}

// CursorValues represents cursor values for keyset pagination.
type CursorValues struct {
	SortField string
	LastValue any
	LastID    string
}

// CompileOptions provides configuration for the query compilation.
type CompileOptions struct {
	Scope  *ScopeConstraint
	Sort   SortSpec
	Cursor *CursorValues
	Limit  int
}

// CompiledQuery contains the deterministic parameterized SQL and positional arguments.
type CompiledQuery struct {
	SQL         string
	WhereClause string
	Args        []any
}

// Compiler compiles a validated FilterNode AST into deterministic PostgreSQL queries.
type Compiler struct {
	args     []any
	argIndex int
}

// NewCompiler creates a new Compiler instance.
func NewCompiler() *Compiler {
	return &Compiler{
		args:     make([]any, 0, 8),
		argIndex: 1,
	}
}

// CompileVMQuery produces a complete SELECT query with mandatory active check, scope,
// filter AST, keyset cursor seek, sorting, and limit.
func CompileVMQuery(filter *FilterNode, opts CompileOptions) (*CompiledQuery, error) {
	if filter != nil {
		if err := Validate(filter); err != nil {
			return nil, err
		}
	}

	c := NewCompiler()
	var whereClauses []string

	// 1. Mandatory soft-delete barrier
	whereClauses = append(whereClauses, "vm.deleted_at IS NULL")

	// 2. Mandatory tenant scope injection (cannot be bypassed by user filter)
	if opts.Scope != nil {
		scopeSQL, err := c.compileScope(opts.Scope)
		if err != nil {
			return nil, err
		}
		if scopeSQL != "" {
			whereClauses = append(whereClauses, scopeSQL)
		}
	}

	// 3. User filter AST
	if filter != nil {
		if err := Validate(filter); err != nil {
			return nil, err
		}
		filterSQL, err := c.compileNode(filter)
		if err != nil {
			return nil, err
		}
		if filterSQL != "" {
			whereClauses = append(whereClauses, filterSQL)
		}
	}

	// 4. Keyset pagination seek clause
	sortField := opts.Sort.Field
	if sortField == "" {
		sortField = "name"
	}
	sortDir := strings.ToUpper(opts.Sort.Direction)
	if sortDir != "DESC" {
		sortDir = "ASC"
	}

	if opts.Cursor != nil && opts.Cursor.LastID != "" {
		cursorSQL, err := c.compileCursor(sortField, sortDir, opts.Cursor)
		if err != nil {
			return nil, err
		}
		if cursorSQL != "" {
			whereClauses = append(whereClauses, cursorSQL)
		}
	}

	fullWhere := strings.Join(whereClauses, " AND ")

	// 5. Build ORDER BY
	var orderBy string
	switch sortField {
	case "updated_at":
		orderBy = fmt.Sprintf("ORDER BY vm.updated_at %s, vm.id %s", sortDir, sortDir)
	case "first_seen_at":
		orderBy = fmt.Sprintf("ORDER BY vm.first_seen_at %s, vm.id %s", sortDir, sortDir)
	default:
		orderBy = fmt.Sprintf("ORDER BY vm.name %s, vm.id %s", sortDir, sortDir)
	}

	// 6. Limit
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	limitParam := c.addArg(limit)

	fullSQL := fmt.Sprintf(`SELECT
    vm.id, vm.vcenter_id, vm.moref, vm.instance_uuid, vm.bios_uuid,
    vm.name, vm.power_state, vm.connection_state, vm.cpu_cores, vm.memory_mb,
    vm.folder_id, vm.folder_path::text, vm.host_id, vm.cluster_id, vm.datacenter_id,
    vm.guest_os, vm.guest_host_name, vm.guest_ip_address::text, vm.annotation,
    vm.created_at, vm.updated_at, vm.first_seen_at
FROM virtual_machines vm
WHERE %s
%s
LIMIT %s`, fullWhere, orderBy, limitParam)

	return &CompiledQuery{
		SQL:         fullSQL,
		WhereClause: fullWhere,
		Args:        c.args,
	}, nil
}

func (c *Compiler) addArg(val any) string {
	idx := c.argIndex
	c.argIndex++
	c.args = append(c.args, val)
	return fmt.Sprintf("$%d", idx)
}

func (c *Compiler) compileScope(scope *ScopeConstraint) (string, error) {
	var scopeParts []string

	// Allowed vCenters
	if len(scope.AllowedVCenterIDs) > 0 {
		params := make([]string, len(scope.AllowedVCenterIDs))
		for i, id := range scope.AllowedVCenterIDs {
			params[i] = c.addArg(id)
		}
		scopeParts = append(scopeParts, fmt.Sprintf("vm.vcenter_id IN (%s)", strings.Join(params, ", ")))
	}

	// Allowed folders (ltree descendant check: vm.folder_path <@ path)
	if len(scope.AllowedFolderPaths) > 0 {
		folderParts := make([]string, len(scope.AllowedFolderPaths))
		for i, path := range scope.AllowedFolderPaths {
			param := c.addArg(path)
			folderParts[i] = fmt.Sprintf("vm.folder_path <@ %s::ltree", param)
		}
		if len(folderParts) == 1 {
			scopeParts = append(scopeParts, folderParts[0])
		} else {
			scopeParts = append(scopeParts, fmt.Sprintf("(%s)", strings.Join(folderParts, " OR ")))
		}
	}

	if len(scopeParts) == 0 {
		return "1 = 0", nil
	}
	return strings.Join(scopeParts, " AND "), nil
}

func (c *Compiler) compileCursor(sortField, sortDir string, cursor *CursorValues) (string, error) {
	op := ">"
	if sortDir == "DESC" {
		op = "<"
	}

	valParam := c.addArg(cursor.LastValue)
	idParam := c.addArg(cursor.LastID)

	switch sortField {
	case "updated_at":
		return fmt.Sprintf("(vm.updated_at, vm.id) %s (%s, %s::uuid)", op, valParam, idParam), nil
	case "first_seen_at":
		return fmt.Sprintf("(vm.first_seen_at, vm.id) %s (%s, %s::uuid)", op, valParam, idParam), nil
	default:
		return fmt.Sprintf("(vm.name, vm.id) %s (%s, %s::uuid)", op, valParam, idParam), nil
	}
}

func (c *Compiler) compileNode(node *FilterNode) (string, error) {
	if node == nil {
		return "", nil
	}

	switch node.Op {
	case OpAnd:
		canonicalSortNodes(node.Operands)
		parts := make([]string, len(node.Operands))
		for i, child := range node.Operands {
			childSQL, err := c.compileNode(child)
			if err != nil {
				return "", err
			}
			parts[i] = childSQL
		}
		return fmt.Sprintf("(%s)", strings.Join(parts, " AND ")), nil

	case OpOr:
		canonicalSortNodes(node.Operands)
		parts := make([]string, len(node.Operands))
		for i, child := range node.Operands {
			childSQL, err := c.compileNode(child)
			if err != nil {
				return "", err
			}
			parts[i] = childSQL
		}
		return fmt.Sprintf("(%s)", strings.Join(parts, " OR ")), nil

	case OpNot:
		child := node.Operand
		if child == nil && len(node.Operands) > 0 {
			child = node.Operands[0]
		}
		childSQL, err := c.compileNode(child)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("NOT (%s)", childSQL), nil

	case OpEQ:
		p := c.addArg(node.Value)
		return fmt.Sprintf("vm.%s = %s", node.Field, p), nil

	case OpNE:
		p := c.addArg(node.Value)
		return fmt.Sprintf("vm.%s != %s", node.Field, p), nil

	case OpIN:
		values := node.Values
		if len(values) == 0 {
			if s, ok := node.Value.([]any); ok {
				values = s
			}
		}
		params := make([]string, len(values))
		for i, v := range values {
			params[i] = c.addArg(v)
		}
		return fmt.Sprintf("vm.%s IN (%s)", node.Field, strings.Join(params, ", ")), nil

	case OpContains:
		valStr := fmt.Sprintf("%v", node.Value)
		p := c.addArg("%" + valStr + "%")
		return fmt.Sprintf("vm.%s ILIKE %s", node.Field, p), nil

	case OpPrefix:
		valStr := fmt.Sprintf("%v", node.Value)
		p := c.addArg(valStr + "%")
		return fmt.Sprintf("vm.%s LIKE %s", node.Field, p), nil

	case OpCIDRContains:
		valStr := fmt.Sprintf("%v", node.Value)
		p := c.addArg(valStr)
		return fmt.Sprintf("vm.guest_ip_address <<= %s::inet", p), nil

	case OpCIDROverlap:
		valStr := fmt.Sprintf("%v", node.Value)
		p := c.addArg(valStr)
		return fmt.Sprintf("vm.guest_ip_address && %s::inet", p), nil

	case OpVLANOverlap:
		valStr := fmt.Sprintf("%v", node.Value)
		norm, err := vlan.NormalizeSpec(valStr)
		if err != nil {
			return "", err
		}
		p := c.addArg(norm)
		return fmt.Sprintf("EXISTS (SELECT 1 FROM vm_network_interfaces vnic WHERE vnic.vm_id = vm.id AND vnic.vlan_ids && %s::int4multirange)", p), nil

	case OpDescendantsOfFolder:
		valStr := fmt.Sprintf("%v", node.Value)
		p := c.addArg(valStr)
		return fmt.Sprintf("vm.folder_path <@ %s::ltree", p), nil

	case OpHasTag:
		cat := strings.TrimSpace(node.Category)
		if cat == "" {
			cat = strings.TrimSpace(node.Field)
		}
		tag := strings.TrimSpace(node.Tag)
		catParam := c.addArg(cat)
		if tag != "" {
			tagParam := c.addArg(tag)
			return fmt.Sprintf("EXISTS (SELECT 1 FROM vm_tags vt WHERE vt.vm_id = vm.id AND vt.category_name = %s AND vt.tag_name = %s)", catParam, tagParam), nil
		}
		return fmt.Sprintf("EXISTS (SELECT 1 FROM vm_tags vt WHERE vt.vm_id = vm.id AND vt.category_name = %s)", catParam), nil

	case OpHasAttribute:
		name := strings.TrimSpace(node.Name)
		if name == "" {
			name = strings.TrimSpace(node.Field)
		}
		nameParam := c.addArg(name)
		if node.Value != nil && strings.TrimSpace(fmt.Sprintf("%v", node.Value)) != "" {
			valParam := c.addArg(fmt.Sprintf("%v", node.Value))
			return fmt.Sprintf("EXISTS (SELECT 1 FROM vm_custom_attributes vca WHERE vca.vm_id = vm.id AND vca.name = %s AND vca.value = %s)", nameParam, valParam), nil
		}
		return fmt.Sprintf("EXISTS (SELECT 1 FROM vm_custom_attributes vca WHERE vca.vm_id = vm.id AND vca.name = %s)", nameParam), nil

	default:
		return "", fmt.Errorf("unsupported operator %q", node.Op)
	}
}

// canonicalSortNodes sorts a slice of FilterNodes in-place to guarantee deterministic query plans.
func canonicalSortNodes(nodes []*FilterNode) {
	sort.SliceStable(nodes, func(i, j int) bool {
		a, b := nodes[i], nodes[j]
		if a.Op != b.Op {
			return a.Op < b.Op
		}
		if a.Field != b.Field {
			return a.Field < b.Field
		}
		if a.Category != b.Category {
			return a.Category < b.Category
		}
		if a.Tag != b.Tag {
			return a.Tag < b.Tag
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		aVal := fmt.Sprintf("%v", a.Value)
		bVal := fmt.Sprintf("%v", b.Value)
		return aVal < bVal
	})
}
