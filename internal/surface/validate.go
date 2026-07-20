package surface

import (
	"errors"
	"fmt"
	"strings"
)

// maxDepth is the deepest the component tree may nest, counting root as 1.
const maxDepth = 32

// validator accumulates every problem found so the full set can be returned to
// the caller (an LLM self-correction loop) at once.
type validator struct {
	surface *Surface
	actions Actions
	data    map[string]any
	index   map[string]*Component
	errs    []error
}

// add records a component-scoped error, e.g.
// `component "t1" (Text): missing required property "value"`.
func (v *validator) add(c *Component, format string, args ...any) {
	v.errs = append(v.errs, fmt.Errorf("component %q (%s): %s", c.ID, c.Type, fmt.Sprintf(format, args...)))
}

// addf records a non-component error (structural or action-declaration level).
func (v *validator) addf(format string, args ...any) {
	v.errs = append(v.errs, fmt.Errorf(format, args...))
}

// Validate checks a surface and its actions for structural and semantic
// correctness. When data is nil, $data references are checked syntactically
// only; when data is non-nil, each $data path must resolve in data (and
// rows-like references must resolve to arrays). It returns a single error
// joining all problems, or nil if the surface is valid.
func Validate(s *Surface, actions Actions, data map[string]any) error {
	if s == nil || len(s.Components) == 0 {
		return fmt.Errorf("surface has no components")
	}
	v := &validator{
		surface: s,
		actions: actions,
		data:    data,
		index:   make(map[string]*Component, len(s.Components)),
	}

	// Build the ID index (unique, non-empty IDs).
	for i := range s.Components {
		c := &s.Components[i]
		if c.ID == "" {
			v.addf("component at index %d has an empty id", i)
			continue
		}
		if _, dup := v.index[c.ID]; dup {
			v.addf("duplicate component id %q", c.ID)
			continue
		}
		v.index[c.ID] = c
	}

	if _, ok := v.index["root"]; !ok {
		v.addf(`no component with id "root" found`)
	}

	v.validateActions()

	for i := range s.Components {
		v.validateComponent(&s.Components[i])
	}

	v.validateTree()

	return errors.Join(v.errs...)
}

// validateActions checks the action declarations themselves (independent of the
// components that reference them).
func (v *validator) validateActions() {
	for name, a := range v.actions {
		switch a.Type {
		case ActionInsert, ActionUpdate, ActionDelete:
			if strings.TrimSpace(a.Table) == "" {
				v.addf("action %q (%s): missing required field %q", name, a.Type, "table")
			} else if strings.HasPrefix(a.Table, "_") {
				v.addf("action %q (%s): table %q is a system table and cannot be written", name, a.Type, a.Table)
			}
		case ActionQuery:
			// table is optional and informational for query actions.
		case "":
			v.addf("action %q: missing required field %q", name, "type")
		default:
			v.addf("action %q: unknown type %q (want insert, update, delete, or query)", name, a.Type)
		}
		for _, p := range a.Params {
			if !isIdentifier(p) {
				v.addf("action %q: param %q is not a valid identifier", name, p)
			}
		}
	}
}

// validateComponent checks one component's type, properties, and child rules.
func (v *validator) validateComponent(c *Component) {
	if c.ID == "" {
		return // already reported during indexing
	}
	def, known := defByName[c.Type]
	if !known {
		v.add(c, "unknown component type")
		return
	}

	// Children are only permitted on layout components.
	if len(c.Children) > 0 && !def.AllowChildren {
		v.add(c, "children are not allowed on this component type")
	}

	// Reject unknown property names (strict — catches hallucinations).
	allowed := make(map[string]propSpec, len(def.Props))
	for _, ps := range def.Props {
		allowed[ps.Name] = ps
	}
	for key := range c.Properties {
		if _, ok := allowed[key]; !ok {
			v.add(c, "unknown property %q", key)
		}
	}

	// Required properties must be present; present ones are type-checked.
	for _, ps := range def.Props {
		val, present := c.Properties[ps.Name]
		if !present {
			if ps.Required {
				v.add(c, "missing required property %q", ps.Name)
			}
			continue
		}
		v.validateProp(c, ps, val)
	}

	if def.crossValidate != nil {
		def.crossValidate(v, c)
	}
}

