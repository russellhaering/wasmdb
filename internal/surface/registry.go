package surface

// fieldTypes are the value types a form field or input may declare. They mirror
// the database's document field types plus "select" for enumerated inputs.
var fieldTypes = []string{"string", "int", "float", "bool", "datetime", "select"}

// columnTypes are the optional value types a DataTable column may declare.
var columnTypes = []string{"string", "int", "float", "bool", "datetime"}

// propKind selects the validation and documentation behavior for a property.
type propKind int

const (
	kindString         propKind = iota // scalar string
	kindInt                            // integral number
	kindStringOrNumber                 // string or number
	kindBool                           // boolean
	kindEnum                           // string from a fixed set (propSpec.Enum)
	kindOptions                        // array of strings (conditional; see crossValidate)
	kindActionRef                      // string naming a declared action of any type
	kindParams                         // object; values may be $data refs
	kindColumns                        // DataTable columns definition
	kindRows                           // DataTable rows: literal array or $data ref to an array
	kindRowActions                     // DataTable row actions
	kindFields                         // Form fields definition
	kindSubmit                         // Form submit definition
)

// propSpec describes one property of a component: its validation kind and the
// metadata used to render the LLM-facing spec.
type propSpec struct {
	Name      string
	Kind      propKind
	TypeDesc  string // human-readable type for the spec, e.g. "enum(body|heading)"
	Required  bool
	AllowData bool // a $data ref is accepted in place of a concrete value
	Enum      []string
	Desc      string
	// HasRange, when true, bounds a kindInt value to [Min, Max] inclusive.
	HasRange bool
	Min      int
	Max      int
}

// componentDef is the definition of one component type. The registry of these
// is the single source of truth for both Validate and SpecMarkdown.
type componentDef struct {
	Name          string
	Summary       string
	AllowChildren bool
	Props         []propSpec
	// crossValidate runs component-level rules that span multiple properties
	// (e.g. Input.options is required only when Input.type == "select"). It is
	// optional and appends to v.errs.
	crossValidate func(v *validator, c *Component)
}

func layoutProps() []propSpec {
	return []propSpec{
		{Name: "gap", Kind: kindInt, TypeDesc: "int", HasRange: true, Min: 0, Max: 256, Desc: "Spacing between children (0-256)."},
		{Name: "align", Kind: kindEnum, TypeDesc: "enum(start|center|end)", Enum: []string{"start", "center", "end"}, Desc: "Cross-axis alignment of children."},
	}
}

