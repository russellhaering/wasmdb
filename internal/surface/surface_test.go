package surface

import (
	"flag"
	"os"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "update golden files")

// parse is a test helper that parses a surface + actions from JSON, failing the
// test on parse errors.
func parse(t *testing.T, surfaceJSON, actionsJSON string) (*Surface, Actions) {
	t.Helper()
	s, err := ParseSurface([]byte(surfaceJSON))
	if err != nil {
		t.Fatalf("ParseSurface: %v", err)
	}
	a, err := ParseActions([]byte(actionsJSON))
	if err != nil {
		t.Fatalf("ParseActions: %v", err)
	}
	return s, a
}

// wrap embeds a component's JSON as the single child of a root Column so that
// per-component tests only need to specify the component under test.
func wrap(componentJSON string) string {
	return `{"components":[
		{"id":"root","type":"Column","children":["x"]},
		` + componentJSON + `
	]}`
}

func TestValidComponents(t *testing.T) {
	cases := []struct {
		name      string
		component string
		actions   string
		data      map[string]any
	}{
		{
			name:      "Column",
			component: `{"id":"x","type":"Column","properties":{"gap":8,"align":"center"}}`,
		},
		{
			name:      "Row",
			component: `{"id":"x","type":"Row","properties":{"gap":4,"align":"start"}}`,
		},
		{
			name:      "Card",
			component: `{"id":"x","type":"Card","properties":{"title":"Panel"}}`,
		},
		{
			name:      "Divider",
			component: `{"id":"x","type":"Divider"}`,
		},
		{
			name:      "Text",
			component: `{"id":"x","type":"Text","properties":{"value":"Hi","variant":"heading"}}`,
		},
		{
			name:      "Text $data value",
			component: `{"id":"x","type":"Text","properties":{"value":{"$data":"greeting"}}}`,
			data:      map[string]any{"greeting": "hello"},
		},
		{
			name:      "Metric string value",
			component: `{"id":"x","type":"Metric","properties":{"label":"Count","value":"12","unit":"orders"}}`,
		},
		{
			name:      "Metric number value",
			component: `{"id":"x","type":"Metric","properties":{"label":"Count","value":12}}`,
		},
		{
			name:      "DataTable literal rows",
			component: `{"id":"x","type":"DataTable","properties":{"columns":[{"key":"a","label":"A","type":"int"}],"rows":[{"a":1}]}}`,
		},
		{
			name:      "DataTable $data rows",
			component: `{"id":"x","type":"DataTable","properties":{"columns":[{"key":"a","label":"A"}],"rows":{"$data":"orders"},"empty_text":"none","row_actions":[{"action":"del","label":"Delete","confirm":true}]}}`,
			actions:   `{"del":{"type":"delete","table":"orders"}}`,
			data:      map[string]any{"orders": []any{map[string]any{"a": 1}}},
		},
		{
			name:      "Form",
			component: `{"id":"x","type":"Form","properties":{"fields":[{"name":"f","label":"F","type":"select","options":["a","b"]}],"submit":{"action":"ins","label":"Save"}}}`,
			actions:   `{"ins":{"type":"insert","table":"t"}}`,
		},
		{
			name:      "Input select",
			component: `{"id":"x","type":"Input","properties":{"name":"q","type":"select","options":["a"],"bind":true}}`,
		},
		{
			name:      "Input string",
			component: `{"id":"x","type":"Input","properties":{"name":"q","type":"string","placeholder":"search"}}`,
		},
		{
			name:      "Button with params",
			component: `{"id":"x","type":"Button","properties":{"label":"Go","action":"q","params":{"since":{"$data":"query.since"}},"confirm":false}}`,
			actions:   `{"q":{"type":"query","params":["since"]}}`,
			data:      map[string]any{"query": map[string]any{"since": "2020"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, a := parse(t, wrap(tc.component), tc.actions)
			if err := Validate(s, a, tc.data); err != nil {
				t.Fatalf("expected valid, got: %v", err)
			}
		})
	}
}

func TestInvalidComponents(t *testing.T) {
	cases := []struct {
		name      string
		component string
		actions   string
		want      string
	}{
		// Missing required properties.
		{"Text missing value", `{"id":"x","type":"Text","properties":{}}`, "", `missing required property "value"`},
		{"Metric missing label", `{"id":"x","type":"Metric","properties":{"value":1}}`, "", `missing required property "label"`},
		{"Metric missing value", `{"id":"x","type":"Metric","properties":{"label":"L"}}`, "", `missing required property "value"`},
		{"DataTable missing columns", `{"id":"x","type":"DataTable","properties":{"rows":[]}}`, "", `missing required property "columns"`},
		{"DataTable missing rows", `{"id":"x","type":"DataTable","properties":{"columns":[{"key":"a","label":"A"}]}}`, "", `missing required property "rows"`},
		{"Form missing fields", `{"id":"x","type":"Form","properties":{"submit":{"action":"i","label":"S"}}}`, `{"i":{"type":"insert","table":"t"}}`, `missing required property "fields"`},
		{"Form missing submit", `{"id":"x","type":"Form","properties":{"fields":[{"name":"f","label":"F","type":"string"}]}}`, "", `missing required property "submit"`},
		{"Input missing name", `{"id":"x","type":"Input","properties":{"type":"string"}}`, "", `missing required property "name"`},
		{"Input missing type", `{"id":"x","type":"Input","properties":{"name":"q"}}`, "", `missing required property "type"`},
		{"Button missing label", `{"id":"x","type":"Button","properties":{"action":"a"}}`, `{"a":{"type":"query"}}`, `missing required property "label"`},
		{"Button missing action", `{"id":"x","type":"Button","properties":{"label":"L"}}`, "", `missing required property "action"`},

		// Wrong types.
		{"Text value wrong type", `{"id":"x","type":"Text","properties":{"value":42}}`, "", `property "value" must be a string`},
		{"Column gap wrong type", `{"id":"x","type":"Column","properties":{"gap":"big"}}`, "", `property "gap" must be an integer`},
		{"Metric value wrong type", `{"id":"x","type":"Metric","properties":{"label":"L","value":true}}`, "", `property "value" must be a string or number`},
		{"Button confirm wrong type", `{"id":"x","type":"Button","properties":{"label":"L","action":"a","confirm":"yes"}}`, `{"a":{"type":"query"}}`, `property "confirm" must be a boolean`},

		// Unknown type and property.
		{"unknown type", `{"id":"x","type":"Widget"}`, "", "unknown component type"},
		{"unknown property", `{"id":"x","type":"Text","properties":{"value":"v","color":"red"}}`, "", `unknown property "color"`},

		// Enum violations.
		{"Text bad variant", `{"id":"x","type":"Text","properties":{"value":"v","variant":"huge"}}`, "", `property "variant" value "huge" is not one of`},
		{"Column bad align", `{"id":"x","type":"Column","properties":{"align":"middle"}}`, "", `property "align" value "middle" is not one of`},
		{"Input bad type", `{"id":"x","type":"Input","properties":{"name":"q","type":"json"}}`, "", `property "type" value "json" is not one of`},

		// Nested definition errors.
		{"DataTable column missing key", `{"id":"x","type":"DataTable","properties":{"columns":[{"label":"A"}],"rows":[]}}`, "", `columns[0] missing required field "key"`},
		{"DataTable bad column type", `{"id":"x","type":"DataTable","properties":{"columns":[{"key":"a","label":"A","type":"json"}],"rows":[]}}`, "", "columns[0].type must be one of"},
		{"DataTable empty columns", `{"id":"x","type":"DataTable","properties":{"columns":[],"rows":[]}}`, "", `property "columns" must not be empty`},
		{"Form select field no options", `{"id":"x","type":"Form","properties":{"fields":[{"name":"f","label":"F","type":"select"}],"submit":{"action":"i","label":"S"}}}`, `{"i":{"type":"insert","table":"t"}}`, `fields[0] is type select but missing required field "options"`},
		{"Form duplicate field names", `{"id":"x","type":"Form","properties":{"fields":[{"name":"f","label":"A","type":"string"},{"name":"f","label":"B","type":"string"}],"submit":{"action":"i","label":"S"}}}`, `{"i":{"type":"insert","table":"t"}}`, `duplicate name "f"`},

		// Input conditional options.
		{"Input options without select", `{"id":"x","type":"Input","properties":{"name":"q","type":"string","options":["a"]}}`, "", `property "options" is only allowed when type is "select"`},
		{"Input select without options", `{"id":"x","type":"Input","properties":{"name":"q","type":"select"}}`, "", `required property "options" is missing`},

		// $data where not allowed.
		{"Metric label $data", `{"id":"x","type":"Metric","properties":{"label":{"$data":"l"},"value":1}}`, "", `property "label" does not accept a $data reference`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, a := parse(t, wrap(tc.component), tc.actions)
			err := Validate(s, a, nil)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestStructural(t *testing.T) {
	cases := []struct {
		name    string
		surface string
		want    string
	}{
		{"empty surface", `{"components":[]}`, "surface has no components"},
		{"missing root", `{"components":[{"id":"a","type":"Divider"}]}`, `no component with id "root" found`},
		{"empty id", `{"components":[{"id":"root","type":"Divider"},{"id":"","type":"Divider"}]}`, "empty id"},
		{"duplicate id", `{"components":[{"id":"root","type":"Column","children":["d"]},{"id":"d","type":"Divider"},{"id":"d","type":"Divider"}]}`, `duplicate component id "d"`},
		{"unresolved child", `{"components":[{"id":"root","type":"Column","children":["ghost"]}]}`, `references unknown child "ghost"`},
		{"children on leaf", `{"components":[{"id":"root","type":"Column","children":["t"]},{"id":"t","type":"Text","properties":{"value":"v"},"children":["d"]},{"id":"d","type":"Divider"}]}`, "children are not allowed"},
		{"cycle", `{"components":[{"id":"root","type":"Column","children":["a"]},{"id":"a","type":"Card","children":["b"]},{"id":"b","type":"Card","children":["a"]}]}`, "cycle detected"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, a := parse(t, tc.surface, "")
			err := Validate(s, a, nil)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestDepthOverflow(t *testing.T) {
	// Build a chain of Cards deeper than maxDepth.
	var b strings.Builder
	b.WriteString(`{"components":[`)
	b.WriteString(`{"id":"root","type":"Column","children":["c0"]}`)
	n := maxDepth + 5
	for i := 0; i < n; i++ {
		child := ""
		if i < n-1 {
			child = `"c` + itoa(i+1) + `"`
		}
		b.WriteString(`,{"id":"c` + itoa(i) + `","type":"Card","children":[` + child + `]}`)
	}
	b.WriteString(`]}`)
	s, a := parse(t, b.String(), "")
	err := Validate(s, a, nil)
	if err == nil || !strings.Contains(err.Error(), "maximum nesting depth") {
		t.Fatalf("expected depth overflow error, got: %v", err)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf []byte
	for i > 0 {
		buf = append([]byte{byte('0' + i%10)}, buf...)
		i /= 10
	}
	return string(buf)
}

func TestActionValidation(t *testing.T) {
	cases := []struct {
		name      string
		component string
		actions   string
		want      string
	}{
		{"undeclared button action", `{"id":"x","type":"Button","properties":{"label":"L","action":"nope"}}`, `{}`, `references undeclared action "nope"`},
		{"undeclared form action", `{"id":"x","type":"Form","properties":{"fields":[{"name":"f","label":"F","type":"string"}],"submit":{"action":"nope","label":"S"}}}`, `{}`, `references undeclared action "nope"`},
		{"undeclared row action", `{"id":"x","type":"DataTable","properties":{"columns":[{"key":"a","label":"A"}],"rows":[],"row_actions":[{"action":"nope","label":"X"}]}}`, `{}`, `references undeclared action "nope"`},
		{"insert as row action", `{"id":"x","type":"DataTable","properties":{"columns":[{"key":"a","label":"A"}],"rows":[],"row_actions":[{"action":"ins","label":"X"}]}}`, `{"ins":{"type":"insert","table":"t"}}`, `not permitted here`},
		{"delete as form submit", `{"id":"x","type":"Form","properties":{"fields":[{"name":"f","label":"F","type":"string"}],"submit":{"action":"del","label":"S"}}}`, `{"del":{"type":"delete","table":"t"}}`, `not permitted here`},
		{"missing table on insert", `{"id":"x","type":"Button","properties":{"label":"L","action":"ins"}}`, `{"ins":{"type":"insert"}}`, `missing required field "table"`},
		{"system table action", `{"id":"x","type":"Button","properties":{"label":"L","action":"ins"}}`, `{"ins":{"type":"insert","table":"_users"}}`, "system table"},
		{"unknown action type", `{"id":"x","type":"Button","properties":{"label":"L","action":"a"}}`, `{"a":{"type":"upsert","table":"t"}}`, `unknown type "upsert"`},
		{"bad param identifier", `{"id":"x","type":"Button","properties":{"label":"L","action":"q"}}`, `{"q":{"type":"query","params":["1bad"]}}`, `not a valid identifier`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, a := parse(t, wrap(tc.component), tc.actions)
			err := Validate(s, a, nil)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestDataRefs(t *testing.T) {
	dataTable := `{"id":"x","type":"DataTable","properties":{"columns":[{"key":"a","label":"A"}],"rows":{"$data":"orders"}}}`

	t.Run("syntactic only when data nil", func(t *testing.T) {
		s, a := parse(t, wrap(dataTable), "")
		if err := Validate(s, a, nil); err != nil {
			t.Fatalf("expected valid (syntactic-only), got: %v", err)
		}
	})

	t.Run("resolves with data", func(t *testing.T) {
		s, a := parse(t, wrap(dataTable), "")
		data := map[string]any{"orders": []any{map[string]any{"a": 1}}}
		if err := Validate(s, a, data); err != nil {
			t.Fatalf("expected valid, got: %v", err)
		}
	})

	t.Run("missing path with data", func(t *testing.T) {
		s, a := parse(t, wrap(dataTable), "")
		data := map[string]any{"other": []any{}}
		err := Validate(s, a, data)
		if err == nil || !strings.Contains(err.Error(), `$data path "orders" not found`) {
			t.Fatalf("expected not-found error, got: %v", err)
		}
	})

	t.Run("rows ref not an array", func(t *testing.T) {
		s, a := parse(t, wrap(dataTable), "")
		data := map[string]any{"orders": "not-an-array"}
		err := Validate(s, a, data)
		if err == nil || !strings.Contains(err.Error(), "did not resolve to an array") {
			t.Fatalf("expected non-array error, got: %v", err)
		}
	})

	t.Run("empty path syntax", func(t *testing.T) {
		s, a := parse(t, wrap(`{"id":"x","type":"Text","properties":{"value":{"$data":""}}}`), "")
		err := Validate(s, a, nil)
		if err == nil || !strings.Contains(err.Error(), "must not be empty") {
			t.Fatalf("expected empty-path error, got: %v", err)
		}
	})
}

func TestResolvePath(t *testing.T) {
	data := map[string]any{
		"a": map[string]any{"b": map[string]any{"c": 42}},
		"x": 7,
	}
	cases := []struct {
		path   string
		want   any
		wantOK bool
	}{
		{"a.b.c", 42, true},
		{"x", 7, true},
		{"a.b", map[string]any{"c": 42}, true},
		{"a.b.z", nil, false}, // missing segment
		{"x.y", nil, false},   // non-map intermediate
		{"", nil, false},      // empty path
		{"a..c", nil, false},  // empty segment
	}
	for _, tc := range cases {
		got, ok := ResolvePath(data, tc.path)
		if ok != tc.wantOK {
			t.Errorf("ResolvePath(%q) ok=%v want %v", tc.path, ok, tc.wantOK)
			continue
		}
		if tc.wantOK {
			if _, isMap := tc.want.(map[string]any); isMap {
				continue // shallow check is enough for the nested map case
			}
			if got != tc.want {
				t.Errorf("ResolvePath(%q)=%v want %v", tc.path, got, tc.want)
			}
		}
	}
}

func TestIsDataRef(t *testing.T) {
	cases := []struct {
		name   string
		val    any
		want   string
		wantOK bool
	}{
		{"valid", map[string]any{"$data": "a.b"}, "a.b", true},
		{"not a map", "x", "", false},
		{"extra key", map[string]any{"$data": "a", "other": 1}, "", false},
		{"non-string value", map[string]any{"$data": 5}, "", false},
		{"wrong key", map[string]any{"data": "a"}, "", false},
	}
	for _, tc := range cases {
		got, ok := IsDataRef(tc.val)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("%s: IsDataRef=%q,%v want %q,%v", tc.name, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestParseActions(t *testing.T) {
	t.Run("nil input", func(t *testing.T) {
		a, err := ParseActions(nil)
		if err != nil || len(a) != 0 {
			t.Fatalf("nil: got %v, %v", a, err)
		}
	})
	t.Run("empty input", func(t *testing.T) {
		a, err := ParseActions([]byte("  "))
		if err != nil || len(a) != 0 {
			t.Fatalf("empty: got %v, %v", a, err)
		}
	})
	t.Run("malformed json", func(t *testing.T) {
		if _, err := ParseActions([]byte("{not json")); err == nil {
			t.Fatal("expected parse error")
		}
	})
	t.Run("unknown type parses ok", func(t *testing.T) {
		// ParseActions is purely structural; the unknown type is flagged later
		// by Validate, not at parse time.
		a, err := ParseActions([]byte(`{"a":{"type":"upsert","table":"t"}}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a["a"].Type != "upsert" {
			t.Fatalf("expected type preserved, got %q", a["a"].Type)
		}
	})
}

func TestMultiError(t *testing.T) {
	// A surface with three distinct defects: unknown type, missing required
	// property, and an undeclared action reference.
	surface := `{"components":[
		{"id":"root","type":"Column","children":["a","b","c"]},
		{"id":"a","type":"Widget"},
		{"id":"b","type":"Text","properties":{}},
		{"id":"c","type":"Button","properties":{"label":"L","action":"ghost"}}
	]}`
	s, a := parse(t, surface, "")
	err := Validate(s, a, nil)
	if err == nil {
		t.Fatal("expected errors")
	}
	msg := err.Error()
	for _, want := range []string{"unknown component type", `missing required property "value"`, `undeclared action "ghost"`} {
		if !strings.Contains(msg, want) {
			t.Errorf("multi-error missing %q in:\n%s", want, msg)
		}
	}
}

func TestSpecMarkdownGolden(t *testing.T) {
	got := SpecMarkdown()
	const path = "testdata/spec.md"
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update to generate): %v", err)
	}
	if got != string(want) {
		t.Errorf("SpecMarkdown() differs from %s; run: go test ./internal/surface -run TestSpecMarkdownGolden -update", path)
	}
}

func TestSpecMarkdownContent(t *testing.T) {
	spec := SpecMarkdown()
	// Every component type must be documented.
	for _, def := range registry {
		if !strings.Contains(spec, "### "+def.Name) {
			t.Errorf("spec missing section for %q", def.Name)
		}
	}
	if len(strings.Split(spec, "\n")) > 200 {
		t.Errorf("spec is %d lines, expected under 200", len(strings.Split(spec, "\n")))
	}
}