// validateProp dispatches to the right check for a property's kind.
func (v *validator) validateProp(c *Component, ps propSpec, val any) {
	// $data refs are handled uniformly where allowed.
	if path, ok := IsDataRef(val); ok {
		if !ps.AllowData {
			v.add(c, "property %q does not accept a $data reference", ps.Name)
			return
		}
		v.checkDataPath(c, ps.Name, path, ps.Kind == kindRows)
		return
	}

	switch ps.Kind {
	case kindString:
		v.expectString(c, ps.Name, val)
	case kindInt:
		v.expectInt(c, ps, val)
	case kindStringOrNumber:
		if _, ok := val.(string); ok {
			return
		}
		if !isNumber(val) {
			v.add(c, "property %q must be a string or number", ps.Name)
		}
	case kindBool:
		if _, ok := val.(bool); !ok {
			v.add(c, "property %q must be a boolean", ps.Name)
		}
	case kindEnum:
		v.expectEnum(c, ps, val)
	case kindOptions:
		v.expectStringArray(c, ps.Name, val)
	case kindActionRef:
		v.checkActionRef(c, ps.Name, val, nil)
	case kindParams:
		v.checkParams(c, ps.Name, val)
	case kindColumns:
		v.checkColumns(c, val)
	case kindRows:
		v.checkRows(c, val)
	case kindRowActions:
		v.checkRowActions(c, val)
	case kindFields:
		v.checkFields(c, val)
	case kindSubmit:
		v.checkSubmit(c, val)
	}
}

func (v *validator) expectString(c *Component, name string, val any) {
	if _, ok := val.(string); !ok {
		v.add(c, "property %q must be a string", name)
	}
}

func (v *validator) expectInt(c *Component, ps propSpec, val any) {
	if !isInteger(val) {
		v.add(c, "property %q must be an integer", ps.Name)
		return
	}
	if ps.HasRange {
		n := intValue(val)
		if n < ps.Min || n > ps.Max {
			v.add(c, "property %q must be between %d and %d", ps.Name, ps.Min, ps.Max)
		}
	}
}

func (v *validator) expectEnum(c *Component, ps propSpec, val any) {
	s, ok := val.(string)
	if !ok {
		v.add(c, "property %q must be one of %s", ps.Name, strings.Join(ps.Enum, ", "))
		return
	}
	for _, e := range ps.Enum {
		if s == e {
			return
		}
	}
	v.add(c, "property %q value %q is not one of %s", ps.Name, s, strings.Join(ps.Enum, ", "))
}

func (v *validator) expectStringArray(c *Component, name string, val any) {
	arr, ok := val.([]any)
	if !ok {
		v.add(c, "property %q must be an array of strings", name)
		return
	}
	for i, e := range arr {
		if _, ok := e.(string); !ok {
			v.add(c, "property %q[%d] must be a string", name, i)
		}
	}
}

// checkDataPath validates a $data path. When data is nil the check is
// syntactic; otherwise the path must resolve (and, if requireArray, to an array).
func (v *validator) checkDataPath(c *Component, name, path string, requireArray bool) {
	if err := validatePathSyntax(path); err != nil {
		v.add(c, "property %q %s", name, err)
		return
	}
	if v.data == nil {
		return
	}
	resolved, ok := ResolvePath(v.data, path)
	if !ok {
		v.add(c, "property %q $data path %q not found in query data", name, path)
		return
	}
	if requireArray {
		arr, isArr := resolved.([]any)
		if !isArr {
			v.add(c, "property %q $data path %q did not resolve to an array", name, path)
			return
		}
		// A non-empty array must hold objects (rows), so downstream rendering can
		// key columns by field. Report the first offending element index.
		for i, e := range arr {
			if _, ok := e.(map[string]any); !ok {
				v.add(c, "property %q $data path %q element %d must be an object", name, path, i)
			}
		}
	}
}

// checkActionRef validates that val names a declared action. If allowedTypes is
// non-nil, the action's type must be one of them.
func (v *validator) checkActionRef(c *Component, ctx string, val any, allowedTypes []ActionType) {
	name, ok := val.(string)
	if !ok || name == "" {
		v.add(c, "%s must name a declared action", ctx)
		return
	}
	a, exists := v.actions[name]
	if !exists {
		v.add(c, "%s references undeclared action %q", ctx, name)
		return
	}
	if allowedTypes != nil {
		for _, t := range allowedTypes {
			if a.Type == t {
				return
			}
		}
		v.add(c, "%s references action %q of type %q, which is not permitted here", ctx, name, a.Type)
	}
}