// registry lists every component type in a deterministic order. SpecMarkdown
// iterates it directly, so the order here is the order in the generated spec.
var registry = []componentDef{
	{
		Name:          "Column",
		Summary:       "Vertical layout container. Stacks its children top to bottom.",
		AllowChildren: true,
		Props:         layoutProps(),
	},
	{
		Name:          "Row",
		Summary:       "Horizontal layout container. Places its children left to right.",
		AllowChildren: true,
		Props:         layoutProps(),
	},
	{
		Name:          "Card",
		Summary:       "Titled panel that groups its children.",
		AllowChildren: true,
		Props: []propSpec{
			{Name: "title", Kind: kindString, TypeDesc: "string", AllowData: true, Desc: "Optional panel heading."},
		},
	},
	{
		Name:    "Divider",
		Summary: "Horizontal separator. Has no properties and no children.",
	},
	{
		Name:    "Text",
		Summary: "A run of text.",
		Props: []propSpec{
			{Name: "value", Kind: kindString, TypeDesc: "string", Required: true, AllowData: true, Desc: "The text to display."},
			{Name: "variant", Kind: kindEnum, TypeDesc: "enum(body|heading|caption|code)", Enum: []string{"body", "heading", "caption", "code"}, Desc: "Visual style."},
		},
	},
	{
		Name:    "Metric",
		Summary: "A single labeled statistic (stat tile).",
		Props: []propSpec{
			{Name: "label", Kind: kindString, TypeDesc: "string", Required: true, Desc: "What the value measures."},
			{Name: "value", Kind: kindStringOrNumber, TypeDesc: "string|number", Required: true, AllowData: true, Desc: "The statistic itself."},
			{Name: "unit", Kind: kindString, TypeDesc: "string", Desc: "Optional unit suffix."},
		},
	},
	{
		Name:    "DataTable",
		Summary: "Tabular display of rows, optionally bound to query data.",
		Props: []propSpec{
			{Name: "columns", Kind: kindColumns, TypeDesc: "array of {key:string, label:string, type?:string}", Required: true, Desc: "Column definitions. type is one of string|int|float|bool|datetime."},
			{Name: "rows", Kind: kindRows, TypeDesc: "array of objects OR a $data ref resolving to an array", Required: true, AllowData: true, Desc: "Row data. Usually a $data ref into query data."},
			{Name: "empty_text", Kind: kindString, TypeDesc: "string", Desc: "Text shown when there are no rows."},
			{Name: "row_actions", Kind: kindRowActions, TypeDesc: "array of {action:string, label:string, confirm?:bool}", Desc: "Per-row buttons. action must name an update/delete/query action."},
		},
	},
	{
		Name:    "Form",
		Summary: "Group of input fields with a submit button.",
		Props: []propSpec{
			{Name: "fields", Kind: kindFields, TypeDesc: "array of {name, label, type, required?, options?, default?}", Required: true, Desc: "Field definitions. type is string|int|float|bool|datetime|select; options is required when type is select."},
			{Name: "submit", Kind: kindSubmit, TypeDesc: "{action:string, label:string}", Required: true, Desc: "Submit button. action must name an insert/update/query action."},
		},
	},
	{
		Name:    "Input",
		Summary: "Standalone control, e.g. a search box. Bound inputs feed render params.",
		Props: []propSpec{
			{Name: "name", Kind: kindString, TypeDesc: "string", Required: true, Desc: "Parameter name this input feeds."},
			{Name: "type", Kind: kindEnum, TypeDesc: "enum(string|int|float|bool|datetime|select)", Required: true, Enum: fieldTypes, Desc: "Value type."},
			{Name: "label", Kind: kindString, TypeDesc: "string", Desc: "Field label."},
			{Name: "placeholder", Kind: kindString, TypeDesc: "string", Desc: "Placeholder text."},
			{Name: "options", Kind: kindOptions, TypeDesc: "array of strings", Desc: "Choices; required when type is select, otherwise omitted."},
			{Name: "bind", Kind: kindBool, TypeDesc: "bool", Desc: "Whether the value feeds render params. Defaults to true."},
		},
		crossValidate: func(v *validator, c *Component) {
			typ, _ := c.Properties["type"].(string)
			_, hasOptions := c.Properties["options"]
			if typ == "select" && !hasOptions {
				v.add(c, "type is %q but required property %q is missing", "select", "options")
			}
			if typ != "select" && hasOptions {
				v.add(c, "property %q is only allowed when type is %q", "options", "select")
			}
		},
	},
	{
		Name:    "Button",
		Summary: "Triggers a declared action when clicked.",
		Props: []propSpec{
			{Name: "label", Kind: kindString, TypeDesc: "string", Required: true, Desc: "Button text."},
			{Name: "action", Kind: kindActionRef, TypeDesc: "string (declared action name)", Required: true, Desc: "The action to run."},
			{Name: "params", Kind: kindParams, TypeDesc: "object; values may be $data refs", Desc: "Static or bound parameters passed to the action."},
			{Name: "confirm", Kind: kindBool, TypeDesc: "bool", Desc: "Prompt for confirmation before running."},
		},
	},
}

// defByName indexes the registry for validation lookups.
var defByName = func() map[string]*componentDef {
	m := make(map[string]*componentDef, len(registry))
	for i := range registry {
		m[registry[i].Name] = &registry[i]
	}
	return m
}()
