package search

import (
	"fmt"
	"net/netip"
	"regexp"
	"strings"

	"github.com/infrastructure/sims/internal/vlan"
)

// Operator represents a query filter operator.
type Operator string

const (
	// Logical
	OpAnd Operator = "AND"
	OpOr  Operator = "OR"
	OpNot Operator = "NOT"

	// Comparison
	OpEQ       Operator = "EQ"
	OpNE       Operator = "NE"
	OpIN       Operator = "IN"
	OpContains Operator = "CONTAINS"
	OpPrefix   Operator = "PREFIX"

	// Network & Range
	OpCIDRContains Operator = "CIDR_CONTAINS"
	OpCIDROverlap  Operator = "CIDR_OVERLAP"
	OpVLANOverlap  Operator = "VLAN_OVERLAP"

	// Hierarchy
	OpDescendantsOfFolder Operator = "DESCENDANTS_OF_FOLDER"

	// Metadata
	OpHasTag       Operator = "HAS_TAG"
	OpHasAttribute Operator = "HAS_ATTRIBUTE"
)

// FilterNode represents a node in the typed recursive filter AST.
type FilterNode struct {
	Op       Operator      `json:"op"`
	Field    string        `json:"field,omitempty"`
	Value    any           `json:"value,omitempty"`
	Values   []any         `json:"values,omitempty"`
	Operands []*FilterNode `json:"operands,omitempty"`
	Operand  *FilterNode   `json:"operand,omitempty"`

	// Metadata specifics
	Category string `json:"category,omitempty"`
	Tag      string `json:"tag,omitempty"`
	Name     string `json:"name,omitempty"`
}