func (v *validator) checkParams(c *Component, name string, val any) {
	m, ok := val.(map[string]any)
	if !ok {
		v.add(c, "property %q must be an object", name)
		return
	}
	for k, pv := range m {
		if path, ok := IsDataRef(pv); ok {
			v.checkDataPath(c, fmt.Sprintf("%s.%s", name, k), path, false)
		}
	}
}

func (v *validator) checkColumns(c *Component, val any) {
	arr, ok := val.([]any)
	if !ok {
		v.add(c, "property %q must be an array of column definitions", "columns")
		return
	}
	if len(arr) == 0 {
		v.add(c, "property %q must not be empty", "columns")
	}
	seen := make(map[string]bool)
	for i, e := range arr {
		m, ok := e.(map[string]any)
		if !ok {
			v.add(c, "columns[%d] must be an object", i)
			continue
		}
		requireStringField(v, c, fmt.Sprintf("columns[%d]", i), m, "key")
		if key, ok := m["key"].(string); ok && key != "" {
			if seen[key] {
				v.add(c, "columns[%d] has duplicate key %q", i, key)
			}
			seen[key] = true
		}
		requireStringField(v, c, fmt.Sprintf("columns[%d]", i), m, "label")
		if t, present := m["type"]; present {
			ts, ok := t.(string)
			if !ok || !contains(columnTypes, ts) {
				v.add(c, "columns[%d].type must be one of %s", i, strings.Join(columnTypes, ", "))
			}
		}
		for k := range m {
			if k != "key" && k != "label" && k != "type" {
				v.add(c, "columns[%d] has unknown field %q", i, k)
			}
		}
	}
}

func (v *validator) checkRows(c *Component, val any) {
	arr, ok := val.([]any)
	if !ok {
		v.add(c, "property %q must be an array of objects or a $data reference", "rows")
		return
	}
	for i, e := range arr {
		if _, ok := e.(map[string]any); !ok {
			v.add(c, "rows[%d] must be an object", i)
		}
	}
}

func (v *validator) checkRowActions(c *Component, val any) {
	arr, ok := val.([]any)
	if !ok {
		v.add(c, "property %q must be an array", "row_actions")
		return
	}
	allowed := []ActionType{ActionUpdate, ActionDelete, ActionQuery}
	for i, e := range arr {
		m, ok := e.(map[string]any)
		if !ok {
			v.add(c, "row_actions[%d] must be an object", i)
			continue
		}
		requireStringField(v, c, fmt.Sprintf("row_actions[%d]", i), m, "label")
		if act, present := m["action"]; present {
			v.checkActionRef(c, fmt.Sprintf("row_actions[%d].action", i), act, allowed)
		} else {
			v.add(c, "row_actions[%d] missing required field %q", i, "action")
		}
		if cf, present := m["confirm"]; present {
			if _, ok := cf.(bool); !ok {
				v.add(c, "row_actions[%d].confirm must be a boolean", i)
			}
		}
		for k := range m {
			if k != "action" && k != "label" && k != "confirm" {
				v.add(c, "row_actions[%d] has unknown field %q", i, k)
			}
		}
	}
}

func (v *validator) checkFields(c *Component, val any) {
	arr, ok := val.([]any)
	if !ok {
		v.add(c, "property %q must be an array of field definitions", "fields")
		return
	}
	if len(arr) == 0 {
		v.add(c, "property %q must not be empty", "fields")
	}
	seen := make(map[string]bool)
	for i, e := range arr {
		m, ok := e.(map[string]any)
		if !ok {
			v.add(c, "fields[%d] must be an object", i)
			continue
		}
		requireStringField(v, c, fmt.Sprintf("fields[%d]", i), m, "label")
		if nm, ok := m["name"].(string); ok && nm != "" {
			if seen[nm] {
				v.add(c, "fields[%d] has duplicate name %q", i, nm)
			}
			seen[nm] = true
		} else {
			v.add(c, "fields[%d] missing required field %q", i, "name")
		}
		typ, _ := m["type"].(string)
		if !contains(fieldTypes, typ) {
			v.add(c, "fields[%d].type must be one of %s", i, strings.Join(fieldTypes, ", "))
		}
		if req, present := m["required"]; present {
			if _, ok := req.(bool); !ok {
				v.add(c, "fields[%d].required must be a boolean", i)
			}
		}
		_, hasOptions := m["options"]
		if typ == "select" {
			if !hasOptions {
				v.add(c, "fields[%d] is type select but missing required field %q", i, "options")
			} else {
				v.expectStringArray(c, fmt.Sprintf("fields[%d].options", i), m["options"])
			}
		} else if hasOptions {
			v.add(c, "fields[%d].options is only allowed when type is select", i)
		}
		for k := range m {
			switch k {
			case "name", "label", "type", "required", "options", "default":
			default:
				v.add(c, "fields[%d] has unknown field %q", i, k)
			}
		}
	}
}

func (v *validator) checkSubmit(c *Component, val any) {
	m, ok := val.(map[string]any)
	if !ok {
		v.add(c, "property %q must be an object", "submit")
		return
	}
	requireStringField(v, c, "submit", m, "label")
	allowed := []ActionType{ActionInsert, ActionUpdate, ActionQuery}
	if act, present := m["action"]; present {
		v.checkActionRef(c, "submit.action", act, allowed)
	} else {
		v.add(c, "submit missing required field %q", "action")
	}
	for k := range m {
		if k != "action" && k != "label" {
			v.add(c, "submit has unknown field %q", k)
		}
	}
}

// validateTree checks child references resolve, that there are no cycles, and
// that nesting does not exceed maxDepth.
func (v *validator) validateTree() {
	for i := range v.surface.Components {
		c := &v.surface.Components[i]
		for _, childID := range c.Children {
			if _, ok := v.index[childID]; !ok {
				v.add(c, "references unknown child %q", childID)
			}
		}
	}

	const (
		unvisited = 0
		visiting  = 1
		visited   = 2
	)
	state := make(map[string]int, len(v.surface.Components))
	cycleReported := false
	depthExceeded := false

	var visit func(id string, depth int)
	visit = func(id string, depth int) {
		if depth > maxDepth && !depthExceeded {
			v.addf("component tree exceeds maximum nesting depth of %d", maxDepth)
			depthExceeded = true
			return
		}
		switch state[id] {
		case visited:
			return
		case visiting:
			if !cycleReported {
				v.addf("cycle detected in component tree at %q", id)
				cycleReported = true
			}
			return
		}
		c, ok := v.index[id]
		if !ok {
			return
		}
		state[id] = visiting
		for _, childID := range c.Children {
			visit(childID, depth+1)
			if depthExceeded {
				break
			}
		}
		state[id] = visited
	}

	if root, ok := v.index["root"]; ok {
		visit(root.ID, 1)
	}
	// Visit any components not reachable from root to catch cycles among them.
	for i := range v.surface.Components {
		if depthExceeded {
			break
		}
		id := v.surface.Components[i].ID
		if id != "" && state[id] == unvisited {
			visit(id, 1)
		}
	}
}

// requireStringField reports an error unless m[field] is a non-empty string.
func requireStringField(v *validator, c *Component, ctx string, m map[string]any, field string) {
	s, ok := m[field].(string)
	if !ok || s == "" {
		v.add(c, "%s missing required field %q", ctx, field)
	}
}

func validatePathSyntax(path string) error {
	if path == "" {
		return fmt.Errorf("$data path must not be empty")
	}
	for _, seg := range strings.Split(path, ".") {
		if seg == "" {
			return fmt.Errorf("$data path %q has an empty segment", path)
		}
	}
	return nil
}

func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func isNumber(v any) bool {
	switch v.(type) {
	case float64, float32, int, int64, int32:
		return true
	default:
		return false
	}
}

// intValue converts a value already known to satisfy isInteger to an int.
func intValue(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case int32:
		return int(n)
	case float64:
		return int(n)
	case float32:
		return int(n)
	default:
		return 0
	}
}

func isInteger(v any) bool {
	switch n := v.(type) {
	case int, int64, int32:
		return true
	case float64:
		return n == float64(int64(n))
	case float32:
		return n == float32(int64(n))
	default:
		return false
	}
}

func contains(set []string, s string) bool {
	for _, e := range set {
		if e == s {
			return true
		}
	}
	return false
}