// ValidationError indicates a syntax, type, or operator violation in the Filter AST.
type ValidationError struct {
	Code    string `json:"code"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

func (e *ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s on field %q: %s", e.Code, e.Field, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// FieldType describes data type classifications for allow-listed fields.
type FieldType string

const (
	TypeString     FieldType = "string"
	TypeUUID       FieldType = "uuid"
	TypeEnum       FieldType = "enum"
	TypeInteger    FieldType = "integer"
	TypeTimestamp  FieldType = "timestamp"
	TypeIP         FieldType = "ip"
	TypeLtree      FieldType = "ltree"
	TypeMultirange FieldType = "multirange"
)

// FieldDef defines metadata and allowed operators for a filterable field.
type FieldDef struct {
	Name             string     `json:"name"`
	Type             FieldType  `json:"type"`
	AllowedOperators []Operator `json:"allowed_operators"`
	Description      string     `json:"description"`
}

// AllowedFields defines the whitelist of filterable VM fields and their supported operators.
var AllowedFields = map[string]FieldDef{
	"id": {
		Name:             "id",
		Type:             TypeUUID,
		AllowedOperators: []Operator{OpEQ, OpNE, OpIN},
		Description:      "Virtual machine internal UUID",
	},
	"vcenter_id": {
		Name:             "vcenter_id",
		Type:             TypeUUID,
		AllowedOperators: []Operator{OpEQ, OpNE, OpIN},
		Description:      "Owning vCenter UUID",
	},
	"moref": {
		Name:             "moref",
		Type:             TypeString,
		AllowedOperators: []Operator{OpEQ, OpNE, OpIN, OpPrefix},
		Description:      "vSphere Managed Object Reference (e.g. vm-42)",
	},
	"instance_uuid": {
		Name:             "instance_uuid",
		Type:             TypeString,
		AllowedOperators: []Operator{OpEQ, OpNE, OpIN},
		Description:      "vSphere VM instance UUID",
	},
	"bios_uuid": {
		Name:             "bios_uuid",
		Type:             TypeString,
		AllowedOperators: []Operator{OpEQ, OpNE, OpIN},
		Description:      "vSphere VM BIOS UUID",
	},
	"name": {
		Name:             "name",
		Type:             TypeString,
		AllowedOperators: []Operator{OpEQ, OpNE, OpIN, OpContains, OpPrefix},
		Description:      "Virtual machine name",
	},
	"power_state": {
		Name:             "power_state",
		Type:             TypeEnum,
		AllowedOperators: []Operator{OpEQ, OpNE, OpIN},
		Description:      "Power state (poweredOn, poweredOff, suspended)",
	},
	"connection_state": {
		Name:             "connection_state",
		Type:             TypeEnum,
		AllowedOperators: []Operator{OpEQ, OpNE, OpIN},
		Description:      "Connection state (connected, disconnected, orphaned, inaccessible, invalid)",
	},
	"cpu_cores": {
		Name:             "cpu_cores",
		Type:             TypeInteger,
		AllowedOperators: []Operator{OpEQ, OpNE, OpIN},
		Description:      "Allocated vCPU count",
	},
	"memory_mb": {
		Name:             "memory_mb",
		Type:             TypeInteger,
		AllowedOperators: []Operator{OpEQ, OpNE, OpIN},
		Description:      "Allocated RAM in megabytes",
	},
	"folder_id": {
		Name:             "folder_id",
		Type:             TypeUUID,
		AllowedOperators: []Operator{OpEQ, OpNE, OpIN},
		Description:      "Direct parent folder UUID",
	},
	"folder_path": {
		Name:             "folder_path",
		Type:             TypeLtree,
		AllowedOperators: []Operator{OpEQ, OpNE, OpPrefix, OpDescendantsOfFolder},
		Description:      "Inventory folder hierarchy path as ltree dot-separated labels",
	},
	"host_id": {
		Name:             "host_id",
		Type:             TypeUUID,
		AllowedOperators: []Operator{OpEQ, OpNE, OpIN},
		Description:      "Assigned ESXi host UUID",
	},
	"cluster_id": {
		Name:             "cluster_id",
		Type:             TypeUUID,
		AllowedOperators: []Operator{OpEQ, OpNE, OpIN},
		Description:      "Assigned compute cluster UUID",
	},
	"datacenter_id": {
		Name:             "datacenter_id",
		Type:             TypeUUID,
		AllowedOperators: []Operator{OpEQ, OpNE, OpIN},
		Description:      "Assigned datacenter UUID",
	},
	"guest_os": {
		Name:             "guest_os",
		Type:             TypeString,
		AllowedOperators: []Operator{OpEQ, OpNE, OpIN, OpContains, OpPrefix},
		Description:      "Guest operating system identifier",
	},
	"guest_host_name": {
		Name:             "guest_host_name",
		Type:             TypeString,
		AllowedOperators: []Operator{OpEQ, OpNE, OpIN, OpContains, OpPrefix},
		Description:      "Guest hostname reported by VMware Tools",
	},
	"guest_ip_address": {
		Name:             "guest_ip_address",
		Type:             TypeIP,
		AllowedOperators: []Operator{OpEQ, OpNE, OpIN, OpCIDRContains, OpCIDROverlap},
		Description:      "Primary guest IP address or CIDR network",
	},
	"annotation": {
		Name:             "annotation",
		Type:             TypeString,
		AllowedOperators: []Operator{OpEQ, OpNE, OpContains, OpPrefix},
		Description:      "User notes / VM annotation",
	},
	"vlan": {
		Name:             "vlan",
		Type:             TypeMultirange,
		AllowedOperators: []Operator{OpVLANOverlap},
		Description:      "VLAN ID or range attached to VM NICs",
	},
	"vlan_ids": {
		Name:             "vlan_ids",
		Type:             TypeMultirange,
		AllowedOperators: []Operator{OpVLANOverlap},
		Description:      "VLAN multirange attached to VM NICs",
	},
	"created_at": {
		Name:             "created_at",
		Type:             TypeTimestamp,
		AllowedOperators: []Operator{OpEQ, OpNE},
		Description:      "Creation timestamp",
	},
	"updated_at": {
		Name:             "updated_at",
		Type:             TypeTimestamp,
		AllowedOperators: []Operator{OpEQ, OpNE},
		Description:      "Last update timestamp",
	},
	"first_seen_at": {
		Name:             "first_seen_at",
		Type:             TypeTimestamp,
		AllowedOperators: []Operator{OpEQ, OpNE},
		Description:      "First discovery timestamp",
	},
}

// ltreeLabelRegex ensures valid PostgreSQL ltree label format: alphanumeric and underscore.
var ltreePathRegex = regexp.MustCompile(`^[A-Za-z0-9_]+(\.[A-Za-z0-9_]+)*$`)

// Validate checks the semantic correctness of a FilterNode tree against the allow-list.
func Validate(node *FilterNode) error {
	if node == nil {
		return &ValidationError{
			Code:    "invalid_filter",
			Message: "filter node cannot be nil",
		}
	}

	switch node.Op {
	case OpAnd, OpOr:
		if len(node.Operands) == 0 {
			return &ValidationError{
				Code:    "invalid_filter",
				Message: fmt.Sprintf("%s node must have at least one operand", node.Op),
			}
		}
		for _, child := range node.Operands {
			if err := Validate(child); err != nil {
				return err
			}
		}
		return nil

	case OpNot:
		if node.Operand == nil {
			if len(node.Operands) == 1 {
				node.Operand = node.Operands[0]
			} else {
				return &ValidationError{
					Code:    "invalid_filter",
					Message: "NOT node must have exactly one operand",
				}
			}
		}
		return Validate(node.Operand)

	case OpHasTag:
		cat := strings.TrimSpace(node.Category)
		if cat == "" && node.Field != "" {
			cat = strings.TrimSpace(node.Field)
			node.Category = cat
		}
		if cat == "" {
			return &ValidationError{
				Code:    "invalid_filter",
				Field:   "category",
				Message: "HAS_TAG requires a non-empty category name",
			}
		}
		return nil

	case OpHasAttribute:
		name := strings.TrimSpace(node.Name)
		if name == "" && node.Field != "" {
			name = strings.TrimSpace(node.Field)
			node.Name = name
		}
		if name == "" {
			return &ValidationError{
				Code:    "invalid_filter",
				Field:   "name",
				Message: "HAS_ATTRIBUTE requires a non-empty attribute name",
			}
		}
		// If value was passed in Value field, normalize to AttrVal or Value
		return nil

	case OpDescendantsOfFolder:
		field := node.Field
		if field == "" {
			field = "folder_path"
			node.Field = field
		}
		if field != "folder_path" {
			return &ValidationError{
				Code:    "invalid_filter",
				Field:   field,
				Message: "DESCENDANTS_OF_FOLDER only applies to folder_path",
			}
		}
		valStr := fmt.Sprintf("%v", node.Value)
		valStr = strings.TrimSpace(valStr)
		if valStr == "" || valStr == "<nil>" {
			return &ValidationError{
				Code:    "invalid_filter",
				Field:   field,
				Message: "DESCENDANTS_OF_FOLDER requires a non-empty folder path",
			}
		}
		if !ltreePathRegex.MatchString(valStr) {
			return &ValidationError{
				Code:    "invalid_filter",
				Field:   field,
				Message: fmt.Sprintf("invalid ltree path syntax %q: labels must be alphanumeric and dot-separated", valStr),
			}
		}
		return nil

	case OpEQ, OpNE, OpContains, OpPrefix, OpIN, OpCIDRContains, OpCIDROverlap, OpVLANOverlap:
		fieldDef, ok := AllowedFields[node.Field]
		if !ok {
			return &ValidationError{
				Code:    "invalid_filter",
				Field:   node.Field,
				Message: fmt.Sprintf("unknown filter field %q", node.Field),
			}
		}

		// Verify operator is allowed for this field
		opAllowed := false
		for _, allowed := range fieldDef.AllowedOperators {
			if allowed == node.Op {
				opAllowed = true
				break
			}
		}
		if !opAllowed {
			return &ValidationError{
				Code:    "invalid_filter",
				Field:   node.Field,
				Message: fmt.Sprintf("operator %s is not permitted for field %q (type: %s)", node.Op, node.Field, fieldDef.Type),
			}
		}

		// Value validation
		if node.Op == OpIN {
			if len(node.Values) == 0 {
				if sliceVal, ok := node.Value.([]any); ok && len(sliceVal) > 0 {
					node.Values = sliceVal
				} else {
					return &ValidationError{
						Code:    "invalid_filter",
						Field:   node.Field,
						Message: "IN operator requires a non-empty array of values",
					}
				}
			}
			return nil
		}

		if node.Value == nil || strings.TrimSpace(fmt.Sprintf("%v", node.Value)) == "" {
			return &ValidationError{
				Code:    "invalid_filter",
				Field:   node.Field,
				Message: fmt.Sprintf("operator %s requires a non-empty value", node.Op),
			}
		}

		valStr := fmt.Sprintf("%v", node.Value)
		switch node.Op {
		case OpCIDRContains, OpCIDROverlap:
			// Must be valid IP address or prefix
			if _, err := netip.ParsePrefix(valStr); err != nil {
				if _, errAddr := netip.ParseAddr(valStr); errAddr != nil {
					return &ValidationError{
						Code:    "invalid_filter",
						Field:   node.Field,
						Message: fmt.Sprintf("invalid CIDR/IP syntax %q: %v", valStr, err),
					}
				}
			}
		case OpVLANOverlap:
			if _, err := vlan.NormalizeSpec(valStr); err != nil {
				return &ValidationError{
					Code:    "invalid_filter",
					Field:   node.Field,
					Message: fmt.Sprintf("invalid VLAN specification %q: %v", valStr, err),
				}
			}
		}
		return nil

	default:
		return &ValidationError{
			Code:    "invalid_filter",
			Message: fmt.Sprintf("unrecognized operator %q", node.Op),
		}
	}
}
